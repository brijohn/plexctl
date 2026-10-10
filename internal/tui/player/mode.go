package player

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"sync"

	"github.com/ygelfand/plexctl/internal/config"
	"github.com/ygelfand/plexctl/internal/term"
	"github.com/ygelfand/plexctl/internal/ui"
)

/*
ResolveOutput maps a configured video output to a concrete one.
wantTerminal marks an explicit terminal-video request (ctrl+p): "auto" and
"window" then resolve to the best terminal protocol. Without it, "auto"
resolves to a normal mpv window.
*/
func ResolveOutput(requested config.VideoOutput, wantTerminal bool) config.VideoOutput {
	switch requested {
	case config.VideoOutputTct, config.VideoOutputKitty:
		return requested
	case config.VideoOutputWindow:
		if !wantTerminal {
			return config.VideoOutputWindow
		}
	}
	if !wantTerminal {
		return config.VideoOutputWindow
	}
	if term.Caps().KittyGraphics && MpvSupportsKitty() {
		return config.VideoOutputKitty
	}
	return config.VideoOutputTct
}

var (
	mpvVerOnce sync.Once
	mpvVerOut  []byte
	mpvVersRe  = regexp.MustCompile(`mpv\s+v?(\d+)\.(\d+)`)
)

// mpvVersionOutput runs `mpv --version` once and caches its output.
// Nil when mpv isn't runnable.
func mpvVersionOutput() []byte {
	mpvVerOnce.Do(func() {
		out, err := exec.Command("mpv", "--version").Output()
		if err == nil {
			mpvVerOut = out
		}
	})
	return mpvVerOut
}

// MpvSupportsKitty reports whether the installed mpv has --vo=kitty (>= 0.36).
func MpvSupportsKitty() bool {
	out := mpvVersionOutput()
	return out != nil && mpvVersionAtLeast(out, 0, 36)
}

// mpvVersionAtLeast parses `mpv --version` output; unparseable versions (git
// builds) are assumed modern, with spawn-time fallback covering the rest.
func mpvVersionAtLeast(out []byte, major, minor int) bool {
	m := mpvVersRe.FindSubmatch(out)
	if m == nil {
		return true
	}
	maj, _ := strconv.Atoi(string(m[1]))
	mnr, _ := strconv.Atoi(string(m[2]))
	return maj > major || (maj == major && mnr >= minor)
}

/*
visualizerGraph returns the mpv lavfi-complex filter graph that renders the
configured music visualizer as the video output, or "" when disabled. The
graph splits the audio into the audio output and a visualization video feed.
*/
func visualizerGraph() string {
	const size = "1024x576"
	var filter string
	switch config.Get().MusicVisualizer {
	case config.VisualizerNone:
		return ""
	case config.VisualizerSpectrum:
		filter = fmt.Sprintf("showspectrum=s=%s:slide=scroll:color=intensity", size)
	case config.VisualizerWaves:
		filter = fmt.Sprintf("showwaves=s=%s:mode=line:rate=30", size)
	case config.VisualizerScope:
		filter = fmt.Sprintf("avectorscope=s=%s:rate=30:draw=line", size)
	default: // showcqt
		filter = fmt.Sprintf("showcqt=s=%s:fps=30", size)
	}
	return fmt.Sprintf("[aid1]asplit[ao][vis];[vis]%s[vo]", filter)
}

/*
terminalVoArgs builds the mpv vo arguments for a terminal video output.
overlay=true confines the video to a region over the running TUI; otherwise
mpv owns the whole terminal. Adding a future terminal vo (e.g. sixel) means
one more entry here plus a config.VideoOutput value.
*/
var terminalVoArgs = map[config.VideoOutput]func(overlay bool) []string{
	config.VideoOutputTct: func(overlay bool) []string {
		args := []string{"-vo", "tct", "--really-quiet"}
		// --vo-tct-buffering exists since mpv 0.38; an unknown option is a
		// fatal mpv startup error.
		if out := mpvVersionOutput(); out != nil && mpvVersionAtLeast(out, 0, 38) {
			args = append(args, "--vo-tct-buffering=frame")
		}
		if overlay {
			args = append(args, "--vo-tct-height=40")
		}
		return args
	},
	// Shared-memory transfer is unreliable in older mpv (silent audio-only
	// playback when the terminal can't open the object), so frames go over
	// the pty by default.
	config.VideoOutputKitty: func(overlay bool) []string {
		args := []string{"-vo", "kitty", "--really-quiet"}
		if overlay {
			layout := ui.GetLayout()
			cols := max(layout.MainAreaContentWidth(), 20)
			rows := max(layout.ContentHeight()-4, 5)
			args = append(args,
				"--vo-kitty-alt-screen=no",
				"--vo-kitty-config-clear=no",
				fmt.Sprintf("--vo-kitty-left=%d", ui.SidebarTotalWidth+2),
				"--vo-kitty-top=2",
				fmt.Sprintf("--vo-kitty-cols=%d", cols),
				fmt.Sprintf("--vo-kitty-rows=%d", rows),
			)
		}
		return args
	},
}
