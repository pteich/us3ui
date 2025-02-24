package windows

import (
	"context"
	"fmt"
	"io"
	"os"
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

const batchSize = 150

type MainWindow struct {
	app                 fyne.App
	window              fyne.Window
	s3svc               *s3.Service
	currentObjects      []minio.ObjectInfo
	allObjects          []minio.ObjectInfo
	selectedIndex       map[int]bool
	searchTerm          string
	searchDebounceTimer *time.Timer

	itemsLabel  *widget.Label
	objectList  *widget.Table
	searchInput *widget.Entry
	progressBar *widget.ProgressBar
	stopBtn     *widget.Button
	deleteBtn   *widget.Button
	downloadBtn *widget.Button
	linkBtn     *widget.Button
}

func NewMainWindow(a fyne.App, s3svc *s3.Service) *MainWindow {
	window := a.NewWindow("Universal s3 UI")
	window.CenterOnScreen()
	window.SetMaster()
	window.SetOnDropped(func(p fyne.Position, files []fyne.URI) {
		// TODO handle dropped files
	})

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
	filtered := len(mw.currentObjects)
	all := len(mw.allObjects)
	if filtered != all {
		mw.itemsLabel.SetText(fmt.Sprintf("Items: %d of %d total", filtered, all))
		return
	}

	mw.itemsLabel.SetText(fmt.Sprintf("Total Items: %d", all))
}

func (mw *MainWindow) createObjectList() *widget.Table {
	objectList := widget.NewTableWithHeaders(
		func() (int, int) {
			return len(mw.currentObjects), 4
		},
		func() fyne.CanvasObject {
			return container.NewStack(
				widget.NewCheck("", nil),
				widget.NewLabel(""),
			)
		},
		func(id widget.TableCellID, co fyne.CanvasObject) {
			box := co.(*fyne.Container)
			check := box.Objects[0].(*widget.Check)
			label := box.Objects[1].(*widget.Label)

			obj := mw.currentObjects[id.Row]

			switch id.Col {
			case 0:
				check.Show()
				check.Refresh()
				label.Hide()
				check.OnChanged = func(checked bool) {
					if checked {
						mw.updateSelect(id.Row, true)
					} else {
						mw.updateSelect(id.Row, false)
					}
				}
			case 1:
				check.Hide()
				label.Show()
				//label.TextStyle = fyne.TextStyle{Bold: true}
				label.Truncation = fyne.TextTruncateEllipsis
				label.SetText(obj.Key)
			case 2:
				check.Hide()
				label.Show()
				label.SetText(fmt.Sprintf("%d kB", obj.Size/1024))
			case 3:
				check.Hide()
				label.Show()
				label.Truncation = fyne.TextTruncateClip
				label.SetText(obj.LastModified.Format("2006-01-02 15:04:05"))
			}
		},
	)

	objectList.SetColumnWidth(0, 40)
	objectList.SetColumnWidth(1, 400)
	objectList.SetColumnWidth(2, 100)
	objectList.SetColumnWidth(3, 210)
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
		case 1:
			b.SetText("Name")
			b.Icon = theme.MoveUpIcon()
		case 2:
			b.SetText("Size")
		case 3:
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
	refreshBtn.Icon = theme.ViewRefreshIcon()

	deleteBtn := widget.NewButton("Delete", func() {
		mw.handleDelete(ctx)
	})
	deleteBtn.Icon = theme.DeleteIcon()
	deleteBtn.Disable()
	mw.deleteBtn = deleteBtn

	uploadBtn := widget.NewButton("Upload", func() {
		mw.handleUpload(ctx)
	})
	uploadBtn.Icon = theme.UploadIcon()

	downloadBtn := widget.NewButton("Download", func() {
		mw.handleDownload(ctx)
	})
	downloadBtn.Icon = theme.DownloadIcon()
	downloadBtn.Disable()
	mw.downloadBtn = downloadBtn

	linkBtn := widget.NewButton("Link", func() {
		mw.handleLink(ctx)
	})
	linkBtn.Icon = theme.MailSendIcon()
	linkBtn.Disable()
	mw.linkBtn = linkBtn

	exitBtn := widget.NewButton("Exit", func() {
		mw.app.Quit()
	})
	exitBtn.Importance = widget.HighImportance
	exitBtn.Alignment = widget.ButtonAlignTrailing
	exitBtn.Icon = theme.CancelIcon()

	return container.NewHBox(refreshBtn, downloadBtn, deleteBtn, linkBtn, uploadBtn, exitBtn)
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

func (mw *MainWindow) updateSelect(idx int, selected bool) {
	if selected {
		if mw.selectedIndex == nil {
			mw.selectedIndex = make(map[int]bool)
		}

		mw.selectedIndex[idx] = selected

		mw.deleteBtn.Enable()
		mw.downloadBtn.Enable()
		mw.linkBtn.Enable()
	} else {
		delete(mw.selectedIndex, idx)
		if len(mw.selectedIndex) == 0 {
			mw.selectedIndex = nil
			mw.deleteBtn.Disable()
			mw.downloadBtn.Disable()
			mw.linkBtn.Disable()
		}
	}
}

func (mw *MainWindow) removeObject(key string) {
	for idx, obj := range mw.allObjects {
		if obj.Key == key {
			if idx == len(mw.allObjects)-1 {
				mw.allObjects = mw.allObjects[:idx]
				return
			}
			mw.allObjects = append(mw.allObjects[:idx], mw.allObjects[idx+1:]...)
			return
		}
	}
}

func (mw *MainWindow) updateObjectList() {
	mw.currentObjects = mw.filterObjects()
	mw.selectedIndex = nil
	mw.objectList.UnselectAll()
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

			mw.selectedIndex = nil
			mw.updateObjectList()
		}
		if err != nil {
			dialog.ShowError(err, mw.window)
			return
		}

		mw.selectedIndex = nil
		mw.updateObjectList()
	}()
}

func (mw *MainWindow) handleDelete(ctx context.Context) {
	if mw.selectedIndex == nil {
		dialog.ShowInformation("Info", "No object selected", mw.window)
		return
	}

	msg := fmt.Sprintf("Do you really want to delete '%d' files?", len(mw.selectedIndex))
	if len(mw.selectedIndex) == 1 {
		msg = "Do you really want to delete this file?"
	}

	confirm := dialog.NewConfirm(
		"Delete Objects",
		msg,
		func(yes bool) {
			if !yes {
				return
			}

			for idx := range mw.selectedIndex {
				obj := mw.currentObjects[idx]
				err := mw.s3svc.DeleteObject(ctx, obj.Key)
				if err != nil {
					dialog.ShowError(err, mw.window)
				} else {
					mw.removeObject(obj.Key)
				}
			}

			mw.updateObjectList()
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

func (mw *MainWindow) handleLink(ctx context.Context) {
	if mw.selectedIndex == nil {
		dialog.ShowInformation("Info", "No object selected", mw.window)
		return
	}

	for idx := range mw.selectedIndex {
		obj := mw.currentObjects[idx]
		linkurl, err := mw.s3svc.GetPresignedURL(ctx, obj.Key, 1*time.Hour)
		if err != nil {
			dialog.ShowError(err, mw.window)
			return
		}

		t := widget.NewEntry()
		t.SetText(linkurl.String())

		d := dialog.NewCustomWithoutButtons("Link to "+obj.Key, t, mw.window)
		d.SetButtons([]fyne.CanvasObject{widget.NewButton("Copy and Close", func() {
			mw.window.Clipboard().SetContent(linkurl.String())
			d.Hide()
		})})
		d.Show()
	}
}

func (mw *MainWindow) handleDownload(ctx context.Context) {
	if mw.selectedIndex == nil {
		dialog.ShowInformation("Info", "No object selected!", mw.window)
		return
	}

	folderSaveDialog := dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
		num := len(mw.selectedIndex)
		i := 0
		for idx := range mw.selectedIndex {
			i++
			mw.progressBar.Show()
			mw.progressBar.SetValue(float64(100*i/num) / 100)
			mw.progressBar.Refresh()
			obj := mw.currentObjects[idx]

			filePath := uri.Path() + "/" + obj.Key

			mw.itemsLabel.SetText(fmt.Sprintf("Downloading %s", obj.Key))

			s3obj, err := mw.s3svc.DownloadObject(ctx, obj.Key)
			if err != nil {
				dialog.ShowError(err, mw.window)
				continue
			}

			f, err := os.Create(filePath)
			if err != nil {
				dialog.ShowError(err, mw.window)
				s3obj.Close()
				continue
			}

			_, copyErr := io.Copy(f, s3obj)
			if copyErr != nil {
				dialog.ShowError(copyErr, mw.window)
			}

			f.Close()
			s3obj.Close()
		}

		mw.progressBar.Hide()
		mw.updateObjectList()

	}, mw.window)

	folderSaveDialog.Show()
}
