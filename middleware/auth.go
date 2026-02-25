// Role:    Connect-RPC auth interceptor (Bearer token validation)
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

// NewAuthInterceptor returns a Connect interceptor that validates Bearer tokens.
//
// Rules:
//   - Super Token (ClipID == ""): allows all procedures.
//   - Clip Token (ClipID != ""): only allows ClipService/Command; rejects PinixService.
//   - Missing or invalid token: CodeUnauthenticated.
func NewAuthInterceptor(store *config.Store) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			token := extractBearer(req.Header().Get("Authorization"))
			if token == "" {
				return nil, connect.NewError(connect.CodeUnauthenticated, nil)
			}

			entry, ok := store.GetToken(token)
			if !ok {
				return nil, connect.NewError(connect.CodeUnauthenticated, nil)
			}

			// Clip Token can only call ClipService/Command.
			if entry.ClipID != "" {
				proc := req.Spec().Procedure
				if !strings.HasPrefix(proc, "/pinix.v1.ClipService/") {
					return nil, connect.NewError(connect.CodePermissionDenied, nil)
				}
			}

			ctx = context.WithValue(ctx, ctxKey{}, entry)
			return next(ctx, req)
		}
	}
}

func extractBearer(header string) string {
	const prefix = "Bearer "
	if len(header) > len(prefix) && strings.EqualFold(header[:len(prefix)], prefix) {
		return header[len(prefix):]
	}
	return ""
}
