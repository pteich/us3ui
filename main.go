package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"github.com/minio/minio-go/v7"

	"github.com/pteich/us3ui/config"
	"github.com/pteich/us3ui/s3"
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

	configWin := a.NewWindow("s3 Server Config")
	configWin.CenterOnScreen()

	endpointEntry := widget.NewEntry()
	endpointEntry.SetText(cfg.Endpoint)
	accessKeyEntry := widget.NewEntry()
	accessKeyEntry.SetText(cfg.AccessKey)
	secretKeyEntry := widget.NewEntry()
	secretKeyEntry.SetText(cfg.SecretKey)
	secretKeyEntry.Password = true
	bucketEntry := widget.NewEntry()
	bucketEntry.SetText(cfg.Bucket)
	prefixEntry := widget.NewEntry()
	prefixEntry.SetPlaceHolder("Optional Prefix")
	regionEntry := widget.NewEntry()
	regionEntry.SetPlaceHolder("Optional Region")
	sslCheck := widget.NewCheck("Use SSL (HTTPS)", nil)
	sslCheck.SetChecked(cfg.UseSSL)

	configForm := &widget.Form{
		Items: []*widget.FormItem{
			{Text: "Endpoint", Widget: endpointEntry},
			{Text: "Access Key", Widget: accessKeyEntry},
			{Text: "Secret Key", Widget: secretKeyEntry},
			{Text: "Bucket Name", Widget: bucketEntry},
			{Text: "Region", Widget: regionEntry},
			{Text: "Prefix", Widget: prefixEntry},
		},
		OnSubmit: func() {
			cfg.Endpoint = endpointEntry.Text
			cfg.AccessKey = accessKeyEntry.Text
			cfg.SecretKey = secretKeyEntry.Text
			cfg.Bucket = bucketEntry.Text
			cfg.UseSSL = sslCheck.Checked
			cfg.Prefix = prefixEntry.Text
			cfg.Region = regionEntry.Text

			s3svc, err := s3.New(cfg)
			if err != nil {
				dialog.ShowError(err, configWin)
				return
			}

			configWin.Hide()
			showMainWindow(ctx, a, s3svc)
		},
		OnCancel: func() {
			a.Quit()
		},
	}
	configForm.SubmitText = "Connect"
	configForm.CancelText = "Abort"

	configForm.AppendItem(&widget.FormItem{Text: "", Widget: sslCheck})

	configWin.SetContent(configForm)
	configWin.Resize(fyne.NewSize(400, 280))
	configWin.ShowAndRun()
}

func showMainWindow(ctx context.Context, a fyne.App, s3svc *s3.Service) {
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
			case 1:
				label.SetText(fmt.Sprintf("%d kB", obj.Size/1024))
			case 2:
				label.SetText(obj.LastModified.String())
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
	objectList.SetColumnWidth(2, 100)
	objectList.SetColumnWidth(3, 300)
	objectList.ShowHeaderColumn = false
	objectList.UpdateHeader = func(id widget.TableCellID, template fyne.CanvasObject) {
		label := template.(*widget.Label)
		switch id.Col {
		case 0:
			label.SetText("Name")
		case 1:
			label.SetText("Size")
		case 2:
			label.SetText("Last Modified")
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
