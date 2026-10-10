package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/spf13/cobra"
	"github.com/ygelfand/plexctl/internal/commands"
	"github.com/ygelfand/plexctl/internal/config"
	"github.com/ygelfand/plexctl/internal/plex"
	"github.com/ygelfand/plexctl/internal/term"
	"github.com/ygelfand/plexctl/internal/tui/player"
)

var (
	tctMode  bool
	voFlag   string
	noResume bool
	trailer  bool
)

// resolvePlayOutput maps the --vo/--tct flags (falling back to config) to a
// concrete video output.
func resolvePlayOutput() (config.VideoOutput, error) {
	requested := config.Get().VideoOutput
	if voFlag != "" {
		requested = config.VideoOutput(voFlag)
	} else if tctMode {
		requested = config.VideoOutputTct
	}

	switch requested {
	case config.VideoOutputAuto, config.VideoOutputWindow, "":
		return config.VideoOutputWindow, nil
	case config.VideoOutputTct:
		return config.VideoOutputTct, nil
	case config.VideoOutputKitty:
		term.Detect()
		if !player.MpvSupportsKitty() {
			fmt.Println("mpv is too old for --vo kitty (needs >= 0.36), using tct instead")
			return config.VideoOutputTct, nil
		}
		return config.VideoOutputKitty, nil
	default:
		return "", fmt.Errorf("invalid video output %q (use auto, window, tct, or kitty)", requested)
	}
}

var playCmd = &cobra.Command{
	Use:     "play [media_id]",
	Short:   "Play a media item",
	Args:    cobra.ExactArgs(1),
	GroupID: "media",
	RunE: commands.RunWithServer(func(ctx context.Context, client *plex.Client, cmd *cobra.Command, args []string, opts *commands.PlexCtlOptions) error {
		mediaID := args[0]
		slog.Debug("CLI Play: Fetching metadata", "mediaID", mediaID)

		output, err := resolvePlayOutput()
		if err != nil {
			return err
		}

		meta, err := plex.GetMetadata(ctx, mediaID, true)
		if err != nil {
			return fmt.Errorf("failed to get metadata: %w", err)
		}

		noReport := false
		if trailer {
			slog.Debug("CLI Play: Resolving trailer", "mediaID", mediaID)
			meta, err = player.ResolveTrailer(meta)
			if err != nil {
				return err
			}
			noReport = true
		}

		offset := int64(0)
		if !trailer && !noResume && meta.ViewOffset != nil {
			offset = int64(*meta.ViewOffset)
		}

		if offset > 0 {
			slog.Info("Resuming", "title", meta.Title, "type", meta.Type, "offset", offset)
		} else {
			slog.Info("Playing", "title", meta.Title, "type", meta.Type)
		}

		title := meta.Title
		if trailer {
			title = title + " (Trailer)"
		}

		pm := player.GetPlayerManager()

		// Terminal outputs run mpv in the foreground with its own keyboard.
		if output != config.VideoOutputWindow {
			req, err := player.BuildPlayRequest(meta, noReport)
			if err != nil {
				return err
			}
			fmt.Printf("Playing %s in the terminal (%s). Press 'q' in mpv to stop.\n", title, output)
			return pm.RunForeground(ctx, req, output, offset)
		}

		playFunc := player.PlayMedia(meta, noReport, output, offset)
		msg := playFunc()
		if err, ok := msg.(error); ok {
			return err
		}
		fmt.Printf("Playing %s . Press Ctrl+C to stop.\n", title)

		// Wait loop for CLI to keep progress reporting alive
		for pm.VerifyConnection() {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(500 * time.Millisecond):
				// Just keep waiting
			}
		}

		return nil
	}),
}

func init() {
	rootCmd.AddCommand(playCmd)
	playCmd.Flags().StringVar(&voFlag, "vo", "", "Video output: auto, window, tct, or kitty")
	playCmd.Flags().BoolVar(&tctMode, "tct", false, "Use terminal video")
	_ = playCmd.Flags().MarkDeprecated("tct", "use --vo tct")
	playCmd.Flags().BoolVar(&noResume, "no-resume", false, "Start playback from the beginning")
	playCmd.Flags().BoolVar(&trailer, "trailer", false, "Play the primary trailer instead of the full media")
}
