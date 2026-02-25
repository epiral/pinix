// Role:    PinixService full implementation (Clip + Token CRUD via Store)
// Depends: internal/config, pinixv1connect, connectrpc
// Exports: PinixServer, NewPinixServer

package service

import (
	"context"

	connect "connectrpc.com/connect"
	v1 "github.com/epiral/pinix/gen/go/pinix/v1"
	"github.com/epiral/pinix/internal/config"
)

// PinixServer implements PinixServiceHandler backed by a config Store.
type PinixServer struct {
	store *config.Store
}

// NewPinixServer creates a PinixServer with the given Store.
func NewPinixServer(store *config.Store) *PinixServer {
	return &PinixServer{store: store}
}

func (s *PinixServer) CreateClip(
	_ context.Context,
	req *connect.Request[v1.CreateClipRequest],
) (*connect.Response[v1.CreateClipResponse], error) {
	entry, err := s.store.AddClip(req.Msg.GetName(), req.Msg.GetWorkdir())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&v1.CreateClipResponse{ClipId: entry.ID}), nil
}

func (s *PinixServer) ListClips(
	_ context.Context,
	_ *connect.Request[v1.ListClipsRequest],
) (*connect.Response[v1.ListClipsResponse], error) {
	entries := s.store.GetClips()
	clips := make([]*v1.Clip, len(entries))
	for i, e := range entries {
		clips[i] = &v1.Clip{ClipId: e.ID, Name: e.Name, Workdir: e.Workdir}
	}
	return connect.NewResponse(&v1.ListClipsResponse{Clips: clips}), nil
}

func (s *PinixServer) DeleteClip(
	_ context.Context,
	req *connect.Request[v1.DeleteClipRequest],
) (*connect.Response[v1.DeleteClipResponse], error) {
	found, err := s.store.DeleteClip(req.Msg.GetClipId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if !found {
		return nil, connect.NewError(connect.CodeNotFound, nil)
	}
	return connect.NewResponse(&v1.DeleteClipResponse{}), nil
}

func (s *PinixServer) GenerateToken(
	_ context.Context,
	req *connect.Request[v1.GenerateTokenRequest],
) (*connect.Response[v1.GenerateTokenResponse], error) {
	clipID := req.Msg.GetClipId()

	// If clip_id is specified, verify the clip exists.
	if clipID != "" {
		if _, ok := s.store.GetClip(clipID); !ok {
			return nil, connect.NewError(connect.CodeNotFound, nil)
		}
	}

	entry, err := s.store.AddToken(clipID, req.Msg.GetLabel())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&v1.GenerateTokenResponse{Token: entry.Token}), nil
}

func (s *PinixServer) RevokeToken(
	_ context.Context,
	req *connect.Request[v1.RevokeTokenRequest],
) (*connect.Response[v1.RevokeTokenResponse], error) {
	found, err := s.store.RevokeToken(req.Msg.GetToken())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if !found {
		return nil, connect.NewError(connect.CodeNotFound, nil)
	}
	return connect.NewResponse(&v1.RevokeTokenResponse{}), nil
}
