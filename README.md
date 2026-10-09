# minidocker

用 Go 语言手写的一个微型 Docker 容器引擎。

本项目在 WSL2 环境下，使用 Go (1.22) 语言完成，实现了容器底层的四大核心机制。

## 核心实现

### 1. Namespace（隔离）
使用 `syscall.SysProcAttr` 设置 `Cloneflags`，实现了四大隔离：
- `CLONE_NEWUTS`: 主机名隔离
- `CLONE_NEWPID`: 进程号隔离
- `CLONE_NEWNS`: 文件系统挂载隔离
- `CLONE_NEWNET`: 网络隔离

**代码片段：**
```go
cmd.SysProcAttr = &syscall.SysProcAttr{
    Cloneflags: syscall.CLONE_NEWUTS | syscall.CLONE_NEWPID | syscall.CLONE_NEWNS | syscall.CLONE_NEWNET,
}
```

### 2. Cgroups（资源限制）
通过操作 `/sys/fs/cgroup/` 目录下的文件，限制容器只能使用 50MB 内存。

**代码片段：**
```go
memoryLimit := []byte("52428800") // 50MB
if err := os.WriteFile(cgroupPath+"/memory.max", memoryLimit, 0644); err != nil {
    fmt.Printf("设置内存限制失败: %v\n", err)
}
```

### 3. Mount（文件系统隔离）
在 init 子进程中重新挂载 `/proc`，让容器拥有独立的进程视图。

**代码片段：**
```go
syscall.Mount("proc", "/proc", "proc", 0, "")
```

### 4. veth pair + NAT（网络隔离）
通过创建虚拟网卡对（veth pair）并配置 NAT 转发，实现容器的独立网络。

**代码片段：**
```go
exec.Command("nsenter", "-t", fmt.Sprintf("%d", pid), "-n", "ip", "link", "set", "lo", "up").Run()

if err := exec.Command("ip", "link", "set", vethContainer, "netns", fmt.Sprintf("%d", pid)).Run(); err != nil {
    fmt.Printf("移入网络命名空间失败: %v\n", err)
    return
}
```

## 架构说明

整个程序的灵魂在于 `main` 函数中的 `switch` 语句。通过重复调用自身（传入 `init` 参数），巧妙地实现了 `fork -> init -> exec` 的经典容器启动流程，从而完成了 `/proc` 的初始化和最终的 Shell 启动。

## 使用方法

```bash
# 编译
sudo env GOTOOLCHAIN=local /usr/local/go/bin/go build -o myminidocker main.go

# 运行
sudo ./myminidocker run /bin/sh
```
