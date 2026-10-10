package plex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/LukeHagar/plexgo/models/components"
	"github.com/LukeHagar/plexgo/models/operations"
	"github.com/ygelfand/plexctl/internal/cache"
	"github.com/ygelfand/plexctl/internal/config"
)

var (
	MediaCacheTTL  = 1 * time.Hour
	PosterCacheTTL = 30 * 24 * time.Hour // Cache rendered strings for 30 days
)

// GetCachedPoster tries to retrieve a rendered poster string from cache.
// mode namespaces the key per renderer so output from different protocols
// never collides.
func GetCachedPoster(mode, ratingKey string, width int) (string, bool) {
	key := fmt.Sprintf("rendered_poster/%s/%s/%d", mode, ratingKey, width)

	cfg := config.Get()
	cm, err := cache.Get(cfg.CacheDir)
	if err != nil {
		return "", false
	}

	var result string
	if err := cm.Get(key, &result); err == nil {
		slog.Debug("Poster Cache HIT (Disk)", "ratingKey", ratingKey, "width", width)
		return result, true
	}
	slog.Debug("Poster Cache MISS", "ratingKey", ratingKey, "width", width)
	return "", false
}

// SetCachedPoster stores a rendered poster string in cache
func SetCachedPoster(mode, ratingKey string, width int, view string) {
	cfg := config.Get()
	cm, err := cache.Get(cfg.CacheDir)
	if err != nil {
		return
	}

	slog.Debug("Poster Cache SAVE", "ratingKey", ratingKey, "width", width)
	key := fmt.Sprintf("rendered_poster/%s/%s/%d", mode, ratingKey, width)
	_ = cm.Set(key, view, PosterCacheTTL)
}

// GetMetadata retrieves full metadata for an item by its rating key. If force is true, cache is bypassed.
func GetMetadata(ctx context.Context, ratingKey string, force bool) (*components.Metadata, error) {
	cfg := config.Get()
	serverID, _, ok := cfg.GetActiveServer()
	if !ok {
		return nil, fmt.Errorf("no active server")
	}

	cm, err := cache.Get(cfg.CacheDir)
	if err != nil {
		return nil, err
	}

	client, err := NewClient()
	if err != nil {
		return nil, err
	}

	var body components.MediaContainerWithMetadata
	req := operations.GetMetadataItemRequest{
		Ids:           []string{ratingKey},
		IncludeExtras: components.BoolIntTrue.ToPointer(),
	}

	ttl := MediaCacheTTL
	if force {
		ttl = 0
	}

	err = cache.AutoCache(cm, serverID, req, ttl, &body, func() (*components.MediaContainerWithMetadata, error) {
		res, err := client.SDK.Content.GetMetadataItem(ctx, req)
		if err != nil {
			return nil, err
		}
		if res.MediaContainerWithMetadata == nil {
			return nil, fmt.Errorf("metadata not found")
		}
		return res.MediaContainerWithMetadata, nil
	})
	if err != nil {
		return nil, err
	}

	if len(body.MediaContainer.Metadata) == 0 {
		return nil, fmt.Errorf("metadata empty")
	}

	return &body.MediaContainer.Metadata[0], nil
}

func ptr[T any](v T) *T {
	return &v
}

// GetChildren retrieves children of an item (e.g. seasons of a show, or episodes of a season)
func GetChildren(ctx context.Context, ratingKey string) ([]components.Metadata, error) {
	return getMetadataList(ctx,
		fmt.Sprintf("children/%s", ratingKey),
		fmt.Sprintf("/library/metadata/%s/children", ratingKey))
}

// GetArtistAlbums lists every album in a section containing tracks by the
// artist, including soundtracks and compilations that are not children of the
// artist item itself.
func GetArtistAlbums(ctx context.Context, sectionID, artistRatingKey string) ([]components.Metadata, error) {
	return getMetadataList(ctx,
		fmt.Sprintf("artist_albums/%s/%s", sectionID, artistRatingKey),
		fmt.Sprintf("/library/sections/%s/all?artist.id=%s&type=9", sectionID, artistRatingKey))
}

// GetArtistTracks lists every track by the artist in a section, across all
// albums including soundtracks and compilations.
func GetArtistTracks(ctx context.Context, sectionID, artistRatingKey string) ([]components.Metadata, error) {
	return getMetadataList(ctx,
		fmt.Sprintf("artist_tracks/%s/%s", sectionID, artistRatingKey),
		fmt.Sprintf("/library/sections/%s/all?artist.id=%s&type=10", sectionID, artistRatingKey))
}

// SectionIDOf extracts the library section ID carried in a metadata item.
func SectionIDOf(meta *components.Metadata) string {
	if meta == nil {
		return ""
	}
	switch v := meta.AdditionalProperties["librarySectionID"].(type) {
	case string:
		return v
	case float64:
		return fmt.Sprintf("%.0f", v)
	}
	return ""
}

// getMetadataList fetches and caches a metadata listing from a server path
// (which may include query parameters, without the token).
func getMetadataList(ctx context.Context, cacheKeySuffix, path string) ([]components.Metadata, error) {
	cfg := config.Get()
	serverID, serverCfg, ok := cfg.GetActiveServer()
	if !ok {
		return nil, fmt.Errorf("no active server")
	}

	cm, err := cache.Get(cfg.CacheDir)
	if err != nil {
		return nil, err
	}

	cacheKey := fmt.Sprintf("%s/%s", serverID, cacheKeySuffix)
	var body components.MediaContainerWithMetadata
	if err := cm.Get(cacheKey, &body); err == nil && body.MediaContainer != nil {
		return body.MediaContainer.Metadata, nil
	}

	url := serverCfg.URL + path
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	url = fmt.Sprintf("%s%sX-Plex-Token=%s", url, separator, cfg.Token)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch %s: %s", path, resp.Status)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(data, &body); err != nil {
		return nil, fmt.Errorf("failed to parse listing: %w", err)
	}

	if body.MediaContainer == nil {
		return nil, fmt.Errorf("no media container in response")
	}

	_ = cm.Set(cacheKey, body, MediaCacheTTL)
	return body.MediaContainer.Metadata, nil
}

// GetImage retrieves image data (thumb/poster) and caches it
func GetImage(ctx context.Context, path string) ([]byte, error) {
	cfg := config.Get()
	serverID, serverCfg, ok := cfg.GetActiveServer()
	if !ok {
		return nil, fmt.Errorf("no active server")
	}

	cm, err := cache.Get(cfg.CacheDir)
	if err != nil {
		return nil, err
	}

	cacheKey := fmt.Sprintf("%s/img/%s", serverID, path)
	var data []byte
	if err := cm.Get(cacheKey, &data); err == nil {
		return data, nil
	}

	url := fmt.Sprintf("%s%s", serverCfg.URL, path)
	separator := "?"
	if strings.Contains(url, "?") {
		separator = "&"
	}
	url = fmt.Sprintf("%s%sX-Plex-Token=%s", url, separator, cfg.Token)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch image: %s", resp.Status)
	}

	data, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	_ = cm.Set(cacheKey, data, MediaCacheTTL)

	return data, nil
}

// GetHomeHubs retrieves all promoted hubs for the home screen
func GetHomeHubs(ctx context.Context) ([]components.Hub, error) {
	client, err := NewClient()
	if err != nil {
		return nil, err
	}

	res, err := client.SDK.Hubs.GetPromotedHubs(ctx, operations.GetPromotedHubsRequest{
		Count: ptr(int64(50)),
	})
	if err != nil {
		return nil, err
	}

	if res.Object == nil || res.Object.MediaContainer == nil {
		return nil, fmt.Errorf("no hubs found")
	}

	return res.Object.MediaContainer.Hub, nil
}
