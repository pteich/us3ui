package sftp

import (
	"errors"
	"fmt"
	"net"
	"os"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// hostsMu serializes writes to the known_hosts file.
var hostsMu sync.Mutex

// hostKeyCallback verifies the server's host key against the known_hosts
// file with trust on first use: an unknown host is recorded on the first
// connection, a later mismatch is refused as a possible man-in-the-middle
// attack.
func (s *Service) hostKeyCallback(hostname string, remote net.Addr, key ssh.PublicKey) error {
	if _, _, err := net.SplitHostPort(hostname); err != nil {
		hostname += ":22" // the known_hosts checker needs host:port form
	}
	static, err := knownhosts.New(s.hostsPath)
	if err != nil {
		return fmt.Errorf("reading known hosts: %w", err)
	}
	err = static(hostname, remote, key)
	if err == nil {
		return nil
	}
	var keyErr *knownhosts.KeyError
	if !errors.As(err, &keyErr) {
		return err
	}
	if len(keyErr.Want) == 0 {
		return appendKnownHost(s.hostsPath, hostname, key) // unknown host: trust on first use
	}
	return fmt.Errorf("HOST KEY HAS CHANGED for %s! This may be a man-in-the-middle attack. If you are certain the server regenerated its key, remove its line from %s", hostname, s.hostsPath)
}

// appendKnownHost records the host key in the known_hosts file.
func appendKnownHost(p, hostname string, key ssh.PublicKey) error {
	hostsMu.Lock()
	defer hostsMu.Unlock()
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintln(f, knownhosts.Line([]string{hostname}, key))
	return err
}
