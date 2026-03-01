// Role:    AdminService implementation (Clip + Token CRUD)
// Depends: internal/config, pinixv1connect, connectrpc
// Exports: AdminServer, NewAdminServer

package server

import (
	"context"

	connect "connectrpc.com/connect"
	v1 "github.com/epiral/pinix/gen/go/pinix/v1"
	"github.com/epiral/pinix/gen/go/pinix/v1/pinixv1connect"
	"github.com/epiral/pinix/internal/config"
)

var _ pinixv1connect.AdminServiceHandler = (*AdminServer)(nil)

// AdminServer implements AdminServiceHandler backed by a config Store.
type AdminServer struct {
	store *config.Store
}

// NewAdminServer creates an AdminServer with the given Store.
func NewAdminServer(store *config.Store) *AdminServer {
	return &AdminServer{store: store}
}

func (s *AdminServer) CreateClip(
	_ context.Context,
	req *connect.Request[v1.CreateClipRequest],
) (*connect.Response[v1.CreateClipResponse], error) {
	entry, err := s.store.AddClip(req.Msg.GetName(), req.Msg.GetWorkdir())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
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

func (s *AdminServer) GenerateToken(
	_ context.Context,
	req *connect.Request[v1.GenerateTokenRequest],
) (*connect.Response[v1.GenerateTokenResponse], error) {
	clipID := req.Msg.GetClipId()
	if clipID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, nil)
	}
	if _, ok := s.store.GetClip(clipID); !ok {
		return nil, connect.NewError(connect.CodeNotFound, nil)
	}

	entry, err := s.store.AddToken(clipID, req.Msg.GetLabel())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
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
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if !found {
		return nil, connect.NewError(connect.CodeNotFound, nil)
	}
	return connect.NewResponse(&v1.RevokeTokenResponse{}), nil
}

// clipWorkdirInfo holds metadata scanned from a clip's workdir.
type clipWorkdirInfo struct {
	desc     string
	commands []string
	hasWeb   bool
}

// scanClipWorkdir reads a clip's workdir to discover commands, web presence, and description.
func scanClipWorkdir(clip config.ClipEntry) clipWorkdirInfo {
	var info clipWorkdirInfo

	// List commands/ directory.
	entries, err := readDirNames(clip.Workdir, "commands")
	if err == nil {
		info.commands = entries
	}

	// Check for web/index.html.
	info.hasWeb = fileExists(clip.Workdir, "web", "index.html")

	// Read description from clip.yaml or AGENTS.md (first line).
	info.desc = readClipDesc(clip.Workdir)

	return info
}
