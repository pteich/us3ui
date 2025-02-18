package main

import (
	"context"
	"os"
	"os/signal"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/dialog"

	"github.com/pteich/us3ui/config"
	"github.com/pteich/us3ui/windows"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	defer cancel()

	cfg, err := config.NewS3Config()
	if err != nil {
		dialog.ShowError(err, nil)
		return
	}

	a := app.NewWithID("de.peter-teich.us3ui")
	iconRes := fyne.NewStaticResource("icon.png", iconPNG)
	a.SetIcon(iconRes)

	windows.ShowConfigWindow(ctx, cfg, a)
}
