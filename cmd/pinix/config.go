// Role:    CLI config command and client config persistence for registry URL resolution
// Depends: fmt, os, strings, internal/config, cobra
// Exports: newConfigCommand, getRegistryURL

package main

import (
	"fmt"
	"os"
	"strings"

	configpkg "github.com/epiral/pinix/internal/config"
	"github.com/spf13/cobra"
)

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

			cfg, err := configpkg.ReadClientConfig()
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

			if err := configpkg.WriteClientConfig(cfg); err != nil {
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

			cfg, err := configpkg.ReadClientConfig()
			if err != nil {
				return err
			}

			switch key {
			case "registry":
				value := cfg.Registry
				if value == "" {
					value = configpkg.DefaultRegistryURL
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

// getRegistryURL resolves the registry URL from: flag > env > client.json > default.
func getRegistryURL(flagValue string) string {
	if v := strings.TrimSpace(flagValue); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("PINIX_REGISTRY")); v != "" {
		return v
	}
	cfg, err := configpkg.ReadClientConfig()
	if err == nil && strings.TrimSpace(cfg.Registry) != "" {
		return strings.TrimSpace(cfg.Registry)
	}
	return configpkg.DefaultRegistryURL
}
