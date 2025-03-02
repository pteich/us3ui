package windows

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/gabriel-vasile/mimetype"
	"github.com/minio/minio-go/v7"

	"github.com/pteich/us3ui/s3"
)

const batchSize = 150

type MainWindow struct {
	app                  fyne.App
	window               fyne.Window
	s3svc                *s3.Service
	currentObjects       []minio.ObjectInfo
	allObjects           []minio.ObjectInfo
	selectedDirStructure []string
	selectedIndex        map[int]bool
	prefixes             map[string]bool
	selectedPrefix       string
	searchTerm           string
	treeData             binding.StringTree
	searchDebounceTimer  *time.Timer

	itemsLabel  *widget.Label
	objectList  *widget.Table
	searchInput *widget.Entry
	progressBar *widget.ProgressBar
	stopBtn     *widget.Button
	deleteBtn   *widget.Button
	downloadBtn *widget.Button
	linkBtn     *widget.Button
	tree        *widget.Tree
}

func NewMainWindow(a fyne.App, s3svc *s3.Service) *MainWindow {
	window := a.NewWindow("Universal s3 UI")
	window.CenterOnScreen()
	window.SetMaster()

	mw := &MainWindow{
		app:    a,
		s3svc:  s3svc,
		window: window,
	}

	return mw
}

func (mw *MainWindow) Show(ctx context.Context) {
	mw.window.SetOnDropped(func(p fyne.Position, files []fyne.URI) {
		for _, file := range files {
			if file.Scheme() == "file" {
				f, err := os.Open(file.Path())
				if err != nil {
					dialog.ShowError(err, mw.window)
					continue
				}

				mw.uploadFile(ctx, f, file)
			}
		}
	})

	mw.setupGUI(ctx)
	mw.loadObjects(ctx)

	mw.window.Show()
}

func (mw *MainWindow) setupGUI(ctx context.Context) {
	treeData := binding.NewStringTree()
	treeData.Append("", "all", "All (Flat view)")
	treeData.Append("", "root", "Root")
	mw.treeData = treeData

	mw.itemsLabel = mw.createItemsLabel()
	mw.objectList = mw.createObjectList()
	mw.searchInput = mw.createSearchInput()
	mw.progressBar = mw.createProgressBar()
	mw.stopBtn = mw.createStopButton()

	tree := mw.createDirTree(treeData)
	tree.Select("all")
	mw.tree = tree

	listContent := container.NewHSplit(tree, mw.objectList)
	listContent.SetOffset(0.2)

	bottomContainer := mw.createBottomContainer()
	btnBar := mw.createButtonBar(ctx)
	topContainer := mw.createTopContainer(btnBar)

	content := container.NewBorder(topContainer, container.NewPadded(bottomContainer), nil, nil, listContent)
	mw.window.SetContent(content)
	mw.window.Resize(fyne.NewSize(950, 500))
}

func (mw *MainWindow) createDirTree(data binding.DataTree) *widget.Tree {
	tree := widget.NewTreeWithData(data, func(b bool) fyne.CanvasObject {
		w := widget.NewLabel("Tree Item")
		w.Truncation = fyne.TextTruncateEllipsis
		return w
	}, func(i binding.DataItem, b bool, o fyne.CanvasObject) {
		o.(*widget.Label).Bind(i.(binding.String))
	})

	tree.OnSelected = func(id string) {
		mw.selectedPrefix = id
		mw.updateObjectList()
	}

	return tree
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
				if mw.selectedIndex != nil {
					_, selected := mw.selectedIndex[id.Row]
					check.Checked = selected
				} else {
					check.Checked = false
				}

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

	objectList.SetColumnWidth(0, 35)
	objectList.SetColumnWidth(1, 370)
	objectList.SetColumnWidth(2, 90)
	objectList.SetColumnWidth(3, 200)
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

	objectList.OnSelected = func(id widget.TableCellID) {
		if mw.selectedIndex != nil {
			_, selected := mw.selectedIndex[id.Row]
			mw.updateSelect(id.Row, !selected)
			return
		}

		mw.updateSelect(id.Row, true)
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

func (mw *MainWindow) updateTree() {
	if mw.treeData == nil {
		return
	}

	ids := make(map[string][]string)
	values := make(map[string]string)

	ids[""] = []string{"all", "root"}
	values["all"] = "All (Flat view)"
	values["root"] = "Root"

	rootChildren := make([]string, 0)

	for prefix := range mw.prefixes {
		parts := strings.Split(prefix, "/")
		currentPath := ""

		for i, part := range parts {
			parentPath := currentPath
			if i > 0 {
				currentPath += "/"
			}
			currentPath += part

			if i == 0 {
				if !contains(rootChildren, currentPath) {
					rootChildren = append(rootChildren, currentPath)
				}
				values[currentPath] = part
				continue
			}

			if _, exists := ids[parentPath]; !exists {
				ids[parentPath] = make([]string, 0)
			}
			if !contains(ids[parentPath], currentPath) {
				ids[parentPath] = append(ids[parentPath], currentPath)
				values[currentPath] = part
			}
		}
	}

	ids["root"] = rootChildren

	if err := mw.treeData.Set(ids, values); err != nil {
		fmt.Printf("Error updating tree: %v\n", err)
	}
}

func contains(slice []string, str string) bool {
	for _, v := range slice {
		if v == str {
			return true
		}
	}
	return false
}

func (mw *MainWindow) filterObjects() []minio.ObjectInfo {
	if mw.searchTerm == "" && (mw.selectedPrefix == "" || mw.selectedPrefix == "all") {
		return mw.allObjects
	}

	searchTermLower := strings.ToLower(mw.searchTerm)
	filteredObjects := make([]minio.ObjectInfo, 0, len(mw.allObjects)/2)

	for _, obj := range mw.allObjects {
		switch {
		case (mw.selectedPrefix == "" || mw.selectedPrefix == "all") && strings.Contains(strings.ToLower(obj.Key), searchTermLower):
			fallthrough
		case searchTermLower == "" && strings.HasPrefix(obj.Key, mw.selectedPrefix):
			fallthrough
		case strings.HasPrefix(obj.Key, mw.selectedPrefix) && strings.Contains(strings.ToLower(obj.Key), searchTermLower):
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
	mw.objectList.RefreshItem(widget.TableCellID{Row: idx, Col: 0})
	mw.objectList.Refresh()
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
	mw.updateTree()
	fyne.Do(func() {
		mw.objectList.UnselectAll()
		mw.objectList.Refresh()
		mw.updateItemsLabel()
		mw.tree.Refresh()
	})
}

func (mw *MainWindow) loadObjects(ctx context.Context) {
	mw.progressBar.Show()
	mw.stopBtn.Show()
	mw.progressBar.SetValue(0)
	mw.searchInput.SetText("")

	go func() {
		defer func() {
			fyne.Do(func() {
				mw.progressBar.Hide()
				mw.stopBtn.Hide()
			})
		}()

		var err error
		mw.allObjects = []minio.ObjectInfo{}
		mw.prefixes = make(map[string]bool)
		var lastKey string

		for {
			batch, err := mw.s3svc.ListObjectsBatch(ctx, lastKey, batchSize)
			if err != nil {
				dialog.ShowError(err, mw.window)
				return
			}

			mw.allObjects = append(mw.allObjects, batch...)

			fyne.Do(func() {
				progress := float64(len(mw.allObjects)) / float64(len(mw.allObjects)+len(batch))
				mw.progressBar.SetValue(progress)
			})

			if len(batch) > 0 {
				lastKey = batch[len(batch)-1].Key
			}

			prefix := ""
			for i := range batch {
				pos := strings.LastIndex(batch[i].Key, "/")
				if pos == -1 {
					continue
				}

				prefix = batch[i].Key[:pos]
				_, found := mw.prefixes[prefix]
				if !found {
					mw.prefixes[prefix] = true
				}
			}

			mw.selectedIndex = nil
			mw.updateObjectList()

			if len(batch) < batchSize {
				break
			}
		}
		if err != nil {
			dialog.ShowError(err, mw.window)
			return
		}
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
				mw.updateSelect(idx, false)
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

		mw.uploadFile(ctx, reader, reader.URI())
	}, mw.window)

	fd.Show()
}

func (mw *MainWindow) uploadFile(ctx context.Context, reader io.ReadCloser, path fyne.URI) {
	fullname := path.Name()
	if mw.selectedPrefix != "root" && mw.selectedPrefix != "all" {
		fullname = mw.selectedPrefix + "/" + fullname
	}

	// Get file size for progress calculation
	fileInfo, err := os.Stat(path.Path())
	if err != nil {
		dialog.ShowError(err, mw.window)
		return
	}
	totalSize := fileInfo.Size()

	bufreader := bufio.NewReader(reader)
	detectBytes, err := bufreader.Peek(1024)
	if err != nil {
		fmt.Println("Error reading file: ", err)
		return
	}

	mt := mimetype.Detect(detectBytes)

	// Show progress bar before starting upload
	mw.progressBar.Show()
	mw.progressBar.SetValue(0)
	mw.itemsLabel.SetText(fmt.Sprintf("Uploading %s", fullname))

	// Perform upload in a separate goroutine
	go func() {
		defer reader.Close()

		// Create a Reader that tracks progress
		pr := &ProgressReader{
			Reader: bufreader,
			Total:  totalSize,
			OnProgress: func(bytesRead int64) {
				progress := float64(bytesRead) / float64(totalSize)
				fyne.Do(func() {
					mw.progressBar.Show()
					mw.progressBar.SetValue(progress)
					mw.itemsLabel.SetText(fmt.Sprintf("Uploading %s: %.1f%%", fullname, progress*100))
				})
			},
		}

		err = mw.s3svc.UploadObjectReader(ctx, fullname, pr, totalSize, mt.String())
		if err != nil {
			dialog.ShowError(err, mw.window)
			return
		}

		// Update UI when upload is complete
		fyne.Do(func() {
			mw.progressBar.Hide()
			mw.itemsLabel.SetText("")
			dialog.ShowInformation("OK", fmt.Sprintf("Uploaded file %s!", fullname), mw.window)
			mw.loadObjects(ctx)
		})
	}()
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
