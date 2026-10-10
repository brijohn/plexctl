package player

import (
	"testing"

	"github.com/ygelfand/plexctl/internal/config"
)

// With no kitty capabilities detected (the zero value in tests), terminal
// requests resolve to tct and window requests to window.
func TestResolveOutput(t *testing.T) {
	tests := []struct {
		requested    config.VideoOutput
		wantTerminal bool
		want         config.VideoOutput
	}{
		{config.VideoOutputAuto, false, config.VideoOutputWindow},
		{config.VideoOutputAuto, true, config.VideoOutputTct},
		{config.VideoOutputWindow, false, config.VideoOutputWindow},
		{config.VideoOutputWindow, true, config.VideoOutputTct},
		{config.VideoOutputTct, false, config.VideoOutputTct},
		{config.VideoOutputTct, true, config.VideoOutputTct},
		{config.VideoOutputKitty, false, config.VideoOutputKitty},
		{config.VideoOutputKitty, true, config.VideoOutputKitty},
		{"", false, config.VideoOutputWindow},
		{"", true, config.VideoOutputTct},
	}
	for _, tt := range tests {
		if got := ResolveOutput(tt.requested, tt.wantTerminal); got != tt.want {
			t.Errorf("ResolveOutput(%q, %v) = %q, want %q", tt.requested, tt.wantTerminal, got, tt.want)
		}
	}
}

func TestMpvVersionAtLeast(t *testing.T) {
	tests := []struct {
		version      string
		major, minor int
		ok           bool
	}{
		{"mpv 0.37.0 Copyright", 0, 36, true},
		{"mpv v0.36.0", 0, 36, true},
		{"mpv 0.35.1", 0, 36, false},
		{"mpv 0.37.0", 0, 38, false},
		{"mpv 0.38.0", 0, 38, true},
		{"mpv v0.41.0 Copyright", 0, 38, true},
		{"mpv 1.0.0", 0, 38, true},
		{"mpv git-abcdef", 0, 38, true},
	}
	for _, tt := range tests {
		if got := mpvVersionAtLeast([]byte(tt.version), tt.major, tt.minor); got != tt.ok {
			t.Errorf("version %q >= %d.%d = %v, want %v", tt.version, tt.major, tt.minor, got, tt.ok)
		}
	}
}
