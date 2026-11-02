package sftp

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func testKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := ssh.NewPublicKey(priv.Public())
	if err != nil {
		t.Fatal(err)
	}
	return pub
}

// fakeAddr is a net.Addr for callback tests; knownhosts reads its address.
type fakeAddr string

func (f fakeAddr) Network() string { return "tcp" }
func (f fakeAddr) String() string  { return string(f) }

func TestHostKeyTrustOnFirstUse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	s := &Service{hostsPath: path}
	host := "example.com:22"
	key := testKey(t)

	// Unknown host: recorded (trust on first use).
	if err := s.hostKeyCallback(host, fakeAddr("1.2.3.4:22"), key); err != nil {
		t.Fatalf("first use: %v", err)
	}
	if raw, err := os.ReadFile(path); err != nil || !strings.Contains(string(raw), "example.com") {
		t.Fatalf("known_hosts should record example.com: %q, %v", raw, err)
	}

	// Same host, same key: accepted.
	if err := s.hostKeyCallback(host, fakeAddr("1.2.3.4:22"), key); err != nil {
		t.Fatalf("second use: %v", err)
	}

	// Same host, different key: refused as a possible MITM.
	if err := s.hostKeyCallback(host, fakeAddr("1.2.3.4:22"), testKey(t)); err == nil || !strings.Contains(err.Error(), "HOST KEY HAS CHANGED") {
		t.Fatalf("changed key err = %v, want HOST KEY HAS CHANGED", err)
	}
}

func TestHostKeyMatchesWithAndWithoutDefaultPort(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	s := &Service{hostsPath: path}
	key := testKey(t)

	// Recorded without a port...
	if err := s.hostKeyCallback("example.com", fakeAddr("1.2.3.4:22"), key); err != nil {
		t.Fatalf("first use: %v", err)
	}
	// ...verified with the default port appended, and vice versa.
	if err := s.hostKeyCallback("example.com:22", fakeAddr("1.2.3.4:22"), key); err != nil {
		t.Fatalf("with port: %v", err)
	}
	s2 := &Service{hostsPath: path}
	if err := s2.hostKeyCallback("other.example.com:2222", fakeAddr("1.2.3.4:2222"), key); err != nil {
		t.Fatalf("first use with port: %v", err)
	}
	if err := s2.hostKeyCallback("other.example.com", fakeAddr("1.2.3.4:2222"), key); err != nil {
		t.Fatalf("normalized without port: %v", err)
	}
}
