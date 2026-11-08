package ui

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/pteich/us3ui/config"
	"github.com/pteich/us3ui/s3"
	"github.com/pteich/us3ui/storage"
)

func testApp(t *testing.T) *app {
	t.Helper()
	svc, err := s3.New(config.S3Config{Endpoint: "localhost:9000", AccessKey: "a", SecretKey: "b", Bucket: "bkt"})
	if err != nil {
		t.Fatal(err)
	}
	a := &app{cfg: &config.Config{}, svc: svc}
	a.linker = svc
	a.files.init()
	a.conn.init(a.cfg)
	now := time.Now()
	for _, k := range []string{"logs/app.log", "logs/old/a.log", "data/report.csv", "README.md"} {
		a.files.all = append(a.files.all, storage.ObjectInfo{Key: k, Size: 1500, LastModified: now})
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

func TestTransferShowsStopButton(t *testing.T) {
	a := testApp(t)
	a.files.xfer = xfer{active: true, label: "Downloading a.txt", frac: 0.5}
	tt := ui.NewTester(a.view, 1000, 600)
	if !tt.HasText("Stop") {
		t.Errorf("no Stop button during a transfer: %q", tt.Texts())
	}

	a.files.xfer = xfer{}
	tt.Frame()
	if tt.HasText("Stop") {
		t.Errorf("Stop button shown without a transfer: %q", tt.Texts())
	}
}

func TestDownloadConflictDialogChoices(t *testing.T) {
	for _, tc := range []struct {
		button string
		choice storage.DownloadConflict
	}{
		{"Replace", storage.ReplaceDownload},
		{"Keep Both", storage.KeepBothDownloads},
		{"Skip", storage.SkipDownload},
	} {
		t.Run(tc.button, func(t *testing.T) {
			a := testApp(t)
			a.files.beginTransfer("Download")
			defer a.files.finishTransfer()
			reply := make(chan storage.DownloadConflict, 1)
			a.files.conflict.request("/downloads/report.pdf", reply)
			tt := ui.NewTester(a.view, 1000, 600)
			if err := tt.Click("Apply to remaining conflicts"); err != nil {
				t.Fatal(err)
			}
			if err := tt.Click(tc.button); err != nil {
				t.Fatal(err)
			}
			select {
			case choice := <-reply:
				if choice != tc.choice || !a.files.conflict.apply {
					t.Fatalf("choice=%v, apply=%v", choice, a.files.conflict.apply)
				}
			default:
				t.Fatal("dialog did not release the waiting download")
			}
			if tt.HasText("File already exists") {
				t.Fatal("resolved conflict dialog stayed open")
			}
			tt.Frame()
		})
	}
}

func TestDownloadConflictDialogDismissalCancelsTransfer(t *testing.T) {
	for _, dismiss := range []string{"Escape", "Cancel Download"} {
		t.Run(dismiss, func(t *testing.T) {
			a := testApp(t)
			ctx := a.files.beginTransfer("Download")
			defer a.files.finishTransfer()
			a.files.conflict.request("a.txt", make(chan storage.DownloadConflict, 1))
			tt := ui.NewTester(a.view, 1000, 600)
			if dismiss == "Escape" {
				tt.Key(0, ui.KeyEscape)
			} else if err := tt.Click(dismiss); err != nil {
				t.Fatal(err)
			}
			if ctx.Err() == nil || a.files.conflict.open {
				t.Fatal("dismissed dialog left a download waiting")
			}
		})
	}
}
