// Role:    Registry auth commands plus local credential persistence for the pinix CLI
// Depends: bufio, encoding/json, fmt, io, os, path/filepath, strings, internal/client, internal/daemon, cobra
// Exports: (package-internal helpers)

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/epiral/pinix/internal/client"
	daemonpkg "github.com/epiral/pinix/internal/daemon"
	"github.com/spf13/cobra"
)

type registryCredential struct {
	Token    string `json:"token,omitempty"`
	Username string `json:"username,omitempty"`
}

type registryCredentialsFile struct {
	Registries map[string]registryCredential `json:"registries"`
}

type interactivePrompter struct {
	reader *bufio.Reader
	output io.Writer
}

func newRegisterCommand() *cobra.Command {
	var registryURL string

	cmd := &cobra.Command{
		Use:   "register",
		Short: "Register a Pinix Registry account",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			reg, err := requireRegistryClient(registryURL)
			if err != nil {
				return err
			}
			prompter := newInteractivePrompter(cmd.InOrStdin(), cmd.ErrOrStderr())
			username, err := prompter.readRequired("Username", true)
			if err != nil {
				return err
			}
			email, err := prompter.readRequired("Email", true)
			if err != nil {
				return err
			}
			password, err := prompter.readRequired("Password", false)
			if err != nil {
				return err
			}
			resp, err := reg.Register(cmd.Context(), username, email, password)
			if err != nil {
				return err
			}
			username = firstNonEmpty(resp.GetUsername(), username)
			if err := saveRegistryCredential(reg.BaseURL(), username, resp.Token); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "registered %s\n", username)
			return nil
		},
	}
	cmd.Flags().StringVar(&registryURL, "registry", "", "Pinix Registry base URL (default: from config or https://api.pinix.ai)")
	return cmd
}

func newLoginCommand() *cobra.Command {
	var registryURL string

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in to a Pinix Registry",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			reg, err := requireRegistryClient(registryURL)
			if err != nil {
				return err
			}
			prompter := newInteractivePrompter(cmd.InOrStdin(), cmd.ErrOrStderr())
			username, err := prompter.readRequired("Username", true)
			if err != nil {
				return err
			}
			password, err := prompter.readRequired("Password", false)
			if err != nil {
				return err
			}
			resp, err := reg.Login(cmd.Context(), username, password)
			if err != nil {
				return err
			}
			username = firstNonEmpty(resp.GetUsername(), username)
			if err := saveRegistryCredential(reg.BaseURL(), username, resp.Token); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "logged in as %s\n", username)
			return nil
		},
	}
	cmd.Flags().StringVar(&registryURL, "registry", "", "Pinix Registry base URL (default: from config or https://api.pinix.ai)")
	return cmd
}

func newLogoutCommand() *cobra.Command {
	var registryURL string

	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Log out from a Pinix Registry",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resolvedURL := getRegistryURL(registryURL)
			normalizedURL, err := normalizeRegistryCredentialRegistry(resolvedURL)
			if err != nil {
				return err
			}
			credentials, err := loadRegistryCredentialsFile()
			if err != nil {
				return err
			}
			delete(credentials.Registries, normalizedURL)
			if err := saveRegistryCredentialsFile(credentials); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "logged out from %s\n", normalizedURL)
			return nil
		},
	}
	cmd.Flags().StringVar(&registryURL, "registry", "", "Pinix Registry base URL (default: from config or https://api.pinix.ai)")
	return cmd
}

func newWhoAmICommand() *cobra.Command {
	var registryURL string

	cmd := &cobra.Command{
		Use:   "whoami",
		Short: "Show the current Pinix Registry user",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			reg, err := requireRegistryClient(registryURL)
			if err != nil {
				return err
			}
			token, err := loadRegistryToken(reg.BaseURL())
			if err != nil {
				return err
			}
			resp, err := reg.WhoAmI(cmd.Context(), token)
			if err != nil {
				return err
			}
			username := strings.TrimSpace(resp.GetUsername())
			if username == "" {
				return fmt.Errorf("registry whoami returned an empty username")
			}
			fmt.Fprintln(cmd.OutOrStdout(), username)
			return nil
		},
	}
	cmd.Flags().StringVar(&registryURL, "registry", "", "Pinix Registry base URL (default: from config or https://api.pinix.ai)")
	return cmd
}

func newInteractivePrompter(input io.Reader, output io.Writer) *interactivePrompter {
	return &interactivePrompter{reader: bufio.NewReader(input), output: output}
}

func (p *interactivePrompter) readRequired(label string, trimSpaces bool) (string, error) {
	if _, err := fmt.Fprintf(p.output, "%s: ", label); err != nil {
		return "", fmt.Errorf("write %s prompt: %w", strings.ToLower(label), err)
	}
	value, err := p.reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("read %s: %w", strings.ToLower(label), err)
	}
	value = strings.TrimRight(value, "\r\n")
	if trimSpaces {
		value = strings.TrimSpace(value)
	}
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s is required", strings.ToLower(label))
	}
	return value, nil
}

// requireRegistryClient resolves the registry URL using getRegistryURL
// (flag > env > config > default) and creates a client.
func requireRegistryClient(registryURL string) (*client.RegistryClient, error) {
	resolved := getRegistryURL(registryURL)
	return client.NewRegistry(resolved)
}

func saveRegistryCredential(registryURL, username, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("registry auth response did not include a token")
	}
	registryURL, err := normalizeRegistryCredentialRegistry(registryURL)
	if err != nil {
		return err
	}
	credentials, err := loadRegistryCredentialsFile()
	if err != nil {
		return err
	}
	credentials.Registries[registryURL] = registryCredential{
		Token:    token,
		Username: strings.TrimSpace(username),
	}
	if err := saveRegistryCredentialsFile(credentials); err != nil {
		return err
	}
	return nil
}

func loadRegistryToken(registryURL string) (string, error) {
	registryURL, err := normalizeRegistryCredentialRegistry(registryURL)
	if err != nil {
		return "", err
	}
	credentials, err := loadRegistryCredentialsFile()
	if err != nil {
		return "", err
	}
	entry, ok := credentials.Registries[registryURL]
	if !ok || strings.TrimSpace(entry.Token) == "" {
		return "", fmt.Errorf("registry credentials not found for %s; run \"pinix login --registry %s\"", registryURL, registryURL)
	}
	return strings.TrimSpace(entry.Token), nil
}

func normalizeRegistryCredentialRegistry(registryURL string) (string, error) {
	reg, err := client.NewRegistry(registryURL)
	if err != nil {
		return "", err
	}
	return reg.BaseURL(), nil
}

func registryCredentialsPath() (string, error) {
	root, err := daemonpkg.DefaultRootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "credentials.json"), nil
}

func loadRegistryCredentialsFile() (*registryCredentialsFile, error) {
	path, err := registryCredentialsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &registryCredentialsFile{Registries: map[string]registryCredential{}}, nil
		}
		return nil, fmt.Errorf("read registry credentials: %w", err)
	}
	var credentials registryCredentialsFile
	if err := json.Unmarshal(data, &credentials); err != nil {
		return nil, fmt.Errorf("parse registry credentials: %w", err)
	}
	if credentials.Registries == nil {
		credentials.Registries = make(map[string]registryCredential)
	}
	return &credentials, nil
}

func saveRegistryCredentialsFile(credentials *registryCredentialsFile) error {
	if credentials == nil {
		credentials = &registryCredentialsFile{}
	}
	if credentials.Registries == nil {
		credentials.Registries = make(map[string]registryCredential)
	}
	path, err := registryCredentialsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create registry credentials dir: %w", err)
	}
	data, err := json.MarshalIndent(credentials, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal registry credentials: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write registry credentials: %w", err)
	}
	return nil
}
