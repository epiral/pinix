// Role:    Mock WebSocket provider daemon for pinixd integration testing
// Depends: context, encoding/json, flag, fmt, log, os/signal, syscall, golang.org/x/net/websocket
// Exports: main

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os/signal"
	"syscall"

	"golang.org/x/net/websocket"
)

type registerMessage struct {
	Type     string   `json:"type"`
	Name     string   `json:"name"`
	Commands []string `json:"capabilities"`
}

type invokeMessage struct {
	ID      string          `json:"id"`
	Command string          `json:"command"`
	Input   json.RawMessage `json:"input,omitempty"`
}

type responseMessage struct {
	ID     string          `json:"id"`
	Output json.RawMessage `json:"output,omitempty"`
	Error  *responseError  `json:"error,omitempty"`
}

type responseError struct {
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

func main() {
	var (
		serverURL string
		name      string
	)

	flag.StringVar(&serverURL, "server", "ws://127.0.0.1:9000/ws/provider", "pinixd provider WebSocket URL")
	flag.StringVar(&name, "name", "test-clip", "clip name to register")
	flag.Parse()

	origin := "http://127.0.0.1/"
	ws, err := websocket.Dial(serverURL, "", origin)
	if err != nil {
		log.Fatalf("dial %s: %v", serverURL, err)
	}
	defer ws.Close()

	register := registerMessage{
		Type:     "register",
		Name:     name,
		Commands: []string{"hello", "echo"},
	}
	if err := websocket.JSON.Send(ws, register); err != nil {
		log.Fatalf("send register: %v", err)
	}
	log.Printf("registered clip %s at %s", name, serverURL)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		errCh <- serve(ws, name)
	}()

	select {
	case <-ctx.Done():
		log.Printf("shutting down mock provider")
	case err := <-errCh:
		if err != nil {
			log.Fatalf("serve provider: %v", err)
		}
	}
}

func serve(ws *websocket.Conn, name string) error {
	for {
		var req invokeMessage
		if err := websocket.JSON.Receive(ws, &req); err != nil {
			return err
		}

		resp := responseMessage{ID: req.ID}
		switch req.Command {
		case "hello":
			resp.Output = mustMarshal(map[string]any{
				"clip":    name,
				"command": req.Command,
				"input":   decodeInput(req.Input),
				"message": fmt.Sprintf("hello from %s", name),
			})
		case "echo":
			resp.Output = cloneRaw(req.Input)
			if len(resp.Output) == 0 {
				resp.Output = json.RawMessage(`{}`)
			}
		default:
			resp.Error = &responseError{Message: fmt.Sprintf("unknown command %q", req.Command), Code: "UNKNOWN_COMMAND"}
		}

		if err := websocket.JSON.Send(ws, resp); err != nil {
			return err
		}
	}
}

func decodeInput(input json.RawMessage) any {
	if len(input) == 0 {
		return map[string]any{}
	}
	var decoded any
	if err := json.Unmarshal(input, &decoded); err != nil {
		return map[string]any{"raw": string(input)}
	}
	return decoded
}

func mustMarshal(value any) json.RawMessage {
	data, err := json.Marshal(value)
	if err != nil {
		log.Fatalf("marshal mock response: %v", err)
	}
	return data
}

func cloneRaw(data json.RawMessage) json.RawMessage {
	if len(data) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), data...)
}
