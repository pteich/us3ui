package windows

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/pteich/us3ui/config"
	"github.com/pteich/us3ui/connections"
	"github.com/pteich/us3ui/s3"
)

type ConnectDialog struct {
	app               fyne.App
	parentWindow      fyne.Window
	cfg               *config.Config
	connectionManager *connections.Manager
	dialog            *widget.PopUp
	onConnected       func(*s3.Service, string)

	// UI elements
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

func NewConnectDialog(a fyne.App, cfg *config.Config, parent fyne.Window, onConnected func(*s3.Service, string)) *ConnectDialog {
	cd := &ConnectDialog{
		app:          a,
		parentWindow: parent,
		cfg:          cfg,
		onConnected:  onConnected,
	}

	cd.connectionManager = connections.NewManager(cd.cfg)

	return cd
}

func (cd *ConnectDialog) Show() {
	if cd.dialog != nil && cd.dialog.Visible() {
		return
	}

	// Create content
	content := cd.buildDialogContent()

	// Add content top popup
	cd.dialog = widget.NewModalPopUp(content, cd.parentWindow.Canvas())

	cd.dialog.Resize(fyne.NewSize(700, 400))

	cd.dialog.Show()
}

func (cd *ConnectDialog) buildDialogContent() fyne.CanvasObject {
	cd.connectionsList = cd.createConnectionsList()
	cd.createFormEntries()
	cd.createToolbarActions()

	listButtons := cd.createListButtons()
	connectionPanel := container.NewBorder(listButtons, nil, nil, nil, cd.connectionsList)
	formPanel := container.NewVBox(cd.createConfigForm())

	split := container.NewHSplit(connectionPanel, formPanel)
	split.SetOffset(0.3)

	return split
}

func (cd *ConnectDialog) createConnectionsList() *widget.List {
	list := widget.NewList(
		func() int { return cd.connectionManager.Count() },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			o.(*widget.Label).SetText(cd.connectionManager.Get(i).Name)
		},
	)
	list.Resize(fyne.NewSize(300, 200))
	list.OnSelected = cd.handleListSelection
	list.OnUnselected = cd.handleListUnselection
	return list
}

func (cd *ConnectDialog) createFormEntries() {
	cd.connectionNameEntry = widget.NewEntry()
	cd.connectionNameEntry.SetPlaceHolder("Connection Name")
	cd.connectionNameEntry.TextStyle = fyne.TextStyle{Bold: true}
	cd.connectionNameEntry.OnChanged = cd.handleNameChange

	cd.endpointEntry = widget.NewEntry()
	cd.endpointEntry.SetPlaceHolder("endpoint with optional port")

	cd.accessKeyEntry = widget.NewEntry()
	cd.secretKeyEntry = widget.NewPasswordEntry()

	cd.bucketEntry = widget.NewEntry()
	cd.prefixEntry = widget.NewEntry()
	cd.prefixEntry.SetPlaceHolder("Optional Prefix")

	cd.regionEntry = widget.NewEntry()
	cd.regionEntry.SetPlaceHolder("Optional Region")

	cd.sslCheck = widget.NewCheck("Use SSL (HTTPS)", nil)
}

func (cd *ConnectDialog) createConfigForm() *widget.Form {
	form := widget.NewForm([]*widget.FormItem{
		{Text: "Name", Widget: cd.connectionNameEntry, HintText: "The name is only used to save connection details."},
		{Text: "Endpoint", Widget: cd.endpointEntry},
		{Text: "Access Key", Widget: cd.accessKeyEntry},
		{Text: "Secret Key", Widget: cd.secretKeyEntry},
		{Text: "Bucket Name", Widget: cd.bucketEntry},
		{Text: "Region", Widget: cd.regionEntry},
		{Text: "Prefix", Widget: cd.prefixEntry},
		{Text: "", Widget: cd.sslCheck},
	}...)

	form.OnSubmit = cd.handleFormSubmit
	form.OnCancel = func() {
		cd.dialog.Hide()
	}
	form.SubmitText = "Connect"
	form.CancelText = "Cancel"

	return form
}

func (cd *ConnectDialog) createToolbarActions() {
	cd.toolbarSaveAction = widget.NewToolbarAction(theme.DocumentSaveIcon(), cd.handleSave)
	cd.toolbarSaveAction.Disable()

	cd.toolbarDeleteAction = widget.NewToolbarAction(theme.ContentRemoveIcon(), cd.handleDelete)
	cd.toolbarDeleteAction.Disable()

	cd.toolbarCopyAction = widget.NewToolbarAction(theme.ContentCopyIcon(), cd.handleCopy)
	cd.toolbarCopyAction.Disable()
}

func (cd *ConnectDialog) createListButtons() *widget.Toolbar {
	return widget.NewToolbar(
		widget.NewToolbarAction(theme.ContentAddIcon(), cd.handleAdd),
		cd.toolbarDeleteAction,
		cd.toolbarCopyAction,
		widget.NewToolbarSpacer(),
		cd.toolbarSaveAction,
	)
}

func (cd *ConnectDialog) handleListSelection(id widget.ListItemID) {
	selectedCfg := cd.connectionManager.Get(id)
	cd.connectionManager.SetSelected(id)
	cd.connectionNameEntry.SetText(selectedCfg.Name)
	cd.endpointEntry.SetText(selectedCfg.Endpoint)
	cd.accessKeyEntry.SetText(selectedCfg.AccessKey)
	cd.secretKeyEntry.SetText(selectedCfg.SecretKey)
	cd.bucketEntry.SetText(selectedCfg.Bucket)
	cd.prefixEntry.SetText(selectedCfg.Prefix)
	cd.regionEntry.SetText(selectedCfg.Region)
	cd.sslCheck.SetChecked(selectedCfg.UseSSL)
	cd.connectionNameEntry.SetText(selectedCfg.Name)
	cd.toolbarDeleteAction.Enable()
	cd.toolbarCopyAction.Enable()
}

func (cd *ConnectDialog) handleListUnselection(id widget.ListItemID) {
	cd.connectionManager.SetSelected(-1)
	cd.toolbarDeleteAction.Disable()
	cd.toolbarCopyAction.Disable()
}

func (cd *ConnectDialog) handleFormSubmit() {
	s3Cfg := config.S3Config{
		Endpoint:  cd.endpointEntry.Text,
		AccessKey: cd.accessKeyEntry.Text,
		SecretKey: cd.secretKeyEntry.Text,
		Bucket:    cd.bucketEntry.Text,
		UseSSL:    cd.sslCheck.Checked,
		Prefix:    cd.prefixEntry.Text,
		Region:    cd.regionEntry.Text,
		Name:      cd.connectionNameEntry.Text,
	}

	// Save the connection if it has a name
	if s3Cfg.Name != "" {
		cd.connectionManager.Add(s3Cfg)
		if err := cd.connectionManager.Save(); err != nil {
			dialog.ShowError(err, cd.parentWindow)
			return
		}
	}

	// Create the S3 service
	s3svc, err := s3.New(s3Cfg)
	if err != nil {
		dialog.ShowError(err, cd.parentWindow)
		return
	}

	// Hide the dialog
	cd.dialog.Hide()

	// Notify that we're connected
	if cd.onConnected != nil {
		cd.onConnected(s3svc, s3Cfg.Prefix)
	}
}

func (cd *ConnectDialog) handleNameChange(name string) {
	if name == "" {
		cd.toolbarSaveAction.Disable()
	} else {
		cd.toolbarSaveAction.Enable()
	}
}

func (cd *ConnectDialog) handleSave() {
	newcfg := config.S3Config{
		Endpoint:  cd.endpointEntry.Text,
		AccessKey: cd.accessKeyEntry.Text,
		SecretKey: cd.secretKeyEntry.Text,
		Bucket:    cd.bucketEntry.Text,
		UseSSL:    cd.sslCheck.Checked,
		Prefix:    cd.prefixEntry.Text,
		Region:    cd.regionEntry.Text,
		Name:      cd.connectionNameEntry.Text,
	}
	cd.connectionManager.Add(newcfg)
	if err := cd.connectionManager.Save(); err != nil {
		dialog.ShowError(err, cd.parentWindow)
		return
	}
	cd.connectionsList.Refresh()
}

func (cd *ConnectDialog) handleDelete() {
	selectedID := cd.connectionManager.GetSelected()
	if selectedID == -1 {
		return
	}
	cd.connectionManager.Remove(selectedID)
	if err := cd.connectionManager.Save(); err != nil {
		dialog.ShowError(err, cd.parentWindow)
		return
	}
	cd.connectionsList.Refresh()
}

func (cd *ConnectDialog) handleCopy() {
	selectedCfg := cd.connectionManager.Get(cd.connectionManager.GetSelected())
	newCfg := selectedCfg
	newCfg.Name = "Copy of " + selectedCfg.Name

	newNameEntry := widget.NewEntry()
	newNameEntry.SetText(newCfg.Name)
	dialog.ShowCustomConfirm("New connection name", "Copy Connection", "Abort", newNameEntry, func(b bool) {
		if b {
			newCfg.Name = newNameEntry.Text
			cd.connectionManager.Add(newCfg)
			if err := cd.connectionManager.Save(); err != nil {
				dialog.ShowError(err, cd.parentWindow)
				return
			}
			cd.connectionsList.Refresh()
		}
	}, cd.parentWindow)
}

func (cd *ConnectDialog) handleAdd() {
	cd.connectionManager.SetSelected(-1)
	selectedCfg := config.S3Config{}
	cd.connectionNameEntry.SetText(selectedCfg.Name)
	cd.endpointEntry.SetText(selectedCfg.Endpoint)
	cd.accessKeyEntry.SetText(selectedCfg.AccessKey)
	cd.secretKeyEntry.SetText(selectedCfg.SecretKey)
	cd.bucketEntry.SetText(selectedCfg.Bucket)
	cd.prefixEntry.SetText(selectedCfg.Prefix)
	cd.regionEntry.SetText(selectedCfg.Region)
	cd.sslCheck.SetChecked(selectedCfg.UseSSL)
	cd.connectionNameEntry.SetText(selectedCfg.Name)
	cd.toolbarSaveAction.Disable()
	cd.toolbarDeleteAction.Disable()
	cd.toolbarCopyAction.Disable()
}
