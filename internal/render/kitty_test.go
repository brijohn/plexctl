package render

import (
	"bytes"
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/ygelfand/plexctl/internal/term"
)

func TestFitRect(t *testing.T) {
	tests := []struct {
		name                         string
		imgW, imgH, boxCols, boxRows int
		cellW, cellH                 int
		wantCols, wantRows           int
	}{
		{"poster 2:3 constrained", 400, 600, 15, 10, 8, 16, 13, 10},
		{"poster 2:3 unconstrained", 400, 600, 25, 0, 8, 16, 25, 19},
		{"wide 16:9 fits", 1920, 1080, 15, 10, 8, 16, 15, 4},
		{"square", 500, 500, 10, 10, 8, 16, 10, 5},
		{"tall clamps to one col min", 10, 1000, 15, 10, 8, 16, 1, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cols, rows := fitRect(tt.imgW, tt.imgH, tt.boxCols, tt.boxRows, tt.cellW, tt.cellH)
			if cols != tt.wantCols || rows != tt.wantRows {
				t.Errorf("fitRect() = %dx%d, want %dx%d", cols, rows, tt.wantCols, tt.wantRows)
			}
		})
	}
}

func TestPlaceholderBlockDimensions(t *testing.T) {
	block := placeholderBlock(0xABCDEF, 5, 3, 15, 10)
	lines := strings.Split(block, "\n")
	if len(lines) != 10 {
		t.Fatalf("got %d lines, want 10", len(lines))
	}
	for i, line := range lines {
		if w := lipgloss.Width(line); w != 15 {
			t.Errorf("line %d measures %d cells, want 15", i, w)
		}
	}
	if !strings.Contains(block, "\x1b[38;2;171;205;239m") {
		t.Errorf("block does not encode image ID 0xABCDEF in foreground color")
	}
	if !strings.Contains(block, string(rune(0x10EEEE))) {
		t.Errorf("block contains no placeholder runes")
	}
}

func TestPlaceholderBlockUnconstrainedHeight(t *testing.T) {
	block := placeholderBlock(1000, 25, 19, 25, 0)
	lines := strings.Split(block, "\n")
	if len(lines) != 19 {
		t.Fatalf("got %d lines, want 19", len(lines))
	}
	for i, line := range lines {
		if w := lipgloss.Width(line); w != 25 {
			t.Errorf("line %d measures %d cells, want 25", i, w)
		}
	}
}

func testImage(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 128, A: 255})
		}
	}
	return img
}

func TestKittyRendererTransmitsOnce(t *testing.T) {
	var buf bytes.Buffer
	r := newKittyRenderer(term.NewSyncWriter(&buf)).(*kittyRenderer)
	key := ItemKey{Key: "rk1", Width: 15}

	out, err := r.Render(testImage(40, 60), key, 15, 10)
	if err != nil {
		t.Fatal(err)
	}
	if out == "" {
		t.Fatal("empty placeholder block")
	}

	seq := buf.String()
	if !strings.HasPrefix(seq, "\x1b_G") {
		t.Errorf("transmission does not start with APC: %q", seq[:min(len(seq), 10)])
	}
	for _, want := range []string{"U=1", "q=2", "f=100", "a=T", "i=1000"} {
		if !strings.Contains(seq, want) {
			t.Errorf("transmission missing %s", want)
		}
	}

	// The payload (base64 PNG after the ';') must actually be present:
	// EncodeGraphics silently encodes nothing unless Transmission is Direct.
	semi := strings.Index(seq, ";")
	if semi < 0 {
		t.Fatal("transmission carries no payload separator")
	}
	payload := seq[semi+1:]
	if end := strings.Index(payload, "\x1b"); end >= 0 {
		payload = payload[:end]
	}
	if len(payload) < 100 {
		t.Errorf("transmission payload is only %d bytes, image data missing", len(payload))
	}
	if !strings.HasPrefix(payload, "iVBOR") {
		t.Errorf("payload does not start with base64 PNG magic")
	}

	buf.Reset()
	if _, err := r.Render(testImage(40, 60), key, 15, 10); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Errorf("second Render retransmitted %d bytes, want 0", buf.Len())
	}
}

func TestKittyRendererResetSessionRetransmits(t *testing.T) {
	var buf bytes.Buffer
	r := newKittyRenderer(term.NewSyncWriter(&buf)).(*kittyRenderer)

	if _, err := r.Render(testImage(40, 60), ItemKey{Key: "a", Width: 15}, 15, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Render(testImage(40, 60), ItemKey{Key: "b", Width: 15}, 15, 10); err != nil {
		t.Fatal(err)
	}

	buf.Reset()
	r.ResetSession()
	seq := buf.String()
	if !strings.Contains(seq, "d=A") {
		t.Errorf("ResetSession did not delete all images")
	}
	if got := strings.Count(seq, "i=1000"); got == 0 {
		t.Errorf("ResetSession did not retransmit image 1000")
	}
	if got := strings.Count(seq, "i=1001"); got == 0 {
		t.Errorf("ResetSession did not retransmit image 1001")
	}
}

func TestKittyRendererShutdownClears(t *testing.T) {
	var buf bytes.Buffer
	r := newKittyRenderer(term.NewSyncWriter(&buf)).(*kittyRenderer)

	if _, err := r.Render(testImage(40, 60), ItemKey{Key: "a", Width: 15}, 15, 10); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	r.Shutdown()
	if !strings.Contains(buf.String(), "d=A") {
		t.Errorf("Shutdown did not delete all images")
	}
	if len(r.entries) != 0 {
		t.Errorf("Shutdown left %d registry entries", len(r.entries))
	}
}
