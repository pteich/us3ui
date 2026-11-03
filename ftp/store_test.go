package ftp

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/textproto"
	"path"
	"strings"
	"sync"
	"testing"
	"time"

	ftpclient "github.com/jlaffaye/ftp"

	"github.com/pteich/us3ui/storage"
)

var errNet = errors.New("connection reset by peer")

func statusErr(code int) error { return &textproto.Error{Code: code, Msg: "status"} }

// fakeConn is one pooled connection over the shared fakeFS.
type fakeConn struct {
	fs   *fakeFS
	dead bool
}

func (c *fakeConn) broken() bool { return c.dead }

func (c *fakeConn) List(p string) ([]*ftpclient.Entry, error) {
	if c.broken() {
		return nil, errNet
	}
	return c.fs.list(p)
}

func (c *fakeConn) GetEntry(p string) (*ftpclient.Entry, error) {
	if c.broken() {
		return nil, errNet
	}
	return c.fs.entry(p)
}

func (c *fakeConn) Stor(p string, r io.Reader) error {
	if c.broken() {
		return errNet // fails before reading r, like a dead control connection
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	return c.fs.writeFile(p, data)
}

func (c *fakeConn) Retr(p string) (io.ReadCloser, error) {
	if c.broken() {
		return nil, errNet
	}
	data, err := c.fs.readFile(p)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (c *fakeConn) Delete(p string) error {
	if c.broken() {
		return errNet
	}
	return c.fs.remove(p)
}

func (c *fakeConn) MakeDir(p string) error {
	if c.broken() {
		return errNet
	}
	return c.fs.makeDir(p)
}

func (c *fakeConn) CurrentDir() (string, error) { return "/", nil }
func (c *fakeConn) NoOp() error {
	if c.broken() {
		return errNet
	}
	return nil
}
func (c *fakeConn) Quit() error { return nil }

// fakeFS is an in-memory FTP server.
type fakeFS struct {
	mu    sync.Mutex
	dirs  map[string]bool
	files map[string][]byte
	mod   time.Time

	dials     int
	deadFirst int // the first N dialed connections are dead
}

func newFakeFS() *fakeFS {
	return &fakeFS{dirs: map[string]bool{"/": true}, files: map[string][]byte{}, mod: time.Now()}
}

func (f *fakeFS) clean(p string) string { return path.Clean("/" + strings.TrimPrefix(p, "/")) }

func (f *fakeFS) addFile(p string, data []byte) {
	f.makeDir(path.Dir(f.clean(p)))
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[f.clean(p)] = data
}

func (f *fakeFS) entry(p string) (*ftpclient.Entry, error) {
	p = f.clean(p)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.dirs[p] {
		return &ftpclient.Entry{Name: path.Base(p), Type: ftpclient.EntryTypeFolder, Time: f.mod}, nil
	}
	if data, ok := f.files[p]; ok {
		return &ftpclient.Entry{Name: path.Base(p), Type: ftpclient.EntryTypeFile, Size: uint64(len(data)), Time: f.mod}, nil
	}
	return nil, statusErr(ftpclient.StatusFileUnavailable)
}

func (f *fakeFS) list(p string) ([]*ftpclient.Entry, error) {
	p = f.clean(p)
	f.mu.Lock()
	if !f.dirs[p] {
		f.mu.Unlock()
		return nil, statusErr(ftpclient.StatusFileUnavailable)
	}
	prefix := p
	if prefix != "/" {
		prefix += "/"
	}
	entries := []*ftpclient.Entry{
		{Name: ".", Type: ftpclient.EntryTypeFolder},
		{Name: "..", Type: ftpclient.EntryTypeFolder},
	}
	for d := range f.dirs {
		rest := strings.TrimPrefix(d, prefix)
		if d == p || !strings.HasPrefix(d, prefix) || strings.Contains(rest, "/") {
			continue
		}
		entries = append(entries, &ftpclient.Entry{Name: path.Base(d), Type: ftpclient.EntryTypeFolder, Time: f.mod})
	}
	for file, data := range f.files {
		rest := strings.TrimPrefix(file, prefix)
		if !strings.HasPrefix(file, prefix) || strings.Contains(rest, "/") {
			continue
		}
		entries = append(entries, &ftpclient.Entry{Name: path.Base(file), Type: ftpclient.EntryTypeFile, Size: uint64(len(data)), Time: f.mod})
	}
	f.mu.Unlock()
	return entries, nil
}

func (f *fakeFS) writeFile(p string, data []byte) error {
	p = f.clean(p)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.dirs[p] {
		return statusErr(550)
	}
	f.files[p] = data
	return nil
}

func (f *fakeFS) readFile(p string) ([]byte, error) {
	p = f.clean(p)
	f.mu.Lock()
	defer f.mu.Unlock()
	data, ok := f.files[p]
	if !ok {
		return nil, statusErr(ftpclient.StatusFileUnavailable)
	}
	return data, nil
}

func (f *fakeFS) remove(p string) error {
	p = f.clean(p)
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.files[p]; !ok {
		if f.dirs[p] {
			return statusErr(550) // directories look like a failed delete
		}
		return statusErr(ftpclient.StatusFileUnavailable)
	}
	delete(f.files, p)
	return nil
}

func (f *fakeFS) makeDir(p string) error {
	p = f.clean(p)
	f.mu.Lock()
	defer f.mu.Unlock()
	existed := f.dirs[p]
	for cur := p; cur != "/"; cur = path.Dir(cur) {
		f.dirs[cur] = true
	}
	if existed {
		return statusErr(521) // already exists
	}
	return nil
}

// newTestService returns a Service rooted at "/" of fs, dialing fake
// connections from it.
func newTestService(f *fakeFS) *Service {
	s := &Service{idle: map[ftpConn]time.Time{}, root: "/"}
	s.dial = func() (ftpConn, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.dials++
		return &fakeConn{fs: f, dead: f.dials <= f.deadFirst}, nil
	}
	return s
}

func seededFS() *fakeFS {
	f := newFakeFS()
	f.addFile("/a.txt", []byte("a"))
	f.addFile("/logs/x.log", []byte("xy"))
	f.addFile("/logs/old/y.log", []byte("yyy"))
	f.addFile("/z/deep/f.txt", []byte("four"))
	f.makeDir("/empty")
	return f
}

func keysOf(objects []storage.ObjectInfo) []string {
	out := make([]string, len(objects))
	for i, o := range objects {
		out[i] = o.Key
	}
	return out
}

func TestListWalksSortsAndPages(t *testing.T) {
	s := newTestService(seededFS())
	want := []string{"a.txt", "logs/old/y.log", "logs/x.log", "z/deep/f.txt"}

	b1, err := s.ListObjectsBatch(context.Background(), "", "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if got := keysOf(b1); !equal(got, want[:2]) {
		t.Errorf("batch 1 = %v, want %v", got, want[:2])
	}
	b2, err := s.ListObjectsBatch(context.Background(), want[1], "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if got := keysOf(b2); !equal(got, want[2:]) {
		t.Errorf("batch 2 = %v, want %v", got, want[2:])
	}
	if b3, err := s.ListObjectsBatch(context.Background(), want[3], "", 2); err != nil || len(b3) != 0 {
		t.Errorf("batch 3 = %v, %v; want empty", keysOf(b3), err)
	}
}

func TestListScopedToPrefixDirAndMissingIsEmpty(t *testing.T) {
	s := newTestService(seededFS())
	b, err := s.ListObjectsBatch(context.Background(), "", "logs", 100)
	if err != nil {
		t.Fatal(err)
	}
	if got := keysOf(b); !equal(got, []string{"logs/old/y.log", "logs/x.log"}) {
		t.Errorf("prefix listing = %v", got)
	}
	if b, err := s.ListObjectsBatch(context.Background(), "", "nope", 100); err != nil || len(b) != 0 {
		t.Errorf("missing prefix = %v, %v; want empty", keysOf(b), err)
	}
}

func TestUploadCreatesDirectories(t *testing.T) {
	f := seededFS()
	s := newTestService(f)
	if err := s.UploadObjectReader(context.Background(), "d1/d2/f.txt", strings.NewReader("hello"), 5, ""); err != nil {
		t.Fatal(err)
	}
	if got, err := f.readFile("/d1/d2/f.txt"); err != nil || string(got) != "hello" {
		t.Fatalf("uploaded content = %q, %v", got, err)
	}
}

func TestDownloadRoundTripAndCancellation(t *testing.T) {
	f := seededFS()
	f.addFile("/dl.txt", []byte("payload"))
	s := newTestService(f)

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
	rc2, err := s.DownloadObject(ctx, "dl.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer rc2.Close()
	cancel()
	if _, err := rc2.Read(make([]byte, 8)); err != context.Canceled {
		t.Errorf("read after cancel = %v, want context.Canceled", err)
	}
}

func TestDeleteFileAndDirectoryHint(t *testing.T) {
	f := seededFS()
	s := newTestService(f)
	if err := s.DeleteObject(context.Background(), "logs/x.log"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.readFile("/logs/x.log"); err == nil {
		t.Error("deleted file still present")
	}
	if err := s.DeleteObject(context.Background(), "logs"); err == nil || !strings.Contains(err.Error(), "directory") {
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

func TestPoolReusesIdleConnections(t *testing.T) {
	f := seededFS()
	s := newTestService(f)
	for range 3 {
		if _, err := s.ListObjectsBatch(context.Background(), "", "", 10); err != nil {
			t.Fatal(err)
		}
		if _, err := s.StatObject(context.Background(), "a.txt"); err != nil {
			t.Fatal(err)
		}
	}
	if f.dials != 1 {
		t.Errorf("dialed %d connections for sequential operations, want 1", f.dials)
	}
}

func TestParallelTransfersUseSeparateConnections(t *testing.T) {
	f := seededFS()
	f.addFile("/one.txt", []byte("1"))
	f.addFile("/two.txt", []byte("2"))
	s := newTestService(f)

	// Two open transfers hold one connection each.
	r1, err := s.DownloadObject(context.Background(), "one.txt")
	if err != nil {
		t.Fatal(err)
	}
	r2, err := s.DownloadObject(context.Background(), "two.txt")
	if err != nil {
		t.Fatal(err)
	}
	if f.dials != 2 {
		t.Errorf("dialed %d connections for two open transfers, want 2", f.dials)
	}
	r1.Close()
	r2.Close()

	// Afterwards the idle connections are reused.
	if _, err := s.StatObject(context.Background(), "a.txt"); err != nil {
		t.Fatal(err)
	}
	if f.dials != 2 {
		t.Errorf("dialed %d connections after releasing, want reuse", f.dials)
	}
}

func TestRecoversFromDeadConnection(t *testing.T) {
	f := seededFS()
	f.deadFirst = 1 // the first dialed connection is dead
	s := newTestService(f)

	if _, err := s.ListObjectsBatch(context.Background(), "", "", 100); err != nil {
		t.Fatalf("listing with a dead first connection: %v", err)
	}
	if f.dials != 2 {
		t.Errorf("dialed %d connections, want the dead one replaced", f.dials)
	}
}

func TestRetrRetriesOnDeadConnection(t *testing.T) {
	f := seededFS()
	f.addFile("/dl.txt", []byte("payload"))
	f.deadFirst = 1
	s := newTestService(f)

	rc, err := s.DownloadObject(context.Background(), "dl.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil || string(data) != "payload" {
		t.Fatalf("download = %q, %v", data, err)
	}
}

func TestUploadRetriesWhenNoDataWasSent(t *testing.T) {
	f := seededFS()
	f.deadFirst = 1
	s := newTestService(f)

	if err := s.UploadObjectReader(context.Background(), "f.txt", strings.NewReader("data"), 4, ""); err != nil {
		t.Fatal(err)
	}
	if got, err := f.readFile("/f.txt"); err != nil || string(got) != "data" {
		t.Fatalf("uploaded content = %q, %v", got, err)
	}
}

func TestCloseRejectsFurtherUse(t *testing.T) {
	s := newTestService(seededFS())
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StatObject(context.Background(), "a.txt"); err == nil {
		t.Error("stat on a closed service should fail")
	}
}

func TestSplitHostPort(t *testing.T) {
	for _, tt := range []struct {
		endpoint, host, port string
	}{
		{"example.com", "example.com", "21"},
		{"example.com:2121", "example.com", "2121"},
		{"[::1]:2121", "::1", "2121"},
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

func TestEntryFromList(t *testing.T) {
	f := newFakeFS()
	f.addFile("/logs/a.txt", []byte("abc"))
	c := &fakeConn{fs: f}

	if e, err := entryFromList(c, "/"); err != nil || e.Type != ftpclient.EntryTypeFolder {
		t.Fatalf("root: %v %v", e, err)
	}
	if e, err := entryFromList(c, "/logs"); err != nil || e.Type != ftpclient.EntryTypeFolder {
		t.Fatalf("dir: %v %v", e, err)
	}
	if e, err := entryFromList(c, "/logs/a.txt"); err != nil || e.Size != 3 {
		t.Fatalf("file: %v %v", e, err)
	}
	if _, err := entryFromList(c, "/logs/missing"); !notFound(err) {
		t.Fatalf("missing: want not found, got %v", err)
	}
}

type pipeDialer struct{}

func (pipeDialer) Dial(network, addr string) (net.Conn, error) { c, _ := net.Pipe(); return c, nil }

func TestProxyDialFuncWrapsDataConnsInTLS(t *testing.T) {
	dial := proxyDialFunc(pipeDialer{}, true, &tls.Config{})
	ctrl, _ := dial("tcp", "a:21")
	data, _ := dial("tcp", "a:2000")
	if _, ok := ctrl.(*tls.Conn); ok {
		t.Fatal("control connection must stay plain until AUTH TLS")
	}
	if _, ok := data.(*tls.Conn); !ok {
		t.Fatal("data connection must be TLS with FTPS")
	}
	if c, _ := proxyDialFunc(pipeDialer{}, false, nil)("tcp", "a:2000"); c == nil {
		t.Fatal("plain dial failed")
	}
}
