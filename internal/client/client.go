// Role:    Connect-RPC HubService client used by pinix CLI and mock providers
// Depends: context, crypto/tls, encoding/json, errors, fmt, io, net, net/http, strings, connectrpc, pinix v2, pinixv2connect, http2, pidfile
// Exports: Client, HubError, FallbackServerURL, DefaultServerURL, New

package client

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"

	connect "connectrpc.com/connect"
	pinixv2 "github.com/epiral/pinix/gen/go/pinix/v2"
	"github.com/epiral/pinix/gen/go/pinix/v2/pinixv2connect"
	"github.com/epiral/pinix/internal/pidfile"
	"golang.org/x/net/http2"
)

const FallbackServerURL = "http://127.0.0.1:9000"

// DefaultServerURL returns the Hub URL, auto-discovering from PID file if available.
func DefaultServerURL() string {
	pf, err := pidfile.ReadPIDFile()
	if err == nil && pf != nil {
		return pf.HubURL
	}
	return FallbackServerURL
}

type Client struct {
	baseURL string
	hub     pinixv2connect.HubServiceClient
}

type HubError struct {
	Code    string
	Message string
}

func (e *HubError) Error() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.Code) == "" {
		return e.Message
	}
	return fmt.Sprintf("%s (%s)", e.Message, e.Code)
}

func New(serverURL string) (*Client, error) {
	serverURL = strings.TrimSpace(serverURL)
	if serverURL == "" {
		serverURL = DefaultServerURL()
	}

	transport := &http2.Transport{
		AllowHTTP: true,
		DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, network, addr)
		},
	}
	httpClient := &http.Client{Transport: transport}

	return &Client{
		baseURL: strings.TrimRight(serverURL, "/"),
		hub:     pinixv2connect.NewHubServiceClient(httpClient, serverURL, connect.WithGRPC()),
	}, nil
}

func (c *Client) BaseURL() string {
	if c == nil {
		return ""
	}
	return c.baseURL
}

func (c *Client) ProviderStream(ctx context.Context, hubToken string) *connect.BidiStreamForClient[pinixv2.ProviderMessage, pinixv2.HubMessage] {
	stream := c.hub.ProviderStream(ctx)
	setAuthHeader(stream.RequestHeader(), hubToken)
	return stream
}

func (c *Client) RuntimeStream(ctx context.Context, hubToken string) *connect.BidiStreamForClient[pinixv2.RuntimeMessage, pinixv2.HubRuntimeMessage] {
	stream := c.hub.RuntimeStream(ctx)
	setAuthHeader(stream.RequestHeader(), hubToken)
	return stream
}

func (c *Client) ListClips(ctx context.Context, hubToken string) ([]*pinixv2.ClipInfo, error) {
	req := connect.NewRequest(&pinixv2.ListClipsRequest{})
	setAuthHeader(req.Header(), hubToken)
	resp, err := c.hub.ListClips(ctx, req)
	if err != nil {
		return nil, err
	}
	return resp.Msg.GetClips(), nil
}

func (c *Client) ListProviders(ctx context.Context, hubToken string) ([]*pinixv2.ProviderInfo, error) {
	req := connect.NewRequest(&pinixv2.ListProvidersRequest{})
	setAuthHeader(req.Header(), hubToken)
	resp, err := c.hub.ListProviders(ctx, req)
	if err != nil {
		return nil, err
	}
	return resp.Msg.GetProviders(), nil
}

func (c *Client) GetManifest(ctx context.Context, clipName, hubToken string) (*pinixv2.ClipManifest, error) {
	req := connect.NewRequest(&pinixv2.GetManifestRequest{ClipName: strings.TrimSpace(clipName)})
	setAuthHeader(req.Header(), hubToken)
	resp, err := c.hub.GetManifest(ctx, req)
	if err != nil {
		return nil, err
	}
	return resp.Msg.GetManifest(), nil
}

func (c *Client) Add(ctx context.Context, source, requestedAlias, provider, clipToken, hubToken string) (*pinixv2.ClipInfo, error) {
	requestedAlias = strings.TrimSpace(requestedAlias)
	req := connect.NewRequest(&pinixv2.AddClipRequest{
		Source:         strings.TrimSpace(source),
		Name:           requestedAlias,
		Provider:       strings.TrimSpace(provider),
		ClipToken:      strings.TrimSpace(clipToken),
		RequestedAlias: requestedAlias,
	})
	setAuthHeader(req.Header(), hubToken)
	resp, err := c.hub.AddClip(ctx, req)
	if err != nil {
		return nil, err
	}
	return resp.Msg.GetClip(), nil
}

func (c *Client) Remove(ctx context.Context, clipName, hubToken string) (string, error) {
	req := connect.NewRequest(&pinixv2.RemoveClipRequest{ClipName: strings.TrimSpace(clipName)})
	setAuthHeader(req.Header(), hubToken)
	resp, err := c.hub.RemoveClip(ctx, req)
	if err != nil {
		return "", err
	}
	return resp.Msg.GetClipName(), nil
}

func (c *Client) Invoke(ctx context.Context, clipName, command string, input json.RawMessage, clipToken, hubToken string) (json.RawMessage, error) {
	if len(input) == 0 {
		input = json.RawMessage(`{}`)
	}

	req := connect.NewRequest(&pinixv2.InvokeRequest{
		ClipName:  strings.TrimSpace(clipName),
		Command:   strings.TrimSpace(command),
		Input:     cloneBytes(input),
		ClipToken: strings.TrimSpace(clipToken),
	})
	setAuthHeader(req.Header(), hubToken)

	stream, err := c.hub.Invoke(ctx, req)
	if err != nil {
		return nil, err
	}
	defer stream.Close()

	chunks := make([][]byte, 0, 4)
	for stream.Receive() {
		msg := stream.Msg()
		if msg.GetError() != nil {
			return nil, protoHubError(msg.GetError())
		}
		if len(msg.GetOutput()) > 0 {
			chunks = append(chunks, cloneBytes(msg.GetOutput()))
		}
	}
	if err := stream.Err(); err != nil {
		if errors.Is(err, io.EOF) {
			return aggregateOutputs(chunks), nil
		}
		return nil, err
	}
	return aggregateOutputs(chunks), nil
}

func (c *Client) OpenInvoke(ctx context.Context, clipName, command string, input json.RawMessage, clipToken, hubToken string) (*connect.ServerStreamForClient[pinixv2.InvokeResponse], error) {
	if len(input) == 0 {
		input = json.RawMessage(`{}`)
	}

	req := connect.NewRequest(&pinixv2.InvokeRequest{
		ClipName:  strings.TrimSpace(clipName),
		Command:   strings.TrimSpace(command),
		Input:     cloneBytes(input),
		ClipToken: strings.TrimSpace(clipToken),
	})
	setAuthHeader(req.Header(), hubToken)
	return c.hub.Invoke(ctx, req)
}

func setAuthHeader(header http.Header, token string) {
	token = strings.TrimSpace(token)
	if token == "" {
		return
	}
	header.Set("Authorization", "Bearer "+token)
}

func protoHubError(err *pinixv2.HubError) error {
	if err == nil {
		return nil
	}
	return &HubError{Code: strings.TrimSpace(err.GetCode()), Message: strings.TrimSpace(err.GetMessage())}
}

func aggregateOutputs(chunks [][]byte) json.RawMessage {
	if len(chunks) == 0 {
		return json.RawMessage(`{}`)
	}
	if len(chunks) == 1 {
		chunk := cloneBytes(chunks[0])
		if json.Valid(chunk) {
			return json.RawMessage(chunk)
		}
		wrapped, _ := json.Marshal(string(chunk))
		return json.RawMessage(wrapped)
	}

	size := 0
	for _, chunk := range chunks {
		size += len(chunk)
	}
	combined := make([]byte, 0, size)
	for _, chunk := range chunks {
		combined = append(combined, chunk...)
	}
	if len(combined) == 0 {
		return json.RawMessage(`{}`)
	}
	if json.Valid(combined) {
		return json.RawMessage(combined)
	}
	wrapped, _ := json.Marshal(string(combined))
	return json.RawMessage(wrapped)
}

func cloneBytes(data []byte) []byte {
	if len(data) == 0 {
		return nil
	}
	return append([]byte(nil), data...)
}
