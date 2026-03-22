// Role:    Shared helpers for clip web asset resolution, byte-range parsing, and ETag matching
// Depends: fmt, mime, net/http, os, path/filepath, strconv, strings
// Exports: (package-internal helpers)

package daemon

import (
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type clipWebReadOptions struct {
	Offset      int64
	Length      int64
	IfNoneMatch string
}

type clipWebReadResult struct {
	Content     []byte
	ContentType string
	ETag        string
	TotalSize   int64
	NotModified bool
}

type clipWebRangeRequest struct {
	Offset  int64
	Length  int64
	Partial bool
}

func readClipWebFile(webRoot, requestedPath string, opts clipWebReadOptions) (*clipWebReadResult, error) {
	webRoot = filepath.Clean(strings.TrimSpace(webRoot))
	requestedPath = filepath.Clean(strings.TrimPrefix(strings.TrimSpace(requestedPath), "/"))
	if requestedPath == "." {
		requestedPath = ""
	}

	targetPath := filepath.Clean(filepath.Join(webRoot, requestedPath))
	if !isWithinDir(targetPath, webRoot) {
		return nil, daemonError{Code: "not_found", Message: "clip web asset not found"}
	}

	if requestedPath == "" {
		targetPath = filepath.Join(webRoot, "index.html")
	} else {
		info, err := os.Stat(targetPath)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, daemonError{Code: "not_found", Message: "clip web asset not found"}
			}
			return nil, daemonError{Code: "internal", Message: fmt.Sprintf("stat clip web file %q: %v", targetPath, err)}
		}
		if info.IsDir() {
			targetPath = filepath.Join(targetPath, "index.html")
		}
	}

	data, err := os.ReadFile(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, daemonError{Code: "not_found", Message: "clip web asset not found"}
		}
		return nil, daemonError{Code: "internal", Message: fmt.Sprintf("read clip web file %q: %v", targetPath, err)}
	}

	contentType := mime.TypeByExtension(filepath.Ext(targetPath))
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	etag := makeETag(data)
	result := &clipWebReadResult{
		ContentType: contentType,
		ETag:        etag,
		TotalSize:   int64(len(data)),
	}
	if etagMatches(opts.IfNoneMatch, etag) {
		result.NotModified = true
		return result, nil
	}

	start, end, err := resolveClipWebSlice(opts.Offset, opts.Length, int64(len(data)))
	if err != nil {
		return nil, err
	}
	result.Content = cloneBytes(data[start:end])
	return result, nil
}

func resolveClipWebSlice(offset, length, totalSize int64) (int64, int64, error) {
	if offset < 0 {
		return 0, 0, daemonError{Code: "invalid_argument", Message: "offset must be >= 0"}
	}
	if length < 0 {
		return 0, 0, daemonError{Code: "invalid_argument", Message: "length must be >= 0"}
	}
	if offset > totalSize {
		return 0, 0, daemonError{Code: "invalid_argument", Message: "offset must be <= file size"}
	}
	if length == 0 {
		return offset, totalSize, nil
	}
	end := offset + length
	if end < offset {
		return 0, 0, daemonError{Code: "invalid_argument", Message: "offset + length overflow"}
	}
	if end > totalSize {
		end = totalSize
	}
	return offset, end, nil
}

func parseHTTPRangeHeader(value string) (clipWebRangeRequest, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return clipWebRangeRequest{}, nil
	}

	unit, spec, ok := strings.Cut(value, "=")
	if !ok || !strings.EqualFold(strings.TrimSpace(unit), "bytes") {
		return clipWebRangeRequest{}, daemonError{Code: "invalid_argument", Message: "range must use bytes unit"}
	}
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return clipWebRangeRequest{}, daemonError{Code: "invalid_argument", Message: "range is required"}
	}
	if strings.Contains(spec, ",") {
		return clipWebRangeRequest{}, daemonError{Code: "invalid_argument", Message: "multiple ranges are not supported"}
	}

	startRaw, endRaw, ok := strings.Cut(spec, "-")
	if !ok {
		return clipWebRangeRequest{}, daemonError{Code: "invalid_argument", Message: "range must use start-end format"}
	}
	startRaw = strings.TrimSpace(startRaw)
	endRaw = strings.TrimSpace(endRaw)
	if startRaw == "" {
		return clipWebRangeRequest{}, daemonError{Code: "invalid_argument", Message: "suffix ranges are not supported"}
	}

	start, err := strconv.ParseInt(startRaw, 10, 64)
	if err != nil || start < 0 {
		return clipWebRangeRequest{}, daemonError{Code: "invalid_argument", Message: "range start must be a non-negative integer"}
	}
	if endRaw == "" {
		return clipWebRangeRequest{Offset: start, Length: 0, Partial: true}, nil
	}

	end, err := strconv.ParseInt(endRaw, 10, 64)
	if err != nil || end < 0 {
		return clipWebRangeRequest{}, daemonError{Code: "invalid_argument", Message: "range end must be a non-negative integer"}
	}
	if end < start {
		return clipWebRangeRequest{}, daemonError{Code: "invalid_argument", Message: "range end must be >= start"}
	}

	return clipWebRangeRequest{Offset: start, Length: end - start + 1, Partial: true}, nil
}

func etagMatches(ifNoneMatch, etag string) bool {
	etag = strings.TrimSpace(etag)
	if etag == "" {
		return false
	}
	for _, candidate := range strings.Split(ifNoneMatch, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || candidate == etag {
			return true
		}
	}
	return false
}
