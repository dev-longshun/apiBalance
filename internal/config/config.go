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
}

type ServerConfig struct {
	Port int `yaml:"port"`
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

func Load(path string) (*Config, error) {
	cfg := &Config{
		Server:    ServerConfig{Port: 8080},
		Database:  DatabaseConfig{Path: "./data/quota-sentinel.db"},
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
}
