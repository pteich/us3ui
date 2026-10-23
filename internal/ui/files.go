package ui

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/gabriel-vasile/mimetype"
	"github.com/minio/minio-go/v7"

	"github.com/pteich/us3ui/s3"
)

const (
	batchSize         = 500
	uiUpdateInterval  = 500 * time.Millisecond
	maxObjectsDefault = 50000
)

type linkItem struct{ key, url string }

// linkExpiries are the choices for how long a presigned link stays valid;
// S3 caps it at 7 days.
var linkExpiries = []struct {
	label string
	d     time.Duration
}{
	{"15 minutes", 15 * time.Minute},
	{"1 hour", time.Hour},
	{"6 hours", 6 * time.Hour},
	{"24 hours", 24 * time.Hour},
	{"3 days", 72 * time.Hour},
	{"7 days", 7 * 24 * time.Hour},
}

func linkExpiryLabels() []string {
	out := make([]string, len(linkExpiries))
	for i, e := range linkExpiries {
		out[i] = e.label
	}
	return out
}

func linkExpiry(label string) time.Duration {
	for _, e := range linkExpiries {
		if e.label == label {
			return e.d
		}
	}
	return time.Hour
}

type filesState struct {
	prefix  string // the prefix field
	base    string // the prefix last loaded
	search  string
	maxText string
	maxObjs int
	more    bool // load stopped at the limit
	loading bool
	status  string
	gen     int // load generation: results of older loads are dropped
	cancel  context.CancelFunc

	all     []minio.ObjectInfo
	visible []minio.ObjectInfo
	tree    *dirTree
	dir     string // selected directory, "" for all

	outline  ui.OutlineState[string]
	row      int
	table    ui.ListState
	sel      ui.Selection[string]
	sort     ui.SortOrder
	lastSort ui.SortOrder
	lastTerm string

	xfer       xfer
	askDelete  bool
	split      float32
	links      []linkItem
	linkKeys   []string
	linkExpire string // label of the chosen linkExpiries entry
	linksUntil time.Time
	linkGen    int
	signing    bool
	linksOpen  bool
}

func (f *filesState) init() {
	f.split = 240
	f.linkExpire = "1 hour"
	f.maxText = strconv.Itoa(maxObjectsDefault)
	f.maxObjs = maxObjectsDefault
	f.tree = newDirTree()
	f.table.Sort = &f.sort
	f.table.Selection = &f.sel
	f.table.Key = func(i int) any { return f.visible[i].Key }
	f.table.Label = func(i int) string { return f.visible[i].Key }
	f.outline.List.Selected = &f.row
}

// refilter recomputes the rows shown from the search, directory and sort.
func (f *filesState) refilter() {
	rows := filterObjects(f.all, f.search, f.dir)
	if f.sort.Column != "" {
		rows = slices.Clone(rows)
		slices.SortStableFunc(rows, func(a, b minio.ObjectInfo) int {
			var d int
			switch f.sort.Column {
			case "size":
				d = cmpInt(a.Size, b.Size)
			case "modified":
				d = a.LastModified.Compare(b.LastModified)
			default:
				d = strings.Compare(a.Key, b.Key)
			}
			if f.sort.Descending {
				return -d
			}
			return d
		})
	}
	f.visible = rows
	f.lastSort, f.lastTerm = f.sort, f.search

	// Keep only selected rows still shown, so Delete never touches hidden ones.
	if f.sel.Len() > 0 {
		shown := make(map[string]struct{}, len(rows))
		for _, o := range rows {
			shown[o.Key] = struct{}{}
		}
		for _, k := range slices.Collect(f.sel.All()) {
			if _, ok := shown[k]; !ok {
				f.sel.Remove(k)
			}
		}
	}
}

func cmpInt(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func (f *filesState) countLabel() string {
	suffix := ""
	if f.more {
		suffix = fmt.Sprintf(" (limited to %d, use higher limit to see more)", f.maxObjs)
	}
	if len(f.visible) != len(f.all) {
		return fmt.Sprintf("Items: %d of %d total%s", len(f.visible), len(f.all), suffix)
	}
	return fmt.Sprintf("Total Items: %d%s", len(f.all), suffix)
}

func (a *app) filesView(c *ui.Context) {
	f := &a.files

	// Pick up what the user changed since the last frame.
	if f.row >= 0 && f.row < f.outline.Rows() {
		if dir := f.outline.Item(f.row); dir != f.dir {
			f.dir = dir
			f.refilter()
		}
	}
	if f.sort != f.lastSort || f.search != f.lastTerm {
		f.refilter()
	}

	nSel := f.sel.Len()
	t := c.Theme()
	panel := func(e ui.Element) ui.Element {
		return e.Radius(10).Background(t.Surface).Border(1, t.Border)
	}
	ui.Column(c).Fill().Padding(10).Gap(10).Children(func() {
		panel(ui.Column(c)).Padding(8).Gap(8).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				if ui.Button(c, "Refresh").Clicked() {
					a.loadObjects(f.prefix)
				}
				if ui.PrimaryButton(c, "Upload").Clicked() {
					a.pickAndUpload()
				}
				ui.Box(c).Width(1).Height(22).Background(t.Border).Margin(0, 6)
				if ui.Button(c, "Download").Disabled(nSel == 0).Clicked() {
					a.download()
				}
				if ui.Button(c, "Link").Disabled(nSel == 0).Clicked() {
					a.makeLinks()
				}
				if ui.Button(c, "Delete").Disabled(nSel == 0).Clicked() {
					f.askDelete = true
				}
				ui.Spacer(c)
				if ui.Button(c, "Change Connection").Clicked() {
					a.conn.open = true
				}
			})
			ui.Divider(c)
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "Prefix").TextColor(t.TextMuted)
				if ui.TextInput(c.Key("prefix"), &f.prefix).Placeholder("optional").Width(220).Submitted() {
					a.loadObjects(f.prefix)
				}
				ui.Text(c, "Max").TextColor(t.TextMuted)
				if ui.TextInput(c.Key("max"), &f.maxText).Width(80).Changed() {
					n, err := strconv.Atoi(f.maxText)
					switch {
					case f.maxText == "":
						f.maxObjs = 0 // unlimited
					case err == nil && n >= 0:
						f.maxObjs = n
					}
				}
				if ui.Button(c, "Load").Clicked() {
					a.loadObjects(f.prefix)
				}
				if f.more && !f.loading && ui.Button(c, "Load More").Clicked() {
					a.continueLoading()
				}
				if f.loading && ui.Button(c, "Stop").Clicked() && f.cancel != nil {
					f.cancel()
				}
				ui.SearchField(c.Key("search"), &f.search).Placeholder("Search...").Grow(1)
			})
		})

		ui.Split(c, &f.split, func() {
			panel(ui.Column(c)).Padding(8).Gap(6).Grow(1).Children(func() {
				ui.Text(c, "FOLDERS").FontSize(11).Bold().TextColor(t.TextMuted).Padding(2, 6)
				ui.Divider(c)
				ui.Outline(c, &f.outline, []string{""}, f.tree.children, func(dir string) {
					if dir == "" {
						ui.Text(c, "All objects").SingleLine()
						return
					}
					ui.Text(c, path.Base(dir)).SingleLine()
				}).Grow(1)
			})
		}, func() {
			panel(ui.Column(c)).Padding(4).Grow(1).Children(func() {
				a.objectTable(c)
			})
		}).Grow(1)

		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			if f.loading {
				ui.Spinner(c)
			}
			ui.Text(c, a.statusText()).SingleLine().Grow(1)
			if f.xfer.active {
				ui.Progress(c, f.xfer.frac).Width(300)
			} else {
				label(c, "Drop files anywhere to upload")
			}
		})
	})

	if ui.AlertDialog(c, &f.askDelete, "Delete Objects", deleteQuestion(nSel), "Cancel", "Delete") == 1 {
		a.deleteSelected()
	}
}

func deleteQuestion(n int) string {
	if n == 1 {
		return "Do you really want to delete this file?"
	}
	return fmt.Sprintf("Do you really want to delete %d files?", n)
}

func (a *app) statusText() string {
	f := &a.files
	if f.xfer.active {
		return f.xfer.label
	}
	text := f.status
	if text == "" {
		text = f.countLabel()
	}
	if n := f.sel.Len(); n > 0 {
		text = fmt.Sprintf("%d selected · %s", n, text)
	}
	return text
}

func (a *app) objectTable(c *ui.Context) {
	f := &a.files
	cols := []ui.TableColumn{
		{Title: "Name", ID: "name", Sortable: true},
		{Title: "Size", ID: "size", Width: 90, Align: ui.End, Sortable: true},
		{Title: "Last Modified", ID: "modified", Width: 160, Sortable: true},
	}
	ui.Table(c, &f.table, cols, len(f.visible), func(row, col int) {
		o := f.visible[row]
		switch col {
		case 0:
			ui.Text(c, o.Key).SingleLine()
		case 1:
			ui.Text(c, byteCountSI(o.Size)).SingleLine()
		case 2:
			ui.Text(c, o.LastModified.Local().Format("2006-01-02 15:04:05")).SingleLine()
		}
	}).Grow(1)
}

// loadObjects starts listing under prefix from scratch.
func (a *app) loadObjects(prefix string) {
	f := &a.files
	if f.cancel != nil {
		f.cancel()
	}
	f.base = strings.TrimLeft(strings.TrimSpace(prefix), "/")
	f.prefix = f.base
	f.search = ""
	f.all, f.visible = nil, nil
	f.tree = newDirTree()
	f.dir, f.row = "", 0
	f.sel.Clear()
	f.outline.Open.Clear()
	f.outline.Open.Add("")
	f.more = false
	a.startListing("")
}

// continueLoading lists on after the last object loaded.
func (a *app) continueLoading() {
	f := &a.files
	last := ""
	if n := len(f.all); n > 0 {
		last = f.all[n-1].Key
	}
	a.startListing(last)
}

func (a *app) startListing(startAfter string) {
	f := &a.files
	f.gen++
	gen, base, limit, svc := f.gen, f.base, f.maxObjs, a.svc
	ctx, cancel := context.WithCancel(context.Background())
	f.cancel = cancel
	f.loading, f.more = true, false
	f.status = "Loading objects…"
	start := len(f.all)

	go func() {
		defer cancel()
		var (
			lastKey    = startAfter
			loaded     = start
			lastUpdate time.Time
			began      = time.Now()
			more       bool
			failure    error
		)
		for {
			size := batchSize
			if limit > 0 {
				if loaded >= limit {
					more = true
					break
				}
				size = min(size, limit-loaded)
			}
			batch, err := svc.ListObjectsBatch(ctx, lastKey, base, size)
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					failure = err
				}
				break
			}
			if len(batch) == 0 {
				break
			}
			update := lastUpdate.IsZero() || time.Since(lastUpdate) >= uiUpdateInterval || len(batch) < size
			if update {
				lastUpdate = time.Now()
			}
			loaded += len(batch)
			lastKey = batch[len(batch)-1].Key
			a.post(func() {
				if f.gen != gen {
					return
				}
				f.all = append(f.all, batch...)
				for _, o := range batch {
					if i := strings.LastIndex(o.Key, "/"); i != -1 {
						f.tree.add(o.Key[:i])
					}
				}
				if update {
					f.refilter()
					f.status = fmt.Sprintf("Loaded %d objects…", len(f.all))
				}
			})
			if len(batch) < size {
				break
			}
		}
		canceled := ctx.Err() != nil
		a.post(func() {
			if f.gen != gen {
				return
			}
			f.loading, f.cancel = false, nil
			f.more = more || (canceled && len(f.all) > 0)
			f.refilter()
			switch {
			case failure != nil:
				f.status = "Failed to load objects"
				a.alertErr(failure)
			case canceled:
				f.status = fmt.Sprintf("Load canceled (%d objects loaded)", len(f.all))
			case len(f.all) == 0:
				f.status = "No objects found"
			default:
				f.status = ""
				a.toast(fmt.Sprintf("Loaded %d objects in %.1fs", len(f.all), time.Since(began).Seconds()))
			}
		})
	}()
}

func (a *app) selectedKeys() []string {
	return slices.Collect(a.files.sel.All())
}

func (a *app) deleteSelected() {
	f := &a.files
	keys, svc := a.selectedKeys(), a.svc
	if len(keys) == 0 {
		return
	}
	go func() {
		deleted := map[string]struct{}{}
		var failures []string
		for _, k := range keys {
			if err := svc.DeleteObject(context.Background(), k); err != nil {
				failures = append(failures, fmt.Sprintf("%s: %v", k, err))
				continue
			}
			deleted[k] = struct{}{}
		}
		a.post(func() {
			f.all = slices.DeleteFunc(f.all, func(o minio.ObjectInfo) bool {
				_, ok := deleted[o.Key]
				return ok
			})
			f.sel.Clear()
			f.refilter()
			if len(failures) > 0 {
				a.alert("Delete Failed", transferSummaryMessage("Deleted", len(deleted), len(keys), failures))
			}
		})
	}()
}

func (a *app) makeLinks() {
	f := &a.files
	f.linkKeys = a.selectedKeys()
	slices.Sort(f.linkKeys)
	f.linksOpen = true
	a.signLinks()
}

// signLinks presigns the links of linkKeys for the chosen expiry. Without a
// configured region the client first looks the bucket's region up over the
// network, so it runs off the main thread; a newer request supersedes an
// older one still in flight.
func (a *app) signLinks() {
	f := &a.files
	svc, keys, d := a.svc, slices.Clone(f.linkKeys), linkExpiry(f.linkExpire)
	f.linkGen++
	gen := f.linkGen
	f.signing = true
	go func() {
		items, err := presign(svc, keys, d)
		a.post(func() {
			if f.linkGen != gen {
				return
			}
			f.signing = false
			if err != nil {
				f.linksOpen = false
				a.alertErr(err)
				return
			}
			f.links, f.linksUntil = items, time.Now().Add(d)
		})
	}()
}

func presign(svc *s3.Service, keys []string, d time.Duration) ([]linkItem, error) {
	items := make([]linkItem, 0, len(keys))
	for _, k := range keys {
		u, err := svc.GetPresignedURL(context.Background(), k, d)
		if err != nil {
			return nil, err
		}
		items = append(items, linkItem{k, u.String()})
	}
	return items, nil
}

func (a *app) linkModal(c *ui.Context) {
	f := &a.files
	ui.Modal(c, &f.linksOpen, func() {
		ui.Column(c).Gap(8).Width(560).Children(func() {
			ui.Text(c, "Share links").Bold()
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "Expires in")
				if ui.Select(c.Key("expiry"), &f.linkExpire, linkExpiryLabels()).Changed() {
					a.signLinks()
				}
				if f.signing {
					ui.Spinner(c)
				} else {
					label(c, "until "+f.linksUntil.Format("2006-01-02 15:04")).Grow(1)
				}
			})
			for i, l := range f.links {
				ui.Column(c.Key(l.key)).Gap(4).Children(func() {
					label(c, l.key).SingleLine()
					ui.Row(c).Gap(8).Children(func() {
						u := l.url
						ui.TextInput(c.Key("url"+strconv.Itoa(i)), &u).ReadOnly(true).Grow(1)
						if ui.Button(c, "Copy").Clicked() {
							mygo.Clipboard.WriteText(l.url)
							a.toast("Link copied")
						}
					})
				})
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if len(f.links) > 1 && ui.Button(c, "Copy All").Clicked() {
					urls := make([]string, len(f.links))
					for i, l := range f.links {
						urls[i] = l.url
					}
					mygo.Clipboard.WriteText(strings.Join(urls, "\n"))
					a.toast("Links copied")
				}
				if ui.PrimaryButton(c, "Close").Clicked() {
					f.linksOpen = false
				}
			})
		})
	})
}

func (a *app) pickAndUpload() {
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: a.win, Title: "Upload", Multiple: true})
		if err != nil || len(paths) == 0 {
			return
		}
		a.uploadFiles(paths)
	}()
}

// uploadFiles uploads local files one after the other into the selected
// directory. Safe from any goroutine.
func (a *app) uploadFiles(paths []string) {
	a.post(func() {
		if a.svc == nil {
			a.alert("Upload", "No active connection.")
			return
		}
		f := &a.files
		svc, dir, base := a.svc, f.dir, f.base
		go func() {
			var tick throttle
			uploaded := 0
			var failures []string
			for i, p := range paths {
				name := uploadObjectName(dir, p)
				prefix := fmt.Sprintf("Uploading %d of %d: %s", i+1, len(paths), name)
				a.post(func() { f.xfer = xfer{true, prefix, 0} })
				err := uploadFile(svc, p, name, func(frac float64) {
					if tick.ok(100 * time.Millisecond) {
						a.post(func() { f.xfer = xfer{true, fmt.Sprintf("%s (%.1f%%)", prefix, frac*100), frac} })
					}
				})
				if err != nil {
					failures = append(failures, fmt.Sprintf("%s: %v", filepath.Base(p), err))
					continue
				}
				uploaded++
			}
			a.post(func() {
				f.xfer = xfer{}
				a.alert("Upload Complete", transferSummaryMessage("Uploaded", uploaded, len(paths), failures))
				if uploaded > 0 {
					a.loadObjects(base)
				}
			})
		}()
	})
}

func uploadFile(svc *s3.Service, localPath, objectName string, onProgress func(float64)) error {
	file, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.IsDir() {
		return errors.New("is a directory")
	}
	total := info.Size()

	br := bufio.NewReader(file)
	head, err := br.Peek(1024)
	if err != nil && err != io.EOF {
		return err
	}
	pr := &progressReader{r: br, on: func(n int64) {
		frac := 1.0
		if total > 0 {
			frac = float64(n) / float64(total)
		}
		onProgress(frac)
	}}
	return svc.UploadObjectReader(context.Background(), localPath, objectName, pr, total, mimetype.Detect(head).String())
}

func (a *app) download() {
	f := &a.files
	keys, svc := a.selectedKeys(), a.svc
	slices.Sort(keys)
	go func() {
		dirs, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: a.win, Title: "Download to…", Directory: true, CreateDirectories: true})
		if err != nil || len(dirs) == 0 {
			return
		}
		downloaded := 0
		var failures []string
		for i, key := range keys {
			frac := float64(i+1) / float64(len(keys))
			a.post(func() { f.xfer = xfer{true, "Downloading " + key, frac} })
			if err := downloadObject(svc, key, filepath.Join(dirs[0], filepath.Base(key))); err != nil {
				failures = append(failures, fmt.Sprintf("%s: %v", key, err))
				continue
			}
			downloaded++
		}
		a.post(func() {
			f.xfer = xfer{}
			a.alert("Download Complete", transferSummaryMessage("Downloaded", downloaded, len(keys), failures))
		})
	}()
}

func downloadObject(svc *s3.Service, key, dest string) error {
	obj, err := svc.DownloadObject(context.Background(), key)
	if err != nil {
		return err
	}
	defer obj.Close()
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, obj); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
