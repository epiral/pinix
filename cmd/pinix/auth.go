// Role:    Top-level login/logout/whoami commands with device code flow
// Depends: context, encoding/json, fmt, net/http, os, os/exec, runtime, strings, time, internal/config, cobra
// Exports: newAuthLoginCommand, newAuthLogoutCommand, newAuthWhoAmICommand

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	configpkg "github.com/epiral/pinix/internal/config"
	"github.com/spf13/cobra"
)

// deviceCodeResponse is the response from POST /auth/device/code.
type deviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURL string `json:"verification_url"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// devicePollResponse is the response from POST /auth/device/poll.
type devicePollResponse struct {
	Status   string            `json:"status"` // "pending", "complete", "expired"
	Token    string            `json:"token,omitempty"`
	User     *devicePollUser   `json:"user,omitempty"`
	Username string            `json:"username,omitempty"`
	Scope    string            `json:"scope,omitempty"`
	Hub      string            `json:"hub,omitempty"`
	Error    string            `json:"error,omitempty"`
}

type devicePollUser struct {
	Username string `json:"username,omitempty"`
	Scope    string `json:"scope,omitempty"`
}

func newAuthLoginCommand() *cobra.Command {
	var token string
	var server string

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in to Pinix",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			serverURL := resolveAuthServer(server)

			if strings.TrimSpace(token) != "" {
				return loginWithToken(cmd, serverURL, strings.TrimSpace(token))
			}
			return loginWithDeviceCode(cmd, serverURL)
		},
	}
	cmd.Flags().StringVar(&token, "token", "", "authenticate with an existing token (skips device code flow)")
	cmd.Flags().StringVar(&server, "server", "", "auth server URL (default: https://api.pinixai.com)")
	return cmd
}

func newAuthLogoutCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Log out from Pinix",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := configpkg.ReadClientConfig()
			if err != nil {
				return err
			}
			cfg.HubToken = ""
			cfg.User = nil
			if err := configpkg.WriteClientConfig(cfg); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Logged out")
			return nil
		},
	}
}

func newAuthWhoAmICommand() *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show the current Pinix user",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := configpkg.ReadClientConfig()
			if err != nil {
				return err
			}
			if cfg.User == nil || strings.TrimSpace(cfg.User.Username) == "" {
				fmt.Fprintln(cmd.OutOrStdout(), "Not logged in")
				os.Exit(1)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "@%s\n", cfg.User.Username)
			return nil
		},
	}
}

// resolveAuthServer returns the auth server URL from: flag > env > config > default.
func resolveAuthServer(flagValue string) string {
	if v := strings.TrimSpace(flagValue); v != "" {
		return strings.TrimRight(v, "/")
	}
	if v := strings.TrimSpace(os.Getenv("PINIX_AUTH_SERVER")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return configpkg.DefaultAuthServerURL
}

// loginWithToken writes the given token directly to config (manual/fallback mode).
func loginWithToken(cmd *cobra.Command, serverURL, token string) error {
	cfg, err := configpkg.ReadClientConfig()
	if err != nil {
		return err
	}
	cfg.HubToken = token
	// Try to resolve user info via whoami
	user, hub, _ := authWhoAmI(cmd.Context(), serverURL, token)
	if user != nil {
		cfg.User = &configpkg.UserInfo{
			Username: user.Username,
			Scope:    user.Scope,
		}
	}
	if hub != "" {
		cfg.Hub = hub
	}
	if cfg.Hub == "" {
		cfg.Hub = hubURLFromAuthServer(serverURL)
	}
	if err := configpkg.WriteClientConfig(cfg); err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if cfg.User != nil && cfg.User.Username != "" {
		fmt.Fprintf(out, "Logged in as @%s\n", cfg.User.Username)
	} else {
		fmt.Fprintln(out, "Logged in")
	}
	if hubDisplay := cfg.Hub; hubDisplay != "" {
		fmt.Fprintf(out, "  Hub: %s\n", hubDisplay)
	}
	return nil
}

// loginWithDeviceCode performs the device code OAuth flow.
func loginWithDeviceCode(cmd *cobra.Command, serverURL string) error {
	ctx := cmd.Context()

	// Step 1: Request device code
	dcResp, err := requestDeviceCode(ctx, serverURL)
	if err != nil {
		return fmt.Errorf("request device code: %w", err)
	}

	// Step 2: Print verification URL and try to open browser
	verifyURL := dcResp.VerificationURL
	if !strings.Contains(verifyURL, "code=") {
		sep := "?"
		if strings.Contains(verifyURL, "?") {
			sep = "&"
		}
		verifyURL = verifyURL + sep + "code=" + dcResp.UserCode
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "\nVisit: %s\n", verifyURL)
	fmt.Fprintf(out, "Waiting for login...\n")

	_ = openBrowser(verifyURL)

	// Step 3: Poll for completion
	interval := dcResp.Interval
	if interval < 1 {
		interval = 5
	}
	expiresIn := dcResp.ExpiresIn
	if expiresIn < 1 {
		expiresIn = 300
	}
	deadline := time.Now().Add(time.Duration(expiresIn) * time.Second)

	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("login timed out; please try again")
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(interval) * time.Second):
		}

		pollResp, err := pollDeviceCode(ctx, serverURL, dcResp.DeviceCode)
		if err != nil {
			return fmt.Errorf("poll device code: %w", err)
		}

		switch pollResp.Status {
		case "complete":
			return saveLoginResult(cmd, serverURL, pollResp)
		case "expired":
			return fmt.Errorf("login expired; please try again")
		case "pending", "authorization_pending":
			continue
		default:
			if pollResp.Error != "" {
				return fmt.Errorf("login failed: %s", pollResp.Error)
			}
			continue
		}
	}
}

// saveLoginResult writes the successful login response to config.
func saveLoginResult(cmd *cobra.Command, serverURL string, resp *devicePollResponse) error {
	cfg, err := configpkg.ReadClientConfig()
	if err != nil {
		return err
	}
	cfg.HubToken = strings.TrimSpace(resp.Token)

	username := strings.TrimSpace(resp.Username)
	scope := strings.TrimSpace(resp.Scope)
	if resp.User != nil {
		if username == "" {
			username = strings.TrimSpace(resp.User.Username)
		}
		if scope == "" {
			scope = strings.TrimSpace(resp.User.Scope)
		}
	}
	if username != "" {
		cfg.User = &configpkg.UserInfo{
			Username: username,
			Scope:    scope,
		}
	}
	if hub := strings.TrimSpace(resp.Hub); hub != "" {
		cfg.Hub = hub
	}
	if cfg.Hub == "" {
		cfg.Hub = hubURLFromAuthServer(serverURL)
	}
	if err := configpkg.WriteClientConfig(cfg); err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	fmt.Fprintln(out)
	if username != "" {
		fmt.Fprintf(out, "Logged in as @%s\n", username)
	} else {
		fmt.Fprintln(out, "Logged in")
	}
	if hubDisplay := cfg.Hub; hubDisplay != "" {
		fmt.Fprintf(out, "  Hub: %s\n", hubDisplay)
	}
	return nil
}

// requestDeviceCode calls POST /auth/device/code.
func requestDeviceCode(ctx context.Context, serverURL string) (*deviceCodeResponse, error) {
	body, _ := json.Marshal(map[string]string{"client_id": "pinix-cli"})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, serverURL+"/auth/device/code", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, decodeAuthError(resp, "request device code")
	}

	var result deviceCodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode device code response: %w", err)
	}
	return &result, nil
}

// pollDeviceCode calls POST /auth/device/poll.
func pollDeviceCode(ctx context.Context, serverURL, deviceCode string) (*devicePollResponse, error) {
	body, _ := json.Marshal(map[string]string{"device_code": deviceCode})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, serverURL+"/auth/device/poll", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, decodeAuthError(resp, "poll device code")
	}

	var result devicePollResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode poll response: %w", err)
	}
	return &result, nil
}

// authWhoAmI calls GET /auth/whoami to resolve user info from a token.
func authWhoAmI(ctx context.Context, serverURL, token string) (*configpkg.UserInfo, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, serverURL+"/auth/whoami", nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("whoami: HTTP %d", resp.StatusCode)
	}

	var result struct {
		Username string `json:"username"`
		Scope    string `json:"scope"`
		Hub      string `json:"hub"`
		User     *struct {
			Username string `json:"username"`
		} `json:"user"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, "", err
	}
	username := result.Username
	if username == "" && result.User != nil {
		username = result.User.Username
	}
	if username == "" {
		return nil, result.Hub, nil
	}
	return &configpkg.UserInfo{
		Username: username,
		Scope:    result.Scope,
	}, result.Hub, nil
}

// decodeAuthError reads an error response from the auth server.
func decodeAuthError(resp *http.Response, action string) error {
	var body struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err == nil && body.Error != "" {
		return fmt.Errorf("%s: %s", action, body.Error)
	}
	return fmt.Errorf("%s: HTTP %d", action, resp.StatusCode)
}

// hubURLFromAuthServer derives the Hub URL from the auth server URL.
// e.g. "https://api.pinixai.com" → "https://hub.pinixai.com"
//
//	"https://api.pinix.ai" → "https://hub.pinix.ai"
func hubURLFromAuthServer(serverURL string) string {
	serverURL = strings.TrimRight(serverURL, "/")
	if strings.Contains(serverURL, "api.pinixai.com") {
		return "https://hub.pinixai.com"
	}
	if strings.Contains(serverURL, "api.pinix.ai") {
		return "https://hub.pinix.ai"
	}
	// Generic: replace "api." with "hub."
	return strings.Replace(serverURL, "://api.", "://hub.", 1)
}

// openBrowser tries to open a URL in the default browser.
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		return fmt.Errorf("unsupported platform")
	}
	return cmd.Start()
}
