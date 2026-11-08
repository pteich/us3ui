package safepath

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJoinMapsKeysToPaths(t *testing.T) {
	dest := t.TempDir()

	tests := []struct {
		key  string
		want string
	}{
		{"README.md", "README.md"},
		{"logs/app.log", filepath.Join("logs", "app.log")},
		{"a/b/c/d.txt", filepath.Join("a", "b", "c", "d.txt")},
		{"weißes foto.jpg", "weißes foto.jpg"},
	}
	for _, tt := range tests {
		got, err := Join(dest, tt.key)
		if err != nil {
			t.Errorf("Join(%q): %v", tt.key, err)
			continue
		}
		if want := filepath.Join(dest, tt.want); got != want {
			t.Errorf("Join(%q) = %q, want %q", tt.key, got, want)
		}
	}

	// Nested keys get their parent directories created.
	if _, err := os.Stat(filepath.Join(dest, "a", "b", "c")); err != nil {
		t.Errorf("parent directories not created: %v", err)
	}
}

func TestJoinRejectsUnsafeKeys(t *testing.T) {
	for _, key := range []string{
		"",
		"..",
		"../escape.txt",
		"logs/../../escape.txt",
		"a/./b.txt",
		"/absolute.txt",
		"dir/",
		"a//b.txt",
		`back\slash.txt`,
		"C:drive.txt",
		"colon:name.txt",
		"control\x01char.txt",
		"trailing dot.txt.",
		"trailing space ",
		"CON",
		"con.txt",
		"NUL",
		"COM1.log",
		"LPT9.log",
		"CONIN$",
		"..hidden", // legal: dotfiles are fine
	} {
		if key == "..hidden" {
			continue // covered below as an accepted case
		}
		if got, err := Join(t.TempDir(), key); err == nil {
			t.Errorf("Join(%q) = %q, want an error", key, got)
		}
	}

	// Dotfiles and names containing reserved words mid-name are fine.
	dest := t.TempDir()
	for _, key := range []string{"..hidden", "conman.txt", "my.nul.txt", "icon"} {
		if _, err := Join(dest, key); err != nil {
			t.Errorf("Join(%q): %v", key, err)
		}
	}
}

func TestPublishNeverReplaces(t *testing.T) {
	dir := t.TempDir()
	final := filepath.Join(dir, "report.pdf")
	if err := os.WriteFile(final, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}

	tmp := filepath.Join(dir, "temp.pdf")
	if err := os.WriteFile(tmp, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Publish(tmp, final); !errors.Is(err, ErrExists) {
		t.Errorf("Publish onto existing file: err = %v, want ErrExists", err)
	}
	if got, err := os.ReadFile(final); err != nil || string(got) != "existing" {
		t.Errorf("existing file changed: %q, %v", got, err)
	}
	if _, err := os.Stat(tmp); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("temp file not removed: %v", err)
	}
}

func TestPublishesFreshFile(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, "temp")
	if err := os.WriteFile(tmp, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	final := filepath.Join(dir, "final")
	if err := Publish(tmp, final); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	got, err := os.ReadFile(final)
	if err != nil || string(got) != "content" {
		t.Errorf("published content: %q, %v", got, err)
	}
	if _, err := os.Stat(tmp); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("temp file still present: %v", err)
	}
}

func TestJoinResultStaysInsideDest(t *testing.T) {
	// Even a crafted key that survives segment checks must not resolve
	// outside the destination via symlinks created below.
	dest := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dest, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	got, err := Join(dest, "link/../file.txt")
	if err == nil && !strings.HasPrefix(got, dest) {
		t.Errorf("Join resolved outside dest: %q", got)
	}
}
