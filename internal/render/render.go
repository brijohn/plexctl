package render

import (
	"image"

	"github.com/ygelfand/plexctl/internal/config"
	"github.com/ygelfand/plexctl/internal/term"
)

// ItemKey identifies a rendered image for session tracking and cache naming.
type ItemKey struct {
	Key   string // stable identity: rating key, or thumb path when absent
	Width int    // target width in cells
}

/*
Renderer converts an image into a terminal-displayable string. Implementations
cover one terminal graphics protocol each; new protocols (sixel, iTerm2, ...)
plug in by adding a factory to the registry below.
*/
type Renderer interface {
	// Name is a stable identifier ("halfcell", "kitty") used to namespace
	// rendered-string cache keys and to match the poster_renderer config value.
	Name() string
	// CacheView reports whether Render output may be persisted to disk.
	// Protocols whose output references session state must return false.
	CacheView() bool
	// Render returns a string of at most widthCells columns per line.
	// heightCells <= 0 derives the height from the image aspect ratio;
	// heightCells > 0 letterboxes into exactly widthCells x heightCells.
	// It may write protocol data (e.g. kitty transmissions) out of band.
	Render(img image.Image, key ItemKey, widthCells, heightCells int) (string, error)
	// ResetSession re-asserts protocol state after another process wrote to
	// the terminal (e.g. mpv's kitty vo wipes all terminal images).
	ResetSession()
	// Shutdown releases protocol state on exit.
	Shutdown()
}

type factory struct {
	name      string
	supported func() bool
	build     func(out *term.SyncWriter) Renderer
}

// Ordered by preference for auto-selection; halfcell is the universal fallback.
var factories = []factory{
	{
		name:      "kitty",
		supported: func() bool { return term.Caps().KittyPlaceholders },
		build:     func(out *term.SyncWriter) Renderer { return newKittyRenderer(out) },
	},
	{
		name:      "halfcell",
		supported: func() bool { return true },
		build:     func(*term.SyncWriter) Renderer { return halfcellRenderer{} },
	},
}

var (
	active  Renderer = halfcellRenderer{}
	lastOut *term.SyncWriter
)

// Init selects the active renderer: an explicit mode forces that renderer,
// otherwise the first supported one wins. Call once at startup, after
// term.Detect and before any poster rendering.
func Init(mode config.PosterRenderer, out *term.SyncWriter) {
	lastOut = out
	for _, f := range factories {
		if string(mode) == f.name {
			active = f.build(out)
			return
		}
	}
	for _, f := range factories {
		if f.supported() {
			active = f.build(out)
			return
		}
	}
}

// ReInit re-selects the renderer after a config change, keeping the writer.
// A change that resolves to the already-active protocol is a no-op.
func ReInit(mode config.PosterRenderer) {
	if selectedName(mode) == active.Name() {
		return
	}
	prev := active
	Init(mode, lastOut)
	prev.Shutdown()
}

// selectedName returns the renderer name that Init would pick for mode.
func selectedName(mode config.PosterRenderer) string {
	for _, f := range factories {
		if string(mode) == f.name {
			return f.name
		}
	}
	for _, f := range factories {
		if f.supported() {
			return f.name
		}
	}
	return active.Name()
}

// Active returns the selected renderer (halfcell until Init runs).
func Active() Renderer {
	return active
}
