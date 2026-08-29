package jot

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

type Config struct {
	Vault  string `json:"vault"`
	Remote string `json:"remote,omitempty"`
}

func configPath() (string, error) {
	if p := os.Getenv("JOT_CONFIG"); p != "" {
		return p, nil
	}
	d, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "jot", "config.json"), nil
}

func defaultVaultPath() (string, error) {
	if p := os.Getenv("JOT_DIR"); p != "" {
		return filepath.Abs(p)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "jot", "vault"), nil
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "jot", "vault"), nil
	}
	return filepath.Join(home, ".local", "share", "jot", "vault"), nil
}

func loadConfig() (Config, error) {
	if p := os.Getenv("JOT_DIR"); p != "" {
		abs, err := filepath.Abs(p)
		return Config{Vault: abs}, err
	}
	p, err := configPath()
	if err != nil {
		return Config{}, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, codedf(ExitNotInited, "not initialized; run jot init OWNER/REPO or set JOT_DIR")
	}
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	if cfg.Vault == "" {
		return Config{}, codedf(ExitNotInited, "config has no vault path")
	}
	return cfg, nil
}

func saveConfig(cfg Config) error {
	p, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return atomicWrite(p, b, 0o600)
}
