// Role:    CLI config command and client config persistence for registry URL resolution
// Depends: encoding/json, fmt, os, path/filepath, strings, internal/daemon, cobra
// Exports: newConfigCommand, loadClientConfig, saveClientConfig, getRegistryURL

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	daemonpkg "github.com/epiral/pinix/internal/daemon"
	"github.com/spf13/cobra"
)

const defaultRegistryURL = "https://api.pinix.ai"

type clientConfig struct {
	Registry string `json:"registry,omitempty"`
	Hub      string `json:"hub,omitempty"`
	HubToken string `json:"hub_token,omitempty"`
}

func newConfigCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage pinix CLI configuration",
	}
	cmd.AddCommand(newConfigSetCommand())
	cmd.AddCommand(newConfigGetCommand())
	return cmd
}

func newConfigSetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a config value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := strings.TrimSpace(args[0])
			value := strings.TrimSpace(args[1])

			cfg, err := loadClientConfig()
			if err != nil {
				return err
			}

			switch key {
			case "registry":
				cfg.Registry = value
			case "hub":
				cfg.Hub = value
			case "hub-token":
				cfg.HubToken = value
			default:
				return fmt.Errorf("unknown config key %q; supported keys: registry, hub, hub-token", key)
			}

			if err := saveClientConfig(cfg); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s = %s\n", key, value)
			return nil
		},
	}
}

func newConfigGetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Get a config value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := strings.TrimSpace(args[0])

			cfg, err := loadClientConfig()
			if err != nil {
				return err
			}

			switch key {
			case "registry":
				value := cfg.Registry
				if value == "" {
					value = defaultRegistryURL
				}
				fmt.Fprintln(cmd.OutOrStdout(), value)
			case "hub":
				fmt.Fprintln(cmd.OutOrStdout(), cfg.Hub)
			case "hub-token":
				fmt.Fprintln(cmd.OutOrStdout(), cfg.HubToken)
			default:
				return fmt.Errorf("unknown config key %q; supported keys: registry, hub, hub-token", key)
			}
			return nil
		},
	}
}

func clientConfigPath() (string, error) {
	root, err := daemonpkg.DefaultRootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "client.json"), nil
}

func loadClientConfig() (*clientConfig, error) {
	path, err := clientConfigPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &clientConfig{}, nil
		}
		return nil, fmt.Errorf("read client config: %w", err)
	}
	var cfg clientConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse client config: %w", err)
	}
	return &cfg, nil
}

func saveClientConfig(cfg *clientConfig) error {
	if cfg == nil {
		cfg = &clientConfig{}
	}
	path, err := clientConfigPath()
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

// getRegistryURL resolves the registry URL from: flag > env > client.json > default.
func getRegistryURL(flagValue string) string {
	if v := strings.TrimSpace(flagValue); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("PINIX_REGISTRY")); v != "" {
		return v
	}
	cfg, err := loadClientConfig()
	if err == nil && strings.TrimSpace(cfg.Registry) != "" {
		return strings.TrimSpace(cfg.Registry)
	}
	return defaultRegistryURL
}
