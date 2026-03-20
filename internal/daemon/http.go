// Role:    Embedded HTTP server for Pinix portal APIs, capability WebSockets, clip web files, and static assets
// Depends: context, encoding/json, errors, fmt, mime, net, net/http, os, path/filepath, strings, github.com/epiral/pinix/web, golang.org/x/net/websocket
// Exports: Daemon.ServeHTTP

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	portalweb "github.com/epiral/pinix/web"
	"golang.org/x/net/websocket"
)

type invokeHTTPRequest struct {
	Clip    string          `json:"clip"`
	Command string          `json:"command"`
	Input   json.RawMessage `json:"input,omitempty"`
	Token   string          `json:"token,omitempty"`
}

type capabilityInvokeHTTPRequest struct {
	Capability string          `json:"capability"`
	Command    string          `json:"command"`
	Input      json.RawMessage `json:"input,omitempty"`
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
	mux.HandleFunc("/clips/", d.handleClipWeb)
	mux.HandleFunc("/api/list", d.handleAPIList)
	mux.HandleFunc("/api/capabilities", d.handleAPICapabilities)
	mux.HandleFunc("/api/invoke", d.handleAPIInvoke)
	mux.HandleFunc("/api/capability/invoke", d.handleAPICapabilityInvoke)
	mux.HandleFunc("/api/manifest", d.handleAPIManifest)
	mux.Handle("/ws/capability", d.capabilityWebSocketHandler())

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

func (d *Daemon) handleClipWeb(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}

	clipName, filePath, ok := parseClipWebPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}

	clip, found, err := d.registry.GetClip(clipName)
	if err != nil {
		writeJSONError(w, daemonError{Code: "internal", Message: fmt.Sprintf("load clip %q: %v", clipName, err)})
		return
	}
	if !found {
		http.NotFound(w, r)
		return
	}

	webRoot := filepath.Clean(filepath.Join(clip.Path, "web"))
	requestedPath := filepath.Clean(strings.TrimPrefix(filePath, "/"))
	if requestedPath == "." {
		requestedPath = ""
	}

	targetPath := filepath.Clean(filepath.Join(webRoot, requestedPath))
	if !isWithinDir(targetPath, webRoot) {
		http.NotFound(w, r)
		return
	}

	if requestedPath == "" {
		targetPath = filepath.Join(webRoot, "index.html")
	} else {
		info, err := os.Stat(targetPath)
		if err != nil {
			if os.IsNotExist(err) {
				http.NotFound(w, r)
				return
			}
			writeJSONError(w, daemonError{Code: "internal", Message: fmt.Sprintf("stat clip web file %q: %v", targetPath, err)})
			return
		}
		if info.IsDir() {
			targetPath = filepath.Join(targetPath, "index.html")
		}
	}

	data, err := os.ReadFile(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.NotFound(w, r)
			return
		}
		writeJSONError(w, daemonError{Code: "internal", Message: fmt.Sprintf("read clip web file %q: %v", targetPath, err)})
		return
	}

	if contentType := mime.TypeByExtension(filepath.Ext(targetPath)); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	_, _ = w.Write(data)
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

func (d *Daemon) handleAPICapabilities(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}

	result, err := d.ListCapabilities()
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

func (d *Daemon) handleAPICapabilityInvoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}

	defer r.Body.Close()

	var req capabilityInvokeHTTPRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeJSONError(w, daemonError{Code: "invalid_argument", Message: fmt.Sprintf("decode request: %v", err)})
		return
	}

	result, err := d.InvokeCapability(r.Context(), req.Capability, req.Command, req.Input)
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

func parseClipWebPath(requestPath string) (clipName, filePath string, ok bool) {
	trimmed := strings.TrimPrefix(requestPath, "/clips/")
	if trimmed == requestPath {
		return "", "", false
	}

	parts := strings.SplitN(trimmed, "/", 2)
	clipName = strings.TrimSpace(parts[0])
	if clipName == "" {
		return "", "", false
	}
	if len(parts) == 2 {
		filePath = parts[1]
	}
	return clipName, filePath, true
}

func isWithinDir(path, base string) bool {
	path = filepath.Clean(path)
	base = filepath.Clean(base)
	if path == base {
		return true
	}
	return strings.HasPrefix(path, base+string(filepath.Separator))
}

func (d *Daemon) capabilityWebSocketHandler() http.Handler {
	server := websocket.Server{
		Handshake: func(*websocket.Config, *http.Request) error {
			return nil
		},
		Handler: websocket.Handler(func(conn *websocket.Conn) {
			if d.capability == nil {
				_ = conn.Close()
				return
			}
			_ = d.capability.HandleConnection(conn)
		}),
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws/capability" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		server.ServeHTTP(w, r)
	})
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
	switch strings.ToLower(respErr.Code) {
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
	case "timeout":
		return http.StatusGatewayTimeout
	case "canceled", "cancelled":
		return http.StatusRequestTimeout
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
