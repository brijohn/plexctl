package player

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ygelfand/plexctl/internal/config"
)

// SuspendPlayMsg asks the TUI controller to suspend the program and run mpv
// in the foreground with full terminal ownership.
type SuspendPlayMsg struct {
	Request     *PlayRequest
	Output      config.VideoOutput
	StartOffset int64
}

// SuspendFinishedMsg reports a finished foreground mpv run back to the TUI.
type SuspendFinishedMsg struct {
	Request SuspendPlayMsg
	Err     error
	Elapsed time.Duration
}

// VideoFallbackMsg reports that playback fell back to another video output.
type VideoFallbackMsg struct {
	From, To config.VideoOutput
}

/*
foregroundArgs builds the argv for a foreground mpv run: mpv plays one file
with its own keyboard and OSD, while the IPC socket stays available for
progress reporting.
*/
func foregroundArgs(req *PlayRequest, output config.VideoOutput, startOffset int64, socketPath string) []string {
	args := []string{
		req.URL,
		"--no-resume-playback",
		fmt.Sprintf("--input-ipc-server=%s", socketPath),
		fmt.Sprintf("--start=%.3f", float64(startOffset)/1000.0),
		"--force-media-title=" + req.Title,
	}
	args = append(args, terminalVoArgs[output](false)...)
	for _, sub := range req.Subtitles {
		args = append(args, "--sub-file="+sub.URL)
	}
	return args
}

// BuildSuspendCmd tears down any detached mpv instance (freeing the IPC
// socket) and builds the foreground mpv command for tea.ExecProcess.
func (pm *PlayerManager) BuildSuspendCmd(msg SuspendPlayMsg) (*exec.Cmd, error) {
	if err := pm.ensureMpv(); err != nil {
		return nil, err
	}
	if pm.conn != nil {
		pm.conn.Call("quit")
		pm.cleanup()
	} else if runtime.GOOS != "windows" {
		os.Remove(pm.socketPath)
	}

	args := foregroundArgs(msg.Request, msg.Output, msg.StartOffset, pm.socketPath)
	slog.Debug("PlayerManager: building suspend mpv command", "args", args)
	cmd := exec.Command("mpv", args...)
	// Wire the real tty explicitly; tea.ExecProcess would otherwise hand mpv
	// the program's wrapped output writer, a pipe on which terminal size
	// detection and the terminal vo fail.
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd, nil
}

// AttachSuspend sets player state and connects to the foreground mpv's IPC
// socket so progress keeps reporting to Plex while the TUI is suspended.
func (pm *PlayerManager) AttachSuspend(msg SuspendPlayMsg) tea.Cmd {
	return func() tea.Msg {
		pm.status = PlayerStatus{
			Title:      msg.Request.Title,
			File:       msg.Request.URL,
			Key:        msg.Request.RatingKey,
			NoReport:   msg.Request.NoReport,
			Output:     msg.Output,
			Foreground: true,
			Time:       float64(msg.StartOffset) / 1000.0,
		}
		pm.status.Play()

		if err := pm.connect(10 * time.Second); err != nil {
			slog.Warn("PlayerManager: no IPC for suspended mpv, progress will not report", "error", err)
			return nil
		}
		pm.reportProgressWithKey()
		return nil
	}
}

/*
RunForeground plays a media item with mpv owning the terminal, blocking until
mpv exits. Used by the CLI for terminal video outputs; progress reporting runs
over IPC in the background.
*/
func (pm *PlayerManager) RunForeground(ctx context.Context, req *PlayRequest, output config.VideoOutput, startOffset int64) error {
	if err := pm.ensureMpv(); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		os.Remove(pm.socketPath)
	}

	args := foregroundArgs(req, output, startOffset, pm.socketPath)
	slog.Debug("PlayerManager: running foreground mpv", "args", args)
	cmd := exec.CommandContext(ctx, "mpv", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	pm.status = PlayerStatus{
		Title:      req.Title,
		File:       req.URL,
		Key:        req.RatingKey,
		NoReport:   req.NoReport,
		Output:     output,
		Foreground: true,
		Time:       float64(startOffset) / 1000.0,
	}
	pm.status.Play()

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start mpv: %w", err)
	}

	go func() {
		if err := pm.connect(10 * time.Second); err != nil {
			slog.Warn("PlayerManager: no IPC for foreground mpv, progress will not report", "error", err)
			return
		}
		pm.reportProgressWithKey()
	}()

	return cmd.Wait()
}
