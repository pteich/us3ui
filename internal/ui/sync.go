package ui

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/pteich/us3ui/config"
	"github.com/pteich/us3ui/s3"
)

// syncState copies objects from one saved connection to another (rclone-style
// copy: transfers objects missing on the target or whose size differs, never
// deletes). Bytes stream source->target without touching local disk.
type syncState struct {
	open bool

	src, dst             string // connection names
	srcPrefix, dstPrefix string
	names                []string
	conns                map[string]config.S3Config

	summary  string
	scanning bool
	copying  bool
	plan     []s3.SyncAction
	cancel   context.CancelFunc
	logList  ui.ListState

	// Written by the copy workers, read by the view.
	mu       sync.Mutex
	frac     float64
	current  string
	logLines []string
	failures []string
	tick     throttle
}

func (a *app) openSync() {
	sy := &a.sync
	sy.names = sy.names[:0]
	sy.conns = map[string]config.S3Config{}
	for _, cn := range a.cfg.Settings.Connections {
		if cn.Name == "" || cn.Endpoint == "" {
			continue
		}
		sy.names = append(sy.names, cn.Name)
		sy.conns[cn.Name] = cn
	}
	if sy.summary == "" {
		sy.summary = "Pick a source and target, then Dry Run."
	}
	sy.logList.FollowEnd = true
	sy.open = true
}

func (a *app) syncModal(c *ui.Context) {
	sy := &a.sync
	busy := sy.scanning || sy.copying

	ui.Modal(c, &sy.open, func() {
		ui.Column(c).Gap(8).Width(620).Children(func() {
			ui.Text(c, "Sync objects from source to target connection").Bold()
			ui.Form(c, func() {
				ui.Field(c, "Source", func() {
					if ui.Select(c.Key("src"), &sy.src, sy.names).Disabled(busy).Changed() && sy.srcPrefix == "" {
						sy.srcPrefix = sy.conns[sy.src].Prefix
					}
				})
				ui.Field(c, "Source Prefix", func() {
					ui.TextInput(c.Key("sp"), &sy.srcPrefix).Placeholder("source prefix (optional)").Disabled(busy)
				})
				ui.Field(c, "Target", func() {
					if ui.Select(c.Key("dst"), &sy.dst, sy.names).Disabled(busy).Changed() && sy.dstPrefix == "" {
						sy.dstPrefix = sy.conns[sy.dst].Prefix
					}
				})
				ui.Field(c, "Target Prefix", func() {
					ui.TextInput(c.Key("dp"), &sy.dstPrefix).Placeholder("target prefix (optional)").Disabled(busy)
				})
			})

			sy.mu.Lock()
			frac, current := sy.frac, sy.current
			lines := sy.logLines
			if len(lines) > 200 {
				lines = lines[len(lines)-200:]
			}
			sy.mu.Unlock()

			ui.Text(c, sy.summary)
			if sy.scanning {
				ui.Progress(c, -1)
			}
			if sy.copying {
				ui.Progress(c, frac)
				label(c, current).SingleLine()
			}
			ui.List(c, &sy.logList, len(lines), func(i int) {
				ui.Text(c, lines[i]).SingleLine().Font("monospace")
			}).Height(140)

			ui.Row(c).Gap(8).Children(func() {
				if ui.Button(c, "Dry Run").Disabled(busy).Clicked() {
					a.syncDryRun()
				}
				if ui.PrimaryButton(c, "Start Copy").Disabled(busy || len(sy.plan) == 0).Clicked() {
					a.syncStart()
				}
				if sy.copying && ui.Button(c, "Cancel").Clicked() && sy.cancel != nil {
					sy.cancel()
				}
				ui.Spacer(c)
				if ui.Button(c, "Close").Disabled(busy).Clicked() {
					sy.open = false
				}
			})
		})
	})
}

// selection validates the two chosen connections.
func (a *app) syncSelection() (src, dst config.S3Config, ok bool) {
	sy := &a.sync
	src, srcOK := sy.conns[sy.src]
	dst, dstOK := sy.conns[sy.dst]
	if !srcOK || !dstOK {
		a.alert("Sync", "Select both a source and a target connection.")
		return src, dst, false
	}
	if src.Name == dst.Name && normalizePrefix(sy.srcPrefix) == normalizePrefix(sy.dstPrefix) {
		a.alert("Sync", "Source and target are identical.")
		return src, dst, false
	}
	return src, dst, true
}

func buildServices(srcCfg, dstCfg config.S3Config) (*s3.Service, *s3.Service, error) {
	src, err := s3.New(srcCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("source connection: %w", err)
	}
	dst, err := s3.New(dstCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("target connection: %w", err)
	}
	return src, dst, nil
}

func (a *app) syncDryRun() {
	sy := &a.sync
	srcCfg, dstCfg, ok := a.syncSelection()
	if !ok {
		return
	}
	src, dst, err := buildServices(srcCfg, dstCfg)
	if err != nil {
		a.alertErr(err)
		return
	}
	srcPrefix, dstPrefix := sy.srcPrefix, sy.dstPrefix
	sy.scanning, sy.plan = true, nil
	sy.summary = "Scanning both sides..."
	sy.mu.Lock()
	sy.logLines = nil
	sy.mu.Unlock()

	go func() {
		plan, err := s3.PlanSync(context.Background(), src, dst, srcPrefix, dstPrefix)
		a.post(func() {
			sy.scanning = false
			if err != nil {
				sy.summary = "Scan failed."
				a.alertErr(err)
				return
			}
			sy.plan = plan
			var bytes int64
			for _, p := range plan {
				bytes += p.Size
			}
			sy.summary = fmt.Sprintf("%d to copy - %s", len(plan), byteCountSI(bytes))
		})
	}()
}

func (a *app) syncStart() {
	sy := &a.sync
	srcCfg, dstCfg, ok := a.syncSelection()
	if !ok {
		return
	}
	src, dst, err := buildServices(srcCfg, dstCfg)
	if err != nil {
		a.alertErr(err)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	sy.cancel, sy.copying = cancel, true
	plan := sy.plan
	total := len(plan)
	sy.summary = fmt.Sprintf("Copying 0 of %d...", total)
	sy.mu.Lock()
	sy.logLines, sy.failures, sy.frac, sy.current = nil, nil, 0, ""
	sy.mu.Unlock()

	go func() {
		err := s3.RunSync(ctx, src, dst, plan, 0,
			func(doneBytes, totalBytes int64, doneCount, total int, cur string) {
				sy.mu.Lock()
				if totalBytes > 0 {
					sy.frac = float64(doneBytes) / float64(totalBytes)
				}
				sy.current = fmt.Sprintf("%d of %d - %s", doneCount, total, cur)
				sy.mu.Unlock()
				if sy.tick.ok(200 * time.Millisecond) {
					a.win.Invalidate()
				}
			},
			func(act s3.SyncAction, resErr error) {
				sy.mu.Lock()
				if resErr != nil {
					sy.failures = append(sy.failures, fmt.Sprintf("%s: %v", act.DstKey, resErr))
					sy.logLines = append(sy.logLines, "[FAIL] "+act.DstKey)
				} else {
					sy.logLines = append(sy.logLines, "[OK]   "+act.DstKey)
				}
				sy.mu.Unlock()
				if sy.tick.ok(200 * time.Millisecond) {
					a.win.Invalidate()
				}
			},
		)

		a.post(func() {
			cancel()
			sy.cancel, sy.copying, sy.plan = nil, false, nil
			sy.mu.Lock()
			failures := append([]string(nil), sy.failures...)
			sy.mu.Unlock()
			done := total - len(failures)
			if err == context.Canceled {
				sy.summary = fmt.Sprintf("Cancelled - %d of %d copied.", done, total)
				return
			}
			sy.summary = fmt.Sprintf("%d of %d copied.", done, total)
			a.alert("Sync Complete", transferSummaryMessage("Copied", done, total, failures))
		})
	}()
}
