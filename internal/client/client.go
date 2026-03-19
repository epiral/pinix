// Role:    Unix socket JSON client used by pinix CLI
// Depends: context, encoding/json, fmt, net, internal/daemon
// Exports: Client, New, Call, Add, Remove, List, Invoke

package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net"

	"github.com/epiral/pinix/internal/daemon"
)

type Client struct {
	socketPath string
}

func New(socketPath string) (*Client, error) {
	if socketPath == "" {
		var err error
		socketPath, err = daemon.DefaultSocketPath()
		if err != nil {
			return nil, err
		}
	}
	return &Client{socketPath: socketPath}, nil
}

func (c *Client) Call(ctx context.Context, method string, params any, token string, out any) error {
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", c.socketPath)
	if err != nil {
		return fmt.Errorf("dial pinixd: %w", err)
	}
	defer conn.Close()

	request := daemon.Request{Method: method, Token: token}
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("marshal request params: %w", err)
		}
		request.Params = data
	}

	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)

	if err := encoder.Encode(&request); err != nil {
		return fmt.Errorf("send request: %w", err)
	}

	var response daemon.SocketResponse
	if err := decoder.Decode(&response); err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if response.Error != nil {
		return response.Error
	}
	if out == nil || len(response.Result) == 0 {
		return nil
	}
	if err := json.Unmarshal(response.Result, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func (c *Client) Add(ctx context.Context, source, clipToken, authToken string) (*daemon.AddResult, error) {
	var result daemon.AddResult
	err := c.Call(ctx, "add", daemon.AddParams{Source: source, Token: clipToken}, authToken, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) Remove(ctx context.Context, name, authToken string) (*daemon.RemoveResult, error) {
	var result daemon.RemoveResult
	err := c.Call(ctx, "remove", daemon.RemoveParams{Name: name}, authToken, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) List(ctx context.Context) (*daemon.ListResult, error) {
	var result daemon.ListResult
	err := c.Call(ctx, "list", struct{}{}, "", &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) Invoke(ctx context.Context, clip, command string, input json.RawMessage, authToken string) (json.RawMessage, error) {
	var result json.RawMessage
	err := c.Call(ctx, "invoke", daemon.InvokeParams{Clip: clip, Command: command, Input: input}, authToken, &result)
	if err != nil {
		return nil, err
	}
	return result, nil
}
