package download

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

type publishedDownload struct {
	path string
	size int64
	root *os.Root
	name string
	info os.FileInfo
}

// remove rolls back a publication while its media root is still open.
func (p publishedDownload) remove() error {
	info, err := p.root.Lstat(p.name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !os.SameFile(p.info, info) {
		return errors.New("download output changed after publication")
	}
	return p.root.Remove(p.name)
}

func openMediaRoot(dir string) (*os.Root, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return os.OpenRoot(dir)
}

func publishDownload(ctx context.Context, root *os.Root, source, desired, remoteID string) (publishedDownload, error) {
	if err := ctx.Err(); err != nil {
		return publishedDownload{}, err
	}
	sourceName, err := filepath.Rel(root.Name(), source)
	if err != nil || !filepath.IsLocal(sourceName) {
		return publishedDownload{}, errors.New("download source outside media directory")
	}
	name, err := filepath.Rel(root.Name(), desired)
	if err != nil || !filepath.IsLocal(name) || name == "." {
		return publishedDownload{}, errors.New("download destination outside media directory")
	}
	info, err := root.Lstat(sourceName)
	if err != nil {
		return publishedDownload{}, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 {
		return publishedDownload{}, errors.New("下载文件为空或不是普通文件")
	}
	if err := root.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return publishedDownload{}, err
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	label := sanitizeSegment(remoteID)
	if label == "" {
		label = "download"
	}
	label = truncateFilename(label, 40)
	linkSupported := true
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return publishedDownload{}, err
		}
		candidate := name
		if attempt > 0 {
			suffix := " [" + label + "]" + ext
			if attempt > 1 {
				suffix = fmt.Sprintf(" [%s-%d]%s", label, attempt, ext)
			}
			candidate = filepath.Join(filepath.Dir(stem), truncateFilename(filepath.Base(stem), 255-len(suffix))+suffix)
		}
		if linkSupported {
			err := root.Link(sourceName, candidate)
			if errors.Is(err, os.ErrExist) {
				continue
			}
			if err == nil {
				linked, err := root.Lstat(candidate)
				if err != nil {
					return publishedDownload{}, err
				}
				if !os.SameFile(info, linked) {
					return publishedDownload{}, errors.New("download source changed during publication")
				}
				published := publishedDownload{path: filepath.Join(root.Name(), candidate), size: info.Size(), root: root, name: candidate, info: linked}
				if err := ctx.Err(); err != nil {
					return publishedDownload{}, errors.Join(err, published.remove())
				}
				return published, nil
			}
			// Some filesystems or mount points disallow hard links.
			linkSupported = false
		}
		copied, err := copyDownloadExclusive(ctx, root, sourceName, candidate, info)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return publishedDownload{}, err
		}
		return publishedDownload{path: filepath.Join(root.Name(), candidate), size: info.Size(), root: root, name: candidate, info: copied}, nil
	}
}

func truncateFilename(name string, limit int) string {
	if len(name) <= limit {
		return name
	}
	for limit > 0 && !utf8.RuneStart(name[limit]) {
		limit--
	}
	return name[:limit]
}

func copyDownloadExclusive(ctx context.Context, root *os.Root, source, dest string, info os.FileInfo) (os.FileInfo, error) {
	in, err := root.Open(source)
	if err != nil {
		return nil, err
	}
	defer in.Close()
	opened, err := in.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, opened) {
		return nil, errors.New("download source changed during publication")
	}
	out, err := root.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return nil, err
	}
	copied, statErr := out.Stat()
	var copyErr error
	if statErr == nil {
		var written int64
		written, copyErr = io.Copy(out, downloadCopyReader{ctx, in})
		if copyErr == nil && written != info.Size() {
			copyErr = errors.New("download source size changed during publication")
		}
	}
	if err := errors.Join(statErr, copyErr, out.Close(), ctx.Err()); err != nil {
		if copied != nil {
			_ = (publishedDownload{root: root, name: dest, info: copied}).remove()
		}
		return nil, err
	}
	return copied, nil
}

type downloadCopyReader struct {
	ctx context.Context
	io.Reader
}

func (r downloadCopyReader) Read(buf []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(buf)
}
