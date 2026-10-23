package ui

import (
	"github.com/pteich/us3ui/config"
	"github.com/pteich/us3ui/s3"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
)

func TestByteCountSI(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{0, "0 B"},
		{999, "999 B"},
		{1000, "1.0 kB"},
		{1500, "1.5 kB"},
		{1000000, "1.0 MB"},
		{1500000, "1.5 MB"},
		{1000000000, "1.0 GB"},
	}
	for _, tt := range tests {
		if got := byteCountSI(tt.input); got != tt.expected {
			t.Errorf("byteCountSI(%d) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func objs(keys ...string) []minio.ObjectInfo {
	out := make([]minio.ObjectInfo, len(keys))
	for i, k := range keys {
		out[i] = minio.ObjectInfo{Key: k}
	}
	return out
}

func keys(o []minio.ObjectInfo) []string {
	out := make([]string, len(o))
	for i := range o {
		out[i] = o[i].Key
	}
	return out
}

func TestFilterObjects(t *testing.T) {
	all := objs("logs/app.log", "logs2/x.log", "data/report.csv", "README.md", "LOGS/system.log")
	tests := []struct {
		name, term, dir string
		want            []string
	}{
		{"no filter", "", "", keys(all)},
		{"search is case-insensitive", "log", "", []string{"logs/app.log", "logs2/x.log", "LOGS/system.log"}},
		{"dir is a path boundary", "", "logs", []string{"logs/app.log"}},
		{"dir and search", "app", "logs", []string{"logs/app.log"}},
	}
	for _, tt := range tests {
		if got := keys(filterObjects(all, tt.term, tt.dir)); !slices.Equal(got, tt.want) {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestDirTree(t *testing.T) {
	tr := newDirTree()
	tr.add("a/b")
	tr.add("a/b") // duplicates are ignored
	tr.add("a/c")
	tr.add("z")

	if got, want := tr.children(""), []string{"a", "z"}; !slices.Equal(got, want) {
		t.Errorf("root = %v, want %v", got, want)
	}
	if got, want := tr.children("a"), []string{"a/b", "a/c"}; !slices.Equal(got, want) {
		t.Errorf("a = %v, want %v", got, want)
	}
	if tr.children("z") != nil {
		t.Errorf("z should be a leaf")
	}
}

func TestUploadObjectName(t *testing.T) {
	if got := uploadObjectName("", "/tmp/a.txt"); got != "a.txt" {
		t.Errorf("got %q", got)
	}
	if got := uploadObjectName("logs/2024", "/tmp/a.txt"); got != "logs/2024/a.txt" {
		t.Errorf("got %q", got)
	}
}

func TestTransferSummaryMessage(t *testing.T) {
	if got := transferSummaryMessage("Uploaded", 1, 1, nil); got != "Uploaded 1 file." {
		t.Errorf("got %q", got)
	}
	if got := transferSummaryMessage("Uploaded", 3, 3, nil); got != "Uploaded 3 files." {
		t.Errorf("got %q", got)
	}
	got := transferSummaryMessage("Downloaded", 1, 3, []string{"a: x", "b: y"})
	if got != "Downloaded 1 of 3 files.\n\nFailed:\n- a: x\n- b: y" {
		t.Errorf("got %q", got)
	}
}

func TestRefilterDropsHiddenSelection(t *testing.T) {
	var f filesState
	f.init()
	f.all = objs("a.txt", "b.txt")
	f.sel.Add("a.txt")
	f.sel.Add("b.txt")
	f.search = "a.t"
	f.refilter()
	if f.sel.Len() != 1 || !f.sel.Has("a.txt") {
		t.Errorf("selection = %d, want only a.txt", f.sel.Len())
	}
}

func TestLinkExpiry(t *testing.T) {
	for _, e := range linkExpiries {
		if e.d > 7*24*time.Hour {
			t.Errorf("%s exceeds the 7 day S3 limit", e.label)
		}
		if linkExpiry(e.label) != e.d {
			t.Errorf("linkExpiry(%q) mismatch", e.label)
		}
	}
	if linkExpiry("nonsense") != time.Hour {
		t.Error("unknown label should fall back to 1 hour")
	}
}

func TestPresignFollowsExpiry(t *testing.T) {
	// a region set: signing is offline
	svc, err := s3.New(config.S3Config{Endpoint: "localhost:9000", AccessKey: "a", SecretKey: "b", Bucket: "bkt", Region: "us-east-1"})
	if err != nil {
		t.Fatal(err)
	}
	items, err := presign(svc, []string{"README.md"}, 7*24*time.Hour)
	if err != nil || len(items) != 1 {
		t.Fatalf("presign: %v, %d items", err, len(items))
	}
	if !strings.Contains(items[0].url, "X-Amz-Expires=604800") {
		t.Errorf("url %q should expire in 604800s", items[0].url)
	}
}
