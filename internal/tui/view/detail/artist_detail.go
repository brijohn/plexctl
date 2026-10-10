package detail

import (
	"context"
	"fmt"
	"strings"

	"github.com/LukeHagar/plexgo/models/components"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	tint "github.com/lrstanley/bubbletint"
	"github.com/ygelfand/plexctl/internal/plex"
	"github.com/ygelfand/plexctl/internal/tui/widget/poster"
	"github.com/ygelfand/plexctl/internal/ui"
)

// Distinct from the library grid's list ID so in-flight poster loads from the
// grid never land in the album row.
const artistAlbumListID = 101

type ArtistDetailView struct {
	DetailBase
	children      []components.Metadata
	albums        *poster.PosterList
	selectedAlbum *AlbumDetailView
}

func NewArtistDetailView(ratingKey string, theme tint.Tint) *ArtistDetailView {
	albums := poster.NewPosterList(artistAlbumListID, "Albums", nil, theme)
	albums.Active = true
	return &ArtistDetailView{
		DetailBase: NewDetailBase(ratingKey, theme),
		albums:     albums,
	}
}

func (v *ArtistDetailView) Init() tea.Cmd {
	return v.fetchData
}

func (v *ArtistDetailView) Refresh() tea.Cmd {
	if v.selectedAlbum != nil {
		return v.selectedAlbum.Refresh()
	}
	v.Loading = true
	return func() tea.Msg {
		ctx := context.Background()
		meta, err := plex.GetMetadata(ctx, v.RatingKey, true)
		if err != nil {
			return err
		}
		return detailDataMsg{metadata: meta, children: fetchArtistAlbums(ctx, meta, v.RatingKey)}
	}
}

// fetchArtistAlbums lists all albums with tracks by the artist (including
// soundtracks and compilations); the plain children listing is the fallback
// when the section is unknown or the query fails.
func fetchArtistAlbums(ctx context.Context, meta *components.Metadata, ratingKey string) []components.Metadata {
	if sid := plex.SectionIDOf(meta); sid != "" {
		if albums, err := plex.GetArtistAlbums(ctx, sid, ratingKey); err == nil && len(albums) > 0 {
			return albums
		}
	}
	children, _ := plex.GetChildren(ctx, ratingKey)
	return children
}

func (v *ArtistDetailView) GetSelectedMetadata() *components.Metadata {
	if v.selectedAlbum != nil {
		return v.selectedAlbum.GetSelectedMetadata()
	}
	if v.albums.Cursor >= 0 && v.albums.Cursor < len(v.albums.Items) {
		return &v.albums.Items[v.albums.Cursor].Metadata
	}
	return v.Metadata
}

func (v *ArtistDetailView) fetchData() tea.Msg {
	ctx := context.Background()
	meta, err := plex.GetMetadata(ctx, v.RatingKey, false)
	if err != nil {
		return err
	}

	return detailDataMsg{metadata: meta, children: fetchArtistAlbums(ctx, meta, v.RatingKey)}
}

func (v *ArtistDetailView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if v.selectedAlbum != nil {
		newAlbum, cmd := v.selectedAlbum.Update(msg)
		if _, ok := msg.(BackMsg); ok {
			v.selectedAlbum = nil
			return v, nil
		}
		v.selectedAlbum = newAlbum.(*AlbumDetailView)
		return v, cmd
	}

	var cmds []tea.Cmd
	v.albums.Cols = max((ui.GetLayout().InnerWidth()-4)/ui.PosterStepWidth, 1)

	switch msg := msg.(type) {
	case detailDataMsg:
		v.children = msg.children
		cmds = append(cmds, v.albums.SetItems(msg.children))
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			if v.albums.Cursor >= 0 && v.albums.Cursor < len(v.albums.Items) {
				child := v.albums.Items[v.albums.Cursor].Metadata
				if child.RatingKey != nil {
					v.selectedAlbum = NewAlbumDetailView(*child.RatingKey, v.Theme)
					return v, v.selectedAlbum.Init()
				}
			}
		case "esc", "backspace":
			return v, func() tea.Msg { return BackMsg{} }
		}
	}

	cmd := v.DetailBase.Update(msg)
	cmds = append(cmds, cmd)

	_, listCmd := v.albums.Update(msg)
	cmds = append(cmds, listCmd)

	return v, tea.Batch(cmds...)
}

func (v *ArtistDetailView) IsAtRoot() bool {
	return v.selectedAlbum == nil
}

func (v *ArtistDetailView) HelpKeys() []ui.HelpKey {
	if v.selectedAlbum != nil {
		return v.selectedAlbum.HelpKeys()
	}
	return []ui.HelpKey{
		{Key: "enter", Desc: "Select Album"},
		{Key: "p", Desc: "Play Selected Album"},
		{Key: "left/right", Desc: "Browse Albums"},
		{Key: "esc", Desc: "Back"},
	}
}

func (v *ArtistDetailView) View() string {
	if v.selectedAlbum != nil {
		return v.selectedAlbum.View()
	}
	if v.Loading {
		return lipgloss.NewStyle().Padding(2).Render("Loading artist details...")
	}
	if v.Metadata == nil {
		return "No metadata available"
	}

	rightWidth := v.SynopsisVP.Width

	var headerParts []string
	if len(v.children) > 0 {
		headerParts = append(headerParts, fmt.Sprintf("%d Albums", len(v.children)))
	} else if v.Metadata.ChildCount != nil {
		headerParts = append(headerParts, fmt.Sprintf("%d Albums", *v.Metadata.ChildCount))
	}
	headerInfo := strings.Join(headerParts, "  •  ")

	var genres []string
	for _, g := range v.Metadata.Genre {
		genres = append(genres, g.Tag)
	}
	genreStr := strings.Join(genres, ", ")

	labelStyle := lipgloss.NewStyle().Foreground(v.Theme.BrightBlack()).Bold(true)
	valueStyle := lipgloss.NewStyle().Foreground(v.Theme.White())

	detailsSection := lipgloss.NewStyle().Width(rightWidth).
		Render(lipgloss.JoinHorizontal(lipgloss.Top, labelStyle.Render("GENRE: "), valueStyle.Render(genreStr)))

	infoSection := lipgloss.JoinVertical(lipgloss.Left,
		v.RenderHeader(rightWidth),
		lipgloss.NewStyle().Foreground(v.Theme.BrightCyan()).MarginBottom(1).Width(rightWidth).Render(headerInfo),
		detailsSection,
		"",
		lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder(), true, false, true, false).
			BorderForeground(v.Theme.BrightBlack()).
			Padding(1, 0).
			Width(rightWidth).
			Render(v.SynopsisVP.View()),
	)

	mainLayout := v.RenderPosterAndInfo(infoSection)

	listContent := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), true, false, false, false).
		BorderForeground(v.Theme.BrightBlack()).
		PaddingTop(1).
		Render(v.albums.View())

	return lipgloss.NewStyle().Padding(1, 2).Render(lipgloss.JoinVertical(lipgloss.Left, mainLayout, listContent)) +
		"\n\n " + lipgloss.NewStyle().Foreground(v.Theme.BrightBlack()).Render("[enter] Select Album | [p] Play Album | [←/→] Browse | [esc] Back")
}
