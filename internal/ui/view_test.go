package ui

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/minio/minio-go/v7"

	"github.com/pteich/us3ui/config"
	"github.com/pteich/us3ui/s3"
)

func testApp(t *testing.T) *app {
	t.Helper()
	svc, err := s3.New(config.S3Config{Endpoint: "localhost:9000", AccessKey: "a", SecretKey: "b", Bucket: "bkt"})
	if err != nil {
		t.Fatal(err)
	}
	a := &app{cfg: &config.Config{}, svc: svc}
	a.files.init()
	a.conn.init(a.cfg)
	now := time.Now()
	for _, k := range []string{"logs/app.log", "logs/old/a.log", "data/report.csv", "README.md"} {
		a.files.all = append(a.files.all, minio.ObjectInfo{Key: k, Size: 1500, LastModified: now})
		if i := len(k) - len(uploadObjectName("", k)); i > 0 {
			a.files.tree.add(k[:i-1])
		}
	}
	a.files.outline.Open.Add("")
	a.files.refilter()
	return a
}

func TestFilesViewRendersAndFilters(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.view, 1000, 600)

	for _, want := range []string{"README.md", "logs/app.log", "All objects", "Total Items: 4"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}

	a.files.search = "report"
	tt.Frame()
	if !tt.HasText("data/report.csv") || tt.HasText("README.md") || !tt.HasText("Items: 1 of 4 total") {
		t.Errorf("search not applied: %q", tt.Texts())
	}
}

func TestModalsRender(t *testing.T) {
	a := testApp(t)
	a.conn.open = true
	tt := ui.NewTester(a.view, 1000, 600)
	if !tt.HasText("Connect") || !tt.HasText("Manage Buckets") {
		t.Errorf("connect dialog missing: %q", tt.Texts())
	}

	a.conn.open = false
	a.openSync()
	tt.Frame()
	if !tt.HasText("Dry Run") {
		t.Errorf("sync dialog missing: %q", tt.Texts())
	}
}
