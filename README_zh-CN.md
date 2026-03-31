<div align="center">

# Gort

**一个轻量级多渠道通信网关，专为桌面 App 单用户场景设计，提供统一的 WebSocket 聊天通信服务接入层。通过标准化的 Channel 接口抽象，实现多种即时通讯平台的统一接入和消息转发。**

[![Go Report Card](https://goreportcard.com/badge/github.com/DotNetAge/gort)](https://goreportcard.com/report/github.com/DotNetAge/gort)
[![Go Version](https://img.shields.io/badge/go-1.23+-blue.svg)](https://golang.org/dl/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Documentation](https://img.shields.io/badge/docs-gort.rayainfo.cn-cbdc38.svg)](https://gort.rayainfo.cn)
[![codecov](https://codecov.io/gh/DotNetAge/gort/graph/badge.svg?token=placeholder)](https://codecov.io/gh/DotNetAge/gort)

[**官方网站**](https://gort.rayainfo.cn) | [**English**](./README.md) | [**中文说明**](./README_zh-CN.md)

</div>

---

用 Go 编写的多渠道通信网关，能够统一处理不同 IM 平台的消息。

## 概述

gort 是一个轻量级、可扩展的网关，用于连接多个即时通讯平台与 WebSocket 客户端。它提供统一的消息格式和清晰的架构，用于构建聊天机器人和消息处理系统。

### 核心特性

- **多渠道支持**：通过统一接口连接 10+ IM 平台
  - 钉钉、飞书、微信（国内）
  - Telegram、Slack、Discord（国际）
  - WhatsApp、Messenger（客户支持）
  - iMessage（macOS 生态）
  - 企业微信（企业）
- **WebSocket 服务器**：实时推送消息到连接的客户端
- **中间件链**：可扩展的中间件系统，处理横切关注点
- **配置管理**：灵活的配置方式，支持文件、环境变量或命令行
- **测试驱动开发**：全面的测试覆盖率和清晰的示例
- **桌面应用优化**：专为单用户桌面场景优化

## 安装

```bash
go get github.com/DotNetAge/gort
```

### 前置要求

- Go 1.23 或更高版本
- Make（可选，用于使用 Makefile 命令）

## 快速开始

```go
package main

import (
    "context"
    "log"
    
    "github.com/DotNetAge/gort/pkg/channel"
    "github.com/DotNetAge/gort/pkg/config"
    "github.com/DotNetAge/gort/pkg/gateway"
    "github.com/DotNetAge/gort/pkg/message"
    "github.com/DotNetAge/gort/pkg/session"
)

func main() {
    // 加载配置
    cfg, err := config.Load("")
    if err != nil {
        log.Fatal(err)
    }
    
    // 创建会话管理器
    sessionMgr := session.NewManager(session.Config{
        OnMessage: func(clientID string, msg *message.Message) {
            log.Printf("从 %s 收到: %s", clientID, msg.Content)
        },
    })
    
    // 创建网关
    gw := gateway.New(gateway.Config{
        WebSocketAddr: ":9000",
        HTTPAddr:      ":8080",
    })
    
    // 注册渠道
    wechat := channel.NewMockChannel("wechat", channel.ChannelTypeWeChat)
    gw.RegisterChannel(wechat)
    
    // 注册消息处理器
    gw.RegisterChannelHandler(func(ctx context.Context, msg *message.Message) error {
        log.Printf("渠道消息: %+v", msg)
        return nil
    })
    
    // 启动网关
    if err := gw.Start(context.Background()); err != nil {
        log.Fatal(err)
    }
    defer gw.Stop(context.Background())
    
    // 保持运行...
    select {}
}
```

## 配置

### 配置文件

gort 支持 JSON、YAML 和 TOML 配置文件。在工作目录中创建 `config.yaml`：

```yaml
server:
  http_port: 8080
  ws_port: 8081
  webhook_path: /webhook
  read_timeout: 30
  write_timeout: 30

channels:
  wechat:
    enabled: true
    app_id: your_app_id
  dingtalk:
    enabled: false
  feishu:
    enabled: false

log:
  level: info
  format: text
  output: stdout
```

### 环境变量

所有配置值都可以通过 `GORT_` 前缀的环境变量覆盖：

```bash
export GORT_SERVER_HTTP_PORT=9090
export GORT_SERVER_WS_PORT=9091
export GORT_LOG_LEVEL=debug
export GORT_CHANNELS_WECHAT_TOKEN=your_token
export GORT_CHANNELS_WECHAT_SECRET=your_secret
```

### 配置优先级

配置值按以下优先级加载（从高到低）：

1. 命令行参数
2. 环境变量
3. 配置文件
4. 默认值

## 架构

```
┌─────────────────────────────────────────────────────────────┐
│                         Gateway                              │
│  ┌─────────────────────────────────────────────────────┐   │
│  │                   Middleware Chain                    │   │
│  │  [日志] → [追踪] → [认证] → [自定义中间件]          │   │
│  └─────────────────────────────────────────────────────┘   │
│                            │                                │
│         ┌──────────────────┼──────────────────┐            │
│         ▼                  ▼                  ▼            │
│  ┌─────────────┐    ┌─────────────┐    ┌─────────────┐    │
│  │  微信       │    │  钉钉       │    │   飞书      │    │
│  │  Channel    │    │  Channel    │    │  Channel    │    │
│  └─────────────┘    └─────────────┘    └─────────────┘    │
│         │                  │                  │            │
└─────────┼──────────────────┼──────────────────┼───────────┘
          ▼                  ▼                  ▼
    ┌──────────┐       ┌──────────┐       ┌──────────┐
    │  微信    │       │  钉钉    │       │  飞书    │
    │  服务器  │       │  服务器  │       │  服务器  │
    └──────────┘       └──────────┘       └──────────┘
```

### 核心组件

| 包名             | 描述                           |
| ---------------- | ------------------------------ |
| `pkg/gateway`    | 核心协调器，管理渠道和消息路由 |
| `pkg/channel`    | 外部 IM 平台的协议适配器       |
| `pkg/session`    | WebSocket 连接管理器           |
| `pkg/message`    | 标准消息格式                   |
| `pkg/middleware` | 中间件链，处理横切关注点       |
| `pkg/config`     | 基于 Viper 的配置管理          |

## 支持的渠道

| 渠道           | 接入方式          | 文档链接                                           |
| -------------- | ----------------- | -------------------------------------------------- |
| 钉钉           | Webhook 机器人    | [文档](https://gort.rayainfo.cn/channel/dingtalk)  |
| 飞书           | 自建应用 + Token  | [文档](https://gort.rayainfo.cn/channel/feishu)    |
| Telegram       | Bot Token         | [文档](https://gort.rayainfo.cn/channel/telegram)  |
| 微信（公众号） | 公众号 + Token    | [文档](https://gort.rayainfo.cn/channel/wechat)    |
| WhatsApp       | Business API      | [文档](https://gort.rayainfo.cn/channel/whatsapp)  |
| iMessage       | macOS + imsg CLI  | [文档](https://gort.rayainfo.cn/channel/imessage)  |
| Messenger      | Page Access Token | [文档](https://gort.rayainfo.cn/channel/messenger) |
| 企业微信       | Webhook 机器人    | [文档](https://gort.rayainfo.cn/channel/wecom)     |
| Slack          | Bot Token         | [文档](https://gort.rayainfo.cn/channel/slack)     |
| Discord        | Bot Token         | [文档](https://gort.rayainfo.cn/channel/discord)   |

## API 文档

### 消息

```go
// 创建新消息
msg := message.NewMessage(
    "msg_001",                    // ID
    "wechat",                     // 渠道 ID
    message.DirectionInbound,     // 方向
    message.UserInfo{             // 发送者
        ID: "user_001",
        Name: "张三",
        Platform: "wechat",
    },
    "你好，世界！",              // 内容
    message.MessageTypeText,      // 类型
)

// 设置元数据
msg.SetMetadata("trace_id", "trace_123")

// 验证
if err := msg.Validate(); err != nil {
    log.Fatal(err)
}
```

### 渠道

```go
// 创建渠道
ch := channel.NewMockChannel("wechat", channel.ChannelTypeWeChat)

// 启动渠道
err := ch.Start(ctx, func(ctx context.Context, msg *message.Message) error {
    // 处理入站消息
    return nil
})

// 发送消息到渠道
err := ch.SendMessage(ctx, msg)

// 停止渠道
err := ch.Stop(ctx)
```

### 中间件

```go
// 创建自定义中间件
type MyMiddleware struct{}

func (m *MyMiddleware) Name() string {
    return "MyMiddleware"
}

func (m *MyMiddleware) Handle(ctx context.Context, msg *message.Message, next middleware.Handler) error {
    // 前置处理
    log.Printf("处理消息: %s", msg.ID)
    
    // 调用下一个处理器
    err := next(ctx, msg)
    
    // 后置处理
    log.Printf("完成处理: %s", msg.ID)
    
    return err
}

// 注册中间件
gw.Use(&MyMiddleware{})
```

## 测试

### 运行测试

```bash
# 运行所有测试
go test ./... -v

# 运行测试并生成覆盖率
go test ./... -cover

# 生成覆盖率报告
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out

# 运行基准测试
go test ./... -bench=. -benchmem
```

### 测试覆盖率

项目保持高测试覆盖率：

| 包名           | 覆盖率 |
| -------------- | ------ |
| pkg/channel    | 100%   |
| pkg/config     | 96%    |
| pkg/gateway    | 84%    |
| pkg/message    | 100%   |
| pkg/middleware | 100%   |
| pkg/session    | 95%    |

## 项目结构

```
gort/
├── pkg/
│   ├── channel/          # 渠道接口和实现
│   │   ├── channel.go    # 接口定义
│   │   ├── wechat/       # 微信渠道
│   │   ├── dingtalk/     # 钉钉渠道
│   │   ├── feishu/       # 飞书渠道
│   │   ├── telegram/     # Telegram 渠道
│   │   ├── wecom/        # 企业微信渠道
│   │   ├── slack/        # Slack 渠道
│   │   ├── discord/      # Discord 渠道
│   │   ├── imessage/     # iMessage 渠道
│   │   ├── whatsapp/     # WhatsApp 渠道
│   │   └── messenger/    # Messenger 渠道
│   ├── config/           # 配置管理
│   │   ├── config.go
│   │   └── config_test.go
│   ├── gateway/          # 核心网关
│   │   ├── gateway.go
│   │   └── gateway_test.go
│   ├── message/          # 消息类型
│   │   ├── message.go
│   │   ├── errors.go
│   │   └── message_test.go
│   ├── middleware/       # 中间件系统
│   │   ├── middleware.go
│   │   └── middleware_test.go
│   └── session/          # 会话管理
│       ├── manager.go
│       └── manager_test.go
├── docs/
│   └── design/           # 设计文档
├── go.mod
├── go.sum
├── README.md
└── README_zh-CN.md
```

## 贡献指南

1. Fork 仓库
2. 创建功能分支 (`git checkout -b feature/amazing-feature`)
3. 为你的更改编写测试
4. 确保所有测试通过 (`go test ./...`)
5. 提交更改 (`git commit -m 'Add amazing feature'`)
6. 推送到分支 (`git push origin feature/amazing-feature`)
7. 创建 Pull Request

### 代码规范

- 遵循 [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)
- 提交前运行 `go fmt`
- 确保 `go vet` 通过
- 保持测试覆盖率在 85% 以上

## 文档

详细文档请访问 [gort.rayainfo.cn](https://gort.rayainfo.cn)

- [Gateway 设计](https://gort.rayainfo.cn/gateway)
- [Channel 设计](https://gort.rayainfo.cn/channel)
- [Session Manager](https://gort.rayainfo.cn/session-manager)
- [Middleware](https://gort.rayainfo.cn/middleware)
- [Message 格式](https://gort.rayainfo.cn/message)
- [配置管理](https://gort.rayainfo.cn/config)

## 许可证

MIT License - 详见 LICENSE 文件
