package windows

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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

const (
	batchSize         = 500                    // Smaller batches for faster initial response
	uiUpdateInterval  = 500 * time.Millisecond // Less frequent UI updates
	maxObjectsDefault = 50000                  // Default limit to prevent loading huge buckets entirely
)

type loadHandle struct {
	cancel context.CancelFunc
}

type FileManager struct {
	app                  fyne.App
	window               fyne.Window
	s3svc                *s3.Service
	Container            fyne.CanvasObject
	currentObjects       []minio.ObjectInfo
	allObjects           []minio.ObjectInfo
	selectedDirStructure []string
	selectedIndex        map[int]bool
	prefixes             map[string]bool
	basePrefix           string
	selectedPrefix       string
	searchTerm           string
	treeData             binding.StringTree
	searchDebounceTimer  *time.Timer
	context              context.Context
	loadHandle           *loadHandle
	maxObjects           int  // Maximum objects to load (0 = unlimited)
	hasMoreObjects       bool // True if load stopped due to limit
	changeConnectionFunc func()

	itemsLabel   *widget.Label
	objectList   *widget.Table
	searchInput  *widget.Entry
	prefixInput  *widget.Entry
	progressBar  *widget.ProgressBar
	loadingBar   *widget.ProgressBarInfinite
	stopBtn      *widget.Button
	deleteBtn    *widget.Button
	downloadBtn  *widget.Button
	linkBtn      *widget.Button
	tree         *widget.Tree
	loadMoreBtn  *widget.Button
	maxObjsInput *widget.Entry
}

func NewFileManager(a fyne.App, s3svc *s3.Service, window fyne.Window, changeConn func()) *FileManager {
	fm := &FileManager{
		app:                  a,
		window:               window,
		s3svc:                s3svc,
		maxObjects:           maxObjectsDefault,
		changeConnectionFunc: changeConn,
	}

	fm.setupUI()

	return fm
}

func (fm *FileManager) setupUI() {
	treeData := binding.NewStringTree()
	treeData.Append("", "all", "All (Flat view)")
	treeData.Append("", "root", "Root")
	fm.treeData = treeData

	fm.itemsLabel = fm.createItemsLabel()
	fm.objectList = fm.createObjectList()
	fm.searchInput = fm.createSearchInput()
	fm.prefixInput = fm.createPrefixInput()
	fm.progressBar = fm.createProgressBar()
	fm.loadingBar = fm.createLoadingBar()

	fm.stopBtn = fm.createStopButton()

	tree := fm.createDirTree(treeData)
	tree.Select("all")
	fm.tree = tree

	listContent := container.NewHSplit(tree, fm.objectList)
	listContent.SetOffset(0.2)

	bottomContainer := fm.createBottomContainer()
	btnBar := fm.createButtonBar()
	topContainer := fm.createTopContainer(btnBar)

	fm.Container = container.NewBorder(topContainer, container.NewPadded(bottomContainer), nil, nil, listContent)

	// Set up drag & drop
	fm.window.SetOnDropped(func(p fyne.Position, files []fyne.URI) {
		for _, file := range files {
			if file.Scheme() == "file" {
				f, err := os.Open(file.Path())
				if err != nil {
					dialog.ShowError(err, fm.window)
					continue
				}

				fm.uploadFile(f, file)
			}
		}
	})
}

func (fm *FileManager) createDirTree(data binding.DataTree) *widget.Tree {
	tree := widget.NewTreeWithData(data, func(b bool) fyne.CanvasObject {
		w := widget.NewLabel("Tree Item")
		w.Truncation = fyne.TextTruncateEllipsis
		return w
	}, func(i binding.DataItem, b bool, o fyne.CanvasObject) {
		o.(*widget.Label).Bind(i.(binding.String))
	})

	tree.OnSelected = func(id string) {
		fm.selectedPrefix = id
		fm.updateObjectList()
	}

	return tree
}

func (fm *FileManager) createItemsLabel() *widget.Label {
	label := widget.NewLabel("")
	label.Resize(fyne.NewSize(200, label.MinSize().Height))
	return label
}

func (fm *FileManager) updateItemsLabel() {
	filtered := len(fm.currentObjects)
	all := len(fm.allObjects)
	suffix := ""
	if fm.hasMoreObjects {
		suffix = fmt.Sprintf(" (limited to %d, use higher limit to see more)", fm.maxObjects)
	}
	if filtered != all {
		fm.itemsLabel.SetText(fmt.Sprintf("Items: %d of %d total%s", filtered, all, suffix))
		return
	}

	fm.itemsLabel.SetText(fmt.Sprintf("Total Items: %d%s", all, suffix))
}

func (fm *FileManager) createObjectList() *widget.Table {
	objectList := widget.NewTableWithHeaders(
		func() (int, int) {
			return len(fm.currentObjects), 4
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

			if id.Row >= len(fm.currentObjects) {
				return // Safeguard against index out of range
			}

			obj := fm.currentObjects[id.Row]

			switch id.Col {
			case 0:
				check.Show()
				check.Refresh()
				label.Hide()
				if fm.selectedIndex != nil {
					_, selected := fm.selectedIndex[id.Row]
					check.Checked = selected
				} else {
					check.Checked = false
				}

				check.OnChanged = func(checked bool) {
					if checked {
						fm.updateSelect(id.Row, true)
					} else {
						fm.updateSelect(id.Row, false)
					}
				}
			case 1:
				check.Hide()
				label.Show()
				label.Truncation = fyne.TextTruncateEllipsis
				label.SetText(obj.Key)
			case 2:
				check.Hide()
				label.Show()
				label.SetText(ByteCountSI(obj.Size))
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
		if fm.selectedIndex != nil {
			_, selected := fm.selectedIndex[id.Row]
			fm.updateSelect(id.Row, !selected)
			return
		}

		fm.updateSelect(id.Row, true)
	}

	return objectList
}

func (fm *FileManager) createSearchInput() *widget.Entry {
	searchInput := widget.NewEntry()
	searchInput.SetPlaceHolder("Search...")
	searchInput.Resize(fyne.NewSize(400, searchInput.MinSize().Height))
	searchInput.OnChanged = func(s string) {
		fm.searchTerm = s
		if fm.searchDebounceTimer != nil {
			fm.searchDebounceTimer.Stop()
		}
		fm.searchDebounceTimer = time.AfterFunc(300*time.Millisecond, fm.updateObjectList)
	}
	return searchInput
}

func (fm *FileManager) createPrefixInput() *widget.Entry {
	entry := widget.NewEntry()
	entry.SetPlaceHolder("Prefix (optional)")
	entry.Resize(fyne.NewSize(300, entry.MinSize().Height))
	entry.OnSubmitted = func(value string) {
		if fm.context == nil {
			return
		}
		fm.LoadObjects(fm.context, value)
	}
	return entry
}

func (fm *FileManager) createProgressBar() *widget.ProgressBar {
	progressBar := widget.NewProgressBar()
	progressBar.Hide()
	progressBar.Resize(fyne.NewSize(400, progressBar.MinSize().Height))
	return progressBar
}

func (fm *FileManager) createLoadingBar() *widget.ProgressBarInfinite {
	progressBar := widget.NewProgressBarInfinite()
	progressBar.Hide()
	progressBar.Resize(fyne.NewSize(100, progressBar.MinSize().Height))
	return progressBar
}

func (fm *FileManager) createStopButton() *widget.Button {
	stopBtn := widget.NewButton("Stop", func() {
		fm.cancelLoad()
	})
	stopBtn.Hide()
	return stopBtn
}

func (fm *FileManager) createBottomContainer() *fyne.Container {
	return container.NewHBox(
		fm.itemsLabel,
		layout.NewSpacer(),
		fm.stopBtn,
		container.NewGridWrap(fyne.NewSize(100, fm.progressBar.MinSize().Height), fm.loadingBar),
		container.NewGridWrap(fyne.NewSize(400, fm.progressBar.MinSize().Height), fm.progressBar),
	)
}

func (fm *FileManager) createButtonBar() *fyne.Container {
	refreshBtn := widget.NewButton("Refresh", func() {
		if fm.context == nil {
			return
		}
		fm.LoadObjects(fm.context, strings.TrimSpace(fm.prefixInput.Text))
	})
	refreshBtn.Icon = theme.ViewRefreshIcon()

	deleteBtn := widget.NewButton("Delete", func() {
		fm.handleDelete()
	})
	deleteBtn.Icon = theme.DeleteIcon()
	deleteBtn.Disable()
	fm.deleteBtn = deleteBtn

	uploadBtn := widget.NewButton("Upload", func() {
		fm.handleUpload()
	})
	uploadBtn.Icon = theme.UploadIcon()

	downloadBtn := widget.NewButton("Download", func() {
		fm.handleDownload()
	})
	downloadBtn.Icon = theme.DownloadIcon()
	downloadBtn.Disable()
	fm.downloadBtn = downloadBtn

	linkBtn := widget.NewButton("Link", func() {
		fm.handleLink()
	})
	linkBtn.Icon = theme.MailSendIcon()
	linkBtn.Disable()
	fm.linkBtn = linkBtn

	exitBtn := widget.NewButton("Exit", func() {
		fm.window.Close()
	})
	exitBtn.Importance = widget.HighImportance
	exitBtn.Alignment = widget.ButtonAlignTrailing
	exitBtn.Icon = theme.CancelIcon()

	changeConnBtn := widget.NewButton("Change Connection", func() {
		if fm.changeConnectionFunc != nil {
			fm.changeConnectionFunc()
		}
	})

	return container.NewHBox(refreshBtn, downloadBtn, deleteBtn, linkBtn, uploadBtn, layout.NewSpacer(), exitBtn, changeConnBtn)
}

func (fm *FileManager) createTopContainer(btnBar *fyne.Container) *fyne.Container {
	loadBtn := widget.NewButton("Load", func() {
		if fm.context == nil {
			return
		}
		fm.LoadObjects(fm.context, strings.TrimSpace(fm.prefixInput.Text))
	})

	// Max objects input
	fm.maxObjsInput = widget.NewEntry()
	fm.maxObjsInput.SetPlaceHolder(fmt.Sprintf("%d", maxObjectsDefault))
	fm.maxObjsInput.SetText(fmt.Sprintf("%d", maxObjectsDefault))
	fm.maxObjsInput.OnChanged = func(s string) {
		if s == "" {
			fm.maxObjects = 0 // unlimited
			return
		}
		if val, err := strconv.Atoi(s); err == nil && val >= 0 {
			fm.maxObjects = val
		}
	}

	// Load more button
	fm.loadMoreBtn = widget.NewButton("Load More", func() {
		if fm.context == nil || !fm.hasMoreObjects {
			return
		}
		fm.continueLoading(fm.context)
	})
	fm.loadMoreBtn.Hide()

	prefixRow := container.NewHBox(
		widget.NewLabel("Prefix:"),
		container.NewGridWrap(fyne.NewSize(250, fm.prefixInput.MinSize().Height), fm.prefixInput),
		loadBtn,
		widget.NewLabel("Max objects:"),
		container.NewGridWrap(fyne.NewSize(80, fm.maxObjsInput.MinSize().Height), fm.maxObjsInput),
		fm.loadMoreBtn,
		layout.NewSpacer(),
	)

	searchBar := container.New(layout.NewStackLayout(), fm.searchInput)
	return container.NewVBox(btnBar, container.NewPadded(prefixRow), container.NewPadded(searchBar))
}

func (fm *FileManager) updateTree() {
	if fm.treeData == nil {
		return
	}

	ids := make(map[string][]string)
	values := make(map[string]string)

	ids[""] = []string{"all", "root"}
	values["all"] = "All (Flat view)"
	values["root"] = "Root"

	rootChildren := make([]string, 0)

	for prefix := range fm.prefixes {
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

	if err := fm.treeData.Set(ids, values); err != nil {
		fmt.Printf("Error updating tree: %v\n", err)
	}
}

func (fm *FileManager) cancelLoad() {
	if fm.loadHandle != nil {
		fm.loadHandle.cancel()
	}
}

func (fm *FileManager) filterObjectsLocked() []minio.ObjectInfo {
	if fm.searchTerm == "" && (fm.selectedPrefix == "" || fm.selectedPrefix == "all") {
		return fm.allObjects
	}

	searchTermLower := strings.ToLower(fm.searchTerm)
	filteredObjects := make([]minio.ObjectInfo, 0, len(fm.allObjects)/2)

	for _, obj := range fm.allObjects {
		switch {
		case (fm.selectedPrefix == "" || fm.selectedPrefix == "all") && strings.Contains(strings.ToLower(obj.Key), searchTermLower):
			fallthrough
		case searchTermLower == "" && strings.HasPrefix(obj.Key, fm.selectedPrefix):
			fallthrough
		case strings.HasPrefix(obj.Key, fm.selectedPrefix) && strings.Contains(strings.ToLower(obj.Key), searchTermLower):
			filteredObjects = append(filteredObjects, obj)
		}
	}

	return filteredObjects
}

func (fm *FileManager) updateSelect(idx int, selected bool) {
	if selected {
		if fm.selectedIndex == nil {
			fm.selectedIndex = make(map[int]bool)
		}

		fm.selectedIndex[idx] = selected

		fm.deleteBtn.Enable()
		fm.downloadBtn.Enable()
		fm.linkBtn.Enable()
	} else {
		delete(fm.selectedIndex, idx)
		if len(fm.selectedIndex) == 0 {
			fm.selectedIndex = nil
			fm.deleteBtn.Disable()
			fm.downloadBtn.Disable()
			fm.linkBtn.Disable()
		}
	}
	fm.objectList.RefreshItem(widget.TableCellID{Row: idx, Col: 0})
	fm.objectList.Refresh()
}

func (fm *FileManager) removeObject(key string) {
	for idx, obj := range fm.allObjects {
		if obj.Key == key {
			if idx == len(fm.allObjects)-1 {
				fm.allObjects = fm.allObjects[:idx]
				return
			}
			fm.allObjects = append(fm.allObjects[:idx], fm.allObjects[idx+1:]...)
			return
		}
	}
}

func (fm *FileManager) updateObjectList() {
	fyne.Do(fm.updateObjectListLocked)
}

func (fm *FileManager) updateObjectListLocked() {
	fm.currentObjects = fm.filterObjectsLocked()
	fm.selectedIndex = nil
	fm.objectList.UnselectAll()
	fm.objectList.Refresh()
	fm.updateItemsLabel()
	fm.tree.Refresh()
}

func (fm *FileManager) LoadObjects(ctx context.Context, prefix string) {
	fm.context = ctx
	fm.cancelLoad()

	cleanPrefix := strings.TrimSpace(prefix)
	cleanPrefix = strings.TrimLeft(cleanPrefix, "/")
	fm.basePrefix = cleanPrefix

	loadCtx, cancel := context.WithCancel(ctx)
	handle := &loadHandle{cancel: cancel}
	fm.loadHandle = handle

	fyne.Do(func() {
		if fm.prefixInput != nil && fm.prefixInput.Text != cleanPrefix {
			fm.prefixInput.SetText(cleanPrefix)
		}
		fm.searchInput.SetText("")
		fm.stopBtn.Show()
		fm.loadingBar.Show()
		fm.loadingBar.Start()
		fm.progressBar.Hide()
		fm.loadMoreBtn.Hide()
		if cleanPrefix != "" {
			fm.itemsLabel.SetText(fmt.Sprintf("Loading objects under %q…", cleanPrefix))
		} else {
			fm.itemsLabel.SetText("Loading objects…")
		}
		fm.selectedIndex = nil
		fm.selectedPrefix = "all"
		fm.currentObjects = nil
		fm.allObjects = nil
		fm.prefixes = make(map[string]bool)
		fm.hasMoreObjects = false
		fm.updateTree()
		fm.objectList.UnselectAll()
		fm.objectList.Refresh()
		fm.tree.Refresh()
		if fm.tree != nil {
			fm.tree.Select("all")
		}
	})

	go fm.loadObjectsAsync(loadCtx, handle, "")
}

func (fm *FileManager) continueLoading(ctx context.Context) {
	if fm.loadHandle != nil {
		return // Already loading
	}

	loadCtx, cancel := context.WithCancel(ctx)
	handle := &loadHandle{cancel: cancel}
	fm.loadHandle = handle

	var lastKey string
	if len(fm.allObjects) > 0 {
		lastKey = fm.allObjects[len(fm.allObjects)-1].Key
	}

	fyne.Do(func() {
		fm.stopBtn.Show()
		fm.loadingBar.Show()
		fm.loadingBar.Start()
		fm.loadMoreBtn.Hide()
		fm.itemsLabel.SetText(fmt.Sprintf("Loading more objects (currently %d)…", len(fm.allObjects)))
	})

	go fm.loadObjectsAsync(loadCtx, handle, lastKey)
}

func (fm *FileManager) loadObjectsAsync(loadCtx context.Context, handle *loadHandle, startAfter string) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("Panic in loadObjectsAsync: %v\n", r)
		}
	}()

	defer loadCtx.Value(nil) // Ensure cancel is called via defer in parent

	lastKey := startAfter
	var lastStatus time.Time
	loadFailed := false
	startTime := time.Now()

	defer fyne.Do(func() {
		if fm.loadHandle != handle {
			return
		}
		fm.loadingBar.Stop()
		fm.loadingBar.Hide()
		fm.stopBtn.Hide()
		fm.progressBar.Hide()

		if fm.hasMoreObjects {
			fm.loadMoreBtn.Show()
		}

		switch {
		case loadCtx.Err() == context.Canceled:
			fm.itemsLabel.SetText(fmt.Sprintf("Load canceled (%d objects loaded)", len(fm.allObjects)))
			if fm.hasMoreObjects {
				fm.loadMoreBtn.Show()
			}
		case loadFailed:
			fm.itemsLabel.SetText("Failed to load objects")
		case len(fm.allObjects) > 0:
			elapsed := time.Since(startTime)
			suffix := ""
			if fm.hasMoreObjects {
				suffix = " (more available)"
			}
			fm.itemsLabel.SetText(fmt.Sprintf("Loaded %d objects in %.1fs%s", len(fm.allObjects), elapsed.Seconds(), suffix))
			fm.updateItemsLabel()
		default:
			fm.itemsLabel.SetText("No objects found")
		}
		fm.loadHandle = nil
	})

	firstBatch := true

	for {
		select {
		case <-loadCtx.Done():
			return
		default:
		}

		// Check if we've hit the limit
		if fm.maxObjects > 0 && len(fm.allObjects) >= fm.maxObjects {
			fm.hasMoreObjects = true
			fyne.Do(func() {
				if fm.loadHandle != handle {
					return
				}
				fm.updateTree()
				fm.updateObjectListLocked()
			})
			return
		}

		// Calculate remaining batch size
		currentBatchSize := batchSize
		if fm.maxObjects > 0 {
			remaining := fm.maxObjects - len(fm.allObjects)
			if remaining < batchSize {
				currentBatchSize = remaining
			}
		}

		batch, err := fm.s3svc.ListObjectsBatch(loadCtx, lastKey, fm.basePrefix, currentBatchSize)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			loadFailed = true
			fyne.Do(func() {
				dialog.ShowError(err, fm.window)
			})
			return
		}

		if len(batch) == 0 {
			fyne.Do(func() {
				if fm.loadHandle != handle {
					return
				}
				fm.updateTree()
				fm.updateObjectListLocked()
			})
			return
		}

		batchCopy := make([]minio.ObjectInfo, len(batch))
		copy(batchCopy, batch)

		batchPrefixes := make(map[string]struct{})
		for i := range batch {
			if idx := strings.LastIndex(batch[i].Key, "/"); idx != -1 {
				batchPrefixes[batch[i].Key[:idx]] = struct{}{}
			}
		}

		// For first batch, update immediately; otherwise use interval
		shouldUpdate := firstBatch || lastStatus.IsZero() ||
			time.Since(lastStatus) >= uiUpdateInterval ||
			len(batch) < currentBatchSize

		fyne.Do(func() {
			if fm.loadHandle != handle {
				return
			}
			fm.allObjects = append(fm.allObjects, batchCopy...)
			for p := range batchPrefixes {
				fm.prefixes[p] = true
			}
			if shouldUpdate {
				fm.updateTree()
				fm.updateObjectListLocked()
				fm.itemsLabel.SetText(fmt.Sprintf("Loaded %d objects…", len(fm.allObjects)))
			}
		})

		if shouldUpdate {
			lastStatus = time.Now()
		}

		firstBatch = false
		lastKey = batch[len(batch)-1].Key

		// Check if we got fewer than requested (end of listing)
		if len(batch) < currentBatchSize {
			fm.hasMoreObjects = false
			fyne.Do(func() {
				if fm.loadHandle != handle {
					return
				}
				fm.updateTree()
				fm.updateObjectListLocked()
			})
			return
		}
	}
}

func (fm *FileManager) handleDelete() {
	if fm.selectedIndex == nil {
		dialog.ShowInformation("Info", "No object selected", fm.window)
		return
	}

	msg := fmt.Sprintf("Do you really want to delete '%d' files?", len(fm.selectedIndex))
	if len(fm.selectedIndex) == 1 {
		msg = "Do you really want to delete this file?"
	}

	confirm := dialog.NewConfirm(
		"Delete Objects",
		msg,
		func(yes bool) {
			if !yes {
				return
			}

			for idx := range fm.selectedIndex {
				obj := fm.currentObjects[idx]
				err := fm.s3svc.DeleteObject(fm.context, obj.Key)
				if err != nil {
					dialog.ShowError(err, fm.window)
				} else {
					fm.removeObject(obj.Key)
				}
				fm.updateSelect(idx, false)
			}

			fm.updateObjectList()
		}, fm.window)
	confirm.Show()
}

func (fm *FileManager) handleUpload() {
	fd := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
		if err != nil || reader == nil {
			return
		}

		fm.uploadFile(reader, reader.URI())
	}, fm.window)

	fd.Show()
}

func (fm *FileManager) uploadFile(reader io.ReadCloser, path fyne.URI) {
	fullname := path.Name()
	if fm.selectedPrefix != "root" && fm.selectedPrefix != "all" {
		fullname = fm.selectedPrefix + "/" + fullname
	}

	// Get file size for progress calculation
	fileInfo, err := os.Stat(path.Path())
	if err != nil {
		dialog.ShowError(err, fm.window)
		return
	}
	totalSize := fileInfo.Size()

	bufreader := bufio.NewReader(reader)
	detectBytes, err := bufreader.Peek(1024)
	if err != nil && err != io.EOF {
		fmt.Println("Error reading file: ", err)
		return
	}

	mt := mimetype.Detect(detectBytes)

	// Show progress bar before starting upload
	fm.progressBar.Show()
	fm.progressBar.SetValue(0)
	fm.itemsLabel.SetText(fmt.Sprintf("Uploading %s", fullname))

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
					fm.progressBar.Show()
					fm.progressBar.SetValue(progress)
					fm.itemsLabel.SetText(fmt.Sprintf("Uploading %s: %.1f%%", fullname, progress*100))
				})
			},
		}

		err = fm.s3svc.UploadObjectReader(fm.context, path.Path(), fullname, pr, totalSize, mt.String())
		if err != nil {
			fmt.Println("Error uploading file: ", err)
			dialog.ShowError(err, fm.window)
			return
		}

		// Update UI when upload is complete
		fyne.Do(func() {
			fm.progressBar.Hide()
			fm.itemsLabel.SetText("")
			dialog.ShowInformation("OK", fmt.Sprintf("Uploaded file %s!", fullname), fm.window)
			fm.LoadObjects(fm.context, fm.basePrefix)
		})
	}()
}

func (fm *FileManager) handleLink() {
	if fm.selectedIndex == nil {
		dialog.ShowInformation("Info", "No object selected", fm.window)
		return
	}

	for idx := range fm.selectedIndex {
		obj := fm.currentObjects[idx]
		linkurl, err := fm.s3svc.GetPresignedURL(fm.context, obj.Key, 1*time.Hour)
		if err != nil {
			dialog.ShowError(err, fm.window)
			return
		}

		t := widget.NewEntry()
		t.SetText(linkurl.String())

		d := dialog.NewCustomWithoutButtons("Link to "+obj.Key, t, fm.window)
		d.SetButtons([]fyne.CanvasObject{widget.NewButton("Copy and Close", func() {
			fm.window.Clipboard().SetContent(linkurl.String())
			d.Hide()
		})})
		d.Show()
	}
}

func (fm *FileManager) handleDownload() {
	if fm.selectedIndex == nil {
		dialog.ShowInformation("Info", "No object selected!", fm.window)
		return
	}

	folderSaveDialog := dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
		if err != nil || uri == nil {
			return
		}

		num := len(fm.selectedIndex)
		i := 0
		for idx := range fm.selectedIndex {
			i++
			fm.progressBar.Show()
			fm.progressBar.SetValue(float64(100*i/num) / 100)
			fm.progressBar.Refresh()
			obj := fm.currentObjects[idx]

			filePath := uri.Path() + "/" + filepath.Base(obj.Key)

			fm.itemsLabel.SetText(fmt.Sprintf("Downloading %s", obj.Key))

			s3obj, err := fm.s3svc.DownloadObject(fm.context, obj.Key)
			if err != nil {
				dialog.ShowError(err, fm.window)
				continue
			}

			f, err := os.Create(filePath)
			if err != nil {
				dialog.ShowError(err, fm.window)
				s3obj.Close()
				continue
			}

			_, copyErr := io.Copy(f, s3obj)
			if copyErr != nil {
				dialog.ShowError(copyErr, fm.window)
			}

			f.Close()
			s3obj.Close()
		}

		fm.progressBar.Hide()
		fm.updateObjectList()

	}, fm.window)

	folderSaveDialog.Show()
}
