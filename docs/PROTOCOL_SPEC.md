# Gort 通信协议规格说明书

## 一、协议概述

Gort 采用双层架构设计：

```
┌─────────────────────────────────────────────────────────────┐
│                      对象层 (Object Layer)                   │
│  Client / Server - 面向对象API，高层抽象                      │
│  Send() / SendJSON() / SendFile() / SendBatch()            │
│  BeginSession() / EndSession() / OnReceived()               │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                      协议层 (Protocol Layer)                 │
│  BEGN / TEXT / JSON / FILE / FRAME / CLSE / OK / ERR       │
│  基于文本指令 + JSON消息体的混合协议                          │
└─────────────────────────────────────────────────────────────┘
```

---

## 二、协议层详细定义

### 2.1 命令格式

**通用格式**：
```
COMMAND|param1|param2|param3|param4
```

**响应格式**：
```
OK|||                    # 成功
ERR|reason|||           # 错误
```

### 2.2 命令列表

| 命令 | 方向 | 描述 | 参数格式 |
|------|------|------|----------|
| `BEGN` | C→S | 开始会话 | `BEGN|sessionId|||` |
| `TEXT` | C→S | 文本消息 | `TEXT|sessionId|content` |
| `JSON` | C→S | JSON消息 | `JSON|sessionId|jsonContent` |
| `FILE` | C→S | 文件消息 | `FILE|sessionId|binaryData` |
| `FRAME` | C→S | 分片传输 | `FRAME|sessionId|index|total|data` |
| `CLSE` | C→S | 结束会话 | `CLSE|sessionId|||` |
| `OK` | S→C | 成功响应 | `OK|||` |
| `ERR` | S→C | 错误响应 | `ERR|reason|||` |

### 2.3 会话流程

#### 单消息模式（无会话）
```
Client                              Server
   |                                   |
   |-------- TEXT|content ------------>|  (立即处理)
   |<-------- OK||| -------------------|
   |                                   |
```

#### 会话模式（多消息）
```
Client                              Server
   |                                   |
   |-------- BEGN|sessId||| ---------->|  (创建会话)
   |<-------- BEGN|sessId|OK|| --------|
   |                                   |
   |-------- TEXT|sessId|msg1 -------->|  (收集)
   |<-------- OK||| -------------------|
   |-------- JSON|sessId|{...} ------->|  (收集)
   |<-------- OK||| -------------------|
   |-------- FILE|sessId|binary ------>|  (收集)
   |<-------- OK||| -------------------|
   |                                   |
   |-------- CLSE|sessId||| ---------->|  (触发handler)
   |<-------- OK||| -------------------|
   |                                   |
```

#### 分片传输模式
```
Client                              Server
   |                                   |
   |-------- BEGN|sessId||| ---------->|
   |<-------- BEGN|sessId|OK|| --------|
   |                                   |
   |-------- FRAME|sessId|0|3|chunk0 ->|
   |<-------- OK||| -------------------|
   |-------- FRAME|sessId|1|3|chunk1 ->|
   |<-------- OK||| -------------------|
   |-------- FRAME|sessId|2|3|chunk2 ->|
   |<-------- OK||| -------------------|
   |                                   |
   |-------- CLSE|sessId||| ---------->|  (组装后触发handler)
   |<-------- OK||| -------------------|
   |                                   |
```

---

## 三、Message 对象设计（业务层视角）

### 3.1 设计原则

Message 对象是**业务层**与**协议层**之间的数据载体，严格排除协议层实现细节。

### 3.2 Message 结构定义

```go
type Message struct {
    ID          string    // 消息唯一标识 (UUID)
    ClientID    string    // 来源客户端ID
    SessionID   string    // 会话ID (关联消息时必填，独立消息可为空)
    Direction   Direction // 传输方向: "inbound" | "outbound"
    Data        []byte    // 消息载荷
    ContentType string    // 内容类型: "text/plain" | "application/json" | "application/octet-stream"
    Timestamp   time.Time // 时间戳
}
```

### 3.3 字段约束

| 字段 | 类型 | 必填 | 约束 |
|------|------|------|------|
| ID | string | 是 | UUID v4 格式 |
| ClientID | string | 是 | 非空，客户端连接时分配 |
| SessionID | string | 否 | 独立消息可为空；会话消息必须匹配 |
| Direction | Direction | 是 | 仅限 "inbound" / "outbound" |
| Data | []byte | 是 | 最大 1MB |
| ContentType | string | 否 | 默认 "application/octet-stream" |
| Timestamp | time.Time | 是 | RFC3339 格式 |

### 3.4 禁止出现的字段

以下字段属于协议层实现细节，**严禁**出现在 Message 结构中：
- `Cmd` - 协议命令标识
- `Ack` - 协议确认状态
- 其他协议控制字段

---

## 四、对象层接口规范

### 4.1 Server 端接口

```go
type MessageHandler func(msg *Message)  // 消息接收处理函数

type Server interface {
    // 生命周期管理
    Start() error
    Shutdown(ctx context.Context) error
    IsRunning() bool
    ClientCount() int

    // 单消息发送
    Send(toClientID string, msg *Message) error
    SendJSON(toClientID string, v interface{}) error
    SendFile(toClientID string, filename string) error

    // 批量发送
    SendBatch(messages []*Message) error

    // 会话管理
    BeginSession(clientID string) (sessionID string, err error)
    EndSession(clientID, sessionID string) error

    // 广播（等同于向所有客户端发送）
    Broadcast(msg *Message) error

    // 消息处理
    OnReceived(handler MessageHandler)
}
```

### 4.2 Client 端接口

```go
type AckHandler func(ack AckType, err error)  // 确认回调

type Client interface {
    // 生命周期管理
    Connect(addr string) error
    Disconnect() error
    IsConnected() bool

    // 单消息发送（带确认）
    Send(msg *Message) error
    SendJSON(v interface{}) error
    SendFile(filename string) error

    // 批量发送
    SendBatch(messages []*Message) error

    // 会话管理
    BeginSession() (sessionID string, err error)
    EndSession() error

    // 消息接收
    OnReceived(handler func(msg *Message))
}
```

### 4.3 OnReceived 接口规范

**核心原则**：
- 接口中**不显式包含会话参数**
- 接口中**不引入"会话"概念**
- 通过 Message 的 SessionID 属性标识消息归属

**正确示例**：
```go
// Server 端
server.OnReceived(func(msg *Message) {
    if msg.SessionID != "" {
        // 处理关联消息
    } else {
        // 处理独立消息
    }
})

// Client 端
client.OnReceived(func(msg *Message) {
    // 处理接收到的消息
})
```

---

## 五、错误处理机制

### 5.1 错误码定义

| 错误码 | 描述 | 处理策略 |
|--------|------|----------|
| `ERR_SESS_NOT_FOUND` | 会话不存在 | 创建新会话或返回错误 |
| `ERR_SESS_EXISTS` | 会话已存在 | 使用现有会话 |
| `ERR_INCOMPLETE_FRAME` | 分片不完整 | 等待更多分片或超时清理 |
| `ERR_INVALID_FORMAT` | 消息格式错误 | 关闭连接 |
| `ERR_MAX_SIZE` | 消息超限 | 拒绝并返回错误 |
| `ERR_TIMEOUT` | 操作超时 | 重试或清理资源 |
| `ERR_CLIENT_NOT_FOUND` | 客户端不存在 | 跳过或记录日志 |

### 5.2 错误响应格式

```
ERR|<error_code>|<detail_message>|||
```

### 5.3 错误处理策略

| 场景 | 检测方法 | 处理策略 |
|------|----------|----------|
| 会话超时 | 10分钟无活动 | 自动清理会话 |
| 分片丢失 | index 不连续 | 通知客户端重传 |
| 消息超限 | Data > 1MB | 返回 ERR_MAX_SIZE |
| 格式错误 | 解析失败 | 记录并关闭连接 |

---

## 六、数据格式约束

### 6.1 消息大小限制

| 类型 | 最大值 | 备注 |
|------|--------|------|
| 单条消息 Data | 1MB | 超过返回错误 |
| 单条消息 Total | 10MB | 包含协议开销 |
| 分片单片大小 | 1MB | 推荐 64KB |
| 会话最大数量 | 1000 | 可配置 |

### 6.2 字符编码

- 所有文本使用 **UTF-8** 编码
- 二进制数据使用 **Base64** 编码（仅用于协议传输层）

### 6.3 时限约束

| 操作 | 超时时间 | 备注 |
|------|----------|------|
| 连接建立 | 10s | |
| 消息发送确认 | 30s | 可配置 |
| 会话空闲 | 30min | 可配置 |
| 分片传输总时间 | 5min | 可配置 |

---

## 七、使用规范与最佳实践

### 7.1 客户端使用规范

**推荐流程**：
```go
// 1. 建立连接
client, _ := gateway.NewClient("ws://localhost:8081/ws")
client.Connect()

// 2. 设置消息处理
client.OnReceived(func(msg *Message) {
    fmt.Printf("Received: %s\n", msg.Text())
})

// 3. 发送消息（自动确认）
client.Send(&Message{
    Data: []byte("Hello"),
    ContentType: "text/plain",
})
```

### 7.2 服务端使用规范

**推荐流程**：
```go
// 1. 创建服务器
server := gateway.New(
    gateway.WithAddr(":8081"),
    gateway.WithPath("/ws"),
)

// 2. 设置消息处理
server.OnReceived(func(msg *Message) {
    fmt.Printf("Received from %s: %s\n", msg.ClientID, msg.Text())
    // 回复
    server.Send(msg.ClientID, &Message{
        Data: []byte("Acknowledged"),
    })
})

// 3. 启动服务
server.Start()
```

### 7.3 禁忌操作

| 禁忌 | 原因 | 后果 |
|------|------|------|
| 在 OnReceived 中同步发送大量消息 | 可能导致死锁 | 服务无响应 |
| 发送大于 1MB 的单条消息 | 协议不支持 | 返回错误 |
| 在多线程中直接访问同一 Client | 非线程安全 | 数据竞争 |
| 不处理错误返回值 | 忽略错误 | 难以调试 |
| 长时间持有 Session 不释放 | 占用服务器资源 | 内存泄漏 |

### 7.4 最佳实践

1. **使用 SendJSON 发送结构化数据**
2. **大文件使用分片传输**
3. **及时调用 EndSession 释放资源**
4. **实现重连机制处理网络故障**
5. **对敏感数据进行加密后再发送**

---

## 八、时序图

### 8.1 完整会话交互

```
Client                              Server
   |                                   |
   | [WebSocket Connection]             |
   |-------- CONNECT ----------------->|
   |                                   |
   |-------- BEGN|sess-123||| -------->|
   |<-------- BEGN|sess-123|OK|| ------|
   |                                   |
   |-------- JSON|sess-123|{...} ----->|
   |<-------- OK||| -------------------|
   |                                   |
   |-------- TEXT|sess-123|Hello ----->|
   |<-------- OK||| -------------------|
   |                                   |
   |-------- FILE|sess-123|{binary} -->|
   |<-------- OK||| -------------------|
   |                                   |
   |-------- CLSE|sess-123||| -------->|
   |<-------- OK||| -------------------|
   |                                   |
```

### 8.2 服务端推送

```
Server                              Client
   |                                   |
   |<-------- {JSON Message} ----------|
   |-------- OK||| ------------------>|
   |                                   |
```

---

## 九、协议版本

当前版本：**1.0**

版本历史：
- 1.0: 初始版本，支持 BEGN/TEXT/JSON/FILE/FRAME/CLSE 命令

---

## 十、附录

### A.1 完整命令列表

| 命令 | 代码 | 方向 | 说明 |
|------|------|------|------|
| BEGN | 0x01 | C→S | 开始会话 |
| TEXT | 0x02 | C→S | 文本消息 |
| JSON | 0x03 | C→S | JSON消息 |
| FILE | 0x04 | C→S | 文件消息 |
| FRAME | 0x05 | C→S | 分片消息 |
| CLSE | 0x06 | C→S | 结束会话 |
| OK | 0x10 | S→C | 成功响应 |
| ERR | 0x11 | S→C | 错误响应 |

### A.2 ContentType 预定义值

| 类型 | 值 | 用途 |
|------|-----|------|
| PlainText | text/plain | 纯文本 |
| JSON | application/json | JSON数据 |
| OctetStream | application/octet-stream | 二进制数据 |
| FormData | multipart/form-data | 表单数据 |
