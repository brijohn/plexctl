package player

import (
	"github.com/LukeHagar/plexgo/models/operations"
	"github.com/ygelfand/plexctl/internal/config"
)

type PlayerStatus struct {
	Time       float64
	Duration   float64
	Title      string
	File       string
	Key        string
	State      operations.State
	NoReport   bool
	Output     config.VideoOutput
	Foreground bool // mpv owns the terminal (suspend/CLI), not drawing over the TUI
}

type ExternalSubtitle struct {
	URL      string
	Title    string
	Language string
}

type (
	PlayerStatusMsg       struct{}
	PlayerStatusMsgPoller struct{}
)

func (ps *PlayerStatus) Paused() bool { return ps.State == operations.StatePaused }

func (ps *PlayerStatus) Active() bool {
	return ps.State != "" && ps.State != operations.StateStopped
}

func (ps *PlayerStatus) Play() {
	ps.State = operations.StatePlaying
}

func (ps *PlayerStatus) Pause() {
	if ps.State == operations.StatePlaying {
		ps.State = operations.StatePaused
	}
}
