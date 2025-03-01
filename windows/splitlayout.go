package windows

import (
	"fyne.io/fyne/v2"
)

type splitLayout struct {
	padding float32
}

func (c *splitLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) != 2 {
		return
	}

	availableWidth := size.Width - c.padding

	objects[0].Resize(fyne.NewSize(availableWidth*0.7, size.Height))
	objects[0].Move(fyne.NewPos(0, 0))
	objects[1].Resize(fyne.NewSize(availableWidth*0.3, size.Height))
	objects[1].Move(fyne.NewPos(availableWidth*0.7+c.padding, 0))
}

func (c *splitLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
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
