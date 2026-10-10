package render

import (
	"bytes"
	"fmt"
	"image"
	"io"
	"math"
	"strings"
	"sync"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"
	"github.com/ygelfand/plexctl/internal/term"
	"golang.org/x/image/draw"
)

const (
	// Fallback cell pixel size when the terminal doesn't report one (1:2 aspect).
	defaultCellWidthPx  = 8
	defaultCellHeightPx = 16

	// Transmitted images kept per session; oldest are deleted beyond this.
	maxEntries = 2048

	// Image IDs start above the low range mpv's kitty vo uses for its frames.
	firstImageID = 1000
)

/*
kittyRenderer draws images with the kitty graphics protocol using Unicode
placeholders: each image is transmitted once with a virtual placement
(U=1), and Render returns a grid of U+10EEEE placeholder cells whose
foreground color carries the image ID. Placeholder cells are ordinary
width-1 grapheme clusters, so the strings survive lipgloss layout,
viewport clipping, and line-based overlay compositing.
*/
type kittyRenderer struct {
	out *term.SyncWriter

	mu      sync.Mutex
	entries map[ItemKey]*kittyEntry
	order   []ItemKey // insertion order for eviction
	nextID  uint32
}

type kittyEntry struct {
	id         uint32
	seq        []byte // complete chunked transmission escape sequence
	cols, rows int
}

func newKittyRenderer(out *term.SyncWriter) Renderer {
	return &kittyRenderer{
		out:     out,
		entries: make(map[ItemKey]*kittyEntry),
		nextID:  firstImageID,
	}
}

func (r *kittyRenderer) Name() string    { return "kitty" }
func (r *kittyRenderer) CacheView() bool { return false }

func (r *kittyRenderer) Render(img image.Image, key ItemKey, widthCells, heightCells int) (string, error) {
	cellW, cellH := term.Caps().CellWidthPx, term.Caps().CellHeightPx
	if cellW <= 0 || cellH <= 0 {
		cellW, cellH = defaultCellWidthPx, defaultCellHeightPx
	}

	bounds := img.Bounds()
	cols, rows := fitRect(bounds.Dx(), bounds.Dy(), widthCells, heightCells, cellW, cellH)

	id, err := r.ensureTransmitted(img, key, cols, rows, cellW, cellH)
	if err != nil {
		return "", err
	}

	return placeholderBlock(id, cols, rows, widthCells, heightCells), nil
}

// ensureTransmitted transmits the image with a virtual placement unless an
// identical transmission already happened this session, and returns its ID.
func (r *kittyRenderer) ensureTransmitted(img image.Image, key ItemKey, cols, rows, cellW, cellH int) (uint32, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if e, ok := r.entries[key]; ok {
		if e.cols == cols && e.rows == rows {
			return e.id, nil
		}
		r.deleteLocked(key, e)
	}

	scaled := image.NewRGBA(image.Rect(0, 0, cols*cellW, rows*cellH))
	draw.ApproxBiLinear.Scale(scaled, scaled.Bounds(), img, img.Bounds(), draw.Over, nil)

	id := r.nextID
	r.nextID++

	var seq bytes.Buffer
	// Transmission must be set explicitly: EncodeGraphics only encodes the
	// image payload when it is kitty.Direct, not on the zero value.
	err := kitty.EncodeGraphics(&seq, scaled, &kitty.Options{
		Action:           kitty.TransmitAndPut,
		Format:           kitty.PNG,
		Transmission:     kitty.Direct,
		ID:               int(id),
		Quite:            2,
		VirtualPlacement: true,
		Columns:          cols,
		Rows:             rows,
		Chunk:            true,
	})
	if err != nil {
		return 0, err
	}

	e := &kittyEntry{id: id, seq: seq.Bytes(), cols: cols, rows: rows}
	if err := r.write(e.seq); err != nil {
		return 0, err
	}

	r.entries[key] = e
	r.order = append(r.order, key)
	for len(r.order) > maxEntries {
		oldest := r.order[0]
		if old, ok := r.entries[oldest]; ok {
			r.deleteLocked(oldest, old)
		} else {
			r.order = r.order[1:]
		}
	}
	return id, nil
}

// deleteLocked removes one image from the terminal and the registry.
func (r *kittyRenderer) deleteLocked(key ItemKey, e *kittyEntry) {
	opts := kitty.Options{Action: kitty.Delete, Delete: kitty.DeleteID, DeleteResources: true, ID: int(e.id), Quite: 2}
	_ = r.write([]byte(ansi.KittyGraphics(nil, opts.Options()...)))
	delete(r.entries, key)
	for i, k := range r.order {
		if k == key {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
}

// ResetSession deletes all terminal images and retransmits every registry
// entry under its original ID, so previously rendered placeholder strings
// display again. Used after mpv's kitty vo has wiped the terminal's images.
func (r *kittyRenderer) ResetSession() {
	r.mu.Lock()
	defer r.mu.Unlock()

	_ = r.out.Locked(func(w io.Writer) error {
		if _, err := io.WriteString(w, deleteAllSeq()); err != nil {
			return err
		}
		for _, key := range r.order {
			if e, ok := r.entries[key]; ok {
				if _, err := w.Write(e.seq); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// Shutdown deletes all terminal images and clears the registry.
func (r *kittyRenderer) Shutdown() {
	r.mu.Lock()
	defer r.mu.Unlock()

	_ = r.write([]byte(deleteAllSeq()))
	r.entries = make(map[ItemKey]*kittyEntry)
	r.order = nil
}

func (r *kittyRenderer) write(seq []byte) error {
	return r.out.Locked(func(w io.Writer) error {
		_, err := w.Write(seq)
		return err
	})
}

func deleteAllSeq() string {
	opts := kitty.Options{Action: kitty.Delete, Delete: kitty.DeleteAll, DeleteResources: true, Quite: 2}
	return ansi.KittyGraphics(nil, opts.Options()...)
}

/*
fitRect computes the placement grid (in cells) that fits the image into a
boxCols x boxRows cell box while preserving aspect ratio. The terminal
stretches the image to the placement grid, so the grid shape carries the
aspect. boxRows <= 0 leaves the height unconstrained.
*/
func fitRect(imgW, imgH, boxCols, boxRows, cellW, cellH int) (cols, rows int) {
	if imgW <= 0 || imgH <= 0 || boxCols <= 0 {
		return 1, 1
	}
	cols = boxCols
	rows = int(math.Round(float64(imgH) / float64(imgW) * float64(cols) * float64(cellW) / float64(cellH)))
	if rows < 1 {
		rows = 1
	}
	if boxRows > 0 && rows > boxRows {
		rows = boxRows
		cols = int(math.Round(float64(imgW) / float64(imgH) * float64(rows) * float64(cellH) / float64(cellW)))
		if cols < 1 {
			cols = 1
		}
		if cols > boxCols {
			cols = boxCols
		}
	}
	return cols, rows
}

/*
placeholderBlock builds the placeholder cell grid for an image: rows lines of
cols cells, each cell U+10EEEE plus row/column diacritics, with the 24-bit
image ID encoded in the foreground color. The block is padded with spaces to
exactly boxCols wide and, when boxRows > 0, centered to exactly boxRows lines.
*/
func placeholderBlock(id uint32, cols, rows, boxCols, boxRows int) string {
	fg := fmt.Sprintf("\x1b[38;2;%d;%d;%dm", (id>>16)&0xFF, (id>>8)&0xFF, id&0xFF)
	leftPad := strings.Repeat(" ", max((boxCols-cols)/2, 0))
	rightPad := strings.Repeat(" ", max(boxCols-cols-len(leftPad), 0))
	blank := strings.Repeat(" ", boxCols)

	total := rows
	topPad := 0
	if boxRows > rows {
		total = boxRows
		topPad = (boxRows - rows) / 2
	}

	lines := make([]string, 0, total)
	for i := 0; i < topPad; i++ {
		lines = append(lines, blank)
	}
	var sb strings.Builder
	for row := 0; row < rows; row++ {
		sb.Reset()
		sb.WriteString(leftPad)
		sb.WriteString(fg)
		rowMark := kitty.Diacritic(row)
		for col := 0; col < cols; col++ {
			sb.WriteRune(kitty.Placeholder)
			sb.WriteRune(rowMark)
			sb.WriteRune(kitty.Diacritic(col))
		}
		sb.WriteString("\x1b[39m")
		sb.WriteString(rightPad)
		lines = append(lines, sb.String())
	}
	for len(lines) < total {
		lines = append(lines, blank)
	}
	return strings.Join(lines, "\n")
}
