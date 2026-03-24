// Role:    Edge Clip Provider binary that connects to a remote Hub registering Linux system capabilities
// Depends: context, crypto/tls, errors, flag, fmt, io, log, net, net/http, os, os/signal, strings, sync, syscall, time, connectrpc, pinix v2, pinixv2connect, edgelinux, http2
// Exports: main

package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	connect "connectrpc.com/connect"
	pinixv2 "github.com/epiral/pinix/gen/go/pinix/v2"
	"github.com/epiral/pinix/gen/go/pinix/v2/pinixv2connect"
	"github.com/epiral/pinix/internal/edgelinux"
	"golang.org/x/net/http2"
)

const (
	heartbeatInterval = 15 * time.Second
	reconnectDelay    = 5 * time.Second
)

func main() {
	hubURL := flag.String("hub", "http://localhost:9007", "Hub URL")
	name := flag.String("name", "", "Provider name (default: linux-<hostname>)")
	flag.Parse()

	providerName := strings.TrimSpace(*name)
	if providerName == "" {
		hostname, _ := os.Hostname()
		if hostname == "" {
			hostname = "unknown"
		}
		providerName = "linux-" + sanitizeComponent(hostname)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	for {
		log.Printf("Connecting to Hub at %s as %q...", *hubURL, providerName)
		if err := runProvider(ctx, *hubURL, providerName); err != nil {
			if ctx.Err() != nil {
				log.Printf("Shutting down.")
				return
			}
			log.Printf("Connection error: %v", err)
		}
		log.Printf("Reconnecting in %s...", reconnectDelay)
		select {
		case <-ctx.Done():
			log.Printf("Shutting down.")
			return
		case <-time.After(reconnectDelay):
		}
	}
}

func runProvider(parent context.Context, hubURL, providerName string) error {
	transport := &http2.Transport{
		AllowHTTP: true,
		DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, network, addr)
		},
	}
	httpClient := &http.Client{Transport: transport}

	client := pinixv2connect.NewHubServiceClient(httpClient, hubURL, connect.WithGRPC())

	sessionCtx, cancel := context.WithCancel(parent)
	defer cancel()

	stream := client.ProviderStream(sessionCtx)
	defer stream.CloseRequest()
	defer stream.CloseResponse()

	// Build clip registrations
	clips := edgelinux.ClipRegistrations()

	// Send register
	if err := sendMessage(stream, &pinixv2.ProviderMessage{
		Payload: &pinixv2.ProviderMessage_Register{
			Register: &pinixv2.RegisterRequest{
				ProviderName: providerName,
				Clips:        clips,
			},
		},
	}); err != nil {
		return fmt.Errorf("send register: %w", err)
	}

	// Wait for register response
	for {
		msg, err := stream.Receive()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return fmt.Errorf("connection closed before registration")
			}
			return fmt.Errorf("receive register response: %w", err)
		}

		if resp := msg.GetRegisterResponse(); resp != nil {
			if !resp.GetAccepted() {
				return fmt.Errorf("registration rejected: %s", resp.GetMessage())
			}
			log.Printf("Registered as %q with %d clips", providerName, len(clips))
			break
		}
	}

	// Start heartbeat goroutine
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-sessionCtx.Done():
				return
			case <-ticker.C:
				if err := sendMessage(stream, &pinixv2.ProviderMessage{
					Payload: &pinixv2.ProviderMessage_Ping{
						Ping: &pinixv2.Heartbeat{
							SentAtUnixMs: time.Now().UnixMilli(),
						},
					},
				}); err != nil {
					return
				}
			}
		}
	}()
	defer func() {
		cancel()
		<-heartbeatDone
	}()

	// Receive loop
	for {
		msg, err := stream.Receive()
		if err != nil {
			if parent.Err() != nil || sessionCtx.Err() != nil {
				return nil
			}
			if errors.Is(err, io.EOF) {
				return fmt.Errorf("hub disconnected")
			}
			return fmt.Errorf("receive: %w", err)
		}

		switch {
		case msg.GetInvokeCommand() != nil:
			go handleInvoke(stream, msg.GetInvokeCommand())
		case msg.GetPong() != nil:
			// heartbeat ack
		default:
			continue
		}
	}
}

var sendMu sync.Mutex

func sendMessage(stream *connect.BidiStreamForClient[pinixv2.ProviderMessage, pinixv2.HubMessage], msg *pinixv2.ProviderMessage) error {
	sendMu.Lock()
	defer sendMu.Unlock()
	return stream.Send(msg)
}

func handleInvoke(stream *connect.BidiStreamForClient[pinixv2.ProviderMessage, pinixv2.HubMessage], cmd *pinixv2.InvokeCommand) {
	requestID := strings.TrimSpace(cmd.GetRequestId())
	if requestID == "" {
		return
	}

	clipName := strings.TrimSpace(cmd.GetClipName())
	command := strings.TrimSpace(cmd.GetCommand())

	output, err := edgelinux.RouteCommand(clipName, command, cmd.GetInput())

	result := &pinixv2.InvokeResult{
		RequestId: requestID,
		Done:      true,
	}

	if err != nil {
		result.Error = &pinixv2.HubError{
			Code:    "internal",
			Message: err.Error(),
		}
	} else {
		if len(output) == 0 {
			output = []byte(`{}`)
		}
		result.Output = output
	}

	_ = sendMessage(stream, &pinixv2.ProviderMessage{
		Payload: &pinixv2.ProviderMessage_InvokeResult{
			InvokeResult: result,
		},
	})
}

func sanitizeComponent(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	var b strings.Builder
	prevDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if b.Len() > 0 && !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}
