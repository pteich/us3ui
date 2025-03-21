package windows

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/Masterminds/semver/v3"
	"github.com/pelletier/go-toml/v2"

	"github.com/pteich/us3ui/config"
	"github.com/pteich/us3ui/s3"
)

type MainWindow struct {
	app       fyne.App
	window    fyne.Window
	cfg       *config.Config
	s3Service *s3.Service
	ctx       context.Context

	// Current content management
	currentContent fyne.CanvasObject
	fileManager    *FileManager
	connectDialog  *ConnectDialog
}

func NewMainWindow(a fyne.App, cfg *config.Config) *MainWindow {
	window := a.NewWindow("Universal S3 UI")
	window.CenterOnScreen()
	window.SetMaster()

	mw := &MainWindow{
		app:    a,
		window: window,
		cfg:    cfg,
	}

	// Set up macOS menu if needed
	if runtime.GOOS == "darwin" {
		window.SetMainMenu(fyne.NewMainMenu(
			fyne.NewMenu("File",
				fyne.NewMenuItem("About", func() {
					mw.showAboutDialog()
				}),
				fyne.NewMenuItem("Connect to Server", func() {
					mw.showConnectionDialog()
				}),
			),
		))
	}

	return mw
}

func (mw *MainWindow) Show(ctx context.Context) {
	mw.ctx = ctx

	// Create an empty placeholder content
	placeholder := widget.NewLabel("Select a connection to begin")
	placeholder.Alignment = fyne.TextAlignCenter

	//mw.window.SetContent(container.NewScroll(container.NewVBox(settings.NewSettings().LoadAppearanceScreen(mw.window), container.NewCenter(placeholder))))
	mw.window.SetContent(container.NewCenter(placeholder))
	mw.window.Resize(fyne.NewSize(950, 500))

	// Show the connection dialog immediately on startup
	mw.showConnectionDialog()

	mw.checkVersion()

	mw.window.ShowAndRun()
}

func (mw *MainWindow) showAboutDialog() {
	dialog.ShowInformation("About", "Universal S3 UI\nVersion: "+fyne.CurrentApp().Metadata().Version, mw.window)
}

func (mw *MainWindow) showConnectionDialog() {
	// Create connection dialog if it doesn't exist
	if mw.connectDialog == nil {
		mw.connectDialog = NewConnectDialog(mw.app, mw.cfg, mw.window, func(service *s3.Service) {
			// This will be called when a connection is established
			mw.s3Service = service
			mw.loadFileManager()
		})
	}

	mw.connectDialog.Show()
}

func (mw *MainWindow) loadFileManager() {
	// Create file manager
	mw.fileManager = NewFileManager(mw.app, mw.s3Service, mw.window)

	// Set up the menu bar with connection controls
	menuBar := container.NewHBox(
		widget.NewButton("Change Connection", func() {
			mw.showConnectionDialog()
		}),
	)

	// Create a border layout with the menu at the top
	content := container.NewBorder(menuBar, nil, nil, nil, mw.fileManager.Container)

	// Update the window content
	mw.window.SetContent(content)

	// Start loading objects
	mw.fileManager.LoadObjects(mw.ctx)
}

func (mw *MainWindow) checkVersion() {
	req, err := http.NewRequest(http.MethodGet, "https://raw.githubusercontent.com/pteich/us3ui/refs/heads/main/FyneApp.toml", nil)
	if err != nil {
		fmt.Println("Error creating request: ", err)
		return
	}
	req.Header.Set("User-Agent", config.Name)

	tr := &http.Transport{
		MaxIdleConns:       10,
		IdleConnTimeout:    30 * time.Second,
		DisableCompression: false,
	}
	client := &http.Client{Transport: tr}

	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("Error sending request: ", err)
		return
	}
	defer resp.Body.Close()

	ai := AppInfo{}

	data, _ := io.ReadAll(resp.Body)
	err = toml.Unmarshal(data, &ai)
	if err != nil {
		fmt.Println("Error decoding response: ", err)
		return
	}

	verRemote, err := semver.NewVersion(ai.Details.Version)
	if err != nil {
		fmt.Println("Error parsing version: ", ai.Details.Version, err)
		return
	}

	verLocal, err := semver.NewVersion(fyne.CurrentApp().Metadata().Version)
	if err != nil {
		fmt.Println("Error parsing version: ", fyne.CurrentApp().Metadata().Version, err)
		return
	}

	if verRemote.GreaterThan(verLocal) {
		downloadLabel := "Download"
		abortLabel := "Abort"
		message := "A new version of us3ui is available.\n\n" +
			"Current Version: " + fyne.CurrentApp().Metadata().Version + "\n" +
			"Latest Version: " + ai.Details.Version + "\n\n" +
			"Do you want to download it from GitHub?"
		dialog.NewCustomConfirm("New Version Available", downloadLabel, abortLabel,
			widget.NewLabel(message), func(confirm bool) {
				if confirm {
					u, err := url.Parse("https://us3ui.pteich.de")
					if err != nil {
						fmt.Println("Error parsing URL:", err)
						return
					}
					_ = fyne.CurrentApp().OpenURL(u)
				}
			}, mw.window).Show()
	}
}
