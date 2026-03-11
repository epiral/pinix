// Role:    Shared CLI helpers — admin client factory, config reader
// Depends: internal/config, pinixv1connect, connectrpc, net/http
// Exports: newAdminClient, newClipClient

package cmd

import (
	"fmt"
	"net/http"

	connect "connectrpc.com/connect"
	"github.com/epiral/pinix/gen/go/pinix/v1/pinixv1connect"
	"github.com/epiral/pinix/internal/config"
)

const defaultServerURL = "http://localhost:8080"

// bearerTransport injects an Authorization header into every request.
type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(req)
}

// newAdminClient creates an AdminService client.
// If serverURL and token are empty, reads from local config.
func newAdminClient(serverURL, token string) (pinixv1connect.AdminServiceClient, error) {
	if serverURL == "" {
		serverURL = defaultServerURL
	}
	if token == "" {
		t, err := loadSuperToken()
		if err != nil {
			return nil, err
		}
		token = t
	}
	httpClient := &http.Client{
		Transport: &bearerTransport{token: token, base: http.DefaultTransport},
	}
	return pinixv1connect.NewAdminServiceClient(httpClient, serverURL, connect.WithGRPC()), nil
}

// newClipClient creates a ClipService client with the given server URL and token.
func newClipClient(serverURL, token string) (pinixv1connect.ClipServiceClient, error) {
	if err := requiredServerToken(serverURL, token); err != nil {
		return nil, err
	}
	httpClient := &http.Client{
		Transport: &bearerTransport{token: token, base: http.DefaultTransport},
	}
	return pinixv1connect.NewClipServiceClient(httpClient, serverURL, connect.WithGRPC()), nil
}

func requiredServerToken(serverURL, token string) error {
	if serverURL == "" || token == "" {
		return fmt.Errorf("--server and --token are required")
	}
	return nil
}

func loadSuperToken() (string, error) {
	cfgPath, err := config.DefaultPath()
	if err != nil {
		return "", err
	}
	store, err := config.NewStore(cfgPath)
	if err != nil {
		return "", err
	}
	t := store.GetSuperToken()
	if t == "" {
		return "", fmt.Errorf("no super_token in %s", cfgPath)
	}
	return t, nil
}
