// Package sftp is the SFTP implementation of storage.Store. Object keys are
// paths relative to the connection's start directory, so the rest of the app
// (directory tree, search, sync) works unchanged.
package sftp

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/kirsle/configdir"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/pteich/us3ui/config"
	"github.com/pteich/us3ui/storage"
)

const (
	dialTimeout    = 10 * time.Second
	keepaliveEvery = 25 * time.Second
)

// sftpConn is the slice of *sftp.Client the service uses, narrowed so tests
// can supply an in-memory fake.
type sftpConn interface {
	Stat(p string) (os.FileInfo, error)
	ReadDir(p string) ([]os.FileInfo, error)
	MkdirAll(p string) error
	Create(p string) (io.WriteCloser, error)
	Open(p string) (io.ReadCloser, error)
	Remove(p string) error
	Getwd() (string, error)
	RealPath(p string) (string, error)
	Close() error
}

// realConn adapts *sftp.Client to sftpConn; only the file methods need
// translating to the io interfaces.
type realConn struct{ *sftp.Client }

func (c realConn) Create(p string) (io.WriteCloser, error) { return c.Client.Create(p) }
func (c realConn) Open(p string) (io.ReadCloser, error)    { return c.Client.Open(p) }

var _ storage.Store = (*Service)(nil)

// Service talks to one SFTP server over a persistent SSH connection. It
// reconnects lazily when the connection drops; failed operations surface
// their error, the next operation runs on a fresh connection.
type Service struct {
	cfg       config.S3Config
	hostsPath string

	mu     sync.Mutex // guards client, root and closed
	client sftpConn
	root   string // absolute directory all keys are relative to

	lister storage.WalkLister // cached tree walk backing ListObjectsBatch

	closed     bool
	stopKeepal chan struct{}
}

// New connects to the SFTP server described by cfg. cfg.Endpoint is the host
// with an optional port (22 by default), cfg.AccessKey the username,
// cfg.SecretKey the password and cfg.KeyPath an optional private key file
// (cfg.KeyPassphrase decrypts it). cfg.Prefix is the start directory all
// object keys are relative to; empty means the account's home directory.
func New(cfg config.S3Config) (*Service, error) {
	if cfg.Endpoint == "" {
		return nil, errors.New("host is required")
	}
	if cfg.AccessKey == "" {
		return nil, errors.New("username is required")
	}
	if cfg.SecretKey == "" && cfg.KeyPath == "" {
		return nil, errors.New("password or private key file is required")
	}
	hostsPath, err := knownHostsPath()
	if err != nil {
		return nil, fmt.Errorf("preparing known hosts file: %w", err)
	}
	s := &Service{cfg: cfg, hostsPath: hostsPath, stopKeepal: make(chan struct{})}
	if err := s.connect(); err != nil {
		return nil, err
	}
	go s.keepalive()
	return s, nil
}

// connect dials the server and stores the live connection. Callers hold s.mu.
func (s *Service) connect() error {
	host, port := splitHostPort(s.cfg.Endpoint)
	auth, err := authMethods(s.cfg)
	if err != nil {
		return err
	}
	sshCfg := &ssh.ClientConfig{
		User:            s.cfg.AccessKey,
		Auth:            auth,
		HostKeyCallback: s.hostKeyCallback,
		Timeout:         dialTimeout,
	}
	conn, err := ssh.Dial("tcp", net.JoinHostPort(host, port), sshCfg)
	if err != nil {
		return fmt.Errorf("failed to connect to server: %w", err)
	}
	sc, err := sftp.NewClient(conn)
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to open SFTP session: %w", err)
	}
	client := realConn{sc}
	root := s.root
	if root == "" {
		root, err = resolveRoot(client, s.cfg.Prefix)
		if err != nil {
			client.Close()
			return err
		}
	}
	s.client, s.root = client, root
	return nil
}

// resolveRoot turns the start directory into an absolute server path.
func resolveRoot(c sftpConn, prefix string) (string, error) {
	p := strings.TrimSpace(prefix)
	if p == "" {
		p = "."
	}
	root, err := c.RealPath(p)
	if err != nil {
		return "", fmt.Errorf("resolving start directory %q: %w", p, err)
	}
	info, err := c.Stat(root)
	if err != nil {
		return "", fmt.Errorf("start directory %q: %w", p, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("start directory %q is not a directory", p)
	}
	return root, nil
}

// splitHostPort splits "host[:port]", defaulting to the SFTP port 22.
func splitHostPort(endpoint string) (host, port string) {
	endpoint = strings.TrimSpace(endpoint)
	if h, p, err := net.SplitHostPort(endpoint); err == nil {
		return h, p
	}
	return endpoint, "22"
}

// authMethods builds the SSH auth methods: the private key when configured
// (falling back to it when the key is encrypted and a passphrase is set),
// then password and keyboard-interactive for servers that require it.
func authMethods(cfg config.S3Config) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod
	if cfg.KeyPath != "" {
		pem, err := os.ReadFile(cfg.KeyPath)
		if err != nil {
			return nil, fmt.Errorf("reading private key: %w", err)
		}
		signer, err := ssh.ParsePrivateKey(pem)
		if err != nil {
			var needsPass *ssh.PassphraseMissingError
			if errors.As(err, &needsPass) && cfg.KeyPassphrase != "" {
				signer, err = ssh.ParsePrivateKeyWithPassphrase(pem, []byte(cfg.KeyPassphrase))
			} else if errors.As(err, &needsPass) {
				return nil, fmt.Errorf("private key %s is encrypted: set a key passphrase", cfg.KeyPath)
			}
		}
		if err != nil {
			return nil, fmt.Errorf("private key %s: %w", cfg.KeyPath, err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}
	if pass := cfg.SecretKey; pass != "" {
		methods = append(methods, ssh.Password(pass))
		methods = append(methods, ssh.KeyboardInteractive(func(_, _ string, questions []string, _ []bool) ([]string, error) {
			answers := make([]string, len(questions))
			for i := range answers {
				answers[i] = pass
			}
			return answers, nil
		}))
	}
	if len(methods) == 0 {
		return nil, errors.New("no authentication method: set a password or private key file")
	}
	return methods, nil
}

// conn returns the live connection, reconnecting when the previous one
// dropped.
func (s *Service) conn() (sftpConn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errors.New("connection is closed")
	}
	if s.client == nil {
		if err := s.connect(); err != nil {
			return nil, err
		}
	}
	return s.client, nil
}

// drop closes c if it is still the current connection.
func (s *Service) drop(c sftpConn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client == c {
		c.Close()
		s.client = nil
	}
}

// failIfDead pings the connection after an error and drops it when it no
// longer responds, so the next operation reconnects instead of failing
// forever. The original error is returned either way.
func (s *Service) failIfDead(c sftpConn, err error) error {
	if err == nil {
		return nil
	}
	if _, pingErr := c.Getwd(); pingErr != nil {
		s.drop(c)
	}
	return err
}

// keepalive probes the connection periodically and drops it when the server
// stopped responding, keeping long idle sessions alive and detecting dead
// ones early.
func (s *Service) keepalive() {
	t := time.NewTicker(keepaliveEvery)
	defer t.Stop()
	for {
		select {
		case <-s.stopKeepal:
			return
		case <-t.C:
			s.mu.Lock()
			c := s.client
			s.mu.Unlock()
			if c == nil {
				continue
			}
			if _, err := c.Getwd(); err != nil {
				s.drop(c)
			}
		}
	}
}

// Close shuts the connection down.
func (s *Service) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	select {
	case <-s.stopKeepal:
	default:
		close(s.stopKeepal)
	}
	if s.client != nil {
		err := s.client.Close()
		s.client = nil
		return err
	}
	return nil
}

// knownHostsPath returns (and creates on first use) the known_hosts file in
// the app's config directory.
func knownHostsPath() (string, error) {
	dir := configdir.LocalConfig(config.Name)
	if err := configdir.MakePath(dir); err != nil {
		return "", err
	}
	p := dir + "/known_hosts"
	if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(p, nil, 0o600); err != nil {
			return "", err
		}
	}
	return p, nil
}
