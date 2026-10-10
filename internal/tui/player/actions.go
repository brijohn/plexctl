package player

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/LukeHagar/plexgo/models/components"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/ygelfand/plexctl/internal/config"
	"github.com/ygelfand/plexctl/internal/plex"
	"github.com/ygelfand/plexctl/internal/ui"
)

// PlayRequest carries everything needed to start playback of one item.
type PlayRequest struct {
	URL       string
	Title     string
	RatingKey string
	NoReport  bool
	Subtitles []ExternalSubtitle
}

// BuildPlayRequest resolves the direct-play URL and external subtitle URLs
// for a media item.
func BuildPlayRequest(metadata *components.Metadata, noReport bool) (*PlayRequest, error) {
	if metadata == nil || len(metadata.Media) == 0 || len(metadata.Media[0].Part) == 0 {
		return nil, fmt.Errorf("no playable media found")
	}

	cfg := config.Get()
	_, serverCfg, ok := cfg.GetActiveServer()
	if !ok {
		return nil, fmt.Errorf("no active server")
	}

	part := metadata.Media[0].Part[0]
	separator := "?"
	if strings.Contains(part.Key, "?") {
		separator = "&"
	}
	playURL := fmt.Sprintf("%s%s%sX-Plex-Token=%s", serverCfg.URL, part.Key, separator, cfg.Token)

	var subtitles []ExternalSubtitle
	for _, stream := range part.Stream {
		if stream.StreamType == components.StreamTypeSubtitle && stream.Key != "" {
			sep := "?"
			if strings.Contains(stream.Key, "?") {
				sep = "&"
			}
			subURL := fmt.Sprintf("%s%s%sX-Plex-Token=%s", serverCfg.URL, stream.Key, sep, cfg.Token)
			lang := ""
			if stream.LanguageCode != nil {
				lang = *stream.LanguageCode
			}
			subtitles = append(subtitles, ExternalSubtitle{
				URL:      subURL,
				Title:    stream.DisplayTitle,
				Language: lang,
			})
		}
	}

	rk := ""
	if metadata.RatingKey != nil {
		rk = *metadata.RatingKey
	}

	title := metadata.Title
	if noReport {
		title = "Trailer: " + title
	}

	return &PlayRequest{
		URL:       playURL,
		Title:     title,
		RatingKey: rk,
		NoReport:  noReport,
		Subtitles: subtitles,
	}, nil
}

func PlayMedia(metadata *components.Metadata, noReport bool, output config.VideoOutput, startOffset int64) tea.Cmd {
	req, err := BuildPlayRequest(metadata, noReport)
	if err != nil {
		return func() tea.Msg { return err }
	}

	slog.Debug("Playing media", "url", req.URL, "output", output, "offset", startOffset, "subtitles", len(req.Subtitles))

	// In the TUI, suspend mode hands the whole terminal to a foreground mpv
	// via tea.ExecProcess; the controller handles SuspendPlayMsg.
	if output != config.VideoOutputWindow && config.IsTUI &&
		config.Get().TerminalVideoMode != config.TerminalVideoOverlay {
		return func() tea.Msg {
			return SuspendPlayMsg{Request: req, Output: output, StartOffset: startOffset}
		}
	}

	return GetPlayerManager().Play(req, output, startOffset)
}

// FetchAndPlay handles the full playback logic: fetch full metadata, check for resume, then play
func FetchAndPlay(ratingKey string, output config.VideoOutput) tea.Cmd {
	return func() tea.Msg {
		meta, err := plex.GetMetadata(context.Background(), ratingKey, true)
		if err != nil {
			return err
		}

		if meta.ViewOffset != nil && *meta.ViewOffset > 0 {
			return ui.ResumeChoiceMsg{
				Metadata: meta,
				Output:   output,
			}
		}

		cmd := PlayMedia(meta, false, output, 0)
		if cmd != nil {
			return cmd()
		}
		return nil
	}
}

// ResolveTrailer finds the trailer item for a media item and returns its metadata.
func ResolveTrailer(parentMeta *components.Metadata) (*components.Metadata, error) {
	targetKey := ""
	if parentMeta.PrimaryExtraKey != nil {
		targetKey = *parentMeta.PrimaryExtraKey
	} else if parentMeta.Extras != nil {
		for _, extra := range parentMeta.Extras.Metadata {
			if extra.Subtype != nil && *extra.Subtype == "trailer" {
				if extra.RatingKey != nil {
					targetKey = *extra.RatingKey
					break
				}
			}
		}
	}

	if targetKey == "" {
		return nil, fmt.Errorf("no trailer found for this item")
	}

	// Normalize key (some extra keys are full paths)
	parts := strings.Split(targetKey, "/")
	ratingKey := parts[len(parts)-1]

	slog.Debug("ResolveTrailer: Resolved trailer key", "parent", parentMeta.Title, "target", ratingKey)

	return plex.GetMetadata(context.Background(), ratingKey, false)
}

// FetchAndPlayTrailer handles finding and playing the correct trailer for a media item
func FetchAndPlayTrailer(parentMeta *components.Metadata, output config.VideoOutput) tea.Cmd {
	return func() tea.Msg {
		meta, err := ResolveTrailer(parentMeta)
		if err != nil {
			return err
		}

		cmd := PlayMedia(meta, true, output, 0)
		if cmd != nil {
			return cmd()
		}
		return nil
	}
}
