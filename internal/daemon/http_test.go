// Role:    Tests for clip web HTTP serving across local and provider-backed clips
// Depends: context, io, net/http, net/http/httptest, os, path/filepath, testing, time, pinix v2
// Exports: (tests)

package daemon

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	pinixv2 "github.com/epiral/pinix/gen/go/pinix/v2"
)

type stubProviderStream struct {
	incoming chan *pinixv2.ProviderMessage
	outgoing chan *pinixv2.HubMessage
}

type httpTestResponse struct {
	status int
	header http.Header
	body   []byte
}

func newStubProviderStream() *stubProviderStream {
	return &stubProviderStream{
		incoming: make(chan *pinixv2.ProviderMessage, 8),
		outgoing: make(chan *pinixv2.HubMessage, 8),
	}
}

func (s *stubProviderStream) Receive() (*pinixv2.ProviderMessage, error) {
	message, ok := <-s.incoming
	if !ok {
		return nil, io.EOF
	}
	return message, nil
}

func (s *stubProviderStream) Send(message *pinixv2.HubMessage) error {
	s.outgoing <- message
	return nil
}

func TestServeClipWebFileLocalSupportsRangeAndIfNoneMatch(t *testing.T) {
	registry := newTestRegistry(t)
	clipDir := t.TempDir()
	content := []byte("body { color: red; }\n")
	if err := os.MkdirAll(filepath.Join(clipDir, "web"), 0o755); err != nil {
		t.Fatalf("mkdir web: %v", err)
	}
	if err := os.WriteFile(filepath.Join(clipDir, "web", "style.css"), content, 0o644); err != nil {
		t.Fatalf("write style.css: %v", err)
	}
	if err := registry.PutClip(ClipConfig{Name: "todo-web", Path: clipDir}); err != nil {
		t.Fatalf("put clip: %v", err)
	}

	daemon := &Daemon{registry: registry, process: &ProcessManager{}, provider: NewProviderManager(registry)}
	handler := daemon.httpMux()

	rangeResp := performRequest(handler, httptest.NewRequest(http.MethodGet, "/clips/todo-web/style.css", nil), func(req *http.Request) {
		req.Header.Set("Range", "bytes=5-10")
	})
	if rangeResp.status != http.StatusPartialContent {
		t.Fatalf("range status = %d, want %d", rangeResp.status, http.StatusPartialContent)
	}
	if got, want := string(rangeResp.body), string(content[5:11]); got != want {
		t.Fatalf("range body = %q, want %q", got, want)
	}
	if got, want := rangeResp.header.Get("Accept-Ranges"), "bytes"; got != want {
		t.Fatalf("accept-ranges = %q, want %q", got, want)
	}
	if got, want := rangeResp.header.Get("Content-Range"), "bytes 5-10/21"; got != want {
		t.Fatalf("content-range = %q, want %q", got, want)
	}
	etag := rangeResp.header.Get("ETag")
	if got, want := etag, makeETag(content); got != want {
		t.Fatalf("etag = %q, want %q", got, want)
	}
	if got := rangeResp.header.Get("Content-Type"); got == "" {
		t.Fatalf("content-type is empty")
	}

	cacheResp := performRequest(handler, httptest.NewRequest(http.MethodGet, "/clips/todo-web/style.css", nil), func(req *http.Request) {
		req.Header.Set("If-None-Match", etag)
	})
	if cacheResp.status != http.StatusNotModified {
		t.Fatalf("cache status = %d, want %d", cacheResp.status, http.StatusNotModified)
	}
	if len(cacheResp.body) != 0 {
		t.Fatalf("cache body = %q, want empty", string(cacheResp.body))
	}
	if got := cacheResp.header.Get("ETag"); got != etag {
		t.Fatalf("cache etag = %q, want %q", got, etag)
	}
}

func TestServeClipWebFileProviderSupportsRangeAndIfNoneMatch(t *testing.T) {
	registry := newTestRegistry(t)
	daemon := &Daemon{registry: registry, provider: NewProviderManager(nil)}
	stream := startTestProviderSession(t, daemon.provider, "remote-runtime", "todo-web")
	handler := daemon.httpMux()

	responseCh := performRequestAsync(handler, httptest.NewRequest(http.MethodGet, "/clips/todo-web/style.css", nil), func(req *http.Request) {
		req.Header.Set("Range", "bytes=2-4")
	})

	message := waitHubMessage(t, stream.outgoing)
	command := message.GetGetClipWebCommand()
	if command == nil {
		t.Fatalf("expected get_clip_web_command, got %#v", message.GetPayload())
	}
	if command.GetClipName() != "todo-web" {
		t.Fatalf("clip name = %q, want %q", command.GetClipName(), "todo-web")
	}
	if command.GetPath() != "style.css" {
		t.Fatalf("path = %q, want %q", command.GetPath(), "style.css")
	}
	if command.GetOffset() != 2 || command.GetLength() != 3 {
		t.Fatalf("range = (%d, %d), want (2, 3)", command.GetOffset(), command.GetLength())
	}
	if command.GetIfNoneMatch() != "" {
		t.Fatalf("if-none-match = %q, want empty", command.GetIfNoneMatch())
	}

	stream.incoming <- &pinixv2.ProviderMessage{Payload: &pinixv2.ProviderMessage_GetClipWebResult{GetClipWebResult: &pinixv2.GetClipWebResult{
		RequestId:   command.GetRequestId(),
		Content:     []byte("dy "),
		ContentType: "text/css; charset=utf-8",
		Etag:        "\"provider-etag\"",
		TotalSize:   20,
	}}}

	rangeResp := waitHTTPResponse(t, responseCh)
	if rangeResp.status != http.StatusPartialContent {
		t.Fatalf("provider range status = %d, want %d", rangeResp.status, http.StatusPartialContent)
	}
	if got, want := string(rangeResp.body), "dy "; got != want {
		t.Fatalf("provider range body = %q, want %q", got, want)
	}
	if got, want := rangeResp.header.Get("Content-Range"), "bytes 2-4/20"; got != want {
		t.Fatalf("provider content-range = %q, want %q", got, want)
	}
	if got, want := rangeResp.header.Get("ETag"), "\"provider-etag\""; got != want {
		t.Fatalf("provider etag = %q, want %q", got, want)
	}
	if got, want := rangeResp.header.Get("Accept-Ranges"), "bytes"; got != want {
		t.Fatalf("provider accept-ranges = %q, want %q", got, want)
	}

	cacheCh := performRequestAsync(handler, httptest.NewRequest(http.MethodGet, "/clips/todo-web/style.css", nil), func(req *http.Request) {
		req.Header.Set("If-None-Match", "\"provider-etag\"")
	})

	message = waitHubMessage(t, stream.outgoing)
	command = message.GetGetClipWebCommand()
	if command == nil {
		t.Fatalf("expected cache get_clip_web_command, got %#v", message.GetPayload())
	}
	if got, want := command.GetIfNoneMatch(), "\"provider-etag\""; got != want {
		t.Fatalf("provider if-none-match = %q, want %q", got, want)
	}

	stream.incoming <- &pinixv2.ProviderMessage{Payload: &pinixv2.ProviderMessage_GetClipWebResult{GetClipWebResult: &pinixv2.GetClipWebResult{
		RequestId:   command.GetRequestId(),
		ContentType: "text/css; charset=utf-8",
		Etag:        "\"provider-etag\"",
		TotalSize:   20,
		NotModified: true,
	}}}

	cacheResp := waitHTTPResponse(t, cacheCh)
	if cacheResp.status != http.StatusNotModified {
		t.Fatalf("provider cache status = %d, want %d", cacheResp.status, http.StatusNotModified)
	}
	if len(cacheResp.body) != 0 {
		t.Fatalf("provider cache body = %q, want empty", string(cacheResp.body))
	}
	if got, want := cacheResp.header.Get("ETag"), "\"provider-etag\""; got != want {
		t.Fatalf("provider cache etag = %q, want %q", got, want)
	}
}

func newTestRegistry(t *testing.T) *Registry {
	t.Helper()
	registry, err := NewRegistry(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	return registry
}

func startTestProviderSession(t *testing.T, manager *ProviderManager, providerName, clipName string) *stubProviderStream {
	t.Helper()
	stream := newStubProviderStream()
	done := make(chan error, 1)
	go func() {
		done <- manager.HandleStream(context.Background(), stream)
	}()

	stream.incoming <- &pinixv2.ProviderMessage{Payload: &pinixv2.ProviderMessage_Register{Register: &pinixv2.RegisterRequest{
		ProviderName: providerName,
		Clips: []*pinixv2.ClipRegistration{{
			Name:   clipName,
			HasWeb: true,
		}},
	}}}

	message := waitHubMessage(t, stream.outgoing)
	response := message.GetRegisterResponse()
	if response == nil {
		t.Fatalf("expected register response, got %#v", message.GetPayload())
	}
	if !response.GetAccepted() {
		t.Fatalf("provider register rejected: %s", response.GetMessage())
	}

	t.Cleanup(func() {
		close(stream.incoming)
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("provider stream error: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for provider stream shutdown")
		}
	})

	return stream
}

func performRequest(handler http.Handler, request *http.Request, mutate func(*http.Request)) httpTestResponse {
	if mutate != nil {
		mutate(request)
	}
	return waitHTTPResponse(nil, performRequestAsync(handler, request, nil))
}

func performRequestAsync(handler http.Handler, request *http.Request, mutate func(*http.Request)) <-chan httpTestResponse {
	if mutate != nil {
		mutate(request)
	}
	results := make(chan httpTestResponse, 1)
	go func() {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		response := recorder.Result()
		body, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		results <- httpTestResponse{status: response.StatusCode, header: response.Header.Clone(), body: body}
	}()
	return results
}

func waitHubMessage(t *testing.T, messages <-chan *pinixv2.HubMessage) *pinixv2.HubMessage {
	t.Helper()
	select {
	case message := <-messages:
		if message == nil {
			t.Fatalf("received nil hub message")
		}
		return message
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for hub message")
		return nil
	}
}

func waitHTTPResponse(t *testing.T, responses <-chan httpTestResponse) httpTestResponse {
	if t != nil {
		t.Helper()
	}
	select {
	case response := <-responses:
		return response
	case <-time.After(2 * time.Second):
		if t != nil {
			t.Fatalf("timed out waiting for http response")
		}
		return httpTestResponse{}
	}
}
