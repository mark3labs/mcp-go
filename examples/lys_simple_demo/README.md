# lys_simple_demo 使用说明

本文档说明了如何在 `stdio`、`http` 和 `sse` 三种不同的通信模式下运行 `lys_simple_demo` 的客户端和服务器。

## 前提条件

首先，编译客户端和服务器的可执行文件。

进入服务器目录并编译：
```bash
cd /Users/lyshen/Desktop/new_project/mcp-go/examples/lys_simple_demo/server
go build .
```

进入客户端目录并编译：
```bash
cd /Users/lyshen/Desktop/new_project/mcp-go/examples/lys_simple_demo/client
go build .
```

---

## 模式 1: Stdio

在 `stdio` 模式下，客户端直接启动服务器进程，并通过标准输入/输出与其通信。

### 运行指令

在 `client` 目录下执行以下命令：
`/Users/lyshen/Desktop/new_project/mcp-go/examples/lys_simple_demo/client`

```bash
./lys_simple_client -mode stdio -server-cmd="/Users/lyshen/Desktop/new_project/mcp-go/examples/lys_simple_demo/server/lys_simple_server -mode stdio"
```

### 预期输出

客户端将打印以下内容，表示工具调用成功：
```
2025/07/21 14:40:35 Calling say_hi tool
Response from server: hi from server
```

---

## 模式 2: HTTP

在 `http` 模式下，服务器作为独立的 HTTP 服务器运行，客户端连接到它。

### 第 1 步：启动服务器

打开一个终端，在 `server` 目录下运行以下命令：
`/Users/lyshen/Desktop/new_project/mcp-go/examples/lys_simple_demo/server`

```bash
./lys_simple_server -mode http
```

服务器将启动并阻塞，等待连接。您将看到：
```
2025/07/21 14:36:40 Starting server in http mode on :8080
```

### 第 2 步：运行客户端

打开**第二个**终端，在 `client` 目录下运行以下命令：
`/Users/lyshen/Desktop/new_project/mcp-go/examples/lys_simple_demo/client`

```bash
./lys_simple_client -mode http
```

### 预期输出

客户端将连接到服务器并打印：
```
2025/07/21 14:36:52 Calling say_hi tool
Response from server: hi from server
```
您可以在服务器的终端中按 `Ctrl+C` 来停止服务器。

---

## 模式 3: SSE (Server-Sent Events)

在 `sse` 模式下，服务器作为提供 SSE 流的 HTTP 服务器运行，客户端连接到它。

### 第 1 步：启动服务器

打开一个终端，在 `server` 目录下运行以下命令：
`/Users/lyshen/Desktop/new_project/mcp-go/examples/lys_simple_demo/server`

```bash
./lys_simple_server -mode sse
```

服务器将启动并阻塞，等待连接。您将看到：
```
2025/07/21 14:40:09 Starting server in sse mode on :8080
```

### 第 2 步：运行客户端

打开**第二个**终端，在 `client` 目录下运行以下命令：
`/Users/lyshen/Desktop/new_project/mcp-go/examples/lys_simple_demo/client`

```bash
./lys_simple_client -mode sse
```

### 预期输出

客户端将连接到服务器的 SSE 端点并打印：
```
2025/07/21 14:40:35 Calling say_hi tool
Response from server: hi from server
```
您可以在服务器的终端中按 `Ctrl+C` 来停止服务器。
