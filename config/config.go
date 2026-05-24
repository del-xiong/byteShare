package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Auth     AuthConfig     `yaml:"auth"`
	Upload   UploadConfig   `yaml:"upload"`
	Database DatabaseConfig `yaml:"database"`
}

type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
	Mode string `yaml:"mode"`
}

type AuthConfig struct {
	Password string `yaml:"password"`
}

type UploadConfig struct {
	Dir           string `yaml:"dir"`
	MaxSize       int64  `yaml:"max_size"`
	DefaultExpiry int    `yaml:"default_expiry"`
	MaxExpiry     int    `yaml:"max_expiry"`
}

type DatabaseConfig struct {
	Path string `yaml:"path"`
}

var App *Config

func Load(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return err
	}
	setDefaults(cfg)
	App = cfg
	return nil
}

func setDefaults(cfg *Config) {
	if cfg.Server.Host == "" {
		cfg.Server.Host = "0.0.0.0"
	}
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	if cfg.Server.Mode == "" {
		cfg.Server.Mode = "web"
	}
	if cfg.Upload.Dir == "" {
		cfg.Upload.Dir = "./uploads"
	}
	if cfg.Upload.MaxSize == 0 {
		cfg.Upload.MaxSize = 500
	}
	if cfg.Upload.DefaultExpiry == 0 {
		cfg.Upload.DefaultExpiry = 3
	}
	if cfg.Upload.MaxExpiry == 0 {
		cfg.Upload.MaxExpiry = 30
	}
	if cfg.Database.Path == "" {
		cfg.Database.Path = "./fileshare.db"
	}
}
