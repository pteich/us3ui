package windows

import (
	"context"
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/pteich/us3ui/config"
	"github.com/pteich/us3ui/s3"
)

type saveConnectionLayout struct {
	padding float32
}

func (c *saveConnectionLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) != 2 {
		return
	}

	availableWidth := size.Width - c.padding

	objects[0].Resize(fyne.NewSize(availableWidth*0.7, size.Height))
	objects[0].Move(fyne.NewPos(0, 0))
	objects[1].Resize(fyne.NewSize(availableWidth*0.3, size.Height))
	objects[1].Move(fyne.NewPos(availableWidth*0.7+c.padding, 0))
}

func (c *saveConnectionLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	minWidth := float32(0)
	minHeight := float32(0)
	for _, child := range objects {
		childMin := child.MinSize()
		minWidth += childMin.Width
		if childMin.Height > minHeight {
			minHeight = childMin.Height
		}
	}
	return fyne.NewSize(minWidth, minHeight)
}

func ShowConnectWindow(ctx context.Context, cfg config.S3Config, a fyne.App) {
	configWin := a.NewWindow("S3 Server Config")
	configWin.CenterOnScreen()

	// Connection Manager Panel
	connections := []config.S3Config{
		{
			Name:      "My Connection",
			Endpoint:  "s3.example.com",
			AccessKey: "myaccesskey",
			SecretKey: "mysecretkey",
			Bucket:    "mybucket",
			Prefix:    "myprefix",
			Region:    "myregion",
			UseSSL:    true,
		},
	}
	connectionsList := widget.NewList(
		func() int { return len(connections) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			o.(*widget.Label).SetText(connections[i].Name)
		},
	)

	connectionsList.OnSelected = func(id widget.ListItemID) {
		// On select, update fields with the chosen connection's details (this requires storage for connections)
		// Load selected connection details into the form entries here
	}

	// Configuration Form
	endpointEntry := widget.NewEntry()
	endpointEntry.SetPlaceHolder("endpoint with optional port")
	endpointEntry.SetText(cfg.Endpoint)
	accessKeyEntry := widget.NewEntry()
	accessKeyEntry.SetText(cfg.AccessKey)
	secretKeyEntry := widget.NewPasswordEntry()
	secretKeyEntry.SetText(cfg.SecretKey)

	saveSecretkey := widget.NewCheck("Save secret key in system keyring", nil)

	bucketEntry := widget.NewEntry()
	bucketEntry.SetText(cfg.Bucket)
	prefixEntry := widget.NewEntry()
	prefixEntry.SetPlaceHolder("Optional Prefix")
	prefixEntry.SetText(cfg.Prefix)
	regionEntry := widget.NewEntry()
	regionEntry.SetPlaceHolder("Optional Region")
	regionEntry.SetText(cfg.Region)
	sslCheck := widget.NewCheck("Use SSL (HTTPS)", nil)
	sslCheck.SetChecked(cfg.UseSSL)

	connectionAddEntry := widget.NewEntry()
	connectionAddEntry.SetPlaceHolder("Connection Name")
	connectionAddEntry.SetText(cfg.Name)

	saveConnectionBtn := widget.NewButton("Save", func() {})

	saveConnection := container.New(&saveConnectionLayout{padding: 10}, connectionAddEntry, saveConnectionBtn)

	sep := widget.NewSeparator()

	configForm := widget.NewForm([]*widget.FormItem{
		{Text: "Endpoint", Widget: endpointEntry},
		{Text: "Access Key", Widget: accessKeyEntry},
		{Text: "Secret Key", Widget: secretKeyEntry},
		{Text: "", Widget: saveSecretkey},
		{Text: "Bucket Name", Widget: bucketEntry},
		{Text: "Region", Widget: regionEntry},
		{Text: "Prefix", Widget: prefixEntry},
		{Text: "", Widget: sslCheck},
		{Text: "Connection Name", Widget: saveConnection},
		{Text: "", Widget: sep},
	}...)
	configForm.OnSubmit = func() {
		cfg.Endpoint = endpointEntry.Text
		cfg.AccessKey = accessKeyEntry.Text
		cfg.SecretKey = secretKeyEntry.Text
		cfg.Bucket = bucketEntry.Text
		cfg.UseSSL = sslCheck.Checked
		cfg.Prefix = prefixEntry.Text
		cfg.Region = regionEntry.Text
		cfg.Name = connectionAddEntry.Text

		configWin.Hide()

		s3svc, err := s3.New(cfg)
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

	listButtons := widget.NewToolbar(
		widget.NewToolbarAction(theme.ContentAddIcon(), func() { fmt.Println("add") }),
		widget.NewToolbarAction(theme.ContentRemoveIcon(), func() { fmt.Println("remove") }),
	)

	// Main Layout
	connectionPanel := container.NewVBox(listButtons, connectionsList)
	formPanel := container.NewVBox(configForm)
	split := container.NewHSplit(connectionPanel, formPanel)
	split.SetOffset(0.3)

	configWin.SetContent(split)
	configWin.Resize(fyne.NewSize(700, 400))
	configWin.ShowAndRun()
}
