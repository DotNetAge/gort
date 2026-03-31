<div align="center">

# Gort

**Gort is a lightweight multi-channel communication gateway, specifically designed for the "desktop App single-user scenario", providing a unified WebSocket chat communication service access layer. Through standardized Channel interface abstraction, unified access and message forwarding for multiple instant messaging platforms are achieved.**

[![Go Report Card](https://goreportcard.com/badge/github.com/DotNetAge/gort)](https://goreportcard.com/report/github.com/DotNetAge/gort)
[![Go Version](https://img.shields.io/badge/go-1.25+-blue.svg)](https://golang.org/dl/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Documentation](https://img.shields.io/badge/docs-gort.rayainfo.cn-6019bd.svg)](https://gort.rayainfo.cn)
[![codecov](https://codecov.io/gh/DotNetAge/gort/graph/badge.svg?token=placeholder)](https://codecov.io/gh/DotNetAge/gort)

[**官方网站**](https://gort.rayainfo.cn) | [**English**](./README.md) | [**中文说明**](./README_zh-CN.md)

</div>

---

A multi-channel communication gateway written in Go that enables unified message handling across different IM platforms.

## Overview

gort is a lightweight, extensible gateway that bridges multiple instant messaging platforms (WeChat, DingTalk, Feishu) with WebSocket clients. It provides a unified message format and a clean architecture for building chatbots and message processing systems.

### Key Features

- **Multi-Channel Support**: Connect to WeChat, DingTalk, and Feishu through a unified interface
- **WebSocket Server**: Push messages to connected clients in real-time
- **Middleware Chain**: Extensible middleware system for cross-cutting concerns
- **Configuration Management**: Flexible configuration via files, environment variables, or command-line
- **Test-Driven Development**: Comprehensive test coverage with clear examples

## Installation

```bash
go get github.com/example/gort
```

### Prerequisites

- Go 1.21 or higher
- Make (optional, for using Makefile commands)

## Quick Start

```go
package main

import (
    "context"
    "log"
    
    "github.com/example/gort/pkg/channel"
    "github.com/example/gort/pkg/config"
    "github.com/example/gort/pkg/gateway"
    "github.com/example/gort/pkg/message"
    "github.com/example/gort/pkg/session"
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
    gw := gateway.New(sessionMgr)
    
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

1. Environment variables
2. Command-line arguments
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
│   │   └── channel_test.go
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
└── README.md
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

## License

MIT License - see LICENSE file for details.
