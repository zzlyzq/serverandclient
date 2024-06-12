package main

import (
    "bufio"
    "flag"
    "fmt"
    "net"
    "os"
    "os/signal"
    "strconv"
    "strings"
    "sync"
    "syscall"
    "time"
)

var (
    host     string
    port     int
    help     bool
    clients  = make(map[int]net.Conn)
    clientID = 0
    mu       sync.Mutex
    commands = make(map[int][]string) // 命令队列
    cmdMutex sync.Mutex
)

func init() {
    flag.StringVar(&host, "h", "0.0.0.0", "监听的IP地址")
    flag.IntVar(&port, "p", 4000, "监听的端口")
    flag.BoolVar(&help, "help", false, "显示帮助信息")
}

func main() {
    flag.Parse()
    if help {
        fmt.Println("服务端帮助信息:")
        fmt.Println("  -h: 监听的IP地址 (默认: 0.0.0.0)")
        fmt.Println("  -p: 监听的端口 (默认: 4000)")
        fmt.Println("  -help: 显示帮助信息")
        return
    }

    addr := fmt.Sprintf("%s:%d", host, port)
    listener, err := net.Listen("tcp", addr)
    if err != nil {
        fmt.Printf("监听端口失败: %v\n", err)
        os.Exit(1)
    }
    defer listener.Close()

    fmt.Printf("服务端已启动，监听地址: %s\n", addr)

    go acceptConnections(listener)

    handleCommands()
}

func acceptConnections(listener net.Listener) {
    for {
        conn, err := listener.Accept()
        if err != nil {
            fmt.Printf("接受客户端连接失败: %v\n", err)
            continue
        }

        mu.Lock()
        clientID++
        clients[clientID] = conn
        mu.Unlock()

        fmt.Printf("客户端 %d (%s) 已连接\n> ", clientID, conn.RemoteAddr())

        go monitorConnection(clientID, conn)
    }
}

func monitorConnection(id int, conn net.Conn) {
    for {
        _, err := conn.Write([]byte("PING\n"))
        if err != nil {
            mu.Lock()
            delete(clients, id)
            mu.Unlock()
            conn.Close()
            fmt.Printf("客户端 %d (%s) 已断开连接\n> ", id, conn.RemoteAddr())
            return
        }
        time.Sleep(10 * time.Second)
    }
}

func handleCommands() {
    reader := bufio.NewReader(os.Stdin)

    for {
        fmt.Print("> ")
        command, err := reader.ReadString('\n')
        if err != nil {
            fmt.Printf("读取命令失败: %v\n", err)
            continue
        }
        command = strings.TrimSpace(command)

        if command == "" {
            continue // 处理空命令，只返回提示符
        }

        if command == "exit" {
            fmt.Println("服务端退出")
            os.Exit(0)
        } else if command == "help" {
            fmt.Println("已有命令:")
            fmt.Println("  list     - 列出所有连接的客户端")
            fmt.Println("  connect  - 连接到指定客户端 (格式: connect <客户端编号>)")
            fmt.Println("  exit     - 退出服务端")
        } else if command == "list" {
            listClients()
        } else if strings.HasPrefix(command, "connect ") {
            parts := strings.Split(command, " ")
            if len(parts) != 2 {
                fmt.Println("命令格式错误，应为: connect <客户端编号>")
                continue
            }
            id, err := strconv.Atoi(parts[1])
            if err != nil {
                fmt.Println("客户端编号应为整数")
                continue
            }
            connectClient(id)
        } else {
            fmt.Println("未知命令")
        }
    }
}

func listClients() {
    mu.Lock()
    defer mu.Unlock()

    if len(clients) == 0 {
        fmt.Println("当前没有连接的客户端")
        return
    }

    fmt.Println("连接的客户端列表:")
    for id, conn := range clients {
        fmt.Printf("  客户端 %d: %s\n", id, conn.RemoteAddr())
    }
}

func connectClient(id int) {
    mu.Lock()
    conn, ok := clients[id]
    mu.Unlock()

    if !ok {
        fmt.Printf("没有找到编号为 %d 的客户端\n> ", id)
        return
    }

    clientAddr := conn.RemoteAddr().String()
    fmt.Printf("与客户端 %d (%s) 交互，输入 'exit' 退出\n", id, clientAddr)

    reader := bufio.NewReader(os.Stdin)
    writer := bufio.NewWriter(conn)
    connReader := bufio.NewReader(conn)

    interrupt := make(chan os.Signal, 1)
    signal.Notify(interrupt, syscall.SIGINT)

    for {
        fmt.Printf("shell %s> ", clientAddr)
        command, err := reader.ReadString('\n')
        if err != nil {
            fmt.Printf("读取命令失败: %v\n> ", err)
            return
        }
        command = strings.TrimSpace(command)

        if command == "exit" {
            fmt.Printf("与客户端 %d (%s) 断开连接\n", id, clientAddr)
            break
        }

        if command == "" {
            continue
        }

        // 将命令加入队列
        addCommandsToQueue(id, command)

        // 处理命令队列
        processCommandQueue(id, writer, connReader, interrupt)
    }
}

func addCommandsToQueue(id int, command string) {
    cmdMutex.Lock()
    commands[id] = append(commands[id], strings.Split(command, "\n")...)
    cmdMutex.Unlock()
}

func processCommandQueue(id int, writer *bufio.Writer, connReader *bufio.Reader, interrupt chan os.Signal) {
    for {
        cmdMutex.Lock()
        if len(commands[id]) == 0 {
            cmdMutex.Unlock()
            break
        }

        command := commands[id][0]
        commands[id] = commands[id][1:]
        cmdMutex.Unlock()

        fmt.Printf("发送命令到客户端 %d: %s\n", id, command)
        fmt.Fprintf(writer, "%s\n", command)
        writer.Flush()

        done := make(chan bool)
        interrupted := make(chan struct{})

        go func() {
            defer func() {
                done <- true
            }()
            var response strings.Builder
            started := false
            for {
                select {
                case <-interrupted:
                    return
                default:
                    line, err := connReader.ReadString('\n')
                    if err != nil {
                        mu.Lock()
                        delete(clients, id)
                        mu.Unlock()
                        fmt.Printf("读取客户端响应失败: %v\n> ", err)
                        return
                    }
                    line = strings.TrimSpace(line)
                    if line == "SERVERANDCLIENTSTB" {
                        started = true
                        fmt.Printf("从客户端 %d 收到响应开始标记\n", id)
                        continue
                    }
                    if line == "<SERVERANDCLIENTEOF>" {
                        if started {
                            fmt.Printf("从客户端 %d 收到完整响应:\n%s", id, response.String())
                            return
                        }
                    }
                    if started && line != "" {
                        response.WriteString(line + "\n")  // 确保行尾带有换行符
                    }
                }
            }
        }()

        select {
        case <-interrupt:
            fmt.Println("\n命令执行被中断")
            close(interrupted)
            // 清空剩余的信号，避免影响后续命令
            for len(interrupt) > 0 {
                <-interrupt
            }
        case <-done:
        }
    }
}
