// Package ftp is the FTP/FTPS implementation of storage.Store. Object keys
// are paths relative to the connection's start directory, like SFTP.
package ftp

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"path"
	"strings"
	"sync"
	"time"

	ftpclient "github.com/jlaffaye/ftp"
	"golang.org/x/net/proxy"

	"github.com/pteich/us3ui/config"
	"github.com/pteich/us3ui/storage"
)

const (
	dialTimeout  = 10 * time.Second
	idleRecheck  = 30 * time.Second // revalidate idle connections older than this
	maxIdleConns = 8
)

// ftpConn is the slice of *ftp.ServerConn the service uses, narrowed so
// tests can supply fakes.
type ftpConn interface {
	List(p string) ([]*ftpclient.Entry, error)
	GetEntry(p string) (*ftpclient.Entry, error)
	Stor(p string, r io.Reader) error
	Retr(p string) (io.ReadCloser, error)
	Delete(p string) error
	MakeDir(p string) error
	CurrentDir() (string, error)
	NoOp() error
	Quit() error
}

// serverConn adapts *ftp.ServerConn to ftpConn; Retr and GetEntry differ.
type serverConn struct{ *ftpclient.ServerConn }

func (c serverConn) Retr(p string) (io.ReadCloser, error) { return c.ServerConn.Retr(p) }

// GetEntry uses MLST and falls back to listing the parent directory on
// servers without MLST (e.g. goftp.io/server answers 502).
func (c serverConn) GetEntry(p string) (*ftpclient.Entry, error) {
	e, err := c.ServerConn.GetEntry(p)
	var proto *textproto.Error
	if errors.As(err, &proto) && (proto.Code == ftpclient.StatusBadCommand || proto.Code == ftpclient.StatusNotImplemented) {
		return entryFromList(c, p)
	}
	return e, err
}

// entryFromList finds p in a LIST of its parent directory. The root has no
// parent to list it in and is always a folder.
func entryFromList(c ftpConn, p string) (*ftpclient.Entry, error) {
	p = path.Clean("/" + strings.TrimPrefix(p, "/"))
	if p == "/" {
		return &ftpclient.Entry{Name: "/", Type: ftpclient.EntryTypeFolder}, nil
	}
	entries, err := c.List(path.Dir(p))
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.Name == path.Base(p) {
			return e, nil
		}
	}
	return nil, &textproto.Error{Code: ftpclient.StatusFileUnavailable, Msg: p + ": no such file or directory"}
}

var _ storage.Store = (*Service)(nil)

// Service talks to one FTP server over a pool of connections: an FTP control
// connection is not concurrent-safe and carries a single data transfer at a
// time, so parallel operations (the sync engine streams a download and an
// upload per worker) borrow a connection each. The pool grows on demand and
// is deliberately not capped — capping it below the callers' demand could
// deadlock a worker that holds one connection while waiting for another.
// Servers that limit sessions per IP answer extra dials with an error.
type Service struct {
	cfg  config.S3Config
	dial func() (ftpConn, error) // replaced in tests

	mu     sync.Mutex // guards idle and closed
	idle   map[ftpConn]time.Time
	closed bool

	root   string
	lister storage.WalkLister // cached tree walk backing ListObjectsBatch
}

// New connects to the FTP server described by cfg. cfg.Endpoint is the host
// with an optional port (21 by default), cfg.AccessKey the username,
// cfg.SecretKey the password (both empty means anonymous login) and
// cfg.Prefix the start directory all object keys are relative to. With
// cfg.UseSSL the connection is upgraded to FTPS (explicit TLS).
func New(cfg config.S3Config) (*Service, error) {
	if cfg.Endpoint == "" {
		return nil, errors.New("host is required")
	}
	user, pass := cfg.AccessKey, cfg.SecretKey
	if user == "" && pass == "" {
		user, pass = "anonymous", "anonymous@" // anonymous login
	}
	host, port := splitHostPort(cfg.Endpoint)
	addr := net.JoinHostPort(host, port)
	s := &Service{cfg: cfg, idle: map[ftpConn]time.Time{}}
	var socks proxy.Dialer
	if ph := strings.TrimSpace(cfg.ProxyHost); ph != "" {
		var auth *proxy.Auth
		if cfg.ProxyUser != "" || cfg.ProxyPassword != "" {
			auth = &proxy.Auth{User: cfg.ProxyUser, Password: cfg.ProxyPassword}
		}
		d, err := proxy.SOCKS5("tcp", ph, auth, &net.Dialer{Timeout: dialTimeout})
		if err != nil {
			return nil, fmt.Errorf("socks proxy: %w", err)
		}
		socks = d
	}
	s.dial = func() (ftpConn, error) { return dialConn(addr, host, cfg.UseSSL, user, pass, socks) }

	if err := s.withConn(context.Background(), func(c ftpConn) error {
		p := strings.TrimSpace(cfg.Prefix)
		if p == "" {
			dir, err := c.CurrentDir()
			if err != nil {
				return err
			}
			p = dir
		}
		e, err := c.GetEntry(path.Clean("/" + strings.TrimPrefix(p, "/")))
		if err != nil {
			return fmt.Errorf("start directory %q: %w", p, err)
		}
		if e.Type != ftpclient.EntryTypeFolder {
			return fmt.Errorf("start directory %q is not a directory", p)
		}
		s.root = path.Clean("/" + strings.TrimPrefix(p, "/"))
		return nil
	}); err != nil {
		return nil, err
	}
	return s, nil
}

// dialConn opens and logs into one FTP(S) connection, through the SOCKS
// proxy when socks is non-nil.
func dialConn(addr, host string, useTLS bool, user, pass string, socks proxy.Dialer) (ftpConn, error) {
	opts := []ftpclient.DialOption{ftpclient.DialWithTimeout(dialTimeout)}
	tlsCfg := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	if useTLS {
		opts = append(opts, ftpclient.DialWithExplicitTLS(tlsCfg))
	}
	if socks != nil {
		opts = append(opts, ftpclient.DialWithDialFunc(proxyDialFunc(socks, useTLS, tlsCfg)))
	}
	conn, err := ftpclient.Dial(addr, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to server: %w", err)
	}
	if err := conn.Login(user, pass); err != nil {
		conn.Quit()
		return nil, fmt.Errorf("login failed: %w", err)
	}
	return serverConn{conn}, nil
}

// proxyDialFunc dials control and data connections through the proxy. With
// a custom dial func the ftp library leaves data connections unencrypted, so
// with FTPS every dial after the first (the control connection, which the
// library upgrades itself via AUTH TLS) is wrapped in TLS here.
func proxyDialFunc(socks proxy.Dialer, useTLS bool, tlsCfg *tls.Config) func(network, address string) (net.Conn, error) {
	control := true
	return func(network, address string) (net.Conn, error) {
		conn, err := socks.Dial(network, address)
		if err != nil {
			return nil, err
		}
		if useTLS && !control {
			return tls.Client(conn, tlsCfg), nil
		}
		control = false
		return conn, nil
	}
}

// splitHostPort splits "host[:port]", defaulting to the FTP port 21.
func splitHostPort(endpoint string) (host, port string) {
	endpoint = strings.TrimSpace(endpoint)
	if h, p, err := net.SplitHostPort(endpoint); err == nil {
		return h, p
	}
	return endpoint, "21"
}

// withConn runs fn on a borrowed connection; when the failure was a network
// problem rather than a server reply, it tries once more on a fresh
// connection.
func (s *Service) withConn(ctx context.Context, fn func(ftpConn) error) error {
	err := s.tryConn(ctx, fn)
	if err != nil && !answered(err) && ctx.Err() == nil {
		return s.tryConn(ctx, fn)
	}
	return err
}

// tryConn borrows a connection, runs fn and returns it to the pool when the
// server is still reachable.
func (s *Service) tryConn(ctx context.Context, fn func(ftpConn) error) error {
	c, err := s.acquire(ctx)
	if err != nil {
		return err
	}
	err = fn(c)
	if answered(err) {
		s.release(c)
	} else {
		s.discard(c)
	}
	return err
}

// acquire borrows a connection, dialing a new one when none is idle. Idle
// connections older than idleRecheck are validated with NOOP first.
func (s *Service) acquire(ctx context.Context) (ftpConn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for range 3 {
		c, since := s.popIdle()
		if c == nil {
			break
		}
		if time.Since(since) < idleRecheck {
			return c, nil
		}
		if err := c.NoOp(); err == nil {
			return c, nil
		}
		c.Quit() // stale; drop it
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return nil, errors.New("connection is closed")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.dial()
}

func (s *Service) popIdle() (ftpConn, time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var oldest ftpConn
	var since time.Time
	for c, t := range s.idle {
		if oldest == nil || t.Before(since) {
			oldest, since = c, t
		}
	}
	if oldest != nil {
		delete(s.idle, oldest)
	}
	return oldest, since
}

// release returns a healthy connection to the pool.
func (s *Service) release(c ftpConn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || len(s.idle) >= maxIdleConns {
		go c.Quit()
		return
	}
	s.idle[c] = time.Now()
}

// discard drops an unhealthy connection.
func (s *Service) discard(c ftpConn) {
	go c.Quit()
}

// Close quits all pooled connections.
func (s *Service) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	for c := range s.idle {
		go c.Quit()
	}
	s.idle = map[ftpConn]time.Time{}
	return nil
}
