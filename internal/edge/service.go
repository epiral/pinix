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

	// Try to find existing edge clip by name (reuse across reconnects)
	clipID, token, isNew := s.findOrCreateEdgeClip(manifest.GetName())

	session, err := NewSession(stream)
	if err != nil {
		return err
	}

	// Register or update EdgeClip in registry
	if existing, found := s.registry.Resolve(clipID); found {
		if ec, ok := existing.(*EdgeClip); ok {
			ec.SetSession(session, manifest)
			log.Printf("[edge] reconnected: name=%s clip_id=%s", manifest.GetName(), clipID)
		}
	} else {
		ec := NewEdgeClip(clipID, token, manifest, session)
		s.registry.Register(ec)
		if isNew {
			log.Printf("[edge] new device: name=%s clip_id=%s", manifest.GetName(), clipID)
		} else {
			log.Printf("[edge] restored: name=%s clip_id=%s", manifest.GetName(), clipID)
		}
	}

	// Send accepted with stable clip_id and token
	if err := session.Send(&v1.EdgeDownstream{Msg: &v1.EdgeDownstream_Accepted{Accepted: &v1.EdgeAccepted{ClipId: clipID, Token: token}}}); err != nil {
		return fmt.Errorf("send accepted: %w", err)
	}

	// Read loop — on disconnect, mark offline instead of unregistering
	defer func() {
		if ec, found := s.registry.Resolve(clipID); found {
			if edgeClip, ok := ec.(*EdgeClip); ok {
				edgeClip.ClearSession()
			}
		}
		session.Close()
		log.Printf("[edge] offline: name=%s clip_id=%s", manifest.GetName(), clipID)
	}()

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

// findOrCreateEdgeClip looks up an existing edge clip by name, or creates a new one.
// Returns (clipID, token, isNew).
func (s *Service) findOrCreateEdgeClip(name string) (string, string, bool) {
	// Check if clip with this name already exists (workdir="" = edge clip)
	if existing, found := s.store.GetClipByName(name); found && existing.Workdir == "" {
		// Find existing token for this clip
		for _, t := range s.store.GetTokens() {
			if t.ClipID == existing.ID {
				return existing.ID, t.Token, false
			}
		}
		// Clip exists but no token — generate one
		tokenEntry, err := s.store.AddToken(existing.ID, "edge:"+name)
		if err != nil {
			log.Printf("[edge] failed to generate token for existing clip %s: %v", existing.ID, err)
		} else {
			return existing.ID, tokenEntry.Token, false
		}
	}

	// Create new edge clip
	entry, err := s.store.AddClip(name, "")
	if err != nil {
		log.Printf("[edge] failed to create clip: %v", err)
		return "", "", true
	}
	tokenEntry, err := s.store.AddToken(entry.ID, "edge:"+name)
	if err != nil {
		log.Printf("[edge] failed to generate token: %v", err)
		return entry.ID, "", true
	}
	return entry.ID, tokenEntry.Token, true
}

// RegisterOfflinePlaceholders creates offline EdgeClip entries for persisted edge clips.
// Called during server startup.
func RegisterOfflinePlaceholders(store *config.Store, registry *clipiface.Registry) {
	for _, entry := range store.GetClips() {
		if entry.Workdir != "" {
			continue // local clip, skip
		}
		// Find token
		var token string
		for _, t := range store.GetTokens() {
			if t.ClipID == entry.ID {
				token = t.Token
				break
			}
		}
		ec := NewOfflinePlaceholder(entry.ID, token, entry.Name)
		registry.Register(ec)
		log.Printf("[edge] offline placeholder: name=%s clip_id=%s", entry.Name, entry.ID)
	}
}
