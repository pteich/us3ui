package main

import (
	"context"
	"os"
	"os/signal"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/pteich/us3ui/config"
	"github.com/pteich/us3ui/windows"
)

var Version = "0.5.1"

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	defer cancel()

	a := app.NewWithID("de.peter-teich.us3ui")
	iconRes := fyne.NewStaticResource("icon.png", iconPNG)
	a.SetIcon(iconRes)

	cfg, err := config.New()
	if err != nil {
		w := a.NewWindow(config.Name)
		w.SetContent(widget.NewLabel(fyne.CurrentApp().Metadata().Version))
		w.Resize(fyne.NewSize(600, 400))
		d := dialog.NewError(err, w)
		d.SetOnClosed(func() {
			a.Quit()
		})
		d.Show()
		w.ShowAndRun()
		return
	}

	mainWin := windows.NewMainWindow(a, cfg)
	mainWin.Show(ctx)
}
