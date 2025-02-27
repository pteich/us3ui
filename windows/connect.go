package windows

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/Masterminds/semver/v3"
	"github.com/pelletier/go-toml/v2"

	"github.com/pteich/us3ui/config"
	"github.com/pteich/us3ui/connections"
	"github.com/pteich/us3ui/s3"
)

type ConnectWindow struct {
	app                 fyne.App
	window              fyne.Window
	cfg                 *config.Config
	connectionManager   *connections.Manager
	connectionsList     *widget.List
	toolbarSaveAction   *widget.ToolbarAction
	toolbarDeleteAction *widget.ToolbarAction
	toolbarCopyAction   *widget.ToolbarAction

	// Form Entries
	connectionNameEntry *widget.Entry
	endpointEntry       *widget.Entry
	accessKeyEntry      *widget.Entry
	secretKeyEntry      *widget.Entry
	bucketEntry         *widget.Entry
	prefixEntry         *widget.Entry
	regionEntry         *widget.Entry
	sslCheck            *widget.Check
}

func NewConnectWindow(a fyne.App, cfg *config.Config) *ConnectWindow {
	window := a.NewWindow("S3 Server Connection")
	window.CenterOnScreen()

	return &ConnectWindow{
		app:    a,
		window: window,
		cfg:    cfg,
	}
}

func (cw *ConnectWindow) Show(ctx context.Context) {
	cw.connectionManager = connections.NewManager(cw.cfg)
	defer func() {
		if err := cw.connectionManager.Save(); err != nil {
			dialog.ShowError(err, cw.window)
		}
	}()

	cw.setupGUI()
	cw.checkVersion()
	cw.window.ShowAndRun()
}

func (cw *ConnectWindow) setupGUI() {
	cw.connectionsList = cw.createConnectionsList()
	cw.createFormEntries()
	cw.createToolbarActions()

	listButtons := cw.createListButtons()
	connectionPanel := container.NewBorder(listButtons, nil, nil, nil, cw.connectionsList)
	formPanel := container.NewVBox(cw.createConfigForm())

	split := container.NewHSplit(connectionPanel, formPanel)
	split.SetOffset(0.3)

	cw.window.SetContent(split)
	cw.window.Resize(fyne.NewSize(700, 400))
}

func (cw *ConnectWindow) createConnectionsList() *widget.List {
	list := widget.NewList(
		func() int { return cw.connectionManager.Count() },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			o.(*widget.Label).SetText(cw.connectionManager.Get(i).Name)
		},
	)
	list.Resize(fyne.NewSize(300, 200))
	list.OnSelected = cw.handleListSelection
	list.OnUnselected = cw.handleListUnselection
	return list
}

func (cw *ConnectWindow) createFormEntries() {
	cw.connectionNameEntry = widget.NewEntry()
	cw.connectionNameEntry.SetPlaceHolder("Connection Name")
	cw.connectionNameEntry.TextStyle = fyne.TextStyle{Bold: true}
	cw.connectionNameEntry.OnChanged = cw.handleNameChange

	cw.endpointEntry = widget.NewEntry()
	cw.endpointEntry.SetPlaceHolder("endpoint with optional port")

	cw.accessKeyEntry = widget.NewEntry()
	cw.secretKeyEntry = widget.NewPasswordEntry()

	cw.bucketEntry = widget.NewEntry()
	cw.prefixEntry = widget.NewEntry()
	cw.prefixEntry.SetPlaceHolder("Optional Prefix")

	cw.regionEntry = widget.NewEntry()
	cw.regionEntry.SetPlaceHolder("Optional Region")

	cw.sslCheck = widget.NewCheck("Use SSL (HTTPS)", nil)
}

func (cw *ConnectWindow) createConfigForm() *widget.Form {
	form := widget.NewForm([]*widget.FormItem{
		{Text: "Name", Widget: cw.connectionNameEntry, HintText: "The name is only used to save connection details."},
		{Text: "Endpoint", Widget: cw.endpointEntry},
		{Text: "Access Key", Widget: cw.accessKeyEntry},
		{Text: "Secret Key", Widget: cw.secretKeyEntry},
		{Text: "Bucket Name", Widget: cw.bucketEntry},
		{Text: "Region", Widget: cw.regionEntry},
		{Text: "Prefix", Widget: cw.prefixEntry},
		{Text: "", Widget: cw.sslCheck},
	}...)

	form.OnSubmit = cw.handleFormSubmit
	form.OnCancel = cw.app.Quit
	form.SubmitText = "Connect"
	form.CancelText = "Abort"

	return form
}

func (cw *ConnectWindow) createToolbarActions() {
	cw.toolbarSaveAction = widget.NewToolbarAction(theme.DocumentSaveIcon(), cw.handleSave)
	cw.toolbarSaveAction.Disable()

	cw.toolbarDeleteAction = widget.NewToolbarAction(theme.ContentRemoveIcon(), cw.handleDelete)
	cw.toolbarDeleteAction.Disable()

	cw.toolbarCopyAction = widget.NewToolbarAction(theme.ContentCopyIcon(), cw.handleCopy)
	cw.toolbarCopyAction.Disable()
}

func (cw *ConnectWindow) createListButtons() *widget.Toolbar {
	return widget.NewToolbar(
		widget.NewToolbarAction(theme.ContentAddIcon(), cw.handleAdd),
		cw.toolbarDeleteAction,
		cw.toolbarCopyAction,
		widget.NewToolbarSpacer(),
		cw.toolbarSaveAction,
	)
}

func (cw *ConnectWindow) handleListSelection(id widget.ListItemID) {
	selectedCfg := cw.connectionManager.Get(id)
	cw.connectionManager.SetSelected(id)
	cw.connectionNameEntry.SetText(selectedCfg.Name)
	cw.endpointEntry.SetText(selectedCfg.Endpoint)
	cw.accessKeyEntry.SetText(selectedCfg.AccessKey)
	cw.secretKeyEntry.SetText(selectedCfg.SecretKey)
	cw.bucketEntry.SetText(selectedCfg.Bucket)
	cw.prefixEntry.SetText(selectedCfg.Prefix)
	cw.regionEntry.SetText(selectedCfg.Region)
	cw.sslCheck.SetChecked(selectedCfg.UseSSL)
	cw.connectionNameEntry.SetText(selectedCfg.Name)
	cw.toolbarDeleteAction.Enable()
	cw.toolbarCopyAction.Enable()
}

func (cw *ConnectWindow) handleListUnselection(id widget.ListItemID) {
	cw.connectionManager.SetSelected(-1)
	cw.toolbarDeleteAction.Disable()
	cw.toolbarCopyAction.Disable()
}

func (cw *ConnectWindow) handleFormSubmit() {
	s3Cfg := config.S3Config{
		Endpoint:  cw.endpointEntry.Text,
		AccessKey: cw.accessKeyEntry.Text,
		SecretKey: cw.secretKeyEntry.Text,
		Bucket:    cw.bucketEntry.Text,
		UseSSL:    cw.sslCheck.Checked,
		Prefix:    cw.prefixEntry.Text,
		Region:    cw.regionEntry.Text,
		Name:      cw.connectionNameEntry.Text,
	}

	cw.window.Hide()

	s3svc, err := s3.New(s3Cfg)
	if err != nil {
		dialog.ShowError(err, cw.window)
		return
	}

	mainWin := NewMainWindow(cw.app, s3svc)
	mainWin.Show(context.Background())
}

func (cw *ConnectWindow) handleNameChange(name string) {
	if name == "" {
		cw.toolbarSaveAction.Disable()
	} else {
		cw.toolbarSaveAction.Enable()
	}
}

func (cw *ConnectWindow) handleSave() {
	newcfg := config.S3Config{
		Endpoint:  cw.endpointEntry.Text,
		AccessKey: cw.accessKeyEntry.Text,
		SecretKey: cw.secretKeyEntry.Text,
		Bucket:    cw.bucketEntry.Text,
		UseSSL:    cw.sslCheck.Checked,
		Prefix:    cw.prefixEntry.Text,
		Region:    cw.regionEntry.Text,
		Name:      cw.connectionNameEntry.Text,
	}
	cw.connectionManager.Add(newcfg)
	cw.connectionsList.Refresh()
}

func (cw *ConnectWindow) handleDelete() {
	selectedID := cw.connectionManager.GetSelected()
	if selectedID == -1 {
		return
	}
	cw.connectionManager.Remove(selectedID)
	cw.connectionsList.Refresh()
}

func (cw *ConnectWindow) handleCopy() {
	selectedCfg := cw.connectionManager.Get(cw.connectionManager.GetSelected())
	newCfg := selectedCfg
	newCfg.Name = "Copy of " + selectedCfg.Name

	newNameEntry := widget.NewEntry()
	newNameEntry.SetText(newCfg.Name)
	dialog.ShowCustomConfirm("New connection name", "Copy Connection", "Abort", newNameEntry, func(b bool) {
		if b {
			newCfg.Name = newNameEntry.Text
			cw.connectionManager.Add(newCfg)
			cw.connectionsList.Refresh()
		}
	}, cw.window)
}

func (cw *ConnectWindow) handleAdd() {
	cw.connectionManager.SetSelected(-1)
	selectedCfg := config.S3Config{}
	cw.connectionNameEntry.SetText(selectedCfg.Name)
	cw.endpointEntry.SetText(selectedCfg.Endpoint)
	cw.accessKeyEntry.SetText(selectedCfg.AccessKey)
	cw.secretKeyEntry.SetText(selectedCfg.SecretKey)
	cw.bucketEntry.SetText(selectedCfg.Bucket)
	cw.prefixEntry.SetText(selectedCfg.Prefix)
	cw.regionEntry.SetText(selectedCfg.Region)
	cw.sslCheck.SetChecked(selectedCfg.UseSSL)
	cw.connectionNameEntry.SetText(selectedCfg.Name)
	cw.toolbarSaveAction.Disable()
	cw.toolbarDeleteAction.Disable()
	cw.toolbarCopyAction.Disable()
}

func (cw *ConnectWindow) checkVersion() {
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
			}, cw.window).Show()
	}
}
