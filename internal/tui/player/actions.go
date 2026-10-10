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

// PlayItem is one entry of a playback queue.
type PlayItem struct {
	URL       string
	Title     string
	RatingKey string
}

// PlayRequest carries everything needed to start playback of one item or,
// for music containers, an ordered queue of tracks.
type PlayRequest struct {
	URL       string
	Title     string
	RatingKey string
	NoReport  bool
	Music     bool // audio-only playback, eligible for a visualizer
	Subtitles []ExternalSubtitle
	Queue     []PlayItem // entries played after URL
}

// partURL builds a tokenized direct-play URL for a media part or stream key.
func partURL(serverURL, token, key string) string {
	separator := "?"
	if strings.Contains(key, "?") {
		separator = "&"
	}
	return fmt.Sprintf("%s%s%sX-Plex-Token=%s", serverURL, key, separator, token)
}

// BuildPlayRequest resolves the direct-play URL and external subtitle URLs
// for a media item. Music containers (artist, album) resolve to a queue of
// their tracks.
func BuildPlayRequest(metadata *components.Metadata, noReport bool) (*PlayRequest, error) {
	cfg := config.Get()
	_, serverCfg, ok := cfg.GetActiveServer()
	if !ok {
		return nil, fmt.Errorf("no active server")
	}

	if metadata != nil && (metadata.Type == "artist" || metadata.Type == "album") {
		return buildQueueRequest(metadata, serverCfg.URL, cfg.Token)
	}

	if metadata == nil || len(metadata.Media) == 0 || len(metadata.Media[0].Part) == 0 {
		return nil, fmt.Errorf("no playable media found")
	}

	part := metadata.Media[0].Part[0]
	playURL := partURL(serverCfg.URL, cfg.Token, part.Key)

	var subtitles []ExternalSubtitle
	for _, stream := range part.Stream {
		if stream.StreamType == components.StreamTypeSubtitle && stream.Key != "" {
			lang := ""
			if stream.LanguageCode != nil {
				lang = *stream.LanguageCode
			}
			subtitles = append(subtitles, ExternalSubtitle{
				URL:      partURL(serverCfg.URL, cfg.Token, stream.Key),
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
		Music:     metadata.Type == "track",
		Subtitles: subtitles,
	}, nil
}

// buildQueueRequest resolves an artist or album to an ordered track queue.
func buildQueueRequest(metadata *components.Metadata, serverURL, token string) (*PlayRequest, error) {
	tracks, err := collectTracks(context.Background(), metadata)
	if err != nil {
		return nil, err
	}

	var items []PlayItem
	for _, t := range tracks {
		if len(t.Media) == 0 || len(t.Media[0].Part) == 0 {
			continue
		}
		title := t.Title
		if t.GrandparentTitle != nil {
			title = *t.GrandparentTitle + " - " + title
		}
		rk := ""
		if t.RatingKey != nil {
			rk = *t.RatingKey
		}
		items = append(items, PlayItem{
			URL:       partURL(serverURL, token, t.Media[0].Part[0].Key),
			Title:     title,
			RatingKey: rk,
		})
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("no playable tracks found in %s", metadata.Title)
	}

	return &PlayRequest{
		URL:       items[0].URL,
		Title:     items[0].Title,
		RatingKey: items[0].RatingKey,
		Music:     true,
		Queue:     items[1:],
	}, nil
}

// collectTracks returns the ordered tracks of an album, or of every album of
// an artist. Tracks missing media info in the children listing are fetched
// individually.
func collectTracks(ctx context.Context, metadata *components.Metadata) ([]components.Metadata, error) {
	if metadata.RatingKey == nil {
		return nil, fmt.Errorf("no rating key for %s", metadata.Title)
	}

	var candidates []components.Metadata
	switch metadata.Type {
	case "album":
		children, err := plex.GetChildren(ctx, *metadata.RatingKey)
		if err != nil {
			return nil, err
		}
		candidates = children
	case "artist":
		// A section query finds the artist's tracks on all albums, including
		// soundtracks and compilations not listed as children of the artist.
		if sid := plex.SectionIDOf(metadata); sid != "" {
			candidates, _ = plex.GetArtistTracks(ctx, sid, *metadata.RatingKey)
		}
		if len(candidates) == 0 {
			children, err := plex.GetChildren(ctx, *metadata.RatingKey)
			if err != nil {
				return nil, err
			}
			for _, album := range children {
				if album.Type != "album" || album.RatingKey == nil {
					continue
				}
				tracks, err := plex.GetChildren(ctx, *album.RatingKey)
				if err != nil {
					return nil, err
				}
				candidates = append(candidates, tracks...)
			}
		}
	default:
		return nil, fmt.Errorf("not a music container: %s", metadata.Type)
	}

	var tracks []components.Metadata
	for _, c := range candidates {
		if c.Type != "track" {
			continue
		}
		if len(c.Media) == 0 || len(c.Media[0].Part) == 0 {
			if c.RatingKey == nil {
				continue
			}
			full, err := plex.GetMetadata(ctx, *c.RatingKey, false)
			if err != nil || full == nil {
				continue
			}
			c = *full
		}
		tracks = append(tracks, c)
	}
	return tracks, nil
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
