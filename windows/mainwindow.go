package windows

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/minio/minio-go/v7"

	"github.com/pteich/us3ui/s3"
)

type MainWindow struct {
	app                 fyne.App
	window              fyne.Window
	s3svc               *s3.Service
	currentObjects      []minio.ObjectInfo
	allObjects          []minio.ObjectInfo
	selectedIndex       int
	searchTerm          string
	searchDebounceTimer *time.Timer

	itemsLabel  *widget.Label
	objectList  *widget.Table
	searchInput *widget.Entry
	progressBar *widget.ProgressBar
	stopBtn     *widget.Button
}

func NewMainWindow(a fyne.App, s3svc *s3.Service) *MainWindow {
	window := a.NewWindow("Universal s3 UI")
	window.CenterOnScreen()
	window.SetMaster()

	return &MainWindow{
		app:    a,
		s3svc:  s3svc,
		window: window,
	}
}

func (mw *MainWindow) Show(ctx context.Context) {
	mw.setupGUI(ctx)
	mw.loadObjects(ctx)

	mw.window.Show()
}

func (mw *MainWindow) setupGUI(ctx context.Context) {
	mw.itemsLabel = mw.createItemsLabel()
	mw.objectList = mw.createObjectList()
	mw.searchInput = mw.createSearchInput()
	mw.progressBar = mw.createProgressBar()
	mw.stopBtn = mw.createStopButton()

	bottomContainer := mw.createBottomContainer()
	btnBar := mw.createButtonBar(ctx)
	topContainer := mw.createTopContainer(btnBar)

	content := container.NewBorder(topContainer, container.NewPadded(bottomContainer), nil, nil, mw.objectList)
	mw.window.SetContent(content)
	mw.window.Resize(fyne.NewSize(800, 400))
}

func (mw *MainWindow) createItemsLabel() *widget.Label {
	label := widget.NewLabel("")
	label.Resize(fyne.NewSize(200, label.MinSize().Height))
	return label
}

func (mw *MainWindow) updateItemsLabel() {
	mw.itemsLabel.SetText(fmt.Sprintf("Total Items: %d", len(mw.currentObjects)))
}

func (mw *MainWindow) createObjectList() *widget.Table {
	objectList := widget.NewTableWithHeaders(
		func() (int, int) {
			return len(mw.currentObjects), 3
		},
		func() fyne.CanvasObject {
			return widget.NewLabel("Objects")
		},
		func(id widget.TableCellID, co fyne.CanvasObject) {
			label := co.(*widget.Label)
			obj := mw.currentObjects[id.Row]

			switch id.Col {
			case 0:
				label.SetText(obj.Key)
				label.Truncation = fyne.TextTruncateEllipsis
			case 1:
				label.SetText(fmt.Sprintf("%d kB", obj.Size/1024))
			case 2:
				label.SetText(obj.LastModified.Format("2006-01-02 15:04:05"))
				label.Truncation = fyne.TextTruncateEllipsis
			}
		},
	)
	objectList.OnSelected = func(id widget.TableCellID) {
		mw.selectedIndex = id.Row
		if id.Col > 1 {
			id.Col = 0
			objectList.Select(id)
		}
	}
	objectList.OnUnselected = func(id widget.TableCellID) {
		if mw.selectedIndex == id.Row {
			mw.selectedIndex = -1
		}
	}
	objectList.SetColumnWidth(0, 400)
	objectList.SetColumnWidth(1, 100)
	objectList.SetColumnWidth(2, 250)
	objectList.ShowHeaderColumn = false
	objectList.CreateHeader = func() fyne.CanvasObject {
		b := widget.NewButton("", func() {})
		b.Alignment = widget.ButtonAlignLeading
		return b
	}
	objectList.UpdateHeader = func(id widget.TableCellID, o fyne.CanvasObject) {
		b := o.(*widget.Button)
		if id.Col == -1 {
			b.SetText(strconv.Itoa(id.Row))
			b.Importance = widget.LowImportance
			b.Disable()
			return
		}

		switch id.Col {
		case 0:
			b.SetText("Name")
			b.Icon = theme.MoveUpIcon()
		case 1:
			b.SetText("Size")
		case 2:
			b.SetText("Last Modified")
		}
	}

	return objectList
}

func (mw *MainWindow) createSearchInput() *widget.Entry {
	searchInput := widget.NewEntry()
	searchInput.SetPlaceHolder("Search...")
	searchInput.Resize(fyne.NewSize(400, searchInput.MinSize().Height))
	searchInput.OnChanged = func(s string) {
		mw.searchTerm = s
		if mw.searchDebounceTimer != nil {
			mw.searchDebounceTimer.Stop()
		}
		mw.searchDebounceTimer = time.AfterFunc(300*time.Millisecond, mw.updateObjectList)
	}
	return searchInput
}

func (mw *MainWindow) createProgressBar() *widget.ProgressBar {
	progressBar := widget.NewProgressBar()
	progressBar.Hide()
	progressBar.Resize(fyne.NewSize(400, progressBar.MinSize().Height))
	return progressBar
}

func (mw *MainWindow) createStopButton() *widget.Button {
	stopBtn := widget.NewButton("Stop", func() {
		// This will be implemented later
	})
	stopBtn.Hide()
	return stopBtn
}

func (mw *MainWindow) createBottomContainer() *fyne.Container {
	return container.NewHBox(
		mw.itemsLabel,
		layout.NewSpacer(),
		mw.stopBtn,
		container.NewGridWrap(fyne.NewSize(400, mw.progressBar.MinSize().Height), mw.progressBar),
	)
}

func (mw *MainWindow) createButtonBar(ctx context.Context) *fyne.Container {
	refreshBtn := widget.NewButton("Refresh", func() {
		mw.loadObjects(ctx)
	})

	deleteBtn := widget.NewButton("Delete", func() {
		mw.handleDelete(ctx)
	})

	uploadBtn := widget.NewButton("Upload", func() {
		mw.handleUpload(ctx)
	})

	downloadBtn := widget.NewButton("Download", func() {
		mw.handleDownload(ctx)
	})

	exitBtn := widget.NewButton("Exit", func() {
		mw.app.Quit()
	})
	exitBtn.Importance = widget.HighImportance
	exitBtn.Alignment = widget.ButtonAlignTrailing

	return container.NewHBox(refreshBtn, downloadBtn, deleteBtn, uploadBtn, exitBtn)
}

func (mw *MainWindow) createTopContainer(btnBar *fyne.Container) *fyne.Container {
	searchBar := container.New(layout.NewStackLayout(), mw.searchInput)
	return container.NewVBox(btnBar, container.NewPadded(searchBar))
}

func (mw *MainWindow) filterObjects() []minio.ObjectInfo {
	if mw.searchTerm == "" {
		return mw.allObjects
	}

	var filteredObjects []minio.ObjectInfo
	for _, obj := range mw.allObjects {
		if strings.Contains(strings.ToLower(obj.Key), strings.ToLower(mw.searchTerm)) {
			filteredObjects = append(filteredObjects, obj)
		}
	}
	return filteredObjects
}

func (mw *MainWindow) updateObjectList() {
	mw.currentObjects = mw.filterObjects()
	mw.objectList.Refresh()
	mw.updateItemsLabel()
}

func (mw *MainWindow) loadObjects(ctx context.Context) {
	mw.progressBar.Show()
	mw.stopBtn.Show()
	mw.progressBar.SetValue(0)
	mw.searchInput.SetText("")

	go func() {
		defer mw.progressBar.Hide()
		defer mw.stopBtn.Hide()

		batchSize := 100
		var err error
		mw.allObjects = []minio.ObjectInfo{}
		var lastKey string

		for {
			batch, err := mw.s3svc.ListObjectsBatch(ctx, lastKey, batchSize)
			if err != nil {
				dialog.ShowError(err, mw.window)
				return
			}

			mw.allObjects = append(mw.allObjects, batch...)

			progress := float64(len(mw.allObjects)) / float64(len(mw.allObjects)+len(batch))
			mw.progressBar.SetValue(progress)

			if len(batch) < batchSize {
				break
			}

			if len(batch) > 0 {
				lastKey = batch[len(batch)-1].Key
			}

			mw.selectedIndex = -1
			mw.updateObjectList()
		}
		if err != nil {
			dialog.ShowError(err, mw.window)
			return
		}

		mw.selectedIndex = -1
		mw.updateObjectList()
	}()
}

func (mw *MainWindow) handleDelete(ctx context.Context) {
	if mw.selectedIndex < 0 || mw.selectedIndex >= len(mw.currentObjects) {
		dialog.ShowInformation("Info", "No object selected", mw.window)
		return
	}
	obj := mw.currentObjects[mw.selectedIndex]
	confirm := dialog.NewConfirm(
		"Delete Object?",
		fmt.Sprintf("Do you really want to delete '%s'?", obj.Key),
		func(yes bool) {
			if yes {
				err := mw.s3svc.DeleteObject(ctx, obj.Key)
				if err != nil {
					dialog.ShowError(err, mw.window)
				} else {
					mw.loadObjects(ctx)
				}
			}
		}, mw.window)
	confirm.Show()
}

func (mw *MainWindow) handleUpload(ctx context.Context) {
	fd := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
		if err != nil || reader == nil {
			return
		}
		defer reader.Close()

		var fileData []byte
		buf := make([]byte, 1024)
		for {
			n, readErr := reader.Read(buf)
			if n > 0 {
				fileData = append(fileData, buf[:n]...)
			}
			if readErr != nil {
				break
			}
		}

		err = mw.s3svc.UploadObject(reader.URI().Name(), fileData)
		if err != nil {
			dialog.ShowError(err, mw.window)
			return
		}
		dialog.ShowInformation("OK", "Upload finished!", mw.window)
		mw.loadObjects(ctx)
	}, mw.window)
	fd.Show()
}

func (mw *MainWindow) handleDownload(ctx context.Context) {
	if mw.selectedIndex < 0 || mw.selectedIndex >= len(mw.currentObjects) {
		dialog.ShowInformation("Info", "No object selected!", mw.window)
		return
	}
	obj := mw.currentObjects[mw.selectedIndex]

	fileSaveDialog := dialog.NewFileSave(func(fc fyne.URIWriteCloser, err error) {
		if err != nil {
			dialog.ShowError(err, mw.window)
			return
		}
		if fc == nil {
			return
		}
		defer fc.Close()

		s3obj, err := mw.s3svc.DownloadObject(ctx, obj.Key)
		if err != nil {
			dialog.ShowError(err, mw.window)
			return
		}
		defer s3obj.Close()

		_, copyErr := io.Copy(fc, s3obj)
		if copyErr != nil {
			dialog.ShowError(copyErr, mw.window)
			return
		}

		dialog.ShowInformation("Download", "Download finished!", mw.window)
	}, mw.window)

	fileSaveDialog.SetFileName(strings.ReplaceAll(obj.Key, "/", "_"))
	fileSaveDialog.Show()
}
