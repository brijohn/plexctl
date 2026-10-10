package render

import (
	"image"

	gopixels "github.com/saran13raj/go-pixels"
)

// halfcellRenderer draws images as true-color Unicode half-blocks (▀▄).
// Works on any true-color terminal; output strings are plain ANSI and cacheable.
type halfcellRenderer struct{}

func (halfcellRenderer) Name() string    { return "halfcell" }
func (halfcellRenderer) CacheView() bool { return true }

func (halfcellRenderer) Render(img image.Image, _ ItemKey, widthCells, _ int) (string, error) {
	return gopixels.FromImageStream(img, widthCells, 0, "halfcell", true)
}

func (halfcellRenderer) ResetSession() {}
func (halfcellRenderer) Shutdown()     {}
