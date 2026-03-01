// Role:    Connect-RPC auth interceptor (Bearer token validation, unary + streaming)
// Depends: internal/config, connectrpc
// Exports: NewInterceptor, ClipIDFromContext

package auth

import (
	"context"
	"strings"

	connect "connectrpc.com/connect"
	"github.com/epiral/pinix/internal/config"
)

type ctxKey struct{}

// authInfo holds the resolved identity from a validated token.
type authInfo struct {
	ClipID string // non-empty for clip tokens
}

// ClipIDFromContext returns the clip ID bound to the authenticated token.
// Returns empty string for super token.
func ClipIDFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(ctxKey{}).(authInfo)
	if !ok {
		return "", false
	}
	return v.ClipID, true
}

// interceptor validates Bearer tokens for both unary and streaming RPCs.
//
// Token resolution order:
//  1. Match against super_token (static, from config) → full access.
//  2. Match against clip tokens → ClipService only, scoped to clip.
//  3. No match → CodeUnauthenticated.
type interceptor struct {
	store *config.Store
}

// NewInterceptor returns a Connect interceptor that validates Bearer tokens.
func NewInterceptor(store *config.Store) connect.Interceptor {
	return &interceptor{store: store}
}

func (a *interceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		info, err := a.authenticate(req.Header().Get("Authorization"), req.Spec().Procedure)
		if err != nil {
			return nil, err
		}
		ctx = context.WithValue(ctx, ctxKey{}, info)
		return next(ctx, req)
	}
}

func (a *interceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (a *interceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		info, err := a.authenticate(conn.RequestHeader().Get("Authorization"), conn.Spec().Procedure)
		if err != nil {
			return err
		}
		ctx = context.WithValue(ctx, ctxKey{}, info)
		return next(ctx, conn)
	}
}

func (a *interceptor) authenticate(authHeader, procedure string) (authInfo, error) {
	token := extractBearer(authHeader)
	if token == "" {
		return authInfo{}, connect.NewError(connect.CodeUnauthenticated, nil)
	}

	// 1. Check super token (static from config).
	if st := a.store.GetSuperToken(); st != "" && token == st {
		return authInfo{}, nil // empty ClipID = super
	}

	// 2. Check clip tokens.
	entry, ok := a.store.LookupToken(token)
	if !ok {
		return authInfo{}, connect.NewError(connect.CodeUnauthenticated, nil)
	}

	// Clip tokens can only call ClipService RPCs.
	if !strings.HasPrefix(procedure, "/pinix.v1.ClipService/") {
		return authInfo{}, connect.NewError(connect.CodePermissionDenied, nil)
	}

	return authInfo{ClipID: entry.ClipID}, nil
}

func extractBearer(header string) string {
	const prefix = "Bearer "
	if len(header) > len(prefix) && strings.EqualFold(header[:len(prefix)], prefix) {
		return header[len(prefix):]
	}
	return ""
}
