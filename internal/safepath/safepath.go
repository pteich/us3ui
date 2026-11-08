// Package safepath turns remote object keys into safe local file paths and
// publishes downloaded files relative to an open directory.
//
// Object keys are attacker-controlled strings in practice: a bucket can hold
// "../.ssh/id_rsa", "C:\boot.ini" or "CON". Join refuses any key that would
// escape the destination or name a file some platform cannot store safely.
// Filesystem operations stay relative to os.Root to contain symlinks too.
package safepath

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrExists reports that a file of the same name already exists at the
// destination. The existing file and temporary copy are left untouched.
var ErrExists = errors.New("a file of that name already exists")

// windowsReserved are device names Windows cannot store as regular files,
// including names that only differ by extension (NUL.txt).
var windowsReserved = map[string]struct{}{
	"con": {}, "prn": {}, "aux": {}, "nul": {},
	"com0": {}, "com1": {}, "com2": {}, "com3": {}, "com4": {},
	"com5": {}, "com6": {}, "com7": {}, "com8": {}, "com9": {},
	"lpt1": {}, "lpt2": {}, "lpt3": {}, "lpt4": {}, "lpt5": {},
	"lpt6": {}, "lpt7": {}, "lpt8": {}, "lpt9": {},
	"conin$": {}, "conout$": {},
}

// Join maps an object key to a relative file path under root, preserving its
// directory structure and creating the parent directories. It rejects keys
// with empty, relative-navigation (".", ".."), absolute (leading slash or
// drive letters), or platform-unsafe segments (backslashes, colons, control
// characters, Windows reserved or trailing dot/space names).
func Join(root *os.Root, key string) (string, error) {
	if key == "" {
		return "", fmt.Errorf("cannot download an empty object key")
	}
	parts := strings.Split(key, "/")
	for _, part := range parts {
		if err := validSegment(part); err != nil {
			return "", fmt.Errorf("cannot download %q: %w", key, err)
		}
	}
	final := filepath.Join(parts...)
	if err := root.MkdirAll(filepath.Dir(final), 0o755); err != nil {
		return "", err
	}
	return final, nil
}

// validSegment rejects one path segment of an object key.
func validSegment(part string) error {
	switch part {
	case "":
		return fmt.Errorf("empty name (duplicate or trailing slash)")
	case ".", "..":
		return fmt.Errorf("relative name %q", part)
	}
	if strings.ContainsAny(part, "\\:\x00") {
		return fmt.Errorf("unsafe character in %q", part)
	}
	for _, r := range part {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("control character in %q", part)
		}
	}
	if strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
		return fmt.Errorf("%q ends with a dot or space", part)
	}
	name := part
	if i := strings.IndexByte(name, '.'); i != -1 {
		name = name[:i]
	}
	if _, reserved := windowsReserved[strings.ToLower(name)]; reserved {
		return fmt.Errorf("%q is a reserved Windows device name", part)
	}
	return nil
}

// Publish publishes a finished temporary file within root. Replacement must
// be explicitly authorized; otherwise an exclusive link protects existing
// files. The caller removes the temporary name after success or failure.
func Publish(root *os.Root, tempPath, final string, replace bool) error {
	if replace {
		return root.Rename(tempPath, final)
	}
	err := root.Link(tempPath, final)
	if errors.Is(err, os.ErrExist) {
		return ErrExists
	}
	if err != nil {
		return fmt.Errorf("cannot publish without replacing an existing file: %w", err)
	}
	return nil
}
