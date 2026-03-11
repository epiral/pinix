// Role:    EdgeService handler for registering edge devices and routing their responses
// Depends: context, fmt, io, log, connectrpc, pinix v1 connect, internal/clip, internal/config
// Exports: Service, NewService

package edge

import (
	"context"
	"fmt"
	"io"
	"log"

	connect "connectrpc.com/connect"
	v1 "github.com/epiral/pinix/gen/go/pinix/v1"
	"github.com/epiral/pinix/gen/go/pinix/v1/pinixv1connect"
	clipiface "github.com/epiral/pinix/internal/clip"
	"github.com/epiral/pinix/internal/config"
)

var _ pinixv1connect.EdgeServiceHandler = (*Service)(nil)

type Service struct {
	registry *clipiface.Registry
	store    *config.Store
}

func NewService(registry *clipiface.Registry, store *config.Store) *Service {
	return &Service{registry: registry, store: store}
}

func (s *Service) Connect(ctx context.Context, stream *connect.BidiStream[v1.EdgeUpstream, v1.EdgeDownstream]) error {
	first, err := stream.Receive()
	if err != nil {
		if err == io.EOF {
			return nil
		}
		return fmt.Errorf("receive initial edge message: %w", err)
	}
	manifest := first.GetManifest()
	if manifest == nil {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("first edge message must be manifest"))
	}
	if manifest.GetName() == "" {
		_ = stream.Send(&v1.EdgeDownstream{Msg: &v1.EdgeDownstream_Rejected{Rejected: &v1.EdgeRejected{Reason: "manifest name required"}}})
		return nil
	}
	if len(manifest.GetCommands()) == 0 {
		_ = stream.Send(&v1.EdgeDownstream{Msg: &v1.EdgeDownstream_Rejected{Rejected: &v1.EdgeRejected{Reason: "manifest commands required"}}})
		return nil
	}
	entry, err := s.store.AddClip(manifest.GetName(), "")
	if err != nil {
		return fmt.Errorf("add edge clip: %w", err)
	}
	tokenEntry, err := s.store.AddToken(entry.ID, "edge:"+manifest.GetName())
	if err != nil {
		_, _ = s.store.DeleteClip(entry.ID)
		return fmt.Errorf("add edge token: %w", err)
	}
	session, err := NewSession(stream)
	if err != nil {
		_, _ = s.store.RevokeTokensByClipID(entry.ID)
		_, _ = s.store.DeleteClip(entry.ID)
		return err
	}
	s.registry.Register(&EdgeClip{clipID: entry.ID, manifest: manifest, session: session, token: tokenEntry.Token})
	defer func() {
		s.registry.Unregister(entry.ID)
		if _, err := s.store.DeleteClip(entry.ID); err != nil {
			log.Printf("edge clip cleanup delete failed: clip_id=%s err=%v", entry.ID, err)
		}
		if _, err := s.store.RevokeTokensByClipID(entry.ID); err != nil {
			log.Printf("edge clip cleanup revoke failed: clip_id=%s err=%v", entry.ID, err)
		}
		session.Close()
		log.Printf("edge clip disconnected: name=%s clip_id=%s", manifest.GetName(), entry.ID)
	}()
	if err := session.Send(&v1.EdgeDownstream{Msg: &v1.EdgeDownstream_Accepted{Accepted: &v1.EdgeAccepted{ClipId: entry.ID, Token: tokenEntry.Token}}}); err != nil {
		return fmt.Errorf("send accepted: %w", err)
	}
	log.Printf("edge clip connected: name=%s clip_id=%s", manifest.GetName(), entry.ID)
	for {
		msg, err := stream.Receive()
		if err != nil {
			if err == io.EOF || ctx.Err() != nil {
				return nil
			}
			return nil
		}
		switch body := msg.GetMsg().(type) {
		case *v1.EdgeUpstream_Response:
			session.HandleResponse(body.Response)
		case *v1.EdgeUpstream_Ping:
			if err := session.Send(&v1.EdgeDownstream{Msg: &v1.EdgeDownstream_Pong{Pong: &v1.EdgePong{}}}); err != nil {
				return nil
			}
		case *v1.EdgeUpstream_Manifest:
			continue
		}
	}
}
