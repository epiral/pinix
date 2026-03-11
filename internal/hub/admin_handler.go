// Role:    AdminService handler for clip and token management
// Depends: context, fmt, os, strings, slices, connectrpc, internal/clip, internal/config, internal/sandbox, internal/scheduler, internal/worker
// Exports: AdminHandler, NewAdminHandler

package hub

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	connect "connectrpc.com/connect"
	v1 "github.com/epiral/pinix/gen/go/pinix/v1"
	"github.com/epiral/pinix/gen/go/pinix/v1/pinixv1connect"
	clipiface "github.com/epiral/pinix/internal/clip"
	"github.com/epiral/pinix/internal/config"
	"github.com/epiral/pinix/internal/sandbox"
	"github.com/epiral/pinix/internal/scheduler"
	"github.com/epiral/pinix/internal/worker"
)

var _ pinixv1connect.AdminServiceHandler = (*AdminHandler)(nil)

type AdminHandler struct {
	store    *config.Store
	registry *clipiface.Registry
	sandbox  *sandbox.Manager
	sched    *scheduler.Scheduler
}

func NewAdminHandler(store *config.Store, registry *clipiface.Registry, mgr *sandbox.Manager, sched *scheduler.Scheduler) *AdminHandler {
	return &AdminHandler{store: store, registry: registry, sandbox: mgr, sched: sched}
}

func (h *AdminHandler) CreateClip(ctx context.Context, req *connect.Request[v1.CreateClipRequest]) (*connect.Response[v1.CreateClipResponse], error) {
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
	entry, err := h.store.AddClip(name, workdir)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("create clip %q: %w", name, err))
	}
	h.registry.Register(worker.NewLocalClip(entry, h.sandbox))
	if h.sched != nil {
		worker.RegisterExistingSchedules(h.store, h.sched)
	}
	return connect.NewResponse(&v1.CreateClipResponse{ClipId: entry.ID}), nil
}

func (h *AdminHandler) ListClips(ctx context.Context, _ *connect.Request[v1.ListClipsRequest]) (*connect.Response[v1.ListClipsResponse], error) {
	resolved := h.registry.List()
	clips := make([]*v1.ClipInfo, 0, len(resolved))
	slices.SortFunc(resolved, func(a, b clipiface.Clip) int { return strings.Compare(a.ID(), b.ID()) })
	for _, c := range resolved {
		info, err := c.GetInfo(ctx)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("get clip info %s: %w", c.ID(), err))
		}
		clips = append(clips, &v1.ClipInfo{ClipId: c.ID(), Name: info.Name, Description: info.Description, Commands: info.Commands, HasWeb: info.HasWeb})
	}
	return connect.NewResponse(&v1.ListClipsResponse{Clips: clips}), nil
}

func (h *AdminHandler) DeleteClip(ctx context.Context, req *connect.Request[v1.DeleteClipRequest]) (*connect.Response[v1.DeleteClipResponse], error) {
	clipID := req.Msg.GetClipId()
	found, err := h.store.DeleteClip(clipID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("delete clip %s: %w", clipID, err))
	}
	if !found {
		return nil, connect.NewError(connect.CodeNotFound, nil)
	}
	h.registry.Unregister(clipID)
	if h.sched != nil {
		h.sched.UnregisterClip(clipID)
	}
	if _, err := h.store.RevokeTokensByClipID(clipID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("revoke clip tokens %s: %w", clipID, err))
	}
	if h.sandbox != nil {
		if err := h.sandbox.RemoveClip(ctx, clipID); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("remove sandbox clip %s: %w", clipID, err))
		}
	}
	return connect.NewResponse(&v1.DeleteClipResponse{}), nil
}

func (h *AdminHandler) GenerateToken(_ context.Context, req *connect.Request[v1.GenerateTokenRequest]) (*connect.Response[v1.GenerateTokenResponse], error) {
	clipID := req.Msg.GetClipId()
	if clipID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("clip_id is required"))
	}
	if _, ok := h.store.GetClip(clipID); !ok {
		return nil, connect.NewError(connect.CodeNotFound, nil)
	}
	entry, err := h.store.AddToken(clipID, req.Msg.GetLabel())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("generate token for clip %s: %w", clipID, err))
	}
	return connect.NewResponse(&v1.GenerateTokenResponse{Id: entry.ID, Token: entry.Token}), nil
}

func (h *AdminHandler) ListTokens(_ context.Context, _ *connect.Request[v1.ListTokensRequest]) (*connect.Response[v1.ListTokensResponse], error) {
	entries := h.store.GetTokens()
	tokens := make([]*v1.TokenInfo, len(entries))
	for i, e := range entries {
		hint := ""
		if len(e.Token) >= 4 {
			hint = e.Token[len(e.Token)-4:]
		}
		tokens[i] = &v1.TokenInfo{Id: e.ID, ClipId: e.ClipID, Label: e.Label, CreatedAt: e.CreatedAt, Hint: hint}
	}
	return connect.NewResponse(&v1.ListTokensResponse{Tokens: tokens}), nil
}

func (h *AdminHandler) RevokeToken(_ context.Context, req *connect.Request[v1.RevokeTokenRequest]) (*connect.Response[v1.RevokeTokenResponse], error) {
	found, err := h.store.RevokeTokenByID(req.Msg.GetId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("revoke token %s: %w", req.Msg.GetId(), err))
	}
	if !found {
		return nil, connect.NewError(connect.CodeNotFound, nil)
	}
	return connect.NewResponse(&v1.RevokeTokenResponse{}), nil
}
