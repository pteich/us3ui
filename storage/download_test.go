package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeStore serves in-memory objects; changed keys fail the ETag condition
// like a remote that was rewritten after listing.
type fakeStore struct {
	objects map[string]string
	changed map[string]bool
}

func (f *fakeStore) ListObjectsBatch(context.Context, string, string, int) ([]ObjectInfo, error) {
	return nil, nil
}

func (f *fakeStore) StatObject(_ context.Context, key string) (ObjectInfo, error) {
	content, ok := f.objects[key]
	if !ok {
		return ObjectInfo{}, errors.New("no such object")
	}
	return ObjectInfo{Key: key, Size: int64(len(content)), ETag: fmt.Sprintf("etag-%s", key)}, nil
}

func (f *fakeStore) UploadObjectReader(context.Context, string, io.Reader, int64, string) error {
	return errors.New("not implemented")
}

func (f *fakeStore) DownloadObject(_ context.Context, key string) (io.ReadCloser, error) {
	content, ok := f.objects[key]
	if !ok {
		return nil, errors.New("no such object")
	}
	return io.NopCloser(strings.NewReader(content)), nil
}

func (f *fakeStore) DeleteObject(context.Context, string) error {
	return errors.New("not implemented")
}

func (f *fakeStore) DownloadObjectIfMatch(_ context.Context, key, etag string) (io.ReadCloser, error) {
	content, ok := f.objects[key]
	if !ok {
		return nil, errors.New("no such object")
	}
	if f.changed[key] || etag != fmt.Sprintf("etag-%s", key) {
		return condReader{}, fmt.Errorf("%w: stale listing", ErrObjectChanged)
	}
	return io.NopCloser(strings.NewReader(content)), nil
}

type condReader struct{}

func (condReader) Read([]byte) (int, error) { return 0, fmt.Errorf("wrapped: %w", ErrObjectChanged) }
func (condReader) Close() error             { return nil }

func TestDownloadToFilePublishesVerifiedContent(t *testing.T) {
	svc := &fakeStore{objects: map[string]string{"logs/app.log": "hello world"}}
	dest := t.TempDir()

	var seen int64
	path, err := DownloadToFile(context.Background(), svc, "logs/app.log",
		ObjectInfo{Key: "logs/app.log", Size: 11, ETag: "etag-logs/app.log"}, dest,
		func(n int64) { seen = n })
	if err != nil {
		t.Fatalf("DownloadToFile: %v", err)
	}
	if path != filepath.Join(dest, "logs", "app.log") {
		t.Errorf("path = %q", path)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "hello world" {
		t.Errorf("content = %q, %v", got, err)
	}
	if seen != 11 {
		t.Errorf("progress reported %d bytes, want 11", seen)
	}

	entries, err := os.ReadDir(dest)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "logs" {
		t.Errorf("destination not clean after download: %v", entries)
	}
}

func TestDownloadToFileRejectsSizeMismatch(t *testing.T) {
	svc := &fakeStore{objects: map[string]string{"a.txt": "12345"}}
	dest := t.TempDir()

	_, err := DownloadToFile(context.Background(), svc, "a.txt", ObjectInfo{Key: "a.txt", Size: 4}, dest, nil)
	if err == nil || !strings.Contains(err.Error(), "bytes") {
		t.Fatalf("size mismatch: err = %v, want a byte-count error", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "a.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("partial file left at target: %v", err)
	}
	if left := tempLeftovers(t, dest); len(left) > 0 {
		t.Errorf("temp files left behind: %v", left)
	}
}

func TestDownloadToFileNeverOverwrites(t *testing.T) {
	svc := &fakeStore{objects: map[string]string{"a.txt": "new"}}
	dest := t.TempDir()
	existing := filepath.Join(dest, "a.txt")
	if err := os.WriteFile(existing, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := DownloadToFile(context.Background(), svc, "a.txt", ObjectInfo{Key: "a.txt", Size: 3}, dest, nil)
	if !errors.Is(err, ErrExists) {
		t.Fatalf("err = %v, want ErrExists", err)
	}
	got, _ := os.ReadFile(existing)
	if string(got) != "old" {
		t.Errorf("existing file was replaced: %q", got)
	}
	if left := tempLeftovers(t, dest); len(left) > 0 {
		t.Errorf("temp files left behind: %v", left)
	}
}

func TestDownloadToFileDetectsChangedObject(t *testing.T) {
	svc := &fakeStore{
		objects: map[string]string{"a.txt": "v2"},
		changed: map[string]bool{"a.txt": true},
	}
	dest := t.TempDir()

	_, err := DownloadToFile(context.Background(), svc, "a.txt", ObjectInfo{Key: "a.txt", Size: 2, ETag: "etag-a.txt"}, dest, nil)
	if !errors.Is(err, ErrObjectChanged) {
		t.Fatalf("err = %v, want ErrObjectChanged", err)
	}
	if left := tempLeftovers(t, dest); len(left) > 0 {
		t.Errorf("temp files left behind: %v", left)
	}
}

func TestDownloadToFileRejectsEscapingKeys(t *testing.T) {
	svc := &fakeStore{objects: map[string]string{"../escape": "x"}}
	dest := t.TempDir()

	if _, err := DownloadToFile(context.Background(), svc, "../escape", ObjectInfo{Key: "../escape", Size: 1}, dest, nil); err == nil {
		t.Fatal("escaped the destination without an error")
	}
}

func tempLeftovers(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".us3ui-download-") {
			out = append(out, e.Name())
		}
	}
	return out
}
