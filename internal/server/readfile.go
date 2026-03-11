// Role:    ClipService ReadFile handler (path validation, range reads, ETag)
// Depends: internal/server ClipServer, connectrpc, os/path/filepath, mime
// Exports: (ClipServer.ReadFile method)

package server

import (
	"context"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"

	connect "connectrpc.com/connect"
	v1 "github.com/epiral/pinix/gen/go/pinix/v1"
)

const readChunkSize = 64 * 1024 // 64 KB

func (s *ClipServer) ReadFile(
	ctx context.Context,
	req *connect.Request[v1.ReadFileRequest],
	stream *connect.ServerStream[v1.ReadFileChunk],
) error {
	relPath := req.Msg.GetPath()

	if strings.Contains(relPath, "..") {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid path: %s", relPath))
	}
	if !strings.HasPrefix(relPath, "web/") && !strings.HasPrefix(relPath, "data/") {
		return connect.NewError(connect.CodePermissionDenied, fmt.Errorf("path must be under web/ or data/"))
	}

	clip, err := s.resolveClip(ctx)
	if err != nil {
		return err
	}

	resolvedPath, err := resolveClipFilePath(clip.Workdir, relPath)
	if err != nil {
		return err
	}

	f, info, err := openRegularFile(resolvedPath, relPath)
	if err != nil {
		return err
	}
	defer f.Close()

	totalSize := info.Size()
	mimeType := mimeTypeFromPath(relPath)
	etag := computeETag(info)
	if req.Msg.GetIfNoneMatch() == etag {
		return stream.Send(&v1.ReadFileChunk{
			MimeType:    mimeType,
			TotalSize:   totalSize,
			Etag:        etag,
			NotModified: true,
		})
	}

	offset, remaining, err := validateReadRange(req.Msg.GetOffset(), req.Msg.GetLength(), totalSize)
	if err != nil {
		return err
	}
	if offset > 0 {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return connect.NewError(connect.CodeInternal, err)
		}
	}

	return streamReadFile(ctx, f, stream, mimeType, totalSize, etag, offset, remaining)
}

func resolveClipFilePath(workdir, relPath string) (string, error) {
	resolvedWorkdir, err := filepath.EvalSymlinks(workdir)
	if err != nil {
		return "", connect.NewError(connect.CodeInternal, fmt.Errorf("resolve workdir: %w", err))
	}
	resolvedWorkdir, err = filepath.Abs(resolvedWorkdir)
	if err != nil {
		return "", connect.NewError(connect.CodeInternal, fmt.Errorf("resolve workdir abs path: %w", err))
	}

	absPath := filepath.Join(workdir, relPath)
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

func streamReadFile(
	ctx context.Context,
	f *os.File,
	stream *connect.ServerStream[v1.ReadFileChunk],
	mimeType string,
	totalSize int64,
	etag string,
	offset int64,
	remaining int64,
) error {
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
			if sendErr := stream.Send(&v1.ReadFileChunk{
				Data:      buf[:n],
				Offset:    currentOffset,
				MimeType:  mimeType,
				TotalSize: totalSize,
				Etag:      etag,
			}); sendErr != nil {
				return sendErr
			}
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
