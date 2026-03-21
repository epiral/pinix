// Role:    Embedded HTTP server for the Pinix portal, Connect-RPC, clip web files, and JSON errors
// Depends: context, encoding/json, errors, fmt, mime, net, net/http, os, path/filepath, strings, time, pinixv2connect, github.com/epiral/pinix/web, http2, h2c
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
	"time"

	"github.com/epiral/pinix/gen/go/pinix/v2/pinixv2connect"
	portalweb "github.com/epiral/pinix/web"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

func (d *Daemon) ServeHTTP(ctx context.Context, addr string) error {
	if strings.TrimSpace(addr) == "" {
		addr = ":9000"
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}

	server := &http.Server{
		Addr:              addr,
		Handler:           h2c.NewHandler(d.httpMux(), &http2.Server{}),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
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
	hubPath, hubHandler := pinixv2connect.NewHubServiceHandler(NewHubService(d))
	mux.Handle(hubPath, hubHandler)
	mux.HandleFunc("/", d.handleIndex)
	mux.HandleFunc("/style.css", d.handleStyle)
	mux.HandleFunc("/app.js", d.handleApp)
	mux.HandleFunc("/clips/", d.handleClipWeb)

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

func setCORSHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Connect-Protocol-Version")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Vary", "Origin")
}

func writeMethodNotAllowed(w http.ResponseWriter, allowed string) {
	w.Header().Set("Allow", allowed+", OPTIONS")
	writeJSONError(w, daemonError{Code: "method_not_allowed", Message: "method not allowed"})
}

func writeJSONError(w http.ResponseWriter, err error) {
	respErr := responseErrorFromErr(err)
	if respErr == nil {
		respErr = &ResponseError{Code: "internal", Message: "internal error"}
	}
	status := httpStatusCode(http.StatusInternalServerError, respErr)
	writeJSON(w, status, map[string]any{"error": respErr})
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
