package config

import (
	"errors"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
)

type Config struct {
	ServerURL      string   `yaml:"server_url"`
	ConnectorID    string   `yaml:"connector_id"`
	ClientKey      string   `yaml:"client_key"`
	BridgeEndpoint string   `yaml:"bridge_endpoint"`
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
	if cfg.ServerURL == "" || cfg.ConnectorID == "" || cfg.ClientKey == "" {
		return Config{}, errors.New("server_url, connector_id and client_key are required")
	}
	if cfg.BridgeEndpoint != "" {
		cfg.BridgeEndpoint, err = filepath.Abs(cfg.BridgeEndpoint)
		if err != nil {
			return Config{}, err
		}
	}
	return cfg, nil
}
