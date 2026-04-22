# Gateway 包 API 文档

## 概述

`gateway` 包提供基于 WebSocket 的双向通信服务端（Server）和客户端（Client），采用文本指令协议（`|` 分隔符）进行会话式数据传输。

**核心设计原则：**
- Server/Client 均使用 Option 模式构造
- Session/SessionManager 为包内封装类型，外部不可直接使用
- 所有消息由服务端构建，客户端只传原始数据

---

## 目录

1. [协议规范](#1-协议规范)
2. [Server API](#2-server-api)
3. [Client API](#3-client-api)
4. [Message 类型](#4-message-类型)
5. [Channel 集成](#5-channel-集成)
6. [错误定义](#6-错误定义)

---

## 1. 协议规范

### 1.1 文本指令格式

所有消息以 `|` 分隔，格式为：

```
指令类型 | 参数1 | 参数2 | 参数3 | 数据体
```

### 1.2 指令列表

| 指令 | 格式 | 说明 |
|------|------|------|
| `SESSION_START` | `SESSION_START\|\|{total}\|` | 初始化新会话，声明总分片数。服务端返回会话ID |
| `DATA` | `DATA\|{sessionID}\|{index}\|{total}\|{data}` | 传输分片数据。服务端缓存并确认 |
| `SESSION_END` | `SESSION_END\|{sessionID}\|\|\|` | 结束会话。服务端合并分片并触发 handler |

### 1.3 完整交互流程

```
客户端                                    服务端
  │                                        │
  │ SESSION_START|||3                     │ → 创建 session (total=3)
  │ ← SESSION_START|{uuid}|OK||           │   返回 session ID
  │                                        │
  │ DATA|{uuid}|0|3|hello                 │ → 缓存 part[0]
  │ ← DATA|{uuid}|0|OK||                  │
  │                                        │
  │ DATA|{uuid}|1|3|<binary>              │ → 缓存 part[1]
  │ ← DATA|{uuid}|1|OK||                  │
  │                                        │
  │ DATA|{uuid}|2|3|<binary>              │ → 缓存 part[2]
  │ ← DATA|{uuid}|2|OK||                  │
  │                                        │
  │ SESSION_END|{uuid}|||                  │ → 拼接全部 parts → handler(msg)
  │ ← SESSION_END|{uuid}|OK||             │
```

### 1.4 服务端出站消息格式

服务端向客户端发送的消息为 **JSON 格式**：

```json
{
  "id": "uuid",
  "channel_id": "",
  "client_id": "uuid",
  "session_id": "uuid",
  "direction": "inbound",
  "data": "base64或文本",
  "content_type": "",
  "timestamp": "2024-01-01T00:00:00Z"
}
```

---

## 2. Server API

### 2.1 构造函数

```go
func New(opts ...Option) *Server
```

**Option 列表：**

| Option | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `WithAddr(addr)` | string | `:8081` | 监听地址（完整地址，如 `0.0.0.0:9090`） |
| `WithPort(port)` | int | `8081` | 监听端口（自动拼接为 `:{port}`），与 WithAddr 二选一 |
| `WithPath(path)` | string | `/ws` | WebSocket 端点路径 |
| `WithHandler(h)` | MessageHandler | nil | 消息处理回调 |
| `WithSessionTimeout(d)` | Duration | 30分钟 | 会话超时时间 |

**示例：**
```go
g := gateway.New(
    gateway.WithPort(8080),
    gateway.WithPath("/ws"),
    gateway.WithHandler(myHandler),
    gateway.WithSessionTimeout(10 * time.Minute),
)
```

### 2.2 生命周期

#### Start()

```go
func (s *Server) Start() error
```

- **行为：** 阻塞式启动 WebSocket 服务器
- **返回：** `ErrAlreadyRunning` 如果已在运行；否则阻塞直到服务器停止或出错
- **注意：** 通常在 goroutine 中调用，或在 main 函数中作为最后一步调用

#### Shutdown()

```go
func (s *Server) Shutdown(ctx context.Context) error
```

- **行为：** 优雅关闭服务器，清理所有 client 的 session
- **参数：** `ctx` — 关闭超时上下文
- **返回：** `ErrNotRunning` 如果未在运行

#### IsRunning()

```go
func (s *Server) IsRunning() bool
```

返回当前运行状态。

### 2.3 消息发送

#### Send()

```go
func (s *Server) Send(to string, message string)
```

向指定客户端发送文本消息。

- **to:** 目标 ClientID，`""` 或 `"*"` 表示广播给所有客户端
- **message:** 文本内容

#### Broadcast()

```go
func (s *Server) Broadcast(message string)
```

等价于 `Send("*", message)`。

#### SendFile()

```go
func (s *Server) SendFile(to string, filename string) error
```

读取文件并发送给指定客户端。

- **返回：** 文件不存在或读取失败时的 error
- **ContentType:** 自动设为 `"file"`

#### BroadcastFile()

```go
func (s *Server) BroadcastFile(filename string) error
```

等价于 `SendFile("*", filename)`。

#### SendJSON()

```go
func (s *Server) SendJSON(to string, v interface{}) error
```

序列化 v 为 JSON 并发送。

- **ContentType:** 自动设为 `"application/json"`
- **返回：** 序列化失败的 error

#### BroadcastJSON()

```go
func (s *Server) BroadcastJSON(v interface{}) error
```

等价于 `SendJSON("*", v)`。

### 2.4 消息处理

```go
type MessageHandler func(g *Server, msg *Message)
```

handler 在收到完整的 SESSION_END 后被调用：
- **g:** Server 引用，可直接调用 Send/Broadcast/SendFile 等
- **msg:** 组装后的完整消息

```go
func myHandler(g *gateway.Server, msg *gateway.Message) {
    fmt.Println("来自:", msg.ClientID)
    fmt.Println("内容:", msg.Text())

    g.Send(msg.ClientID, "已收到")
    g.BroadcastJSON(map[string]string{"status": "delivered"})
}
```

### 2.5 连接管理

#### ClientCount()

```go
func (s *Server) ClientCount() int
```

返回当前在线客户端数量。

### 2.6 Channel 集成

#### RegisterChannel()

```go
func (s *Server) RegisterChannel(ch channel.Channel)
```

注册一个 Channel 适配器。

#### GetChannel()

```go
func (s *Server) GetChannel(name string) (channel.Channel, bool)
```

按名称获取 Channel。

#### Channels()

```go
func (s *Server) Channels() map[string]channel.Channel
```

获取所有已注册 Channel。

---

## 3. Client API

### 3.1 构造函数

```go
func NewClient(opts ...ClientOption) *Client
```

**ClientOption 列表：**

| Option | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `WithClientAddr(addr)` | string | `localhost:8081` | 服务端完整地址 |
| `WithClientPort(port)` | int | `8081` | 服务端端口（自动拼接为 localhost:{port}） |
| `WithClientPath(path)` | string | `/ws` | WebSocket 路径 |

**示例：**
```go
client := gateway.NewClient(
    gateway.WithClientPort(8080),
    gateway.WithClientPath("/ws"),
)
```

### 3.2 连接管理

#### Connect()

```go
func (c *Client) Connect() error
```

建立 WebSocket 连接。内部启动 readPump 和 writePump 协程。

- **返回：** 连接失败时的 error

#### Close()

```go
func (c *Client) Close() error
```

关闭连接，清理资源。

#### IsConnected()

```go
func (c *Client) IsConnected() bool
```

返回连接状态。

### 3.3 消息接收

#### OnMessage()

```go
func (c *Client) OnMessage(h MessageHandlerClient)
```

设置消息接收回调。

```go
type MessageHandlerClient func(msg *Message)
```

### 3.4 消息发送

**重要：** Client 的 Send/SendFile/SendJSON 方法内部自动完成会话握手流程（SESSION_START → DATA → SESSION_END），调用者无需关心会话细节。

#### Send()

```go
func (c *Client) Send(message string) error
```

发送文本消息。内部自动执行单分片会话。

#### SendFile()

```go
func (c *Client) SendFile(r io.Reader) error
```

从 io.Reader 读取数据并发送。

#### SendJSON()

```go
func (c *Client) SendJSON(v interface{}) error
```

序列化后发送。

**示例：**
```go
client := gateway.NewClient(gateway.WithClientPort(8080))
client.Connect()
defer client.Close()

client.OnMessage(func(msg *gateway.Message) {
    fmt.Println("收到:", msg.Text())
})

client.Send("你好世界")
client.SendJSON(map[string]string{"key": "value"})
client.SendFile(os.Open("test.txt"))
```

---

## 4. Message 类型

```go
type Message struct {
    ID          string    `json:"id"`           // 消息唯一标识（UUID）
    ChannelID   string    `json:"channel_id"`   // 关联的 Channel 名称
    ClientID    string    `json:"client_id"`    // 发送方/目标客户端 ID
    SessionID   string    `json:"session_id"`   // 会话 ID
    Direction   Direction `json:"direction"`    // inbound=服务端→客户端, outbound=客户端→服务端
    Data        []byte    `json:"data"`         // 消息体（原始字节）
    ContentType string    `json:"content_type"` // 内容类型: "", "file", "application/json"
    Timestamp   time.Time `json:"timestamp"`     // 创建时间
}

func (m *Message) Text() string  // 将 Data 作为字符串返回
```

---

## 5. Channel 集成

Gateway 通过 `RegisterChannel` 注册 Channel 适配器后，可在 handler 内通过 `msg.ChannelID` 判断消息来源渠道，并通过 `GetChannel` 获取对应 Channel 进行回复：

```go
func handler(g *gateway.Server, msg *gateway.Message) {
    if ch, ok := g.GetChannel(msg.ChannelID); ok {
        ch.SendMessage(ctx, replyMsg)
    }
}
```

---

## 6. 错误定义

| 错误变量 | 说明 |
|----------|------|
| `ErrAlreadyRunning` | Server 已在运行时再次调用 Start() |
| `ErrNotRunning` | Server 未运行时调用 Shutdown() |
| `ErrClientNotFound` | 向不存在的 ClientID 发送消息 |
| `ErrSessionNotFound` | 向不存在的 Session 发送 DATA |
| `ErrInvalidCommand` | 无法识别的命令类型 |

---

## 注意事项

1. **Start() 是阻塞调用** — 应在 goroutine 中启动或作为程序入口的最后一步
2. **Session 超时默认 30 分钟** — 未结束的会话会被自动清理
3. **Client.Send 是同步操作** — 内部包含三次网络往返（start→data→end）
4. **大文件传输建议用 SendFile** — 直接走 io.Reader 流式读取
5. **Send/Broadcast 不保证送达** — 当客户端 send buffer 满时会静默丢弃
