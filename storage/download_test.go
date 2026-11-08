package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeStore serves in-memory objects; changed keys fail the ETag condition
// like a remote that was rewritten after listing.
type fakeStore struct {
	objects   map[string]string
	changed   map[string]bool
	downloads int
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
	f.downloads++
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
	f.downloads++
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
		func(n int64) { seen = n }, nil)
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

	_, err := DownloadToFile(context.Background(), svc, "a.txt", ObjectInfo{Key: "a.txt", Size: 4}, dest, nil, nil)
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

	_, err := DownloadToFile(context.Background(), svc, "a.txt", ObjectInfo{Key: "a.txt", Size: 3}, dest, nil, nil)
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

	_, err := DownloadToFile(context.Background(), svc, "a.txt", ObjectInfo{Key: "a.txt", Size: 2, ETag: "etag-a.txt"}, dest, nil, nil)
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

	if _, err := DownloadToFile(context.Background(), svc, "../escape", ObjectInfo{Key: "../escape", Size: 1}, dest, nil, nil); err == nil {
		t.Fatal("escaped the destination without an error")
	}
}

func TestDownloadToFileRejectsSymlinkEscape(t *testing.T) {
	dest, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dest, "link")); err != nil {
		t.Skip(err)
	}
	svc := &fakeStore{objects: map[string]string{"link/file.txt": "new"}}
	_, err := DownloadToFile(context.Background(), svc, "link/file.txt", ObjectInfo{Size: 3}, dest, nil, nil)
	if err == nil {
		t.Fatal("download accepted a symlink outside the destination")
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
		t.Fatalf("wrote outside the destination: %v, %v", entries, err)
	}
}

func TestDownloadToFileSkipsBeforeOpeningRemote(t *testing.T) {
	dest := t.TempDir()
	if err := os.WriteFile(filepath.Join(dest, "a.txt"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	// An unavailable remote must not turn a local skip into a failed download.
	_, err := DownloadToFile(context.Background(), &fakeStore{}, "a.txt", ObjectInfo{Size: 3}, dest, nil, nil)
	if !errors.Is(err, ErrExists) {
		t.Fatalf("err = %v, want ErrExists without opening the remote", err)
	}
}

func TestDownloadToFileCancellationBeforePublish(t *testing.T) {
	dest := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc := &fakeStore{objects: map[string]string{"a.txt": "new"}}
	_, err := DownloadToFile(ctx, svc, "a.txt", ObjectInfo{Size: 3}, dest, func(int64) { cancel() }, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "a.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("canceled download was published: %v", err)
	}
}

func TestDownloadToFileConflictChoices(t *testing.T) {
	for _, late := range []bool{false, true} {
		for _, tc := range []struct {
			name   string
			choice DownloadConflict
			want   string
		}{
			{"replace", ReplaceDownload, "a.txt"},
			{"keep both", KeepBothDownloads, "a (2).txt"},
			{"skip", SkipDownload, ""},
		} {
			t.Run(fmt.Sprintf("%s/late=%v", tc.name, late), func(t *testing.T) {
				dest := t.TempDir()
				original := filepath.Join(dest, "a.txt")
				write := func(name, content string) {
					t.Helper()
					if err := os.WriteFile(filepath.Join(dest, name), []byte(content), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				write("a (1).txt", "first copy")
				if !late {
					write("a.txt", "old")
				}
				svc := &fakeStore{objects: map[string]string{"a.txt": "new"}}
				asked := 0
				path, err := DownloadToFile(context.Background(), svc, "a.txt", ObjectInfo{Size: 3}, dest,
					func(int64) {
						if late {
							write("a.txt", "old")
						}
					},
					func(path string) (DownloadConflict, error) {
						asked++
						if path != original {
							t.Fatalf("conflict path = %q", path)
						}
						if !late && svc.downloads != 0 {
							t.Fatal("asked only after opening remote")
						}
						if late && len(tempLeftovers(t, dest)) != 1 {
							t.Fatal("lost completed temporary copy before asking")
						}
						return tc.choice, nil
					})
				if tc.choice == SkipDownload {
					if !errors.Is(err, ErrExists) {
						t.Fatalf("skip error = %v", err)
					}
				} else {
					if err != nil || path != filepath.Join(dest, tc.want) {
						t.Fatalf("path=%q, err=%v", path, err)
					}
					if got, err := os.ReadFile(path); err != nil || string(got) != "new" {
						t.Fatalf("download=%q, err=%v", got, err)
					}
				}
				if tc.choice != ReplaceDownload {
					if got, err := os.ReadFile(original); err != nil || string(got) != "old" {
						t.Fatalf("original=%q, err=%v", got, err)
					}
				}
				wantDownloads := 1
				if tc.choice == SkipDownload && !late {
					wantDownloads = 0
				}
				if asked != 1 || svc.downloads != wantDownloads {
					t.Fatalf("asked=%d, remote opens=%d", asked, svc.downloads)
				}
				if left := tempLeftovers(t, dest); len(left) != 0 {
					t.Fatalf("temporary files left: %v", left)
				}
			})
		}
	}
}

func TestDownloadToFileFailedReplacementPreservesOriginal(t *testing.T) {
	for _, cancelDownload := range []bool{false, true} {
		t.Run(fmt.Sprintf("canceled=%v", cancelDownload), func(t *testing.T) {
			dest := t.TempDir()
			original := filepath.Join(dest, "a.txt")
			if err := os.WriteFile(original, []byte("old"), 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			svc := &fakeStore{objects: map[string]string{"a.txt": "new"}}
			_, err := DownloadToFile(ctx, svc, "a.txt", ObjectInfo{Size: 4}, dest,
				func(int64) {
					if cancelDownload {
						cancel()
					}
				},
				func(string) (DownloadConflict, error) { return ReplaceDownload, nil })
			if err == nil {
				t.Fatal("unverified or canceled replacement succeeded")
			}
			if got, err := os.ReadFile(original); err != nil || string(got) != "old" {
				t.Fatalf("original=%q, err=%v", got, err)
			}
			if left := tempLeftovers(t, dest); len(left) != 0 {
				t.Fatalf("temporary files left: %v", left)
			}
		})
	}
}

func TestDownloadToFileKeepBothConcurrent(t *testing.T) {
	dest := t.TempDir()
	if err := os.WriteFile(filepath.Join(dest, "a.txt"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	paths := make(chan string, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			svc := &fakeStore{objects: map[string]string{"a.txt": "new"}}
			path, err := DownloadToFile(context.Background(), svc, "a.txt", ObjectInfo{Size: 3}, dest, nil,
				func(string) (DownloadConflict, error) { return KeepBothDownloads, nil })
			paths <- filepath.Base(path)
			errs <- err
		})
	}
	wg.Wait()
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	a, b := <-paths, <-paths
	if a == b || (a != "a (1).txt" && b != "a (1).txt") || (a != "a (2).txt" && b != "a (2).txt") {
		t.Fatalf("concurrent downloads did not get distinct names: %q, %q", a, b)
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
