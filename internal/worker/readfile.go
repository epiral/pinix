// Role:    Local clip file read helpers
// Depends: context, io, mime, os, path/filepath, strings, connectrpc, internal/clip
// Exports: (package-internal helpers)

package worker

import (
	"context"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"

	connect "connectrpc.com/connect"
	"github.com/epiral/pinix/internal/clip"
)

const readChunkSize = 64 * 1024

func resolveClipFilePath(workdir, relPath string) (string, error) {
	cleanRel := filepath.Clean(relPath)
	if cleanRel == "." || cleanRel == string(os.PathSeparator) {
		return "", connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("path is required"))
	}
	if strings.HasPrefix(cleanRel, "..") || filepath.IsAbs(relPath) {
		return "", connect.NewError(connect.CodePermissionDenied, fmt.Errorf("path escapes workdir"))
	}
	if cleanRel != filepath.Clean(filepath.ToSlash(relPath)) && filepath.Separator == '\\' {
		cleanRel = filepath.Clean(relPath)
	}
	if !(strings.HasPrefix(filepath.ToSlash(cleanRel), "web/") || strings.HasPrefix(filepath.ToSlash(cleanRel), "data/")) && filepath.ToSlash(cleanRel) != "web" && filepath.ToSlash(cleanRel) != "data" {
		return "", connect.NewError(connect.CodePermissionDenied, fmt.Errorf("path must be under web/ or data/"))
	}

	resolvedWorkdir, err := filepath.EvalSymlinks(workdir)
	if err != nil {
		return "", connect.NewError(connect.CodeInternal, fmt.Errorf("resolve workdir: %w", err))
	}
	resolvedWorkdir, err = filepath.Abs(resolvedWorkdir)
	if err != nil {
		return "", connect.NewError(connect.CodeInternal, fmt.Errorf("resolve workdir abs path: %w", err))
	}

	absPath := filepath.Join(workdir, cleanRel)
	resolvedPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return "", connect.NewError(connect.CodeNotFound, fmt.Errorf("file not found: %s", relPath))
	}
	resolvedPath, err = filepath.Abs(resolvedPath)
	if err != nil {
		return "", connect.NewError(connect.CodeInternal, fmt.Errorf("resolve file abs path: %w", err))
	}

	relResolvedPath, err := filepath.Rel(resolvedWorkdir, resolvedPath)
	if err != nil {
		return "", connect.NewError(connect.CodePermissionDenied, fmt.Errorf("path escapes workdir"))
	}
	if relResolvedPath == ".." || strings.HasPrefix(relResolvedPath, ".."+string(os.PathSeparator)) {
		return "", connect.NewError(connect.CodePermissionDenied, fmt.Errorf("path escapes workdir"))
	}

	return resolvedPath, nil
}

func openRegularFile(path, relPath string) (*os.File, os.FileInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("file not found: %s", relPath))
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, nil, connect.NewError(connect.CodeInternal, err)
	}
	if info.IsDir() {
		_ = f.Close()
		return nil, nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("path is a directory: %s", relPath))
	}
	return f, info, nil
}

func validateReadRange(offset, length, totalSize int64) (int64, int64, error) {
	if offset < 0 {
		return 0, 0, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("offset must be >= 0"))
	}
	if length < 0 {
		return 0, 0, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("length must be >= 0"))
	}
	if offset > totalSize {
		return 0, 0, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("offset must be <= file size"))
	}
	if length > 0 {
		return offset, length, nil
	}
	return offset, totalSize - offset, nil
}

func streamReadFile(ctx context.Context, f *os.File, out chan<- clip.FileChunk, mimeType string, totalSize int64, etag string, offset int64, remaining int64) error {
	buf := make([]byte, readChunkSize)
	currentOffset := offset
	for remaining > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		toRead := int64(readChunkSize)
		if toRead > remaining {
			toRead = remaining
		}
		n, err := f.Read(buf[:toRead])
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			out <- clip.FileChunk{Data: chunk, Offset: currentOffset, MimeType: mimeType, TotalSize: totalSize, ETag: etag}
			currentOffset += int64(n)
			remaining -= int64(n)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return connect.NewError(connect.CodeInternal, err)
		}
	}
	return nil
}

func mimeTypeFromPath(relPath string) string {
	mimeType := mime.TypeByExtension(filepath.Ext(relPath))
	if mimeType == "" {
		return "application/octet-stream"
	}
	return mimeType
}

func computeETag(info os.FileInfo) string {
	return fmt.Sprintf("%x-%x", info.ModTime().UnixNano(), info.Size())
}
