<div align="center">

# Gort

**A lightweight multi-channel communication gateway designed for desktop app single-user scenarios, providing a unified WebSocket chat communication service access layer. Through standardized Channel interface abstraction, it achieves unified access and message forwarding for multiple instant messaging platforms.**
[![Go Reference](https://pkg.go.dev/badge/github.com/DotNetAge/gort.svg)](https://pkg.go.dev/github.com/DotNetAge/gort)
[![Go Report Card](https://goreportcard.com/badge/github.com/DotNetAge/gort)](https://goreportcard.com/report/github.com/DotNetAge/gort)
[![Go Version](https://img.shields.io/badge/go-1.23+-blue.svg)](https://golang.org/dl/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Documentation](https://img.shields.io/badge/docs-gort.rayainfo.cn-cbdc38.svg)](https://gort.rayainfo.cn)
[![codecov](https://codecov.io/gh/DotNetAge/gort/graph/badge.svg?token=placeholder)](https://codecov.io/gh/DotNetAge/gort)

[**Official Website**](https://gort.rayainfo.cn) | [**English**](./README.md) | [**中文说明**](./README_zh-CN.md)

</div>

---

A multi-channel communication gateway written in Go that enables unified message handling across different IM platforms.

## Overview

gort is a lightweight, extensible gateway that bridges multiple instant messaging platforms with WebSocket clients. It provides a unified message format and a clean architecture for building chatbots and message processing systems.

### Key Features

- **Multi-Channel Support**: Connect to 10+ IM platforms through a unified interface
  - DingTalk, Feishu, WeChat (domestic)
  - Telegram, Slack, Discord (international)
  - WhatsApp, Messenger (customer support)
  - iMessage (macOS ecosystem)
  - WeCom (enterprise)
- **WebSocket Server**: Push messages to connected clients in real-time
- **Middleware Chain**: Extensible middleware system for cross-cutting concerns
- **Configuration Management**: Flexible configuration via files, environment variables, or command-line
- **Test-Driven Development**: Comprehensive test coverage with clear examples
- **Desktop App Focused**: Optimized for single-user desktop scenarios

## Installation

```bash
go get github.com/DotNetAge/gort
```

### Prerequisites

- Go 1.23 or higher
- Make (optional, for using Makefile commands)

## Quick Start

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
    // Load configuration
    cfg, err := config.Load("")
    if err != nil {
        log.Fatal(err)
    }
    
    // Create session manager
    sessionMgr := session.NewManager(session.Config{
        OnMessage: func(clientID string, msg *message.Message) {
            log.Printf("Received from %s: %s", clientID, msg.Content)
        },
    })
    
    // Create gateway
    gw := gateway.New(gateway.Config{
        WebSocketAddr: ":9000",
        HTTPAddr:      ":8080",
    })
    
    // Register channels
    wechat := channel.NewMockChannel("wechat", channel.ChannelTypeWeChat)
    gw.RegisterChannel(wechat)
    
    // Register message handler
    gw.RegisterChannelHandler(func(ctx context.Context, msg *message.Message) error {
        log.Printf("Channel message: %+v", msg)
        return nil
    })
    
    // Start gateway
    if err := gw.Start(context.Background()); err != nil {
        log.Fatal(err)
    }
    defer gw.Stop(context.Background())
    
    // Keep running...
    select {}
}
```

## Configuration

### Configuration File

gort supports JSON, YAML, and TOML configuration files. Create a `config.yaml` in your working directory:

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

### Environment Variables

All configuration values can be overridden with environment variables using the `GORT_` prefix:

```bash
export GORT_SERVER_HTTP_PORT=9090
export GORT_SERVER_WS_PORT=9091
export GORT_LOG_LEVEL=debug
export GORT_CHANNELS_WECHAT_TOKEN=your_token
export GORT_CHANNELS_WECHAT_SECRET=your_secret
```

### Configuration Priority

Configuration values are loaded with the following priority (highest to lowest):

1. Command-line arguments
2. Environment variables
3. Configuration file
4. Default values

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                         Gateway                              │
│  ┌─────────────────────────────────────────────────────┐   │
│  │                   Middleware Chain                    │   │
│  │  [Logging] → [Trace] → [Auth] → [Your Middleware]   │   │
│  └─────────────────────────────────────────────────────┘   │
│                            │                                │
│         ┌──────────────────┼──────────────────┐            │
│         ▼                  ▼                  ▼            │
│  ┌─────────────┐    ┌─────────────┐    ┌─────────────┐    │
│  │  WeChat     │    │  DingTalk   │    │   Feishu    │    │
│  │  Channel    │    │  Channel    │    │  Channel    │    │
│  └─────────────┘    └─────────────┘    └─────────────┘    │
│         │                  │                  │            │
└─────────┼──────────────────┼──────────────────┼───────────┘
          ▼                  ▼                  ▼
    ┌──────────┐       ┌──────────┐       ┌──────────┐
    │  WeChat  │       │ DingTalk │       │  Feishu  │
    │  Server  │       │  Server  │       │  Server  │
    └──────────┘       └──────────┘       └──────────┘
```

### Components

| Package          | Description                                                |
| ---------------- | ---------------------------------------------------------- |
| `pkg/gateway`    | Core coordinator that manages channels and message routing |
| `pkg/channel`    | Protocol adapters for external IM platforms                |
| `pkg/session`    | WebSocket connection manager                               |
| `pkg/message`    | Standard message format                                    |
| `pkg/middleware` | Middleware chain for cross-cutting concerns                |
| `pkg/config`     | Configuration management with Viper                        |

## Supported Channels

| Channel          | Access Method            | Documentation                                      |
| ---------------- | ------------------------ | -------------------------------------------------- |
| DingTalk (钉钉)  | Webhook Robot            | [docs](https://gort.rayainfo.cn/channel/dingtalk)  |
| Feishu (飞书)    | Self-built App + Token   | [docs](https://gort.rayainfo.cn/channel/feishu)    |
| Telegram         | Bot Token                | [docs](https://gort.rayainfo.cn/channel/telegram)  |
| WeChat (公众号)  | Official Account + Token | [docs](https://gort.rayainfo.cn/channel/wechat)    |
| WhatsApp         | Business API             | [docs](https://gort.rayainfo.cn/channel/whatsapp)  |
| iMessage         | macOS + imsg CLI         | [docs](https://gort.rayainfo.cn/channel/imessage)  |
| Messenger        | Page Access Token        | [docs](https://gort.rayainfo.cn/channel/messenger) |
| WeCom (企业微信) | Webhook Robot            | [docs](https://gort.rayainfo.cn/channel/wecom)     |
| Slack            | Bot Token                | [docs](https://gort.rayainfo.cn/channel/slack)     |
| Discord          | Bot Token                | [docs](https://gort.rayainfo.cn/channel/discord)   |

## API Documentation

### Message

```go
// Create a new message
msg := message.NewMessage(
    "msg_001",                    // ID
    "wechat",                     // Channel ID
    message.DirectionInbound,     // Direction
    message.UserInfo{             // From
        ID: "user_001",
        Name: "Alice",
        Platform: "wechat",
    },
    "Hello, World!",              // Content
    message.MessageTypeText,      // Type
)

// Set metadata
msg.SetMetadata("trace_id", "trace_123")

// Validate
if err := msg.Validate(); err != nil {
    log.Fatal(err)
}
```

### Channel

```go
// Create a channel
ch := channel.NewMockChannel("wechat", channel.ChannelTypeWeChat)

// Start the channel
err := ch.Start(ctx, func(ctx context.Context, msg *message.Message) error {
    // Handle incoming message
    return nil
})

// Send message to channel
err := ch.SendMessage(ctx, msg)

// Stop the channel
err := ch.Stop(ctx)
```

### Middleware

```go
// Create custom middleware
type MyMiddleware struct{}

func (m *MyMiddleware) Name() string {
    return "MyMiddleware"
}

func (m *MyMiddleware) Handle(ctx context.Context, msg *message.Message, next middleware.Handler) error {
    // Pre-processing
    log.Printf("Processing message: %s", msg.ID)
    
    // Call next handler
    err := next(ctx, msg)
    
    // Post-processing
    log.Printf("Finished processing: %s", msg.ID)
    
    return err
}

// Register middleware
gw.Use(&MyMiddleware{})
```

## Testing

### Run Tests

```bash
# Run all tests
go test ./... -v

# Run tests with coverage
go test ./... -cover

# Run tests with coverage report
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out

# Run benchmarks
go test ./... -bench=. -benchmem
```

### Test Coverage

The project maintains high test coverage:

| Package        | Coverage |
| -------------- | -------- |
| pkg/channel    | 100%     |
| pkg/config     | 96%      |
| pkg/gateway    | 84%      |
| pkg/message    | 100%     |
| pkg/middleware | 100%     |
| pkg/session    | 95%      |

## Project Structure

```
gort/
├── pkg/
│   ├── channel/          # Channel interface and implementations
│   │   ├── channel.go    # Interface definitions
│   │   ├── wechat/       # WeChat channel
│   │   ├── dingtalk/     # DingTalk channel
│   │   ├── feishu/       # Feishu channel
│   │   ├── telegram/     # Telegram channel
│   │   ├── wecom/        # WeCom channel
│   │   ├── slack/        # Slack channel
│   │   ├── discord/      # Discord channel
│   │   ├── imessage/     # iMessage channel
│   │   ├── whatsapp/     # WhatsApp channel
│   │   └── messenger/    # Messenger channel
│   ├── config/           # Configuration management
│   │   ├── config.go
│   │   └── config_test.go
│   ├── gateway/          # Core gateway
│   │   ├── gateway.go
│   │   └── gateway_test.go
│   ├── message/          # Message types
│   │   ├── message.go
│   │   ├── errors.go
│   │   └── message_test.go
│   ├── middleware/       # Middleware system
│   │   ├── middleware.go
│   │   └── middleware_test.go
│   └── session/          # Session management
│       ├── manager.go
│       └── manager_test.go
├── docs/
│   └── design/           # Design documents
├── go.mod
├── go.sum
├── README.md
└── README_zh-CN.md
```

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Write tests for your changes
4. Ensure all tests pass (`go test ./...`)
5. Commit your changes (`git commit -m 'Add amazing feature'`)
6. Push to the branch (`git push origin feature/amazing-feature`)
7. Open a Pull Request

### Code Style

- Follow [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)
- Run `go fmt` before committing
- Ensure `go vet` passes
- Maintain test coverage above 85%

## Documentation

For detailed documentation, please visit [gort.rayainfo.cn](https://gort.rayainfo.cn)

- [Gateway Design](https://gort.rayainfo.cn/gateway)
- [Channel Design](https://gort.rayainfo.cn/channel)
- [Session Manager](https://gort.rayainfo.cn/session-manager)
- [Middleware](https://gort.rayainfo.cn/middleware)
- [Message Format](https://gort.rayainfo.cn/message)
- [Configuration](https://gort.rayainfo.cn/config)

## License

MIT License - see LICENSE file for details.
