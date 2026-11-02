package sftp

import (
	"bytes"
	"context"
	"io"
	"os"
	"path"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pteich/us3ui/config"
	"github.com/pteich/us3ui/storage"
)

// memFI is a minimal os.FileInfo for the in-memory filesystem.
type memFI struct {
	name string
	size int64
	mode os.FileMode
	mod  time.Time
}

func (f memFI) Name() string       { return f.name }
func (f memFI) Size() int64        { return f.size }
func (f memFI) Mode() os.FileMode  { return f.mode }
func (f memFI) ModTime() time.Time { return f.mod }
func (f memFI) IsDir() bool        { return f.mode.IsDir() }
func (f memFI) Sys() any           { return nil }

// memFS is an in-memory sftpConn for tests.
type memFS struct {
	mu    sync.Mutex
	dirs  map[string]bool
	files map[string][]byte
	mod   time.Time
}

func newMemFS() *memFS {
	return &memFS{dirs: map[string]bool{"/": true}, files: map[string][]byte{}, mod: time.Now()}
}

func (m *memFS) clean(p string) string { return path.Clean("/" + strings.TrimPrefix(p, "/")) }

func (m *memFS) addFile(p string, data []byte) {
	p = m.clean(p)
	m.MkdirAll(path.Dir(p))
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[p] = data
}

func (m *memFS) Stat(p string) (os.FileInfo, error) {
	p = m.clean(p)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.dirs[p] {
		return memFI{name: path.Base(p), mode: os.ModeDir | 0o755, mod: m.mod}, nil
	}
	if data, ok := m.files[p]; ok {
		return memFI{name: path.Base(p), size: int64(len(data)), mode: 0o644, mod: m.mod}, nil
	}
	return nil, os.ErrNotExist
}

func (m *memFS) ReadDir(p string) ([]os.FileInfo, error) {
	p = m.clean(p)
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.dirs[p] {
		return nil, os.ErrNotExist
	}
	prefix := p
	if prefix != "/" {
		prefix += "/"
	}
	var out []os.FileInfo
	for d := range m.dirs {
		rest := strings.TrimPrefix(d, prefix)
		if d == p || !strings.HasPrefix(d, prefix) || strings.Contains(rest, "/") {
			continue
		}
		out = append(out, memFI{name: path.Base(d), mode: os.ModeDir | 0o755, mod: m.mod})
	}
	for f, data := range m.files {
		rest := strings.TrimPrefix(f, prefix)
		if !strings.HasPrefix(f, prefix) || strings.Contains(rest, "/") {
			continue
		}
		out = append(out, memFI{name: path.Base(f), size: int64(len(data)), mode: 0o644, mod: m.mod})
	}
	return out, nil
}

func (m *memFS) MkdirAll(p string) error {
	p = m.clean(p)
	m.mu.Lock()
	defer m.mu.Unlock()
	for p != "/" {
		m.dirs[p] = true
		p = path.Dir(p)
	}
	return nil
}

type memWriter struct {
	fs  *memFS
	p   string
	buf bytes.Buffer
}

func (w *memWriter) Write(b []byte) (int, error) { return w.buf.Write(b) }

func (w *memWriter) Close() error {
	w.fs.mu.Lock()
	defer w.fs.mu.Unlock()
	w.fs.files[w.p] = w.buf.Bytes()
	return nil
}

func (m *memFS) Create(p string) (io.WriteCloser, error) {
	return &memWriter{fs: m, p: m.clean(p)}, nil
}

func (m *memFS) Open(p string) (io.ReadCloser, error) {
	p = m.clean(p)
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.files[p]
	if !ok {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (m *memFS) Remove(p string) error {
	p = m.clean(p)
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.files[p]; !ok {
		return os.ErrNotExist
	}
	delete(m.files, p)
	return nil
}

func (m *memFS) Getwd() (string, error)            { return "/", nil }
func (m *memFS) RealPath(p string) (string, error) { return m.clean(p), nil }
func (m *memFS) Close() error                      { return nil }

// newTestService returns a Service rooted at "/" of fs, without a live
// connection or keepalive loop.
func newTestService(fs *memFS) *Service {
	return &Service{cfg: config.S3Config{}, client: fs, root: "/", stopKeepal: make(chan struct{})}
}

func seededFS() *memFS {
	fs := newMemFS()
	fs.addFile("/a.txt", []byte("a"))
	fs.addFile("/logs/x.log", []byte("xy"))
	fs.addFile("/logs/old/y.log", []byte("yyy"))
	fs.addFile("/z/deep/f.txt", []byte("four"))
	fs.MkdirAll("/empty")
	return fs
}

func keysOf(objects []storage.ObjectInfo) []string {
	out := make([]string, len(objects))
	for i, o := range objects {
		out[i] = o.Key
	}
	return out
}

func TestListObjectsBatchWalksSortsAndPages(t *testing.T) {
	s := newTestService(seededFS())
	want := []string{"a.txt", "logs/old/y.log", "logs/x.log", "z/deep/f.txt"}

	b1, err := s.ListObjectsBatch(context.Background(), "", "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if got := keysOf(b1); !equal(got, want[:2]) {
		t.Errorf("batch 1 = %v, want %v", got, want[:2])
	}
	b2, err := s.ListObjectsBatch(context.Background(), b1[len(b1)-1].Key, "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if got := keysOf(b2); !equal(got, want[2:]) {
		t.Errorf("batch 2 = %v, want %v", got, want[2:])
	}
	b3, err := s.ListObjectsBatch(context.Background(), want[len(want)-1], "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(b3) != 0 {
		t.Errorf("batch 3 = %v, want empty", keysOf(b3))
	}
}

func TestListObjectsBatchScopedToPrefixDir(t *testing.T) {
	s := newTestService(seededFS())
	b, err := s.ListObjectsBatch(context.Background(), "", "logs", 100)
	if err != nil {
		t.Fatal(err)
	}
	if got := keysOf(b); !equal(got, []string{"logs/old/y.log", "logs/x.log"}) {
		t.Errorf("prefix listing = %v", got)
	}
}

func TestListObjectsBatchMissingPrefixIsEmpty(t *testing.T) {
	s := newTestService(seededFS())
	b, err := s.ListObjectsBatch(context.Background(), "", "nope", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != 0 {
		t.Errorf("listing = %v, want empty", keysOf(b))
	}
}

func TestListRefreshRewalksPaginationServesCache(t *testing.T) {
	fs := seededFS()
	s := newTestService(fs)

	if _, err := s.ListObjectsBatch(context.Background(), "", "", 100); err != nil {
		t.Fatal(err)
	}
	fs.addFile("/new.txt", []byte("n"))

	// A fresh listing (startAfter == "") re-walks and sees the new file.
	fresh, err := s.ListObjectsBatch(context.Background(), "", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(fresh) != 5 {
		t.Errorf("fresh listing = %v, want the new file included", keysOf(fresh))
	}

	// Pagination (startAfter != "") serves the cached walk.
	fs.addFile("/another.txt", []byte("x"))
	cached, err := s.ListObjectsBatch(context.Background(), "a.txt", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(cached) != 4 {
		t.Errorf("paged listing = %v, want the cached walk", keysOf(cached))
	}
}

func TestUploadCreatesDirectoriesAndInvalidatesWalk(t *testing.T) {
	fs := seededFS()
	s := newTestService(fs)

	if err := s.UploadObjectReader(context.Background(), "d1/d2/f.txt", strings.NewReader("hello"), 5, "text/plain"); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(mustOpen(t, fs, "/d1/d2/f.txt"))
	if err != nil || string(data) != "hello" {
		t.Fatalf("uploaded content = %q, %v", data, err)
	}
	b, err := s.ListObjectsBatch(context.Background(), "", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != 5 || !slices.Contains(keysOf(b), "d1/d2/f.txt") {
		t.Errorf("listing after upload = %v", keysOf(b))
	}
}

func TestDownloadRoundTripAndCancellation(t *testing.T) {
	fs := seededFS()
	fs.addFile("/dl.txt", []byte("payload"))
	s := newTestService(fs)

	rc, err := s.DownloadObject(context.Background(), "dl.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil || string(data) != "payload" {
		t.Fatalf("download = %q, %v", data, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rc2, err := s.DownloadObject(ctx, "dl.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer rc2.Close()
	if _, err := rc2.Read(make([]byte, 8)); err != context.Canceled {
		t.Errorf("read after cancel = %v, want context.Canceled", err)
	}
}

func TestDeleteFileInvalidatesWalk(t *testing.T) {
	fs := seededFS()
	s := newTestService(fs)

	if _, err := s.ListObjectsBatch(context.Background(), "", "", 100); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteObject(context.Background(), "logs/x.log"); err != nil {
		t.Fatal(err)
	}
	b, err := s.ListObjectsBatch(context.Background(), "", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range b {
		if o.Key == "logs/x.log" {
			t.Error("deleted file still listed after fresh walk")
		}
	}
}

func TestDeleteDirectoryFailsWithHint(t *testing.T) {
	s := newTestService(seededFS())
	err := s.DeleteObject(context.Background(), "logs")
	if err == nil || !strings.Contains(err.Error(), "directory") {
		t.Fatalf("delete dir err = %v, want a directory hint", err)
	}
}

func TestStatObjectMapsKeyAndSize(t *testing.T) {
	s := newTestService(seededFS())
	info, err := s.StatObject(context.Background(), "logs/x.log")
	if err != nil {
		t.Fatal(err)
	}
	if info.Key != "logs/x.log" || info.Size != 2 {
		t.Errorf("stat = %+v", info)
	}
}

func TestSplitHostPort(t *testing.T) {
	for _, tt := range []struct {
		endpoint, host, port string
	}{
		{"example.com", "example.com", "22"},
		{"example.com:2222", "example.com", "2222"},
		{"[::1]:2222", "::1", "2222"},
		{" example.com ", "example.com", "22"},
	} {
		if h, p := splitHostPort(tt.endpoint); h != tt.host || p != tt.port {
			t.Errorf("splitHostPort(%q) = %q, %q; want %q, %q", tt.endpoint, h, p, tt.host, tt.port)
		}
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func mustOpen(t *testing.T, fs *memFS, p string) io.Reader {
	t.Helper()
	rc, err := fs.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rc.Close() })
	return rc
}
