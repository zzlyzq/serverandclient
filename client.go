package main

import (
    "bufio"
    "flag"
    "fmt"
    "github.com/shirou/gopsutil/cpu"
    "github.com/shirou/gopsutil/disk"
    "github.com/shirou/gopsutil/mem"
    ghwNet "github.com/shirou/gopsutil/net"
    "github.com/jaypipes/ghw"
    "net"
    "os/exec"
    "strings"
    "time"
    "sync"
    "io"
    "strconv"
    "text/tabwriter"
    "bytes"
    "runtime"
)


var (
    host string
    port int
    help bool
)

func init() {
    flag.StringVar(&host, "h", "127.0.0.1", "服务端IP地址")
    flag.IntVar(&port, "p", 4000, "服务端端口")
    flag.BoolVar(&help, "help", false, "显示帮助信息")
}

func main() {
    flag.Parse()
    if help {
        fmt.Println("客户端帮助信息:")
        fmt.Println("  -h: 服务端IP地址 (默认: 127.0.0.1)")
        fmt.Println("  -p: 服务端端口 (默认: 4000)")
        fmt.Println("  -help: 显示帮助信息")
        fmt.Println("程序将在后台持续运行，并尝试每3秒重连服务端。")
        return
    }

    go func() {
        for {
            conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", host, port))
            if err != nil {
                time.Sleep(3 * time.Second)
                continue
            }

            // 发送系统信息
            sendSystemInfo(conn)

            done := make(chan struct{})
            go func() {
                receiveMessages(conn)
                close(done)
            }()

            <-done
        }
    }()

    select {}
}

func sendSystemInfo(conn net.Conn) {
    writer := bufio.NewWriter(conn)
    systemInfo := getSystemInfo()
    fmt.Fprintf(writer, "SYSTEM_INFO:\n%s\n", systemInfo)
    writer.Flush()
}

func getSystemInfo() string {
    cpuInfo, _ := cpu.Info()
    memInfo, _ := mem.VirtualMemory()
    diskInfo, _ := disk.Usage("/")
    product, err := ghw.Product()
    if err != nil {
        fmt.Printf("Error getting product info: %v", err)
    }

    blockInfo, err := ghw.Block()
    if err != nil {
        fmt.Printf("Error getting block device info: %v", err)
    }
    diskTypes := make([]string, 0)
    for _, disk := range blockInfo.Disks {
        if strings.HasPrefix(disk.Name, "dm-") {
            continue
        }
        if strings.HasPrefix(disk.Name, "nvme0c0n1") {
            continue
        }
        name := disk.Name
        driveType := disk.DriveType.String()
        size := disk.SizeBytes / 1024 / 1024 / 1024

        diskInfo := fmt.Sprintf("Name: %s | Type: %s | Size: %dGB", name, driveType, size)
        diskTypes = append(diskTypes, diskInfo)
    }

    raidDetails := getRaidInfo()

    physicalCPUs, err := getPhysicalCPUs()
    if err != nil {
        fmt.Printf("Error getting physical CPU count: %v", err)
    }

    totalCores := 0
    totalThreads := len(cpuInfo)

    for _, ci := range cpuInfo {
        totalCores += int(ci.Cores)
    }

    coresPerCPU := totalCores / physicalCPUs
    cpuDetails := fmt.Sprintf("Model: %s | Physical CPUs: %d | Cores per CPU: %d | Total Cores: %d | Total Threads: %d | Frequency: %.2fGHz",
        cpuInfo[0].ModelName, physicalCPUs, coresPerCPU, totalCores, totalThreads, cpuInfo[0].Mhz/1000)

    networkInterfaces := getNetworkInterfaces()

    var buffer bytes.Buffer
    writer := tabwriter.NewWriter(&buffer, 0, 8, 2, ' ', 0)

    fmt.Fprintln(writer, "Category | Details")
    fmt.Fprintln(writer, "---|---")
    fmt.Fprintf(writer, "CPU | %s\n", cpuDetails)
    fmt.Fprintf(writer, "Memory | %dMB\n", memInfo.Total/1024/1024)
    fmt.Fprintf(writer, "Disk | %dGB\n", diskInfo.Total/1024/1024/1024)
    fmt.Fprintf(writer, "Product | Family: %s | Name: %s | Serial Number: %s | UUID: %s | SKU: %s | Vendor: %s | Version: %s\n",
        product.Family, product.Name, product.SerialNumber, product.UUID, product.SKU, product.Vendor, product.Version)
    fmt.Fprintf(writer, "Disk Types | %s\n", strings.Join(diskTypes, "\n"))
    fmt.Fprintf(writer, "RAID Info | %s\n", raidDetails)
    fmt.Fprintf(writer, "Network Interfaces | %s\n", strings.Join(networkInterfaces, "\n"))

    writer.Flush()
    return buffer.String()
}



func getNetworkInterfaces() []string {
    interfaces, err := ghwNet.Interfaces()
    if err != nil {
        fmt.Printf("Error getting network interfaces: %v", err)
    }

    networkInfo := make([]string, 0)
    for _, iface := range interfaces {
        if iface.HardwareAddr != "" {
            ipAddresses := make([]string, 0)
            for _, addr := range iface.Addrs {
                ipAddresses = append(ipAddresses, addr.Addr)
            }
            netInfo := fmt.Sprintf("Name: %s MAC: %s IPs: [%s]", iface.Name, iface.HardwareAddr, strings.Join(ipAddresses, " "))
            networkInfo = append(networkInfo, netInfo)
        }
    }

    return networkInfo
}




// 通过执行系统命令获取 CPU 信息
func getPhysicalCPUs() (int, error) {
    if runtime.GOOS == "windows" {
        // 在 Windows 环境下，假设物理 CPU 数量为逻辑 CPU 数量的一半
        logicalCPUs := runtime.NumCPU()
        physicalCPUs := logicalCPUs / 2
        if physicalCPUs == 0 {
            physicalCPUs = 1
        }
        return physicalCPUs, nil
    } else {
        // 在 Linux 环境下，使用 lscpu 命令获取物理 CPU 数量
        out, err := exec.Command("lscpu").Output()
        if err != nil {
            return 0, err
        }

        lines := strings.Split(string(out), "\n")
        for _, line := range lines {
            if strings.Contains(line, "Socket(s):") {
                parts := strings.Fields(line)
                if len(parts) >= 2 {
                    return strconv.Atoi(parts[len(parts)-1])
                }
            }
        }
        return 0, fmt.Errorf("failed to find physical CPU count")
    }
}


// 通过执行系统命令获取 RAID 信息
func getRaidInfo() string {
    if runtime.GOOS == "windows" {
        return "RAID information not available on Windows"
    } else {
        out, err := exec.Command("lshw", "-class", "storage").Output()
        if err != nil {
            return fmt.Sprintf("Error getting RAID info: %v", err)
        }

        lines := strings.Split(string(out), "\n")
        raidInfo := make([]string, 0)
        var currentType, description, product, vendor, driver string

        for _, line := range lines {
            line = strings.TrimSpace(line)
            if strings.HasPrefix(line, "*-") {
                if currentType != "" {
                    raidInfo = append(raidInfo, fmt.Sprintf("%s, %s, %s, %s, %s", currentType, description, product, vendor, driver))
                }
                currentType = strings.TrimPrefix(line, "*-")
                description, product, vendor, driver = "", "", "", ""
            } else if strings.HasPrefix(line, "description:") {
                description = strings.TrimPrefix(line, "description: ")
            } else if strings.HasPrefix(line, "product:") {
                product = strings.TrimPrefix(line, "product: ")
            } else if strings.HasPrefix(line, "vendor:") {
                vendor = strings.TrimPrefix(line, "vendor: ")
            } else if strings.HasPrefix(line, "configuration: driver=") {
                driver = strings.TrimPrefix(line, "configuration: driver=")
            }
        }

        // 处理最后一个条目
        if currentType != "" {
            raidInfo = append(raidInfo, fmt.Sprintf("%s, %s, %s, %s, %s", currentType, description, product, vendor, driver))
        }

        if len(raidInfo) == 0 {
            return "No RAID information available"
        }

        return strings.Join(raidInfo, "; ")
    }
}

func getMacAddresses() []string {
    interfaces, err := ghwNet.Interfaces()
    if err != nil {
        fmt.Printf("Error getting network interfaces: %v", err)
    }

    macAddresses := make([]string, 0)
    for _, iface := range interfaces {
        if iface.HardwareAddr != "" {
            macInfo := fmt.Sprintf("Name: %s MAC: %s", iface.Name, iface.HardwareAddr)
            macAddresses = append(macAddresses, macInfo)
        }
    }

    return macAddresses
}



func receiveMessages(conn net.Conn) {
    reader := bufio.NewReader(conn)
    writer := bufio.NewWriter(conn)

    for {
        message, err := reader.ReadString('\n')
        if err != nil {
            fmt.Printf("接收消息错误: %v\n", err)
            return
        }
        message = strings.TrimSpace(message)
        if message == "exit" {
            fmt.Println("接收到退出命令，退出交互模式但不关闭连接")
            continue
        }
        if message == "PING" {
            fmt.Fprintf(writer, "PONG\n")
            writer.Flush()
            continue
        }
        fmt.Printf("收到命令: %s\n", message)
        go executeCommandAndStreamOutput(message, writer)
    }
}

func executeCommandAndStreamOutput(command string, writer *bufio.Writer) {
    fmt.Fprintf(writer, "SERVERANDCLIENTSTB\n")
    writer.Flush()

    cmd := exec.Command("bash", "-c", command)
    stdout, err := cmd.StdoutPipe()
    if err != nil {
        fmt.Fprintf(writer, "创建StdoutPipe失败: %v\n<SERVERANDCLIENTEOF>\n", err)
        writer.Flush()
        return
    }
    defer stdout.Close()

    stderr, err := cmd.StderrPipe()
    if err != nil {
        fmt.Fprintf(writer, "创建StderrPipe失败: %v\n<SERVERANDCLIENTEOF>\n", err)
        writer.Flush()
        return
    }
    defer stderr.Close()

    if err := cmd.Start(); err != nil {
        fmt.Fprintf(writer, "命令启动失败: %v\n<SERVERANDCLIENTEOF>\n", err)
        writer.Flush()
        return
    }

    var wg sync.WaitGroup
    wg.Add(2)

    go func() {
        defer wg.Done()
        io.Copy(writer, stdout)
        writer.Flush()
    }()

    go func() {
        defer wg.Done()
        io.Copy(writer, stderr)
        writer.Flush()
    }()

    wg.Wait()

    err = cmd.Wait()
    if err != nil {
        fmt.Fprintf(writer, "命令执行失败: %v\n", err)
    }

    fmt.Fprintf(writer, "<SERVERANDCLIENTEOF>\n")
    writer.Flush()
}
