package config

import (
	"errors"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	ListenHost     string   `yaml:"host"`
	ListenPort     int      `yaml:"port"`
	StorageRoot    string   `yaml:"storage_root"`
	ClientKey      string   `yaml:"client_key"`
	AdvertiseURL   string   `yaml:"advertise_url"`
	WorkspaceRoots []string `yaml:"workspace_roots"`
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	if cfg.ListenHost == "" {
		cfg.ListenHost = "0.0.0.0"
	}
	if cfg.ListenPort == 0 {
		cfg.ListenPort = 8080
	}
	if cfg.StorageRoot == "" {
		cfg.StorageRoot = "./var/test-data"
	}
	if cfg.ClientKey == "" {
		return Config{}, errors.New("client_key is required")
	}
	cfg.StorageRoot, err = filepath.Abs(cfg.StorageRoot)
	return cfg, err
}
