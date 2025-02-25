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

func ShowConnectWindow(ctx context.Context, cfg *config.Config, a fyne.App) {
	configWin := a.NewWindow("S3 Server Connection")
	configWin.CenterOnScreen()

	connectionManager := connections.NewManager(cfg)
	defer func() {
		err := connectionManager.Save()
		if err != nil {
			dialog.ShowError(err, configWin)
		}
	}()

	connectionsList := widget.NewList(
		func() int { return connectionManager.Count() },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			o.(*widget.Label).SetText(connectionManager.Get(i).Name)
		},
	)
	connectionsList.Resize(fyne.NewSize(300, 200))
	connectionsList.Refresh()

	// Configuration Form
	s3cfg := config.S3Config{}
	if connectionManager.GetSelected() >= 0 {
		s3cfg = connectionManager.Get(connectionManager.GetSelected())
	}

	endpointEntry := widget.NewEntry()
	endpointEntry.SetPlaceHolder("endpoint with optional port")
	endpointEntry.SetText(s3cfg.Endpoint)
	accessKeyEntry := widget.NewEntry()
	accessKeyEntry.SetText(s3cfg.AccessKey)
	secretKeyEntry := widget.NewPasswordEntry()
	secretKeyEntry.SetText(s3cfg.SecretKey)

	// saveSecretkey := widget.NewCheck("Save secret key in system keyring", nil)

	bucketEntry := widget.NewEntry()
	bucketEntry.SetText(s3cfg.Bucket)
	prefixEntry := widget.NewEntry()
	prefixEntry.SetPlaceHolder("Optional Prefix")
	prefixEntry.SetText(s3cfg.Prefix)
	regionEntry := widget.NewEntry()
	regionEntry.SetPlaceHolder("Optional Region")
	regionEntry.SetText(s3cfg.Region)
	sslCheck := widget.NewCheck("Use SSL (HTTPS)", nil)
	sslCheck.SetChecked(s3cfg.UseSSL)

	connectionNameEntry := widget.NewEntry()
	connectionNameEntry.SetPlaceHolder("Connection Name")
	connectionNameEntry.SetText(s3cfg.Name)
	connectionNameEntry.TextStyle = fyne.TextStyle{Bold: true}

	toolbarSaveAction := widget.NewToolbarAction(theme.DocumentSaveIcon(), func() {
		newcfg := config.S3Config{
			Endpoint:  endpointEntry.Text,
			AccessKey: accessKeyEntry.Text,
			SecretKey: secretKeyEntry.Text,
			Bucket:    bucketEntry.Text,
			UseSSL:    sslCheck.Checked,
			Prefix:    prefixEntry.Text,
			Region:    regionEntry.Text,
			Name:      connectionNameEntry.Text,
		}
		connectionManager.Add(newcfg)
		connectionsList.Refresh()
	})
	toolbarSaveAction.Disable()

	connectionNameEntry.OnChanged = func(name string) {
		if name == "" {
			toolbarSaveAction.Disable()
		} else {
			toolbarSaveAction.Enable()
		}
	}

	configForm := widget.NewForm([]*widget.FormItem{
		{Text: "Name", Widget: connectionNameEntry, HintText: "The name is only used to save connection details."},
		{Text: "Endpoint", Widget: endpointEntry},
		{Text: "Access Key", Widget: accessKeyEntry},
		{Text: "Secret Key", Widget: secretKeyEntry},
		// {Text: "", Widget: saveSecretkey},
		{Text: "Bucket Name", Widget: bucketEntry},
		{Text: "Region", Widget: regionEntry},
		{Text: "Prefix", Widget: prefixEntry},
		{Text: "", Widget: sslCheck},
	}...)
	configForm.OnSubmit = func() {
		s3Cfg := config.S3Config{
			Endpoint:  endpointEntry.Text,
			AccessKey: accessKeyEntry.Text,
			SecretKey: secretKeyEntry.Text,
			Bucket:    bucketEntry.Text,
			UseSSL:    sslCheck.Checked,
			Prefix:    prefixEntry.Text,
			Region:    regionEntry.Text,
			Name:      connectionNameEntry.Text,
		}

		configWin.Hide()

		s3svc, err := s3.New(s3Cfg)
		if err != nil {
			dialog.ShowError(err, configWin)
			return
		}

		mainWin := NewMainWindow(a, s3svc)
		mainWin.Show(ctx)
	}
	configForm.OnCancel = func() {
		a.Quit()
	}
	configForm.SubmitText = "Connect"
	configForm.CancelText = "Abort"

	toolbarDeleteAction := widget.NewToolbarAction(theme.ContentRemoveIcon(), func() {
		connectionManager.Remove(connectionManager.GetSelected())
		connectionsList.Refresh()
	})
	toolbarDeleteAction.Disable()

	toolbarAddAction := widget.NewToolbarAction(theme.ContentAddIcon(), func() {
		connectionManager.SetSelected(-1)
		selectedCfg := config.S3Config{}
		connectionNameEntry.SetText(selectedCfg.Name)
		endpointEntry.SetText(selectedCfg.Endpoint)
		accessKeyEntry.SetText(selectedCfg.AccessKey)
		secretKeyEntry.SetText(selectedCfg.SecretKey)
		bucketEntry.SetText(selectedCfg.Bucket)
		prefixEntry.SetText(selectedCfg.Prefix)
		regionEntry.SetText(selectedCfg.Region)
		sslCheck.SetChecked(selectedCfg.UseSSL)
		connectionNameEntry.SetText(selectedCfg.Name)
		toolbarSaveAction.Disable()
		toolbarDeleteAction.Disable()
	})
	toolbarCopyAction := widget.NewToolbarAction(theme.ContentCopyIcon(), func() {
		selectedCfg := connectionManager.Get(connectionManager.GetSelected())
		newCfg := selectedCfg
		newCfg.Name = "Copy of " + selectedCfg.Name

		newNameEntry := widget.NewEntry()
		newNameEntry.SetText(newCfg.Name)
		dialog.ShowCustomConfirm("New connection name", "Copy Connection", "Abort", newNameEntry, func(b bool) {
			if b {
				newCfg.Name = newNameEntry.Text
				connectionManager.Add(newCfg)
				connectionsList.Refresh()
			}
		}, configWin)
	})
	toolbarCopyAction.Disable()

	listButtons := widget.NewToolbar(
		toolbarAddAction,
		toolbarDeleteAction,
		toolbarCopyAction,
		widget.NewToolbarSpacer(),
		toolbarSaveAction,
	)

	connectionsList.OnSelected = func(id widget.ListItemID) {
		selectedCfg := connectionManager.Get(id)
		connectionManager.SetSelected(id)
		connectionNameEntry.SetText(selectedCfg.Name)
		endpointEntry.SetText(selectedCfg.Endpoint)
		accessKeyEntry.SetText(selectedCfg.AccessKey)
		secretKeyEntry.SetText(selectedCfg.SecretKey)
		bucketEntry.SetText(selectedCfg.Bucket)
		prefixEntry.SetText(selectedCfg.Prefix)
		regionEntry.SetText(selectedCfg.Region)
		sslCheck.SetChecked(selectedCfg.UseSSL)
		connectionNameEntry.SetText(selectedCfg.Name)
		toolbarDeleteAction.Enable()
		toolbarCopyAction.Enable()
	}
	connectionsList.OnUnselected = func(id widget.ListItemID) {
		toolbarDeleteAction.Disable()
		toolbarCopyAction.Disable()
		connectionManager.SetSelected(-1)
	}

	// Main Layout
	connectionPanel := container.NewBorder(listButtons, nil, nil, nil, connectionsList)
	// connectionPanel := container.NewVBox(listButtons, connectionsList, layout.NewSpacer())
	formPanel := container.NewVBox(configForm)
	split := container.NewHSplit(connectionPanel, formPanel)
	split.SetOffset(0.3)

	configWin.SetContent(split)
	configWin.Resize(fyne.NewSize(700, 400))

	checkVersion(configWin)

	configWin.ShowAndRun()
}

type AppInfo struct {
	Details struct {
		Version string `toml:"version"`
	} `toml:"details"`
}

func checkVersion(w fyne.Window) {
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
					u, err := url.Parse("https://github.com/pteich/us3ui/releases")
					if err != nil {
						fmt.Println("Error parsing URL:", err)
						return
					}
					_ = fyne.CurrentApp().OpenURL(u)
				}
			}, w).Show()
	}
}
