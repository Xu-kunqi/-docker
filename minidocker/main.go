package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

func main() {
	switch os.Args[1] {
	case "run":
		run()
	case "init":
		initContainer()
	default:
		panic("have no define")
	}
}

func run() {
	fmt.Println("父进程正在运行，准备创建容器...")

	// 1. 克隆自己，进入 init 模式
	cmd := exec.Command("/proc/self/exe", append([]string{"init"}, os.Args[2:]...)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// 2. 设置全部隔离结界（含网络！）
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWUTS | syscall.CLONE_NEWPID | syscall.CLONE_NEWNS | syscall.CLONE_NEWNET,
	}

	// 3. 先启动，拿到子进程 PID
	if err := cmd.Start(); err != nil {
		fmt.Printf("启动失败: %v\n", err)
		os.Exit(1)
	}

	// 4. 给子进程配置网络（在宿主机侧操作）
	setupNetwork(cmd.Process.Pid)

	// 5. 把子进程关进 Cgroups 牢房
	setCgroups(cmd.Process.Pid)

	// 6. 等待进程结束
	if err := cmd.Wait(); err != nil {
		fmt.Printf("执行命令失败: %v\n", err)
		os.Exit(1)
	}
}

// 子进程：挂载 /proc，然后启动 shell
func initContainer() {
	fmt.Println("子进程（init）正在运行，准备挂载 /proc...")

	// 重新挂载 /proc，装订“花名册”
	syscall.Mount("proc", "/proc", "proc", 0, "")

	// 启动真正的 shell
	cmd := exec.Command(os.Args[2])
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		fmt.Printf("执行命令失败: %v\n", err)
		os.Exit(1)
	}
}

// ⭐ 新增：网络配置（父进程负责）
func setupNetwork(pid int) {
	fmt.Printf("正在为容器 (PID %d) 配置网络...\n", pid)

	// 给容器内部的回环网卡 lo 开启
	exec.Command("nsenter", "-t", fmt.Sprintf("%d", pid), "-n", "ip", "link", "set", "lo", "up").Run()

	// 创建一对 veth 网卡（类似一根网线的两头）
	// veth0 留在宿主机，veth1 准备塞进容器
	vethHost := "veth0"
	vethContainer := "veth1"

	// 清理可能存在的旧网卡（防止报错）
	exec.Command("ip", "link", "del", vethHost).Run()

	// 创建 veth pair
	if err := exec.Command("ip", "link", "add", vethHost, "type", "veth", "peer", "name", vethContainer).Run(); err != nil {
		fmt.Printf("创建 veth pair 失败: %v\n", err)
		return
	}

	// 把容器那一端的网卡，塞进容器的网络命名空间
	if err := exec.Command("ip", "link", "set", vethContainer, "netns", fmt.Sprintf("%d", pid)).Run(); err != nil {
		fmt.Printf("移入网络命名空间失败: %v\n", err)
		return
	}

	// 给宿主机的网卡配置 IP：192.168.1.1/24
	exec.Command("ip", "addr", "add", "192.168.1.1/24", "dev", vethHost).Run()
	exec.Command("ip", "link", "set", vethHost, "up").Run()

	// 给容器内的网卡配置 IP：192.168.1.2/24，并开启
	exec.Command("nsenter", "-t", fmt.Sprintf("%d", pid), "-n", "ip", "addr", "add", "192.168.1.2/24", "dev", vethContainer).Run()
	exec.Command("nsenter", "-t", fmt.Sprintf("%d", pid), "-n", "ip", "link", "set", vethContainer, "up").Run()

	// 在容器内设置默认路由（所有流量都走宿主机）
	exec.Command("nsenter", "-t", fmt.Sprintf("%d", pid), "-n", "ip", "route", "add", "default", "via", "192.168.1.1").Run()

	// 开启宿主机 IP 转发
	exec.Command("sysctl", "-w", "net.ipv4.ip_forward=1").Run()

	// 设置 iptables NAT 转发（让容器能访问外网）
	exec.Command("iptables", "-t", "nat", "-A", "POSTROUTING", "-s", "192.168.1.0/24", "-j", "MASQUERADE").Run()

	fmt.Println("网络配置完成！容器内 IP 应为 192.168.1.2")
}

// 设置资源限制（Cgroups）
func setCgroups(pid int) {
	cgroupPath := "/sys/fs/cgroup/myminidocker"
	os.RemoveAll(cgroupPath)
	if err := os.Mkdir(cgroupPath, 0755); err != nil {
		fmt.Printf("创建 cgroup 失败: %v\n", err)
		return
	}
	memoryLimit := []byte("52428800")
	if err := os.WriteFile(cgroupPath+"/memory.max", memoryLimit, 0644); err != nil {
		fmt.Printf("设置内存限制失败: %v\n", err)
	}
	pidBytes := []byte(fmt.Sprintf("%d", pid))
	if err := os.WriteFile(cgroupPath+"/cgroup.procs", pidBytes, 0644); err != nil {
		fmt.Printf("写入 PID 失败: %v\n", err)
	}
	fmt.Printf("Cgroups 设置成功，已将 PID %d 限制在 50MB 内存内\n", pid)
}
