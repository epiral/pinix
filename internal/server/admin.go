// Role:    AdminService implementation (Clip + Token CRUD)
// Depends: internal/config, internal/sandbox, pinixv1connect, connectrpc
// Exports: AdminServer, NewAdminServer

package server

import (
	"context"
	"fmt"
	"os"
	"strings"

	connect "connectrpc.com/connect"
	v1 "github.com/epiral/pinix/gen/go/pinix/v1"
	"github.com/epiral/pinix/gen/go/pinix/v1/pinixv1connect"
	"github.com/epiral/pinix/internal/config"
	"github.com/epiral/pinix/internal/sandbox"
)

var _ pinixv1connect.AdminServiceHandler = (*AdminServer)(nil)

// AdminServer implements AdminServiceHandler backed by a config Store.
type AdminServer struct {
	store   *config.Store
	sandbox *sandbox.Manager
}

// NewAdminServer creates an AdminServer with the given Store and sandbox Manager.
func NewAdminServer(store *config.Store, mgr *sandbox.Manager) *AdminServer {
	return &AdminServer{store: store, sandbox: mgr}
}

func (s *AdminServer) CreateClip(
	ctx context.Context,
	req *connect.Request[v1.CreateClipRequest],
) (*connect.Response[v1.CreateClipResponse], error) {
	name := strings.TrimSpace(req.Msg.GetName())
	if name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name is required"))
	}

	workdir := strings.TrimSpace(req.Msg.GetWorkdir())
	if workdir == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("workdir is required"))
	}

	if _, err := os.Stat(workdir); err != nil {
		if os.IsNotExist(err) {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("workdir does not exist: %s", workdir))
		}
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid workdir: %w", err))
	}

	entry, err := s.store.AddClip(name, workdir)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("create clip %q: %w", name, err))
	}
	return connect.NewResponse(&v1.CreateClipResponse{ClipId: entry.ID}), nil
}

func (s *AdminServer) ListClips(
	_ context.Context,
	_ *connect.Request[v1.ListClipsRequest],
) (*connect.Response[v1.ListClipsResponse], error) {
	entries := s.store.GetClips()
	clips := make([]*v1.ClipInfo, len(entries))
	for i, e := range entries {
		info := scanClipWorkdir(e)
		clips[i] = &v1.ClipInfo{
			ClipId:   e.ID,
			Name:     e.Name,
			Desc:     info.desc,
			Commands: info.commands,
			HasWeb:   info.hasWeb,
		}
	}
	return connect.NewResponse(&v1.ListClipsResponse{Clips: clips}), nil
}

func (s *AdminServer) DeleteClip(
	ctx context.Context,
	req *connect.Request[v1.DeleteClipRequest],
) (*connect.Response[v1.DeleteClipResponse], error) {
	clipID := req.Msg.GetClipId()
	found, err := s.store.DeleteClip(clipID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("delete clip %s: %w", clipID, err))
	}
	if !found {
		return nil, connect.NewError(connect.CodeNotFound, nil)
	}
	if _, err := s.store.RevokeTokensByClipID(clipID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("revoke clip tokens %s: %w", clipID, err))
	}
	if s.sandbox != nil {
		if err := s.sandbox.RemoveClip(ctx, clipID); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("remove sandbox clip %s: %w", clipID, err))
		}
	}
	return connect.NewResponse(&v1.DeleteClipResponse{}), nil
}

func (s *AdminServer) GenerateToken(
	_ context.Context,
	req *connect.Request[v1.GenerateTokenRequest],
) (*connect.Response[v1.GenerateTokenResponse], error) {
	clipID := req.Msg.GetClipId()
	if clipID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("clip_id is required"))
	}
	if _, ok := s.store.GetClip(clipID); !ok {
		return nil, connect.NewError(connect.CodeNotFound, nil)
	}

	entry, err := s.store.AddToken(clipID, req.Msg.GetLabel())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("generate token for clip %s: %w", clipID, err))
	}
	return connect.NewResponse(&v1.GenerateTokenResponse{Id: entry.ID, Token: entry.Token}), nil
}

func (s *AdminServer) ListTokens(
	_ context.Context,
	_ *connect.Request[v1.ListTokensRequest],
) (*connect.Response[v1.ListTokensResponse], error) {
	entries := s.store.GetTokens()
	tokens := make([]*v1.TokenInfo, len(entries))
	for i, e := range entries {
		hint := ""
		if len(e.Token) >= 4 {
			hint = e.Token[len(e.Token)-4:]
		}
		tokens[i] = &v1.TokenInfo{
			Id:        e.ID,
			ClipId:    e.ClipID,
			Label:     e.Label,
			CreatedAt: e.CreatedAt,
			Hint:      hint,
		}
	}
	return connect.NewResponse(&v1.ListTokensResponse{Tokens: tokens}), nil
}

func (s *AdminServer) RevokeToken(
	_ context.Context,
	req *connect.Request[v1.RevokeTokenRequest],
) (*connect.Response[v1.RevokeTokenResponse], error) {
	found, err := s.store.RevokeTokenByID(req.Msg.GetId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("revoke token %s: %w", req.Msg.GetId(), err))
	}
	if !found {
		return nil, connect.NewError(connect.CodeNotFound, nil)
	}
	return connect.NewResponse(&v1.RevokeTokenResponse{}), nil
}
