package main

import (
	"fmt"
	"io"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/minio/minio-go/v7"

	"github.com/pteich/us3ui/s3"
)

type S3Config struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	UseSSL    bool
}

func main() {
	a := app.New()

	configWin := a.NewWindow("s3 Server Config")

	endpointEntry := widget.NewEntry()
	endpointEntry.SetText("play.min.io")
	accessKeyEntry := widget.NewEntry()
	secretKeyEntry := widget.NewEntry()
	bucketEntry := widget.NewEntry()
	bucketEntry.SetText("bucket")
	sslCheck := widget.NewCheck("Use SSL (HTTPS)", nil)
	sslCheck.SetChecked(true)

	configForm := &widget.Form{
		Items: []*widget.FormItem{
			{Text: "Endpoint", Widget: endpointEntry},
			{Text: "Access Key", Widget: accessKeyEntry},
			{Text: "Secret Key", Widget: secretKeyEntry},
			{Text: "Bucket Name", Widget: bucketEntry},
		},
		OnSubmit: func() {
			cfg := S3Config{
				Endpoint:  endpointEntry.Text,
				AccessKey: accessKeyEntry.Text,
				SecretKey: secretKeyEntry.Text,
				Bucket:    bucketEntry.Text,
				UseSSL:    sslCheck.Checked,
			}
			s3svc, err := s3.New(cfg.Endpoint, cfg.AccessKey, cfg.SecretKey, cfg.Bucket, cfg.UseSSL)
			if err != nil {
				dialog.ShowError(err, configWin)
				return
			}

			configWin.Hide()
			showMainWindow(a, s3svc)
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

func showMainWindow(a fyne.App, s3svc *s3.Service) {
	w := a.NewWindow("Universal s3 UI")

	var currentObjects []minio.ObjectInfo
	selectedIndex := -1

	objectList := widget.NewList(
		func() int {
			return len(currentObjects)
		},
		func() fyne.CanvasObject {
			return widget.NewLabel("Object Key")
		},
		func(i widget.ListItemID, co fyne.CanvasObject) {
			label := co.(*widget.Label)
			obj := currentObjects[i]
			label.SetText(fmt.Sprintf("%s (%d bytes)", obj.Key, obj.Size))
		},
	)
	objectList.OnSelected = func(id widget.ListItemID) {
		selectedIndex = id
	}
	objectList.OnUnselected = func(id widget.ListItemID) {
		if selectedIndex == id {
			selectedIndex = -1
		}
	}

	loadObjects := func() {
		objects, err := s3svc.ListObjects()
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		currentObjects = objects
		selectedIndex = -1
		objectList.Refresh()
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
					err := s3svc.DeleteObject(obj.Key)
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

			// Datei in []byte lesen
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

			// Hochladen
			err = s3svc.UploadObject(reader.URI().Name(), fileData)
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			dialog.ShowInformation("OK", "File uploaded!", w)
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

			s3obj, err := s3svc.DownloadObject(obj.Key)
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

	btnBar := container.NewHBox(refreshBtn, downloadBtn, deleteBtn, uploadBtn, exitBtn)
	content := container.NewBorder(btnBar, nil, nil, nil, objectList)

	w.SetContent(content)
	w.Resize(fyne.NewSize(600, 400))

	loadObjects()

	w.Show()
}
