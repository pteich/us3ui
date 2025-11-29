package windows

import (
	"context"
	"fmt"
	"sort"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/minio/minio-go/v7"

	"github.com/pteich/us3ui/s3"
)

type BucketManager struct {
	app          fyne.App
	parentWindow fyne.Window
	s3Service    *s3.Service
	window       fyne.Window

	// UI elements
	bucketList *widget.List
	buckets    []minio.BucketInfo
	selectedID int
	onSelect   func(string)
}

func NewBucketManager(a fyne.App, parent fyne.Window, service *s3.Service, onSelect func(string)) *BucketManager {
	bm := &BucketManager{
		app:          a,
		parentWindow: parent,
		s3Service:    service,
		selectedID:   -1,
		onSelect:     onSelect,
	}
	return bm
}

func (bm *BucketManager) Show() {
	bm.window = bm.app.NewWindow("Manage Buckets")
	bm.window.Resize(fyne.NewSize(600, 400))

	bm.bucketList = widget.NewList(
		func() int { return len(bm.buckets) },
		func() fyne.CanvasObject {
			return container.NewHBox(
				widget.NewIcon(theme.FolderIcon()),
				widget.NewLabel("Template"),
			)
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			box := o.(*fyne.Container)
			label := box.Objects[1].(*widget.Label)
			label.SetText(bm.buckets[i].Name)
		},
	)

	deleteAction := widget.NewToolbarAction(theme.ContentRemoveIcon(), bm.deleteSelectedBucket)
	deleteAction.Disable()

	selectAction := widget.NewToolbarAction(theme.ConfirmIcon(), func() {
		if bm.selectedID >= 0 && bm.selectedID < len(bm.buckets) {
			if bm.onSelect != nil {
				bm.onSelect(bm.buckets[bm.selectedID].Name)
				bm.window.Close()
			}
		}
	})
	selectAction.Disable()

	bm.bucketList.OnSelected = func(id widget.ListItemID) {
		bm.selectedID = id
		deleteAction.Enable()
		selectAction.Enable()
	}

	bm.bucketList.OnUnselected = func(id widget.ListItemID) {
		bm.selectedID = -1
		deleteAction.Disable()
		selectAction.Disable()
	}

	toolbar := widget.NewToolbar(
		widget.NewToolbarAction(theme.ContentAddIcon(), bm.showAddBucketDialog),
		deleteAction,
		widget.NewToolbarSpacer(),
		selectAction,
		widget.NewToolbarAction(theme.ViewRefreshIcon(), bm.refreshBuckets),
	)

	content := container.NewBorder(toolbar, nil, nil, nil, bm.bucketList)
	bm.window.SetContent(content)

	bm.refreshBuckets()
	bm.window.Show()
}

func (bm *BucketManager) refreshBuckets() {
	go func() {
		buckets, err := bm.s3Service.ListBuckets(context.Background())
		if err != nil {
			fyne.Do(func() {
				dialog.ShowError(err, bm.window)
			})
			return
		}

		sort.Slice(buckets, func(i, j int) bool {
			return buckets[i].Name < buckets[j].Name
		})

		fyne.Do(func() {
			bm.buckets = buckets
			bm.bucketList.Refresh()
		})
	}()
}

func (bm *BucketManager) showAddBucketDialog() {
	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("Bucket Name")

	regionEntry := widget.NewEntry()
	regionEntry.SetPlaceHolder("Region (optional)")

	items := []*widget.FormItem{
		{Text: "Name", Widget: nameEntry},
		{Text: "Region", Widget: regionEntry},
	}

	d := dialog.NewForm("Create New Bucket", "Create", "Cancel", items, func(confirm bool) {
		if confirm {
			if nameEntry.Text == "" {
				dialog.ShowError(fmt.Errorf("bucket name cannot be empty"), bm.window)
				return
			}

			go func() {
				err := bm.s3Service.CreateBucket(context.Background(), nameEntry.Text, regionEntry.Text)
				if err != nil {
					fyne.Do(func() {
						dialog.ShowError(err, bm.window)
					})
					return
				}
				bm.refreshBuckets()
			}()
		}
	}, bm.window)

	d.Resize(fyne.NewSize(400, 250))
	d.Show()
}

func (bm *BucketManager) deleteSelectedBucket() {
	if bm.selectedID < 0 || bm.selectedID >= len(bm.buckets) {
		return
	}

	bucketName := bm.buckets[bm.selectedID].Name

	dialog.ShowConfirm("Delete Bucket", fmt.Sprintf("Are you sure you want to delete bucket '%s'?\nThis action cannot be undone.", bucketName), func(confirm bool) {
		if confirm {
			go func() {
				err := bm.s3Service.DeleteBucket(context.Background(), bucketName)
				if err != nil {
					fyne.Do(func() {
						dialog.ShowError(err, bm.window)
					})
					return
				}
				bm.refreshBuckets()
			}()
		}
	}, bm.window)
}
