package ui

import (
	"io"
	"sync"
	"time"
)

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
