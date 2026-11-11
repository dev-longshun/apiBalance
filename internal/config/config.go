package config

import (
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Database  DatabaseConfig  `yaml:"database"`
	Scheduler SchedulerConfig `yaml:"scheduler"`
	Telegram  TelegramConfig  `yaml:"telegram"`
	Email     EmailConfig     `yaml:"email"`
	Admin     AdminConfig     `yaml:"admin"`
}

type AdminConfig struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type ServerConfig struct {
	Host string `yaml:"host"` // 监听地址，空则 0.0.0.0；用 network_mode: host 部署时可设 127.0.0.1
	Port int    `yaml:"port"`
}

type DatabaseConfig struct {
	Path string `yaml:"path"`
}

type SchedulerConfig struct {
	IntervalMinutes int `yaml:"interval_minutes"`
	MaxConcurrency  int `yaml:"max_concurrency"`
}

type TelegramConfig struct {
	BotToken string `yaml:"bot_token"`
	ChatID   string `yaml:"chat_id"`
}

type EmailConfig struct {
	Enabled      bool   `yaml:"enabled"`
	SMTPHost     string `yaml:"smtp_host"`
	SMTPPort     int    `yaml:"smtp_port"`
	SMTPSecurity string `yaml:"smtp_security"`
	SMTPUser     string `yaml:"smtp_user"`
	SMTPPassword string `yaml:"smtp_password"`
	FromName     string `yaml:"from_name"`
	To           string `yaml:"to"`
}

func Load(path string) (*Config, error) {
	cfg := &Config{
		Server:    ServerConfig{Host: "0.0.0.0", Port: 8080},
		Database:  DatabaseConfig{Path: "./data/upstream-balance.db"},
		Scheduler: SchedulerConfig{IntervalMinutes: 30, MaxConcurrency: 10},
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			applyEnv(cfg)
			return cfg, nil
		}
		return nil, err
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	applyEnv(cfg)
	return cfg, nil
}

func applyEnv(cfg *Config) {
	if v := os.Getenv("QS_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			cfg.Server.Port = p
		}
	}
	if v := os.Getenv("QS_DB_PATH"); v != "" {
		cfg.Database.Path = v
	}
	if v := os.Getenv("QS_INTERVAL"); v != "" {
		if m, err := strconv.Atoi(v); err == nil {
			cfg.Scheduler.IntervalMinutes = m
		}
	}
	if v := os.Getenv("QS_BOT_TOKEN"); v != "" {
		cfg.Telegram.BotToken = v
	}
	if v := os.Getenv("QS_CHAT_ID"); v != "" {
		cfg.Telegram.ChatID = v
	}
	if v := os.Getenv("QS_EMAIL_ENABLED"); v != "" {
		cfg.Email.Enabled = v == "true" || v == "1"
	}
	if v := os.Getenv("QS_SMTP_HOST"); v != "" {
		cfg.Email.SMTPHost = v
	}
	if v := os.Getenv("QS_SMTP_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			cfg.Email.SMTPPort = p
		}
	}
	if v := os.Getenv("QS_SMTP_SECURITY"); v != "" {
		cfg.Email.SMTPSecurity = v
	}
	if v := os.Getenv("QS_SMTP_USER"); v != "" {
		cfg.Email.SMTPUser = v
	}
	if v := os.Getenv("QS_SMTP_PASSWORD"); v != "" {
		cfg.Email.SMTPPassword = v
	}
	if v := os.Getenv("QS_SMTP_FROM_NAME"); v != "" {
		cfg.Email.FromName = v
	}
	if v := os.Getenv("QS_EMAIL_TO"); v != "" {
		cfg.Email.To = v
	}
	if v := os.Getenv("QS_ADMIN_USER"); v != "" {
		cfg.Admin.Username = v
	}
	if v := os.Getenv("QS_ADMIN_PASSWORD"); v != "" {
		cfg.Admin.Password = v
	}
}
