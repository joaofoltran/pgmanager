package appconfig

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type ServerConfig struct {
	Listen string `toml:"listen"`
	Port   int    `toml:"port"`
}

type DatabaseConfig struct {
	URL string `toml:"url"`
}

type LoggingConfig struct {
	Level  string `toml:"level"`
	Format string `toml:"format"`
}

type SecurityConfig struct {
	EncryptionKey string `toml:"encryption_key"`
}

type Config struct {
	Server   ServerConfig   `toml:"server"`
	Database DatabaseConfig `toml:"database"`
	Logging  LoggingConfig  `toml:"logging"`
	Security SecurityConfig `toml:"security"`
}

func Defaults() Config {
	return Config{
		Server: ServerConfig{
			Listen: "127.0.0.1",
			Port:   7654,
		},
		Database: DatabaseConfig{
			URL: "postgres://localhost:5432/pgmanager?sslmode=disable",
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "console",
		},
	}
}

func Load(path string) (Config, error) {
	cfg := Defaults()

	if path == "" {
		path = findConfigFile()
	}

	if path != "" {
		if _, err := toml.DecodeFile(path, &cfg); err != nil {
			return cfg, fmt.Errorf("parse config %s: %w", path, err)
		}
	}

	applyEnv(&cfg)
	return cfg, nil
}

func findConfigFile() string {
	candidates := []string{}

	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".pgmanager", "config.toml"))
	}
	candidates = append(candidates, "/etc/pgmanager/config.toml")

	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func applyEnv(cfg *Config) {
	if v := os.Getenv("PGMANAGER_LISTEN"); v != "" {
		cfg.Server.Listen = v
	}
	if v := os.Getenv("PGMANAGER_PORT"); v != "" {
		var port int
		if _, err := fmt.Sscanf(v, "%d", &port); err == nil {
			cfg.Server.Port = port
		}
	}
	if v := os.Getenv("PGMANAGER_DB_URL"); v != "" {
		cfg.Database.URL = v
	}
	if v := os.Getenv("PGMANAGER_LOG_LEVEL"); v != "" {
		cfg.Logging.Level = v
	}
	if v := os.Getenv("PGMANAGER_LOG_FORMAT"); v != "" {
		cfg.Logging.Format = v
	}
	if v := os.Getenv("PGMANAGER_ENCRYPTION_KEY"); v != "" {
		cfg.Security.EncryptionKey = v
	}
}
