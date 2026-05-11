// Role:    File I/O handler for Clip Data operations (read, write, list, delete, stat)
// Depends: fmt, mime, os, path, path/filepath, strings, time
// Exports: handleDataOperation

package daemon

import (
	"encoding/base64"
	"fmt"
	"mime"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	pinixv2 "github.com/epiral/pinix/gen/go/pinix/v2"
)

// handleDataOperation performs a file I/O operation on the clip's data directory.
// It does not start any clip process — data operations are handled directly by pinixd.
func handleDataOperation(registry *Registry, clipName, operation, dataPath string, content []byte, mimeType string) *pinixv2.DataResponse {
	clipName = strings.TrimSpace(clipName)
	if clipName == "" {
		return dataErrorResponse("invalid_argument", "clip_name is required")
	}
	operation = strings.TrimSpace(strings.ToLower(operation))
	if operation == "" {
		return dataErrorResponse("invalid_argument", "operation is required")
	}

	// Validate the path to prevent directory traversal.
	dataPath = strings.TrimSpace(dataPath)
	if err := validateDataPath(dataPath, operation); err != nil {
		return dataErrorResponse("invalid_argument", err.Error())
	}

	dataDir := registry.ClipDataDir(clipName)

	switch operation {
	case "read":
		return handleDataRead(dataDir, clipName, dataPath)
	case "write":
		return handleDataWrite(dataDir, clipName, dataPath, content, mimeType)
	case "list":
		return handleDataList(dataDir, clipName, dataPath)
	case "delete":
		return handleDataDelete(dataDir, clipName, dataPath)
	case "stat":
		return handleDataStat(dataDir, clipName, dataPath)
	default:
		return dataErrorResponse("invalid_argument", fmt.Sprintf("unsupported operation %q; supported: read, write, list, delete, stat", operation))
	}
}

func handleDataRead(dataDir, clipName, dataPath string) *pinixv2.DataResponse {
	fullPath := filepath.Join(dataDir, filepath.FromSlash(dataPath))
	data, err := os.ReadFile(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return dataErrorResponse("not_found", fmt.Sprintf("file %q not found in clip %q", dataPath, clipName))
		}
		return dataErrorResponse("internal", fmt.Sprintf("read file: %v", err))
	}
	return &pinixv2.DataResponse{Content: data}
}

func handleDataWrite(dataDir, clipName, dataPath string, content []byte, mimeType string) *pinixv2.DataResponse {
	fullPath := filepath.Join(dataDir, filepath.FromSlash(dataPath))

	// Auto-create parent directories.
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return dataErrorResponse("internal", fmt.Sprintf("create directory: %v", err))
	}

	if err := os.WriteFile(fullPath, content, 0o644); err != nil {
		return dataErrorResponse("internal", fmt.Sprintf("write file: %v", err))
	}

	uri := fmt.Sprintf("pinix://%s/%s", clipName, dataPath)
	return &pinixv2.DataResponse{Uri: uri}
}

func handleDataList(dataDir, clipName, dataPath string) *pinixv2.DataResponse {
	listPath := dataDir
	if dataPath != "" {
		listPath = filepath.Join(dataDir, filepath.FromSlash(dataPath))
	}

	entries, err := os.ReadDir(listPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Return empty list for nonexistent directories (not an error).
			return &pinixv2.DataResponse{Entries: []*pinixv2.DataEntry{}}
		}
		return dataErrorResponse("internal", fmt.Sprintf("list directory: %v", err))
	}

	result := make([]*pinixv2.DataEntry, 0, len(entries))
	for _, entry := range entries {
		entryType := "file"
		if entry.IsDir() {
			entryType = "directory"
		}

		entryPath := path.Join(dataPath, entry.Name())
		uri := fmt.Sprintf("pinix://%s/%s", clipName, entryPath)

		var size int64
		var entryMime string
		if info, err := entry.Info(); err == nil {
			size = info.Size()
		}
		if !entry.IsDir() {
			entryMime = mimeFromExtension(entry.Name())
		}

		result = append(result, &pinixv2.DataEntry{
			Name: entry.Name(),
			Path: uri,
			Type: entryType,
			Size: size,
			Mime: entryMime,
		})
	}
	return &pinixv2.DataResponse{Entries: result}
}

func handleDataDelete(dataDir, clipName, dataPath string) *pinixv2.DataResponse {
	fullPath := filepath.Join(dataDir, filepath.FromSlash(dataPath))
	if _, err := os.Stat(fullPath); err != nil {
		if os.IsNotExist(err) {
			return dataErrorResponse("not_found", fmt.Sprintf("file %q not found in clip %q", dataPath, clipName))
		}
		return dataErrorResponse("internal", fmt.Sprintf("stat file: %v", err))
	}

	if err := os.Remove(fullPath); err != nil {
		return dataErrorResponse("internal", fmt.Sprintf("delete file: %v", err))
	}

	uri := fmt.Sprintf("pinix://%s/%s", clipName, dataPath)
	return &pinixv2.DataResponse{Uri: uri}
}

func handleDataStat(dataDir, clipName, dataPath string) *pinixv2.DataResponse {
	fullPath := filepath.Join(dataDir, filepath.FromSlash(dataPath))
	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return dataErrorResponse("not_found", fmt.Sprintf("file %q not found in clip %q", dataPath, clipName))
		}
		return dataErrorResponse("internal", fmt.Sprintf("stat file: %v", err))
	}

	return &pinixv2.DataResponse{
		Stat: &pinixv2.DataStat{
			Size:     info.Size(),
			Mime:     mimeFromExtension(info.Name()),
			Modified: info.ModTime().UTC().Format(time.RFC3339),
		},
	}
}

// validateDataPath ensures the path is safe: no absolute paths, no ".." traversal.
func validateDataPath(dataPath string, operation string) error {
	if operation == "list" && dataPath == "" {
		return nil // list root is ok
	}
	if dataPath == "" {
		return fmt.Errorf("path is required")
	}
	if filepath.IsAbs(dataPath) {
		return fmt.Errorf("path must be relative, got %q", dataPath)
	}
	// Clean and check for ".." traversal.
	cleaned := filepath.Clean(filepath.FromSlash(dataPath))
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path %q escapes data directory", dataPath)
	}
	return nil
}

// mimeFromExtension guesses a MIME type from the file extension.
func mimeFromExtension(name string) string {
	ext := filepath.Ext(name)
	if ext == "" {
		return "application/octet-stream"
	}
	mimeType := mime.TypeByExtension(ext)
	if mimeType == "" {
		return "application/octet-stream"
	}
	return mimeType
}

func base64Encode(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

func base64Decode(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}

func dataErrorResponse(code, message string) *pinixv2.DataResponse {
	return &pinixv2.DataResponse{
		Error: &pinixv2.HubError{Code: code, Message: message},
	}
}

// dataResponseToResult converts a DataResponse to a DataResult (for provider stream forwarding).
func dataResponseToResult(requestID string, resp *pinixv2.DataResponse) *pinixv2.DataResult {
	if resp == nil {
		return &pinixv2.DataResult{RequestId: requestID}
	}
	result := &pinixv2.DataResult{
		RequestId: requestID,
		Content:   cloneBytes(resp.GetContent()),
		Uri:       resp.GetUri(),
	}
	if entries := resp.GetEntries(); len(entries) > 0 {
		result.Entries = make([]*pinixv2.DataEntry, len(entries))
		copy(result.Entries, entries)
	}
	if stat := resp.GetStat(); stat != nil {
		result.Stat = &pinixv2.DataStat{
			Size:     stat.GetSize(),
			Mime:     stat.GetMime(),
			Modified: stat.GetModified(),
		}
	}
	if hubErr := resp.GetError(); hubErr != nil {
		result.Error = &pinixv2.HubError{Code: hubErr.GetCode(), Message: hubErr.GetMessage()}
	}
	return result
}

// dataResultToResponse converts a DataResult (from provider stream) to a DataResponse.
func dataResultToResponse(result *pinixv2.DataResult) *pinixv2.DataResponse {
	if result == nil {
		return &pinixv2.DataResponse{}
	}
	resp := &pinixv2.DataResponse{
		Content: cloneBytes(result.GetContent()),
		Uri:     result.GetUri(),
	}
	if entries := result.GetEntries(); len(entries) > 0 {
		resp.Entries = make([]*pinixv2.DataEntry, len(entries))
		copy(resp.Entries, entries)
	}
	if stat := result.GetStat(); stat != nil {
		resp.Stat = &pinixv2.DataStat{
			Size:     stat.GetSize(),
			Mime:     stat.GetMime(),
			Modified: stat.GetModified(),
		}
	}
	if hubErr := result.GetError(); hubErr != nil {
		resp.Error = &pinixv2.HubError{Code: hubErr.GetCode(), Message: hubErr.GetMessage()}
	}
	return resp
}
