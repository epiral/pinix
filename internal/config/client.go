// Role:    Shared client.json persistence for pinix CLI and pinixd
// Depends: encoding/json, fmt, os, path/filepath, strings
// Exports: ClientConfig, DefaultRegistryURL, DefaultClientConfigPath, ReadClientConfig, WriteClientConfig

package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const DefaultRegistryURL = "https://api.pinix.ai"

type ClientConfig struct {
	Registry string `json:"registry,omitempty"`
	Hub      string `json:"hub,omitempty"`
	HubToken string `json:"hub_token,omitempty"`
}

func DefaultClientConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	return filepath.Join(home, ".pinix", "client.json"), nil
}

func ReadClientConfig() (*ClientConfig, error) {
	path, err := DefaultClientConfigPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &ClientConfig{}, nil
		}
		return nil, fmt.Errorf("read client config: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return &ClientConfig{}, nil
	}

	var cfg ClientConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse client config: %w", err)
	}
	return &cfg, nil
}

func WriteClientConfig(cfg *ClientConfig) error {
	if cfg == nil {
		cfg = &ClientConfig{}
	}

	path, err := DefaultClientConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create client config dir: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal client config: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write client config: %w", err)
	}
	return nil
}
