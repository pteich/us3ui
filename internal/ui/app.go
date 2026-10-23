package ui

import (
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"

	"github.com/pteich/us3ui/config"
	"github.com/pteich/us3ui/s3"
)

// app is the state of the one window. It is only read and written on the main
// thread: by the view, and by callbacks run there (app.post for goroutines).
type app struct {
	win *mygo.Window
	cfg *config.Config
	svc *s3.Service

	files   filesState
	conn    connectState
	buckets bucketState
	sync    syncState

	msgOpen       bool
	msgTitle, msg string
	toasts        []string
}

// Run shows the window and blocks in MyGo's event loop until the app quits.
func Run(cfg *config.Config) {
	a := &app{cfg: cfg}
	a.files.init()
	a.conn.init(cfg)

	mygo.Use(native.Plugin) // the update window, and background update checks
	mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu},
		{Label: "File", Submenu: []*mygo.MenuItem{
			{Label: "Connect to Server…", Accelerator: "CmdOrCtrl+K", Click: func(*mygo.MenuItem, *mygo.Window) {
				a.post(func() { a.conn.open = true })
			}},
			updater.MenuItem(),
			mygo.Separator(),
			{Role: mygo.RoleClose},
		}},
		{Role: mygo.RoleEditMenu},
		{Role: mygo.RoleWindowMenu},
	}))
	mygo.App.WhenReady(func() {
		a.win = mygo.NewWindow(mygo.WindowOptions{
			Title:     "Universal S3 GUI",
			Width:     1100,
			Height:    650,
			MinWidth:  720,
			MinHeight: 400,
			Content:   ui.View(a.view),
		})
		a.win.OnFileDrop(func(e *mygo.FileDropEvent) { a.uploadFiles(e.Paths) })
		a.conn.open = true // shown on startup, as before
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// post runs fn on the main thread and draws a new frame; safe from any goroutine.
func (a *app) post(fn func()) { a.win.Update(fn) }

// alert shows a message dialog. Main thread only.
func (a *app) alert(title, msg string) {
	a.msgTitle, a.msg, a.msgOpen = title, msg, true
}

func (a *app) alertErr(err error) { a.alert("Error", err.Error()) }

func (a *app) toast(msg string) { a.toasts = append(a.toasts, msg) }

func (a *app) view(c *ui.Context) {
	if a.svc == nil {
		ui.Column(c).Fill().Center().Children(func() {
			ui.Text(c, "Select a connection to begin").TextColor(c.Theme().TextMuted)
			if ui.PrimaryButton(c, "Connect to Server…").Clicked() {
				a.conn.open = true
			}
		})
	} else {
		a.filesView(c)
	}

	a.connectModal(c)
	a.bucketsModal(c)
	a.syncModal(c)
	a.linkModal(c)

	ui.AlertDialog(c, &a.msgOpen, a.msgTitle, a.msg, "OK")
	for _, t := range a.toasts {
		c.Toast(t)
	}
	a.toasts = nil
}

// label is a muted caption.
func label(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).TextColor(c.Theme().TextMuted)
}
