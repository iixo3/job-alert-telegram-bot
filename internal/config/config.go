package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	SourceURL          string   `json:"source_url"`
	GroupName          string   `json:"group_name"`
	StatePath          string   `json:"state_path"`
	Notifier           string   `json:"notifier"`
	TelegramBotToken   string   `json:"telegram_bot_token"`
	TelegramChatID     string   `json:"telegram_chat_id"`
	SendTimeoutSeconds int      `json:"send_timeout_seconds"`
	SendRetries        int      `json:"send_retries"`
	SendDelaySeconds   int      `json:"send_delay_seconds"`
	MaxPages           int      `json:"max_pages"`
	HTTPTimeoutSeconds int      `json:"http_timeout_seconds"`
	UserAgent          string   `json:"user_agent"`
	TitleDenylist      []string `json:"title_denylist"`
	LocationDenylist   []string `json:"location_denylist"`
}

func Default() Config {
	return Config{
		SourceURL:          "https://plexusrs.com/jobs/",
		GroupName:          "Job alerts",
		StatePath:          "data/seen_jobs.json",
		Notifier:           "telegram_bot",
		SendTimeoutSeconds: 60,
		SendRetries:        5,
		SendDelaySeconds:   3,
		MaxPages:           8,
		HTTPTimeoutSeconds: 20,
		UserAgent:          "job-alert/0.1 (+local personal job monitor)",
	}
}

func Load(path string) (Config, error) {
	cfg := Default()
	baseDir, err := os.Getwd()
	if err != nil {
		return cfg, err
	}

	if path != "" {
		if abs, err := filepath.Abs(path); err == nil {
			baseDir = filepath.Dir(abs)
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			resolvePaths(&cfg, baseDir)
			return cfg, nil
		}
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}
	fillDefaults(&cfg)
	resolvePaths(&cfg, baseDir)
	return cfg, nil
}

func fillDefaults(cfg *Config) {
	def := Default()
	if cfg.SourceURL == "" {
		cfg.SourceURL = def.SourceURL
	}
	if cfg.GroupName == "" {
		cfg.GroupName = def.GroupName
	}
	if cfg.StatePath == "" {
		cfg.StatePath = def.StatePath
	}
	if cfg.Notifier == "" {
		cfg.Notifier = def.Notifier
	}
	if cfg.SendTimeoutSeconds <= 0 {
		cfg.SendTimeoutSeconds = def.SendTimeoutSeconds
	}
	if cfg.SendRetries <= 0 {
		cfg.SendRetries = def.SendRetries
	}
	if cfg.SendDelaySeconds <= 0 {
		cfg.SendDelaySeconds = def.SendDelaySeconds
	}
	if cfg.MaxPages <= 0 {
		cfg.MaxPages = def.MaxPages
	}
	if cfg.HTTPTimeoutSeconds <= 0 {
		cfg.HTTPTimeoutSeconds = def.HTTPTimeoutSeconds
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = def.UserAgent
	}
}

func resolvePaths(cfg *Config, baseDir string) {
	cfg.StatePath = resolvePath(cfg.StatePath, baseDir)
}

func resolvePath(path, baseDir string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(baseDir, path)
}
