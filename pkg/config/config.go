package config

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

var (
	ErrInvalidPort        = errors.New("invalid port number, must be between 1 and 65535")
	ErrInvalidWebhookPath = errors.New("webhook path must start with /")
	ErrInvalidLogLevel   = errors.New("invalid log level, must be one of: debug, info, warn, error")
	ErrMissingCredentials  = errors.New("enabled channel is missing required credentials")
	ErrConfigNotFound    = errors.New("configuration file not found")
)

type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Channels ChannelsConfig `mapstructure:"channels"`
	Log      LogConfig      `mapstructure:"log"`
}

type ServerConfig struct {
	HTTPPort     int    `mapstructure:"http_port"`
	WSPort       int    `mapstructure:"ws_port"`
	WebhookPath  string `mapstructure:"webhook_path"`
	ReadTimeout  int    `mapstructure:"read_timeout"`
	WriteTimeout int    `mapstructure:"write_timeout"`
}

type ChannelsConfig struct {
	WeChat   WeChatConfig   `mapstructure:"wechat"`
	DingTalk DingTalkConfig `mapstructure:"dingtalk"`
	Feishu   FeishuConfig   `mapstructure:"feishu"`
}

type WeChatConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Token   string `mapstructure:"token"`
	AppID   string `mapstructure:"app_id"`
	Secret  string `mapstructure:"secret"`
}

type DingTalkConfig struct {
	Enabled   bool   `mapstructure:"enabled"`
	AppKey    string `mapstructure:"app_key"`
	AppSecret string `mapstructure:"app_secret"`
}

type FeishuConfig struct {
	Enabled   bool   `mapstructure:"enabled"`
	AppID     string `mapstructure:"app_id"`
	AppSecret string `mapstructure:"app_secret"`
}

type LogConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
	Output string `mapstructure:"output"`
}

func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			HTTPPort:     8080,
			WSPort:       8081,
			WebhookPath:  "/webhook",
			ReadTimeout:  30,
			WriteTimeout: 30,
		},
		Channels: ChannelsConfig{
			WeChat:   WeChatConfig{Enabled: false},
			DingTalk: DingTalkConfig{Enabled: false},
			Feishu:   FeishuConfig{Enabled: false},
		},
		Log: LogConfig{
			Level:  "info",
			Format: "text",
			Output: "stdout",
		},
	}
}

func (c *Config) Validate() error {
	if err := c.Server.Validate(); err != nil {
		return fmt.Errorf("server config: %w", err)
	}
	if err := c.Log.Validate(); err != nil {
		return fmt.Errorf("log config: %w", err)
	}
	if err := c.Channels.Validate(); err != nil {
		return fmt.Errorf("channels config: %w", err)
	}
	return nil
}

func (s *ServerConfig) Validate() error {
	if s.HTTPPort < 1 || s.HTTPPort > 65535 {
		return ErrInvalidPort
	}
	if s.WSPort < 1 || s.WSPort > 65535 {
		return ErrInvalidPort
	}
	if s.WebhookPath != "" && !strings.HasPrefix(s.WebhookPath, "/") {
		return ErrInvalidWebhookPath
	}
	return nil
}

func (l *LogConfig) Validate() error {
	validLevels := map[string]bool{
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
	}
	if !validLevels[l.Level] {
		return ErrInvalidLogLevel
	}
	return nil
}

func (c *ChannelsConfig) Validate() error {
	if c.WeChat.Enabled {
		if c.WeChat.Token == "" || c.WeChat.Secret == "" {
			return fmt.Errorf("%w: wechat", ErrMissingCredentials)
		}
	}
	if c.DingTalk.Enabled {
		if c.DingTalk.AppKey == "" || c.DingTalk.AppSecret == "" {
			return fmt.Errorf("%w: dingtalk", ErrMissingCredentials)
		}
	}
	if c.Feishu.Enabled {
		if c.Feishu.AppID == "" || c.Feishu.AppSecret == "" {
			return fmt.Errorf("%w: feishu", ErrMissingCredentials)
		}
	}
	return nil
}

func (s *ServerConfig) HTTPAddr() string {
	return fmt.Sprintf(":%d", s.HTTPPort)
}

func (s *ServerConfig) WSAddr() string {
	return fmt.Sprintf(":%d", s.WSPort)
}

func (s *ServerConfig) ReadTimeoutDuration() time.Duration {
	return time.Duration(s.ReadTimeout) * time.Second
}

func (s *ServerConfig) WriteTimeoutDuration() time.Duration {
	return time.Duration(s.WriteTimeout) * time.Second
}

type Loader struct {
	config *Config
}

func NewLoader() *Loader {
	return &Loader{
		config: DefaultConfig(),
	}
}

func (l *Loader) Load(configPath string) error {
	v := viper.New()
	v.SetConfigName("config")
	v.AddConfigPath(".")
	v.AddConfigPath("./config")
	if configPath != "" {
		v.AddConfigPath(configPath)
	}
	v.SetEnvPrefix("GORT")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	setDefaults(v)
	if err := v.ReadInConfig(); err != nil {
		var configFileNotFound viper.ConfigFileNotFoundError
		if !errors.As(err, &configFileNotFound) {
			return fmt.Errorf("failed to read config: %w", err)
		}
	}
	if err := v.Unmarshal(l.config); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}
	return l.config.Validate()
}

func (l *Loader) GetConfig() *Config {
	return l.config
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("server.http_port", 8080)
	v.SetDefault("server.ws_port", 8081)
	v.SetDefault("server.webhook_path", "/webhook")
	v.SetDefault("server.read_timeout", 30)
	v.SetDefault("server.write_timeout", 30)
	v.SetDefault("log.level", "info")
	v.SetDefault("log.format", "text")
	v.SetDefault("log.output", "stdout")
	v.SetDefault("channels.wechat.enabled", false)
	v.SetDefault("channels.dingtalk.enabled", false)
	v.SetDefault("channels.feishu.enabled", false)
}

func Load(configPath string) (*Config, error) {
	loader := NewLoader()
	if err := loader.Load(configPath); err != nil {
		return nil, err
	}
	return loader.GetConfig(), nil
}

func LoadFromBytes(data []byte, format string) (*Config, error) {
	loader := NewLoader()
	v := viper.New()
	v.SetConfigType(format)
	if err := v.ReadConfig(strings.NewReader(string(data))); err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}
	if err := v.Unmarshal(loader.config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}
	return loader.config, loader.config.Validate()
}
