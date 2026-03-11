package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	connect "connectrpc.com/connect"
	v1 "github.com/epiral/pinix/gen/go/pinix/v1"
	"github.com/epiral/pinix/gen/go/pinix/v1/pinixv1connect"
	"golang.org/x/net/http2"
)

type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(req)
}

func main() {
	server := flag.String("server", "http://localhost:9876", "Pinix server URL")
	token := flag.String("token", "", "super token")
	name := flag.String("name", "test-edge", "edge device name")
	flag.Parse()
	if *token == "" {
		log.Fatal("--token is required")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	h2Transport := &http2.Transport{
		AllowHTTP: true,
		DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
			return net.Dial(network, addr)
		},
	}
	client := pinixv1connect.NewEdgeServiceClient(
		&http.Client{Transport: &bearerTransport{token: *token, base: h2Transport}},
		*server,
		connect.WithGRPC(),
	)
	stream := client.Connect(ctx)

	if err := stream.Send(&v1.EdgeUpstream{Msg: &v1.EdgeUpstream_Manifest{Manifest: &v1.EdgeManifest{
		Name:        *name,
		Description: "test edge device",
		Commands: []*v1.EdgeCommandDef{
			{Name: "echo", Description: "echo stdin back"},
			{Name: "hello", Description: "say hello"},
		},
	}}}); err != nil {
		log.Fatalf("send manifest: %v", err)
	}

	for {
		msg, err := stream.Receive()
		if err != nil {
			if err == io.EOF || ctx.Err() != nil {
				break
			}
			log.Fatalf("receive: %v", err)
		}
		switch body := msg.GetMsg().(type) {
		case *v1.EdgeDownstream_Accepted:
			fmt.Printf("accepted clip_id=%s token=%s\n", body.Accepted.GetClipId(), body.Accepted.GetToken())
		case *v1.EdgeDownstream_Request:
			if err := handleRequest(stream, body.Request); err != nil {
				log.Printf("handle request: %v", err)
			}
		case *v1.EdgeDownstream_Pong:
			continue
		case *v1.EdgeDownstream_Rejected:
			log.Fatalf("rejected: %s", body.Rejected.GetReason())
		}
	}
}

func handleRequest(stream *connect.BidiStreamForClient[v1.EdgeUpstream, v1.EdgeDownstream], req *v1.EdgeRequest) error {
	requestID := req.GetRequestId()
	send := func(resp *v1.EdgeResponse) error {
		return stream.Send(&v1.EdgeUpstream{Msg: &v1.EdgeUpstream_Response{Response: resp}})
	}

	switch body := req.GetBody().(type) {
	case *v1.EdgeRequest_Invoke:
		switch body.Invoke.GetName() {
		case "echo":
			if err := send(&v1.EdgeResponse{RequestId: requestID, Body: &v1.EdgeResponse_InvokeChunk{
				InvokeChunk: &v1.InvokeChunk{Payload: &v1.InvokeChunk_Stdout{Stdout: []byte(body.Invoke.GetStdin())}},
			}}); err != nil {
				return err
			}
		case "hello":
			if err := send(&v1.EdgeResponse{RequestId: requestID, Body: &v1.EdgeResponse_InvokeChunk{
				InvokeChunk: &v1.InvokeChunk{Payload: &v1.InvokeChunk_Stdout{Stdout: []byte("hello from edge device")}},
			}}); err != nil {
				return err
			}
		default:
			if err := send(&v1.EdgeResponse{RequestId: requestID, Body: &v1.EdgeResponse_Error{
				Error: &v1.EdgeError{Message: "unknown command: " + body.Invoke.GetName()},
			}}); err != nil {
				return err
			}
			return nil
		}
		if err := send(&v1.EdgeResponse{RequestId: requestID, Body: &v1.EdgeResponse_InvokeChunk{
			InvokeChunk: &v1.InvokeChunk{Payload: &v1.InvokeChunk_ExitCode{ExitCode: 0}},
		}}); err != nil {
			return err
		}
		return send(&v1.EdgeResponse{RequestId: requestID, Body: &v1.EdgeResponse_Complete{Complete: &v1.EdgeComplete{}}})

	case *v1.EdgeRequest_GetInfo:
		_ = body
		return send(&v1.EdgeResponse{RequestId: requestID, Body: &v1.EdgeResponse_GetInfo{
			GetInfo: &v1.GetInfoResponse{Name: "edge-test", Description: "test edge device", Commands: []string{"echo", "hello"}},
		}})

	case *v1.EdgeRequest_Cancel:
		return send(&v1.EdgeResponse{RequestId: requestID, Body: &v1.EdgeResponse_Complete{Complete: &v1.EdgeComplete{}}})

	default:
		return send(&v1.EdgeResponse{RequestId: requestID, Body: &v1.EdgeResponse_Error{
			Error: &v1.EdgeError{Message: "unsupported request"},
		}})
	}
}
