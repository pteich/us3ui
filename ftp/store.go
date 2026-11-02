package ftp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/textproto"
	"path"
	"strings"

	ftpclient "github.com/jlaffaye/ftp"

	"github.com/pteich/us3ui/storage"
)

// answered reports whether err is an FTP status reply: the server responded,
// so the connection is still healthy. nil counts as answered.
func answered(err error) bool {
	if err == nil {
		return true
	}
	var proto *textproto.Error
	return errors.As(err, &proto)
}

// notFound reports whether the server said the path does not exist.
func notFound(err error) bool {
	var proto *textproto.Error
	return errors.As(err, &proto) && proto.Code == ftpclient.StatusFileUnavailable
}

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
	var out []storage.ObjectInfo
	err := s.withConn(ctx, func(c ftpConn) error {
		out = nil // a retried attempt starts over
		dir := s.path(prefix)
		e, err := c.GetEntry(dir)
		if err != nil || e.Type != ftpclient.EntryTypeFolder {
			if err != nil && !notFound(err) {
				return err
			}
			return nil // prefix directory missing: no objects
		}
		rel := strings.Trim(strings.TrimSpace(prefix), "/")
		return walkDir(ctx, c, dir, rel, &out)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// walkDir appends every file below dir to out, with keys relative to the
// service root. Symlinked directories are not followed (FTP listings cannot
// tell them apart reliably); unreadable subdirectories are skipped.
func walkDir(ctx context.Context, c ftpConn, dir, rel string, out *[]storage.ObjectInfo) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	entries, err := c.List(dir)
	if err != nil {
		return fmt.Errorf("listing %q: %w", rel, err)
	}
	for _, e := range entries {
		if e.Name == "." || e.Name == ".." {
			continue
		}
		key := e.Name
		if rel != "" {
			key = rel + "/" + e.Name
		}
		switch e.Type {
		case ftpclient.EntryTypeFolder:
			if err := walkDir(ctx, c, dir+"/"+e.Name, key, out); err != nil {
				if ctx.Err() != nil {
					return err
				}
				continue // unreadable directory: skip it
			}
		case ftpclient.EntryTypeFile, ftpclient.EntryTypeLink:
			*out = append(*out, storage.ObjectInfo{
				Key:          key,
				Size:         int64(e.Size),
				LastModified: e.Time,
			})
		}
	}
	return nil
}

func (s *Service) StatObject(ctx context.Context, key string) (storage.ObjectInfo, error) {
	var info storage.ObjectInfo
	err := s.withConn(ctx, func(c ftpConn) error {
		e, err := c.GetEntry(s.path(key))
		if err != nil {
			return err
		}
		info = storage.ObjectInfo{Key: key, Size: int64(e.Size), LastModified: e.Time}
		return nil
	})
	if err != nil {
		return storage.ObjectInfo{}, err
	}
	return info, nil
}

// UploadObjectReader writes r to the object, creating parent directories on
// the way. mimeType is ignored: FTP has no content-type concept.
func (s *Service) UploadObjectReader(ctx context.Context, objectName string, r io.Reader, length int64, mimeType string) error {
	p := s.path(objectName)
	if dir := path.Dir(p); dir != "" && dir != "/" && dir != "." {
		if err := s.mkdirAll(ctx, dir); err != nil {
			return err
		}
	}

	// A network failure before any byte left the source is retried once on a
	// fresh connection; once streaming has begun the error surfaces.
	src := &countingCtxReader{ctx: ctx, r: r}
	stor := func(c ftpConn) error { return c.Stor(p, src) }
	err := s.tryConn(ctx, stor)
	if err != nil && src.n == 0 && !answered(err) && ctx.Err() == nil {
		err = s.tryConn(ctx, stor)
	}
	if err != nil {
		return err
	}
	s.lister.Invalidate()
	return nil
}

// mkdirAll creates dir and its parents. Servers reply with a status error
// (usually "already exists") that is tolerated; a genuinely missing parent
// surfaces in the following transfer.
func (s *Service) mkdirAll(ctx context.Context, dir string) error {
	segs := strings.Split(strings.Trim(dir, "/"), "/")
	cur := ""
	for _, seg := range segs {
		if seg == "" {
			continue
		}
		cur += "/" + seg
		if err := s.withConn(ctx, func(c ftpConn) error { return c.MakeDir(cur) }); err != nil && !answered(err) {
			return fmt.Errorf("creating directory %q: %w", seg, err)
		}
	}
	return nil
}

func (s *Service) DownloadObject(ctx context.Context, objectName string) (io.ReadCloser, error) {
	p := s.path(objectName)

	// The borrowed connection stays checked out while the transfer is open;
	// pooledReader.Close returns it to the pool.
	open := func() (io.ReadCloser, ftpConn, error) {
		c, err := s.acquire(ctx)
		if err != nil {
			return nil, nil, err
		}
		r, err := c.Retr(p)
		if err != nil {
			if answered(err) {
				s.release(c)
			} else {
				s.discard(c)
			}
			return nil, nil, err
		}
		return r, c, nil
	}
	r, c, err := open()
	if err != nil && !answered(err) && ctx.Err() == nil {
		r, c, err = open() // the connection dropped before the transfer began
	}
	if err != nil {
		return nil, err
	}
	return &pooledReader{ctx: ctx, r: r, s: s, c: c}, nil
}

// pooledReader streams an open FTP transfer and returns its connection to
// the pool on Close.
type pooledReader struct {
	ctx context.Context
	r   io.ReadCloser
	s   *Service
	c   ftpConn
}

func (p *pooledReader) Read(b []byte) (int, error) {
	if err := p.ctx.Err(); err != nil {
		return 0, err
	}
	return p.r.Read(b)
}

func (p *pooledReader) Close() error {
	err := p.r.Close()
	if answered(err) {
		p.s.release(p.c)
	} else {
		p.s.discard(p.c)
	}
	return err
}

// DeleteObject removes one file. Directories cannot be deleted yet.
func (s *Service) DeleteObject(ctx context.Context, objectName string) error {
	p := s.path(objectName)
	dirHint := false
	err := s.withConn(ctx, func(c ftpConn) error {
		derr := c.Delete(p)
		if derr != nil {
			if e, serr := c.GetEntry(p); serr == nil && e.Type == ftpclient.EntryTypeFolder {
				dirHint = true
			}
		}
		return derr
	})
	if dirHint {
		return fmt.Errorf("%s is a directory; directories cannot be deleted yet", objectName)
	}
	if err != nil {
		return err
	}
	s.lister.Invalidate()
	return nil
}

// countingCtxReader aborts a blocked read when ctx is done and counts the
// bytes that left the source, so uploads that transferred nothing can be
// retried safely.
type countingCtxReader struct {
	ctx context.Context
	r   io.Reader
	n   int64
}

func (r *countingCtxReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.r.Read(p)
	r.n += int64(n)
	return n, err
}
