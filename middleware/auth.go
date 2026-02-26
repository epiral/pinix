// Role:    Connect-RPC auth interceptor (Bearer token validation, unary + streaming)
// Depends: internal/config, connectrpc
// Exports: NewAuthInterceptor, TokenFromContext

package middleware

import (
	"context"
	"strings"

	connect "connectrpc.com/connect"
	"github.com/epiral/pinix/internal/config"
)

type ctxKey struct{}

// TokenFromContext extracts the TokenEntry set by the auth interceptor.
func TokenFromContext(ctx context.Context) (config.TokenEntry, bool) {
	v, ok := ctx.Value(ctxKey{}).(config.TokenEntry)
	return v, ok
}

// authInterceptor validates Bearer tokens for both unary and streaming RPCs.
//
// Rules:
//   - Super Token (ClipID == ""): allows all procedures.
//   - Clip Token (ClipID != ""): only allows ClipService RPCs; rejects PinixService.
//   - Missing or invalid token: CodeUnauthenticated.
type authInterceptor struct {
	store *config.Store
}

// NewAuthInterceptor returns a Connect interceptor that validates Bearer tokens.
func NewAuthInterceptor(store *config.Store) connect.Interceptor {
	return &authInterceptor{store: store}
}

func (a *authInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		entry, err := a.authenticate(req.Header().Get("Authorization"), req.Spec().Procedure)
		if err != nil {
			return nil, err
		}
		ctx = context.WithValue(ctx, ctxKey{}, entry)
		return next(ctx, req)
	}
}

func (a *authInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (a *authInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		entry, err := a.authenticate(conn.RequestHeader().Get("Authorization"), conn.Spec().Procedure)
		if err != nil {
			return err
		}
		ctx = context.WithValue(ctx, ctxKey{}, entry)
		return next(ctx, conn)
	}
}

// authenticate validates the Bearer token and checks procedure access.
func (a *authInterceptor) authenticate(authHeader, procedure string) (config.TokenEntry, error) {
	token := extractBearer(authHeader)
	if token == "" {
		return config.TokenEntry{}, connect.NewError(connect.CodeUnauthenticated, nil)
	}

	entry, ok := a.store.GetToken(token)
	if !ok {
		return config.TokenEntry{}, connect.NewError(connect.CodeUnauthenticated, nil)
	}

	// Clip Token can only call ClipService RPCs.
	if entry.ClipID != "" {
		if !strings.HasPrefix(procedure, "/pinix.v1.ClipService/") {
			return config.TokenEntry{}, connect.NewError(connect.CodePermissionDenied, nil)
		}
	}

	return entry, nil
}

func extractBearer(header string) string {
	const prefix = "Bearer "
	if len(header) > len(prefix) && strings.EqualFold(header[:len(prefix)], prefix) {
		return header[len(prefix):]
	}
	return ""
}
