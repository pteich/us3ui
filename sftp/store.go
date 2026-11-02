package sftp

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/pteich/us3ui/storage"
)

// path maps a storage key to an absolute server path under the root.
func (s *Service) path(key string) string {
	return path.Join(s.root, strings.TrimLeft(strings.TrimSpace(key), "/"))
}

// ListObjectsBatch lists up to batchSize objects with keys ordered
// lexicographically under prefix, starting after startAfter. A fresh listing
// (startAfter == "") walks the directory tree once; pagination and Load More
// (startAfter != "") serve from that cached walk. A missing prefix lists
// nothing, like S3 does.
func (s *Service) ListObjectsBatch(ctx context.Context, startAfter, prefix string, batchSize int) ([]storage.ObjectInfo, error) {
	return s.lister.Page(startAfter, prefix, batchSize, func() ([]storage.ObjectInfo, error) {
		return s.walk(ctx, prefix)
	})
}

// walk lists every file under the prefix directory. The prefix must be a
// directory path; partial name prefixes (S3-style "log" matching "logs/...")
// are not supported.
func (s *Service) walk(ctx context.Context, prefix string) ([]storage.ObjectInfo, error) {
	c, err := s.conn()
	if err != nil {
		return nil, err
	}
	dir := s.path(prefix)
	if _, err := c.Stat(dir); err != nil {
		return nil, nil // prefix directory missing or unreadable: no objects
	}
	var out []storage.ObjectInfo
	rel := strings.Trim(strings.TrimSpace(prefix), "/")
	if err := walkDir(ctx, c, dir, rel, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// walkDir appends every file below dir to out, with keys relative to the
// service root. Directory symlinks are not followed (avoiding cycles);
// unreadable subdirectories are skipped.
func walkDir(ctx context.Context, c sftpConn, dir, rel string, out *[]storage.ObjectInfo) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	entries, err := c.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("listing %q: %w", rel, err)
	}
	for _, e := range entries {
		key := e.Name()
		if rel != "" {
			key = rel + "/" + key
		}
		switch {
		case e.IsDir():
			if err := walkDir(ctx, c, dir+"/"+e.Name(), key, out); err != nil {
				if ctx.Err() != nil {
					return err
				}
				continue // unreadable directory: skip it
			}
		case e.Mode().IsRegular(), e.Mode()&os.ModeSymlink != 0:
			*out = append(*out, storage.ObjectInfo{
				Key:          key,
				Size:         e.Size(),
				LastModified: e.ModTime(),
			})
		}
	}
	return nil
}

// invalidateWalk drops the cached listing after a mutation.
func (s *Service) invalidateWalk() {
	s.lister.Invalidate()
}

func (s *Service) StatObject(ctx context.Context, key string) (storage.ObjectInfo, error) {
	c, err := s.conn()
	if err != nil {
		return storage.ObjectInfo{}, err
	}
	info, err := c.Stat(s.path(key))
	if err != nil {
		return storage.ObjectInfo{}, s.failIfDead(c, err)
	}
	return storage.ObjectInfo{Key: key, Size: info.Size(), LastModified: info.ModTime()}, nil
}

// UploadObjectReader writes r to the object, creating parent directories on
// the way. mimeType is ignored: SFTP has no content-type concept.
func (s *Service) UploadObjectReader(ctx context.Context, objectName string, r io.Reader, length int64, mimeType string) error {
	c, err := s.conn()
	if err != nil {
		return err
	}
	p := s.path(objectName)
	if dir := path.Dir(p); dir != "" && dir != "/" && dir != "." {
		if err := c.MkdirAll(dir); err != nil {
			return s.failIfDead(c, fmt.Errorf("creating directory %q: %w", path.Dir(objectName), err))
		}
	}
	f, err := c.Create(p)
	if err != nil {
		return s.failIfDead(c, fmt.Errorf("opening %q for writing: %w", objectName, err))
	}
	s.invalidateWalk()
	if _, err := io.Copy(f, &ctxReader{ctx: ctx, r: r}); err != nil {
		f.Close()
		s.failIfDead(c, err)
		return err
	}
	return s.failIfDead(c, f.Close())
}

func (s *Service) DownloadObject(ctx context.Context, objectName string) (io.ReadCloser, error) {
	c, err := s.conn()
	if err != nil {
		return nil, err
	}
	f, err := c.Open(s.path(objectName))
	if err != nil {
		return nil, s.failIfDead(c, err)
	}
	return &ctxFile{ctx: ctx, f: f}, nil
}

// DeleteObject removes one file. Directories cannot be deleted yet.
func (s *Service) DeleteObject(ctx context.Context, objectName string) error {
	c, err := s.conn()
	if err != nil {
		return err
	}
	p := s.path(objectName)
	if err := c.Remove(p); err != nil {
		if info, serr := c.Stat(p); serr == nil && info.IsDir() {
			return fmt.Errorf("%s is a directory; directories cannot be deleted yet", objectName)
		}
		return s.failIfDead(c, err)
	}
	s.invalidateWalk()
	return nil
}

// ctxReader aborts a blocked read when ctx is done, so long transfers can be
// canceled.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *ctxReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

// ctxFile wraps an open remote file so reads stop when ctx is canceled.
type ctxFile struct {
	ctx context.Context
	f   io.ReadCloser
}

func (o *ctxFile) Read(p []byte) (int, error) {
	if err := o.ctx.Err(); err != nil {
		return 0, err
	}
	return o.f.Read(p)
}

func (o *ctxFile) Close() error { return o.f.Close() }
