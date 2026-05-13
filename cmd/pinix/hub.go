// Role:    Hub subcommand group — manage Clips through a Pinix Hub
// Depends: fmt, os, strings, internal/client, internal/config, cobra
// Exports: newHubCommand, defaultHubURL, defaultHubToken

package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/epiral/pinix/internal/client"
	configpkg "github.com/epiral/pinix/internal/config"
	"github.com/epiral/pinix/internal/pidfile"
	"github.com/spf13/cobra"
)

// defaultHubURL resolves the Hub URL from: client.json > pidfile > fallback (localhost:9000).
func defaultHubURL() string {
	cfg, err := configpkg.ReadClientConfig()
	if err == nil && strings.TrimSpace(cfg.Hub) != "" {
		return strings.TrimSpace(cfg.Hub)
	}
	if pf, err := pidfile.ReadPIDFile(); err == nil && pf != nil {
		if strings.TrimSpace(pf.Hub) != "" {
			return strings.TrimSpace(pf.Hub)
		}
		return fmt.Sprintf("http://127.0.0.1:%d", pf.Port)
	}
	return client.FallbackServerURL
}

// defaultHubToken resolves the Hub token from: env > client.json.
func defaultHubToken() string {
	if v := strings.TrimSpace(os.Getenv("PINIX_TOKEN")); v != "" {
		return v
	}
	cfg, err := configpkg.ReadClientConfig()
	if err == nil && strings.TrimSpace(cfg.HubToken) != "" {
		return strings.TrimSpace(cfg.HubToken)
	}
	return ""
}

func newHubCommand() *cobra.Command {
	var serverURL string
	var hubToken string

	cmd := &cobra.Command{
		Use:   "hub",
		Short: "Manage Clips through a Pinix Hub",
	}
	cmd.PersistentFlags().StringVar(&serverURL, "server", defaultHubURL(), "Hub URL (default: auto-discover)")
	cmd.PersistentFlags().StringVar(&hubToken, "auth-token", defaultHubToken(), "Hub auth token")

	cmd.AddCommand(newHubLoginCommand(&serverURL))
	cmd.AddCommand(newHubLogoutCommand())
	cmd.AddCommand(newHubWhoAmICommand())
	cmd.AddCommand(newListCommand(&serverURL, &hubToken))
	cmd.AddCommand(newAddCommand(&serverURL, &hubToken))
	cmd.AddCommand(newRemoveCommand(&serverURL, &hubToken))
	cmd.AddCommand(newInfoCommand(&serverURL, &hubToken))
	cmd.AddCommand(newUpdateCommand(&serverURL, &hubToken))
	cmd.AddCommand(newBindCommand(&serverURL, &hubToken))
	cmd.AddCommand(newUnbindCommand(&serverURL, &hubToken))
	cmd.AddCommand(newBindingsCommand(&serverURL, &hubToken))
	cmd.AddCommand(newLogsCommand())
	cmd.AddCommand(newMCPCommand(&serverURL, &hubToken))

	return cmd
}

func newHubLoginCommand(serverURL *string) *cobra.Command {
	var registryURL string
	var username string
	var password string

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in to a Pinix Hub",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			reg, err := requireRegistryClient(registryURL)
			if err != nil {
				return err
			}

			switch {
			case username != "" && password != "":
				// non-interactive: both flags provided
			case username == "" && password == "":
				if !isStdinTerminal() {
					return fmt.Errorf("non-interactive mode requires --username and --password flags")
				}
				prompter := newInteractivePrompter(cmd.InOrStdin(), cmd.ErrOrStderr())
				username, err = prompter.readRequired("Username", true)
				if err != nil {
					return err
				}
				password, err = prompter.readRequired("Password", false)
				if err != nil {
					return err
				}
			default:
				return fmt.Errorf("both --username and --password must be provided together")
			}

			resp, err := reg.Login(cmd.Context(), username, password)
			if err != nil {
				return err
			}
			token := strings.TrimSpace(resp.Token)
			if token == "" {
				return fmt.Errorf("login response did not include a token")
			}
			cfg, err := configpkg.ReadClientConfig()
			if err != nil {
				return err
			}
			cfg.HubToken = token
			if v := strings.TrimSpace(*serverURL); v != "" && v != client.FallbackServerURL {
				cfg.Hub = v
			}
			if err := configpkg.WriteClientConfig(cfg); err != nil {
				return err
			}
			hubDisplay := cfg.Hub
			if hubDisplay == "" {
				hubDisplay = "default"
			}
			username = firstNonEmpty(resp.GetUsername(), username)
			fmt.Fprintf(cmd.OutOrStdout(), "logged in as %s (hub: %s)\n", username, hubDisplay)
			return nil
		},
	}
	cmd.Flags().StringVar(&username, "username", "", "Username (for non-interactive login)")
	cmd.Flags().StringVar(&password, "password", "", "Password (for non-interactive login)")
	cmd.Flags().StringVar(&registryURL, "registry", "", "Registry URL for authentication (default: from config or https://api.pinix.ai)")
	return cmd
}

func isStdinTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func newHubLogoutCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Log out from a Pinix Hub",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := configpkg.ReadClientConfig()
			if err != nil {
				return err
			}
			cfg.HubToken = ""
			if err := configpkg.WriteClientConfig(cfg); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "logged out")
			return nil
		},
	}
}

func newHubWhoAmICommand() *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show the current Hub user and token status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := configpkg.ReadClientConfig()
			if err != nil {
				return err
			}
			token := strings.TrimSpace(cfg.HubToken)
			if token == "" {
				return fmt.Errorf("not logged in; run \"pinix hub login\"")
			}

			if exp, err := parseJWTExpiry(token); err == nil {
				remaining := time.Until(exp)
				if remaining <= 0 {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: token expired %s ago\n", (-remaining).Truncate(time.Minute))
				} else if remaining < 7*24*time.Hour {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: token expires in %s\n", remaining.Truncate(time.Minute))
				}
			}

			reg, err := requireRegistryClient("")
			if err != nil {
				return err
			}
			resp, err := reg.WhoAmI(cmd.Context(), token)
			if err != nil {
				return wrapAuthError(err)
			}
			username := strings.TrimSpace(resp.GetUsername())
			if username == "" {
				return fmt.Errorf("whoami returned an empty username")
			}
			fmt.Fprintln(cmd.OutOrStdout(), username)
			return nil
		},
	}
}

func parseJWTExpiry(token string) (time.Time, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, fmt.Errorf("not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, err
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return time.Time{}, err
	}
	if claims.Exp == 0 {
		return time.Time{}, fmt.Errorf("no exp claim")
	}
	return time.Unix(claims.Exp, 0), nil
}

func wrapAuthError(err error) error {
	if err == nil {
		return nil
	}
	if client.IsAuthError(err) {
		return fmt.Errorf("%w\n\nHint: your token may be expired or invalid. Run \"pinix hub login\" to refresh it.", err)
	}
	return err
}
