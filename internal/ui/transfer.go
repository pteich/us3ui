package ui

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/pteich/us3ui/storage"
)

func (f *filesState) beginTransfer(label string) context.Context {
	if f.xfer.active {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	f.xfer, f.xferCancel = xfer{active: true, label: label}, cancel
	return ctx
}

func (f *filesState) stopTransfer() {
	if f.xferCancel != nil {
		f.xferCancel()
	}
	f.conflict = downloadConflictState{}
}

func (f *filesState) finishTransfer() {
	f.stopTransfer()
	f.xfer, f.xferCancel = xfer{}, nil
}

type downloadConflictState struct {
	open, apply bool
	path        string
	choice      storage.DownloadConflict
	reply       chan storage.DownloadConflict
}

func (d *downloadConflictState) request(path string, reply chan storage.DownloadConflict) {
	if d.apply {
		reply <- d.choice
		return
	}
	d.path, d.reply, d.open = path, reply, true
}

func (d *downloadConflictState) resolve(choice storage.DownloadConflict) {
	d.choice, d.open = choice, false
	if d.reply != nil {
		d.reply <- choice
		d.reply = nil
	}
}

// askDownloadConflict waits on a worker; only posted callbacks touch UI state.
func (a *app) askDownloadConflict(ctx context.Context, path string) (storage.DownloadConflict, error) {
	reply := make(chan storage.DownloadConflict, 1)
	a.post(func() {
		if ctx.Err() == nil {
			a.files.conflict.request(path, reply)
		}
	})
	select {
	case choice := <-reply:
		return choice, ctx.Err()
	case <-ctx.Done():
		return storage.SkipDownload, ctx.Err()
	}
}

func (a *app) downloadConflictModal(c *ui.Context) {
	d := &a.files.conflict
	if ui.Modal(c, &d.open, func() {
		ui.Column(c).Gap(12).Width(520).Children(func() {
			ui.Text(c, "File already exists").Bold()
			ui.Text(c, d.path)
			ui.Text(c, "Choose what to do with the local file.")
			ui.Checkbox(c.Key("download-conflict-apply"), &d.apply, "Apply to remaining conflicts")
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				ui.Button(c, "Cancel Download").OnClick(a.files.stopTransfer)
				ui.Button(c, "Skip").OnClick(func() { d.resolve(storage.SkipDownload) })
				ui.Button(c, "Keep Both").OnClick(func() { d.resolve(storage.KeepBothDownloads) })
				ui.Button(c, "Replace").OnClick(func() { d.resolve(storage.ReplaceDownload) })
			})
		})
	}).Dismissed() {
		a.files.stopTransfer()
	}
}

// progressReader reports the bytes read so far.
type progressReader struct {
	r  io.Reader
	n  int64
	on func(int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.n += int64(n)
	p.on(p.n)
	return n, err
}

// throttle lets an event through at most once per interval.
type throttle struct {
	mu   sync.Mutex
	last time.Time
}

func (t *throttle) ok(every time.Duration) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if time.Since(t.last) < every {
		return false
	}
	t.last = time.Now()
	return true
}

// xfer is the progress of an upload or download shown in the status bar.
type xfer struct {
	active bool
	label  string
	frac   float64
}
