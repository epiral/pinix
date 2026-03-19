// Role:    NDJSON stdin/stdout IPC client for Clip processes
// Depends: bufio, context, encoding/json, errors, fmt, io, strconv, sync, sync/atomic
// Exports: Client, Request, Response, Error, ErrClosed

package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"
	"sync/atomic"
)

var ErrClosed = errors.New("ipc client closed")

type Request struct {
	ID      string          `json:"id"`
	Command string          `json:"command"`
	Input   json.RawMessage `json:"input,omitempty"`
}

type Response struct {
	ID     string          `json:"id"`
	Output json.RawMessage `json:"output,omitempty"`
	Error  *Error          `json:"error,omitempty"`
}

type Error struct {
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Code == "" {
		return e.Message
	}
	return fmt.Sprintf("%s (%s)", e.Message, e.Code)
}

type Client struct {
	enc *json.Encoder

	writeMu sync.Mutex

	pendingMu sync.Mutex
	pending   map[string]chan Response

	closeOnce sync.Once
	closed    chan struct{}

	closeErrMu sync.RWMutex
	closeErr   error

	nextID atomic.Uint64
}

func New(stdin io.Writer, stdout io.Reader) *Client {
	c := &Client{
		enc:     json.NewEncoder(stdin),
		pending: make(map[string]chan Response),
		closed:  make(chan struct{}),
	}
	go c.readLoop(stdout)
	return c
}

func (c *Client) Send(id, command string, input json.RawMessage) error {
	if id == "" {
		return fmt.Errorf("ipc request id is required")
	}
	if command == "" {
		return fmt.Errorf("ipc command is required")
	}
	if len(input) == 0 {
		input = json.RawMessage(`{}`)
	}

	select {
	case <-c.closed:
		return c.err()
	default:
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if err := c.enc.Encode(&Request{ID: id, Command: command, Input: input}); err != nil {
		c.CloseWithError(fmt.Errorf("write ipc request: %w", err))
		return c.err()
	}
	return nil
}

func (c *Client) Call(ctx context.Context, command string, input json.RawMessage) (json.RawMessage, error) {
	id := strconv.FormatUint(c.nextID.Add(1), 10)
	ch := make(chan Response, 1)

	if err := c.register(id, ch); err != nil {
		return nil, err
	}
	defer c.unregister(id)

	if err := c.Send(id, command, input); err != nil {
		return nil, err
	}

	select {
	case resp := <-ch:
		if resp.Error != nil {
			return nil, resp.Error
		}
		return append(json.RawMessage(nil), resp.Output...), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.closed:
		return nil, c.err()
	}
}

func (c *Client) Close() {
	c.CloseWithError(ErrClosed)
}

func (c *Client) CloseWithError(err error) {
	c.closeOnce.Do(func() {
		if err == nil {
			err = ErrClosed
		}

		c.closeErrMu.Lock()
		c.closeErr = err
		c.closeErrMu.Unlock()

		c.pendingMu.Lock()
		pending := c.pending
		c.pending = make(map[string]chan Response)
		c.pendingMu.Unlock()

		for id, ch := range pending {
			resp := Response{ID: id, Error: &Error{Message: err.Error(), Code: "closed"}}
			select {
			case ch <- resp:
			default:
			}
		}

		close(c.closed)
	})
}

func (c *Client) readLoop(stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)

	for scanner.Scan() {
		var resp Response
		if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
			c.CloseWithError(fmt.Errorf("decode ipc response: %w", err))
			return
		}
		c.dispatch(resp)
	}

	if err := scanner.Err(); err != nil {
		c.CloseWithError(fmt.Errorf("read ipc response: %w", err))
		return
	}

	c.CloseWithError(io.EOF)
}

func (c *Client) dispatch(resp Response) {
	c.pendingMu.Lock()
	ch, ok := c.pending[resp.ID]
	c.pendingMu.Unlock()
	if !ok {
		return
	}

	select {
	case ch <- resp:
	default:
	}
}

func (c *Client) register(id string, ch chan Response) error {
	select {
	case <-c.closed:
		return c.err()
	default:
	}

	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	if _, exists := c.pending[id]; exists {
		return fmt.Errorf("duplicate ipc request id: %s", id)
	}
	c.pending[id] = ch
	return nil
}

func (c *Client) unregister(id string) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	delete(c.pending, id)
}

func (c *Client) err() error {
	c.closeErrMu.RLock()
	defer c.closeErrMu.RUnlock()
	if c.closeErr == nil {
		return ErrClosed
	}
	return c.closeErr
}
