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

func ShowMainWindow(ctx context.Context, a fyne.App, s3svc *s3.Service) {
	w := a.NewWindow("Universal s3 UI")
	w.CenterOnScreen()
	w.SetMaster()

	var currentObjects []minio.ObjectInfo
	selectedIndex := -1

	itemsLabel := widget.NewLabel("")
	itemsLabel.Resize(fyne.NewSize(200, itemsLabel.MinSize().Height))
	updateItemsLabel := func() {
		itemsLabel.SetText(fmt.Sprintf("Total Items: %d", len(currentObjects)))
	}

	objectList := widget.NewTableWithHeaders(
		func() (int, int) {
			return len(currentObjects), 3
		},
		func() fyne.CanvasObject {
			return widget.NewLabel("Objects")
		},
		func(id widget.TableCellID, co fyne.CanvasObject) {
			label := co.(*widget.Label)
			obj := currentObjects[id.Row]

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
		selectedIndex = id.Row

		if id.Col > 1 {
			id.Col = 0
			objectList.Select(id)
		}

	}
	objectList.OnUnselected = func(id widget.TableCellID) {
		if selectedIndex == id.Row {
			selectedIndex = -1
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

	var allObjects []minio.ObjectInfo
	var filteredObjects []minio.ObjectInfo
	var searchTerm string
	var searchDebounceTimer *time.Timer

	updateObjectList := func() {
		currentObjects = filteredObjects
		objectList.Refresh()
		updateItemsLabel()
	}

	filterObjects := func() {
		if searchTerm == "" {
			filteredObjects = allObjects
		} else {
			filteredObjects = []minio.ObjectInfo{}
			for _, obj := range allObjects {
				if strings.Contains(strings.ToLower(obj.Key), strings.ToLower(searchTerm)) {
					filteredObjects = append(filteredObjects, obj)
				}
			}
		}
		updateObjectList()
	}

	searchInput := widget.NewEntry()
	searchInput.SetPlaceHolder("Search...")
	searchInput.Resize(fyne.NewSize(400, searchInput.MinSize().Height))
	searchInput.OnChanged = func(s string) {
		searchTerm = s
		if searchDebounceTimer != nil {
			searchDebounceTimer.Stop()
		}
		searchDebounceTimer = time.AfterFunc(300*time.Millisecond, filterObjects)
	}

	searchBar := container.New(layout.NewStackLayout(), searchInput)

	progressBar := widget.NewProgressBar()
	progressBar.Hide()
	progressBar.Resize(fyne.NewSize(400, progressBar.MinSize().Height))

	stopBtn := widget.NewButton("Stop", func() {
		// This will be implemented later
	})
	stopBtn.Hide()

	loadObjects := func() {
		progressBar.Show()
		stopBtn.Show()
		progressBar.SetValue(0)

		go func() {
			defer progressBar.Hide()
			defer stopBtn.Hide()

			batchSize := 100
			var err error
			allObjects = []minio.ObjectInfo{}
			var lastKey string

			for {
				batch, err := s3svc.ListObjectsBatch(ctx, lastKey, batchSize)
				if err != nil {
					dialog.ShowError(err, w)
					return
				}

				allObjects = append(allObjects, batch...)
				filterObjects() // Apply current search filter

				progress := float64(len(allObjects)) / float64(len(allObjects)+len(batch))
				progressBar.SetValue(progress)

				if len(batch) < batchSize {
					break
				}

				if len(batch) > 0 {
					lastKey = batch[len(batch)-1].Key
				}
			}

			if err != nil {
				dialog.ShowError(err, w)
				return
			}

			selectedIndex = -1
			filterObjects() // Final filter application
		}()
	}

	// Buttons
	refreshBtn := widget.NewButton("Refresh", func() {
		loadObjects()
	})

	deleteBtn := widget.NewButton("Delete", func() {
		if selectedIndex < 0 || selectedIndex >= len(currentObjects) {
			dialog.ShowInformation("Info", "No object selected", w)
			return
		}
		obj := currentObjects[selectedIndex]
		confirm := dialog.NewConfirm(
			"Delete Object?",
			fmt.Sprintf("Do you really want to delete '%s'?", obj.Key),
			func(yes bool) {
				if yes {
					err := s3svc.DeleteObject(ctx, obj.Key)
					if err != nil {
						dialog.ShowError(err, w)
					} else {
						loadObjects()
					}
				}
			}, w)
		confirm.Show()
	})

	uploadBtn := widget.NewButton("Upload", func() {
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

			err = s3svc.UploadObject(reader.URI().Name(), fileData)
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			dialog.ShowInformation("OK", "Upload finished!", w)
			loadObjects()
		}, w)
		fd.Show()
	})

	downloadBtn := widget.NewButton("Download", func() {
		if selectedIndex < 0 || selectedIndex >= len(currentObjects) {
			dialog.ShowInformation("Info", "No object selected!", w)
			return
		}
		obj := currentObjects[selectedIndex]

		fileSaveDialog := dialog.NewFileSave(func(fc fyne.URIWriteCloser, err error) {
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			if fc == nil {
				return
			}
			defer fc.Close()

			s3obj, err := s3svc.DownloadObject(ctx, obj.Key)
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			defer s3obj.Close()

			_, copyErr := io.Copy(fc, s3obj)
			if copyErr != nil {
				dialog.ShowError(copyErr, w)
				return
			}

			dialog.ShowInformation("Download", "Download finished!", w)
		}, w)

		fileSaveDialog.SetFileName(strings.ReplaceAll(obj.Key, "/", "_"))
		fileSaveDialog.Show()
	})

	exitBtn := widget.NewButton("Exit", func() {
		a.Quit()
	})
	exitBtn.Importance = widget.HighImportance
	exitBtn.Alignment = widget.ButtonAlignTrailing

	bottomContainer := container.NewHBox(
		itemsLabel,
		layout.NewSpacer(),
		stopBtn,
		container.NewGridWrap(fyne.NewSize(400, progressBar.MinSize().Height), progressBar),
	)

	btnBar := container.NewHBox(refreshBtn, downloadBtn, deleteBtn, uploadBtn, exitBtn)
	topContainer := container.NewVBox(
		btnBar,
		container.NewPadded(searchBar),
	)

	content := container.NewBorder(topContainer, container.NewPadded(bottomContainer), nil, nil, objectList)

	w.SetContent(content)
	w.Resize(fyne.NewSize(800, 400))

	loadObjects()

	w.Show()
}
