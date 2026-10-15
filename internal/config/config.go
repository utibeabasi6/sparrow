package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const DefaultFile = "sparrow.json"

type Config struct {
	Kernel string            `json:"kernel"`
	Rootfs map[string]string `json:"rootfs"`
}

func Load(path string) (Config, error) {
	var cfg Config
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parsing %s: %w", path, err)
	}
	if len(cfg.Rootfs) == 0 {
		return cfg, fmt.Errorf("%s: no rootfs configured", path)
	}

	if cfg.Kernel, err = checkFile(cfg.Kernel); err != nil {
		return cfg, err
	}
	for name, p := range cfg.Rootfs {
		if cfg.Rootfs[name], err = checkFile(p); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}

func checkFile(path string) (string, error) {
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	return filepath.Abs(path)
}
