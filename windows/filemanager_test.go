package windows

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/storage"
	minio "github.com/minio/minio-go/v7"
)

func makeObjects(keys ...string) []minio.ObjectInfo {
	objs := make([]minio.ObjectInfo, len(keys))
	for i, k := range keys {
		objs[i] = minio.ObjectInfo{Key: k}
	}
	return objs
}

func TestFilterObjectsNoFilter(t *testing.T) {
	objs := makeObjects("logs/app.log", "data/report.csv", "README.md")
	fm := &FileManager{
		allObjects:     objs,
		searchTerm:     "",
		selectedPrefix: "all",
	}

	got := fm.filterObjectsLocked()
	if len(got) != len(objs) {
		t.Errorf("filterObjectsLocked() returned %d objects, want %d", len(got), len(objs))
	}
}

func TestFilterObjectsSearchOnly(t *testing.T) {
	objs := makeObjects("logs/app.log", "data/report.csv", "README.md", "LOGS/system.log")
	fm := &FileManager{
		allObjects:     objs,
		searchTerm:     "log", // case-insensitive
		selectedPrefix: "all",
	}

	got := fm.filterObjectsLocked()
	// "logs/app.log" matches ("log" in "logs/app.log")
	// "LOGS/system.log" matches (case-insensitive)
	// "data/report.csv" does not match
	// "README.md" does not match
	if len(got) != 2 {
		t.Errorf("filterObjectsLocked() returned %d objects, want 2; keys: %v", len(got), keysOf(got))
	}
}

func TestFilterObjectsPrefixOnly(t *testing.T) {
	objs := makeObjects("logs/app.log", "logs/system.log", "data/report.csv", "README.md")
	fm := &FileManager{
		allObjects:     objs,
		searchTerm:     "",
		selectedPrefix: "logs",
	}

	got := fm.filterObjectsLocked()
	if len(got) != 2 {
		t.Errorf("filterObjectsLocked() returned %d objects, want 2; keys: %v", len(got), keysOf(got))
	}
	for _, obj := range got {
		if len(obj.Key) < 4 || obj.Key[:4] != "logs" {
			t.Errorf("unexpected key %q — does not start with 'logs'", obj.Key)
		}
	}
}

func TestFileURIsFiltersNonFileSchemes(t *testing.T) {
	fileURI := storage.NewFileURI("/tmp/report.csv")
	httpURI, err := storage.ParseURI("https://example.com/report.csv")
	if err != nil {
		t.Fatal(err)
	}

	got := fileURIs([]fyne.URI{fileURI, httpURI})
	if len(got) != 1 {
		t.Fatalf("fileURIs() returned %d URIs, want 1", len(got))
	}
	if got[0].String() != fileURI.String() {
		t.Fatalf("fileURIs() returned %q, want %q", got[0].String(), fileURI.String())
	}
}

func TestUploadObjectName(t *testing.T) {
	uri := storage.NewFileURI("/tmp/report.csv")

	tests := []struct {
		name   string
		prefix string
		want   string
	}{
		{name: "all", prefix: "all", want: "report.csv"},
		{name: "root", prefix: "root", want: "report.csv"},
		{name: "empty", prefix: "", want: "report.csv"},
		{name: "folder", prefix: "reports/2026", want: "reports/2026/report.csv"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := uploadObjectName(tt.prefix, uri); got != tt.want {
				t.Fatalf("uploadObjectName(%q) = %q, want %q", tt.prefix, got, tt.want)
			}
		})
	}
}

func TestUploadSummaryMessage(t *testing.T) {
	tests := []struct {
		name     string
		uploaded int
		total    int
		failures []string
		want     string
	}{
		{name: "one", uploaded: 1, total: 1, want: "Uploaded 1 file."},
		{name: "many", uploaded: 3, total: 3, want: "Uploaded 3 files."},
		{name: "failures", uploaded: 1, total: 2, failures: []string{"broken.txt: denied"}, want: "Uploaded 1 of 2 files.\n\nFailed:\n- broken.txt: denied"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := uploadSummaryMessage(tt.uploaded, tt.total, tt.failures); got != tt.want {
				t.Fatalf("uploadSummaryMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDownloadSummaryMessage(t *testing.T) {
	tests := []struct {
		name       string
		downloaded int
		total      int
		failures   []string
		want       string
	}{
		{name: "one", downloaded: 1, total: 1, want: "Downloaded 1 file."},
		{name: "many", downloaded: 3, total: 3, want: "Downloaded 3 files."},
		{name: "failures", downloaded: 1, total: 2, failures: []string{"broken.txt: denied"}, want: "Downloaded 1 of 2 files.\n\nFailed:\n- broken.txt: denied"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := downloadSummaryMessage(tt.downloaded, tt.total, tt.failures); got != tt.want {
				t.Fatalf("downloadSummaryMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func keysOf(objs []minio.ObjectInfo) []string {
	keys := make([]string, len(objs))
	for i, o := range objs {
		keys[i] = o.Key
	}
	return keys
}
