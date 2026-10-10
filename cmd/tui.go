package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/ygelfand/plexctl/internal/commands"
	"github.com/ygelfand/plexctl/internal/config"
	"github.com/ygelfand/plexctl/internal/plex"
	"github.com/ygelfand/plexctl/internal/render"
	"github.com/ygelfand/plexctl/internal/term"
	"github.com/ygelfand/plexctl/internal/tui"
)

var tuiCmd = &cobra.Command{
	Use:     "tui",
	Short:   "Launch the interactive TUI",
	GroupID: "tui",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTUI()
	},
}

func runTUI() error {
	config.IsTUI = true
	cfg := config.Get()
	// Always log TUI sessions to a file for easier debugging
	cfg.LogFile = filepath.Join(cfg.CacheDir, "tui.log")
	cfg.SetupLogging()
	slog.Info("TUI Starting", "log_file", cfg.LogFile, "verbosity", cfg.Verbosity)

	if err := commands.EnsureActiveServer(context.Background()); err != nil {
		slog.Error("TUI: Failed to ensure active server", "error", err)
		return err
	}

	// Probe graphics capabilities before anything writes to the terminal,
	// then pick the poster renderer and the output writer shared between
	// Bubble Tea and out-of-band graphics transmissions.
	caps := term.Detect()
	slog.Debug("TUI: terminal capabilities", "kittyGraphics", caps.KittyGraphics,
		"kittyPlaceholders", caps.KittyPlaceholders, "cellPx", fmt.Sprintf("%dx%d", caps.CellWidthPx, caps.CellHeightPx))
	out := term.NewSyncWriter(os.Stdout)
	render.Init(cfg.PosterRenderer, out)

	resultChan := make(chan plex.LoaderResult, 1)

	slog.Debug("TUI: Showing standalone splash")
	tui.ShowStandaloneSplash(1*time.Second, resultChan)

	slog.Debug("TUI: Waiting for loader result")
	result := <-resultChan
	slog.Debug("TUI: Received loader result", "libraries", len(result.Libraries))

	slog.Debug("TUI: Initializing controller and program")
	p := tea.NewProgram(tui.NewController(result), tea.WithAltScreen(), tea.WithOutput(out))

	_, err := p.Run()
	render.Active().Shutdown()
	if err != nil {
		slog.Error("TUI: Program run failed", "error", err)
		return fmt.Errorf("failed to run TUI: %w", err)
	}
	slog.Info("TUI Finished normally")
	return nil
}

func init() {
	rootCmd.AddCommand(tuiCmd)
}
