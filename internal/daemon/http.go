// Role:    Embedded HTTP server for the Pinix portal, Connect-RPC, clip web files, and JSON errors
// Depends: bytes, context, encoding/json, errors, fmt, io, net, net/http, strings, time, connectrpc, pinix v2, pinixv2connect, github.com/epiral/pinix/web
// Exports: Daemon.ServeHTTP

package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	connect "connectrpc.com/connect"
	pinixv2 "github.com/epiral/pinix/gen/go/pinix/v2"
	"github.com/epiral/pinix/gen/go/pinix/v2/pinixv2connect"
	portalweb "github.com/epiral/pinix/web"
)

func (d *Daemon) ServeHTTP(ctx context.Context, addr string) error {
	if strings.TrimSpace(addr) == "" {
		addr = ":9000"
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}

	var protocols http.Protocols
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)

	server := &http.Server{
		Addr:              addr,
		Handler:           d.httpMux(),
		Protocols:         &protocols,
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
	mux.HandleFunc("/clips/", d.handleClipWeb)

	// Serve Vite build output from embedded dist/ directory
	distFS, err := portalweb.DistFS()
	if err != nil {
		// Fallback: if dist/ cannot be resolved, serve a plain error
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			writeJSONError(w, daemonError{Code: "internal", Message: "portal assets not available"})
		})
	} else {
		fileServer := http.FileServer(http.FS(distFS))
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				writeMethodNotAllowed(w, http.MethodGet)
				return
			}
			// Root or SPA fallback: serve index.html directly (avoid FileServer redirect loop)
			path := strings.TrimPrefix(r.URL.Path, "/")
			if path == "" || !strings.Contains(path, ".") {
				http.ServeFileFS(w, r, distFS, "index.html")
				return
			}
			fileServer.ServeHTTP(w, r)
		})
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setCORSHeaders(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (d *Daemon) handleClipWeb(w http.ResponseWriter, r *http.Request) {
	clipName, filePath, ok := parseClipWebPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}

	if filePath == "" && !strings.HasSuffix(r.URL.Path, "/") {
		d.redirectClipWebRoot(w, r, clipName)
		return
	}

	if r.Method == http.MethodPost {
		if command, ok := parseClipAPIPath(filePath); ok {
			d.handleClipWebInvoke(w, r, clipName, command)
			return
		}
	}

	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}

	d.serveClipWebFile(w, r, clipName, filePath)
}

func (d *Daemon) redirectClipWebRoot(w http.ResponseWriter, r *http.Request, clipName string) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}

	found, err := d.hasClip(clipName)
	if err != nil {
		writeJSONError(w, err)
		return
	}
	if !found {
		http.NotFound(w, r)
		return
	}

	location := "/clips/" + clipName + "/"
	if r.URL.RawQuery != "" {
		location += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, location, http.StatusMovedPermanently)
}

func (d *Daemon) handleClipWebInvoke(w http.ResponseWriter, r *http.Request, clipName, command string) {
	input, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONError(w, daemonError{Code: "internal", Message: fmt.Sprintf("read invoke body: %v", err)})
		return
	}
	input = bytes.TrimSpace(input)
	if len(input) == 0 {
		input = []byte(`{}`)
	}
	if !json.Valid(input) {
		writeJSONError(w, daemonError{Code: "invalid_argument", Message: "request body must be valid JSON"})
		return
	}

	// Check if client wants SSE streaming
	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "text/event-stream") {
		d.handleClipWebInvokeSSE(w, r, clipName, command, json.RawMessage(input))
		return
	}

	hub := NewHubService(d)
	output, err := hub.invokeCollect(r.Context(), clipName, command, input)
	if err != nil {
		writeJSONError(w, err)
		return
	}
	writeJSONBytes(w, http.StatusOK, output)
}

func (d *Daemon) handleClipWebInvokeSSE(w http.ResponseWriter, r *http.Request, clipName, command string, input json.RawMessage) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONError(w, daemonError{Code: "internal", Message: "streaming not supported"})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	hub := NewHubService(d)
	err := hub.invokeWithCallback(r.Context(), clipName, command, input, func(chunk json.RawMessage) {
		// If chunk is a JSON string (e.g. "\"...\n\""), unwrap it into raw lines
		// and emit each JSONL line as a separate SSE event
		raw := bytes.TrimSpace(chunk)
		if len(raw) > 0 && raw[0] == '"' {
			var s string
			if json.Unmarshal(raw, &s) == nil {
				for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
					line = strings.TrimSpace(line)
					if line != "" {
						fmt.Fprintf(w, "data: %s\n\n", line)
					}
				}
				flusher.Flush()
				return
			}
		}
		fmt.Fprintf(w, "data: %s\n\n", raw)
		flusher.Flush()
	})

	if err != nil {
		errJSON, _ := json.Marshal(map[string]string{"error": err.Error()})
		fmt.Fprintf(w, "data: %s\n\n", errJSON)
		flusher.Flush()
		return
	}

	fmt.Fprintf(w, "event: done\ndata: {}\n\n")
	flusher.Flush()
}

func (d *Daemon) serveClipWebFile(w http.ResponseWriter, r *http.Request, clipName, filePath string) {
	rangeReq, err := parseHTTPRangeHeader(r.Header.Get("Range"))
	if err != nil {
		writeJSONError(w, err)
		return
	}

	resp, err := NewHubService(d).GetClipWeb(r.Context(), connect.NewRequest(&pinixv2.GetClipWebRequest{
		ClipName:    clipName,
		Path:        filePath,
		Offset:      rangeReq.Offset,
		Length:      rangeReq.Length,
		IfNoneMatch: r.Header.Get("If-None-Match"),
	}))
	if err != nil {
		err = daemonErrorFromConnect(err)
		if isDaemonCode(err, "not_found") {
			http.NotFound(w, r)
			return
		}
		writeJSONError(w, err)
		return
	}

	result := resp.Msg
	w.Header().Set("Accept-Ranges", "bytes")
	if contentType := strings.TrimSpace(result.GetContentType()); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	if etag := strings.TrimSpace(result.GetEtag()); etag != "" {
		w.Header().Set("ETag", etag)
	}
	if result.GetNotModified() {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	status := http.StatusOK
	content := result.GetContent()
	if rangeReq.Partial {
		status = http.StatusPartialContent
		if result.GetTotalSize() > 0 {
			end := rangeReq.Offset + int64(len(content)) - 1
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", rangeReq.Offset, end, result.GetTotalSize()))
		}
	}
	w.WriteHeader(status)
	_, _ = w.Write(content)
}

func (d *Daemon) hasClip(name string) (bool, error) {
	if d == nil {
		return false, daemonError{Code: "internal", Message: "daemon is not configured"}
	}
	_, found, err := d.registry.GetClip(strings.TrimSpace(name))
	if err != nil {
		return false, daemonError{Code: "internal", Message: fmt.Sprintf("load clip %q: %v", name, err)}
	}
	if found {
		return true, nil
	}
	return d.provider != nil && d.provider.HasClip(name), nil
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

func parseClipAPIPath(filePath string) (command string, ok bool) {
	trimmed := strings.Trim(strings.TrimSpace(filePath), "/")
	if trimmed == "" {
		return "", false
	}
	parts := strings.Split(trimmed, "/")
	if len(parts) != 2 || parts[0] != "api" {
		return "", false
	}
	command = strings.TrimSpace(parts[1])
	if command == "" {
		return "", false
	}
	return command, true
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
	case "unauthenticated":
		return http.StatusUnauthorized
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
	case "unavailable", "closed":
		return http.StatusServiceUnavailable
	default:
		return fallback
	}
}

func daemonErrorFromConnect(err error) error {
	if err == nil {
		return nil
	}
	var connectErr *connect.Error
	if !errors.As(err, &connectErr) {
		return err
	}
	return daemonError{Code: daemonCodeFromConnectCode(connectErr.Code()), Message: strings.TrimSpace(connectErr.Message())}
}

func daemonCodeFromConnectCode(code connect.Code) string {
	switch code {
	case connect.CodeInvalidArgument:
		return "invalid_argument"
	case connect.CodeUnauthenticated:
		return "unauthenticated"
	case connect.CodePermissionDenied:
		return "permission_denied"
	case connect.CodeNotFound:
		return "not_found"
	case connect.CodeAlreadyExists:
		return "already_exists"
	case connect.CodeDeadlineExceeded:
		return "timeout"
	case connect.CodeCanceled:
		return "canceled"
	case connect.CodeUnavailable:
		return "unavailable"
	case connect.CodeUnimplemented:
		return "unimplemented"
	default:
		return "internal"
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

func writeJSONBytes(w http.ResponseWriter, status int, payload []byte) {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 {
		trimmed = []byte(`{}`)
	}
	if !json.Valid(trimmed) {
		writeJSONError(w, daemonError{Code: "internal", Message: "clip returned invalid JSON"})
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(append(trimmed, '\n'))
}
