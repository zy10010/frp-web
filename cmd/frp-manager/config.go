package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	toml "github.com/pelletier/go-toml/v2"
)

// WebConfig is the configuration of the manager's own web interface.
type WebConfig struct {
	Addr     string `toml:"addr"`
	Port     int    `toml:"port"`
	User     string `toml:"user"`
	Password string `toml:"password"`
}

// ManagerConfig is the persisted configuration of frp-manager.
type ManagerConfig struct {
	Web WebConfig `toml:"web"`

	FrpsPath   string `toml:"frpsPath"`
	FrpcPath   string `toml:"frpcPath"`
	FrpsConfig string `toml:"frpsConfig"`
	FrpcConfig string `toml:"frpcConfig"`
	WorkDir    string `toml:"workDir"`

	// Mirror is an optional URL prefix used to accelerate downloads from
	// GitHub, e.g. "https://ghproxy.com/". Empty means no mirror is used.
	Mirror string `toml:"mirror"`

	// AutostartFrps / AutostartFrpc control whether the corresponding process
	// is started automatically when the manager boots.
	AutostartFrps bool `toml:"autostartFrps"`
	AutostartFrpc bool `toml:"autostartFrpc"`

	// LogMaxBytes is the maximum size of the in-memory log ring buffer per process.
	LogMaxBytes int64 `toml:"logMaxBytes"`
}

func defaultManagerConfig() ManagerConfig {
	return ManagerConfig{
		Web: WebConfig{
			Addr:     "0.0.0.0",
			Port:     7500,
			User:     "admin",
			Password: randomToken(12),
		},
		FrpsPath:    "./frps",
		FrpcPath:    "./frpc",
		FrpsConfig:  "./frps.toml",
		FrpcConfig:  "./frpc.toml",
		WorkDir:     ".",
		LogMaxBytes: 256 * 1024,
	}
}

func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// Fallback to a deterministic value; should never happen in practice.
		return "admin"
	}
	return hex.EncodeToString(b)[:n]
}

// loadManagerConfig loads the manager config from path, creating it with
// defaults if it does not exist yet.
func loadManagerConfig(path string) (*ManagerConfig, error) {
	cfg := defaultManagerConfig()

	data, err := os.ReadFile(path)
	if err == nil {
		if err := toml.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
	} else if os.IsNotExist(err) {
		if err := saveManagerConfig(path, &cfg); err != nil {
			return nil, err
		}
	} else {
		return nil, err
	}

	// Resolve relative paths against the directory of the config file so the
	// manager works regardless of the process working directory.
	base := filepath.Dir(path)
	cfg.FrpsPath = resolvePath(base, cfg.FrpsPath)
	cfg.FrpcPath = resolvePath(base, cfg.FrpcPath)
	cfg.FrpsConfig = resolvePath(base, cfg.FrpsConfig)
	cfg.FrpcConfig = resolvePath(base, cfg.FrpcConfig)
	cfg.WorkDir = resolvePath(base, cfg.WorkDir)
	return &cfg, nil
}

func saveManagerConfig(path string, cfg *ManagerConfig) error {
	data, err := toml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func resolvePath(base, p string) string {
	if p == "" {
		return p
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	abs, err := filepath.Abs(filepath.Join(base, p))
	if err != nil {
		return filepath.Clean(filepath.Join(base, p))
	}
	return filepath.Clean(abs)
}
