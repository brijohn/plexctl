package term

import (
	"os"
	"strings"
	"sync"
	"time"

	xterm "golang.org/x/term"
)

/*
Capabilities describes terminal graphics support, detected once at startup
before the Bubble Tea program takes over stdin/stdout.
*/
type Capabilities struct {
	KittyGraphics     bool // kitty graphics protocol (sufficient for mpv --vo=kitty)
	KittyPlaceholders bool // Unicode placeholder rendering (needed for inline TUI images)
	CellWidthPx       int  // terminal cell width in pixels, 0 if unknown
	CellHeightPx      int  // terminal cell height in pixels, 0 if unknown
}

var (
	capsOnce sync.Once
	caps     Capabilities
)

// Detect probes the terminal for kitty graphics support. It must run before
// any TUI output starts; results are cached for the process lifetime.
func Detect() Capabilities {
	capsOnce.Do(func() {
		caps = detect()
	})
	return caps
}

// Caps returns the detected capabilities (zero value if Detect never ran).
func Caps() Capabilities {
	return caps
}

func detect() Capabilities {
	c := Capabilities{}

	// Multiplexers don't pass the graphics protocol through reliably.
	termEnv := os.Getenv("TERM")
	if os.Getenv("TMUX") != "" || strings.HasPrefix(termEnv, "screen") || strings.HasPrefix(termEnv, "tmux") {
		return c
	}

	if !xterm.IsTerminal(int(os.Stdin.Fd())) || !xterm.IsTerminal(int(os.Stdout.Fd())) {
		return c
	}

	c.KittyGraphics = queryKittyGraphics(400 * time.Millisecond)
	if c.KittyGraphics {
		// WezTerm answers the graphics query but does not implement Unicode
		// placeholders, so placeholders are gated on known-good terminals.
		c.KittyPlaceholders = envSuggestsPlaceholders(termEnv)
		c.CellWidthPx, c.CellHeightPx = cellSize()
	}
	return c
}

func envSuggestsPlaceholders(termEnv string) bool {
	if os.Getenv("KITTY_WINDOW_ID") != "" {
		return true
	}
	if strings.Contains(termEnv, "kitty") || strings.Contains(termEnv, "ghostty") {
		return true
	}
	return strings.EqualFold(os.Getenv("TERM_PROGRAM"), "ghostty")
}
