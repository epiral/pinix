// Role:    Embedded HTTP server for Pinix portal APIs and static assets
// Depends: context, encoding/json, errors, fmt, net, net/http, strings, github.com/epiral/pinix/web
// Exports: Daemon.ServeHTTP

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	portalweb "github.com/epiral/pinix/web"
)

type invokeHTTPRequest struct {
	Clip    string          `json:"clip"`
	Command string          `json:"command"`
	Input   json.RawMessage `json:"input,omitempty"`
	Token   string          `json:"token,omitempty"`
}

func (d *Daemon) ServeHTTP(ctx context.Context, addr string) error {
	if strings.TrimSpace(addr) == "" {
		addr = ":9000"
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}

	server := &http.Server{
		Addr:    addr,
		Handler: d.httpMux(),
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
	}

	d.mu.Lock()
	d.httpServer = server
	d.closed = false
	d.mu.Unlock()

	defer func() {
		_ = d.Close()
	}()

	go func() {
		<-ctx.Done()
		_ = d.Close()
	}()

	if err := server.Serve(listener); err != nil {
		if d.isClosed() || errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve http on %s: %w", addr, err)
	}
	return nil
}

func (d *Daemon) httpMux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", d.handleIndex)
	mux.HandleFunc("/style.css", d.handleStyle)
	mux.HandleFunc("/app.js", d.handleApp)
	mux.HandleFunc("/api/list", d.handleAPIList)
	mux.HandleFunc("/api/invoke", d.handleAPIInvoke)
	mux.HandleFunc("/api/manifest", d.handleAPIManifest)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setCORSHeaders(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (d *Daemon) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	d.serveAsset(w, "index.html", "text/html; charset=utf-8")
}

func (d *Daemon) handleStyle(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/style.css" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	d.serveAsset(w, "style.css", "text/css; charset=utf-8")
}

func (d *Daemon) handleApp(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/app.js" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	d.serveAsset(w, "app.js", "application/javascript; charset=utf-8")
}

func (d *Daemon) handleAPIList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}

	result, err := d.List()
	if err != nil {
		writeJSONError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (d *Daemon) handleAPIInvoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}

	defer r.Body.Close()

	var req invokeHTTPRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeJSONError(w, daemonError{Code: "invalid_argument", Message: fmt.Sprintf("decode request: %v", err)})
		return
	}

	result, err := d.Invoke(r.Context(), requestToken(r, req.Token), req.Clip, req.Command, req.Input)
	if err != nil {
		writeJSONError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (d *Daemon) handleAPIManifest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}

	manifest, err := d.GetManifest(r.Context(), r.URL.Query().Get("clip"))
	if err != nil {
		writeJSONError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, manifest)
}

func (d *Daemon) serveAsset(w http.ResponseWriter, name, contentType string) {
	data, err := portalweb.ReadFile(name)
	if err != nil {
		writeJSONError(w, daemonError{Code: "internal", Message: fmt.Sprintf("read asset %s: %v", name, err)})
		return
	}
	w.Header().Set("Content-Type", contentType)
	_, _ = w.Write(data)
}

func setCORSHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Vary", "Origin")
}

func requestToken(r *http.Request, bodyToken string) string {
	if token := strings.TrimSpace(bodyToken); token != "" {
		return token
	}

	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(header) < 7 || !strings.EqualFold(header[:7], "Bearer ") {
		return ""
	}
	return strings.TrimSpace(header[7:])
}

func writeMethodNotAllowed(w http.ResponseWriter, allowed string) {
	w.Header().Set("Allow", allowed+", OPTIONS")
	writeJSONError(w, daemonError{Code: "method_not_allowed", Message: "method not allowed"})
}

func writeJSONError(w http.ResponseWriter, err error) {
	resp := errorResponseFromError(err)
	status := httpStatusCode(http.StatusInternalServerError, resp.Error)
	writeJSON(w, status, map[string]any{"error": resp.Error})
}

func httpStatusCode(fallback int, respErr *ResponseError) int {
	if respErr == nil {
		return fallback
	}
	switch respErr.Code {
	case "invalid_argument":
		return http.StatusBadRequest
	case "permission_denied":
		return http.StatusForbidden
	case "not_found", "method_not_found":
		return http.StatusNotFound
	case "already_exists":
		return http.StatusConflict
	case "method_not_allowed":
		return http.StatusMethodNotAllowed
	default:
		return fallback
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	if payload == nil {
		payload = struct{}{}
	}

	data, err := json.Marshal(payload)
	if err != nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"code":"internal","message":"marshal response"}}`))
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(append(data, '\n'))
}
