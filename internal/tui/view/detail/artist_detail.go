package detail

import (
	"context"
	"fmt"
	"strings"

	"github.com/LukeHagar/plexgo/models/components"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	tint "github.com/lrstanley/bubbletint"
	"github.com/ygelfand/plexctl/internal/plex"
	"github.com/ygelfand/plexctl/internal/ui"
)

// albumItem renders an album row with its year, release type, and track count.
type albumItem struct {
	metadata components.Metadata
}

func (i albumItem) Title() string { return i.metadata.Title }

func (i albumItem) Description() string {
	var parts []string
	if i.metadata.Year != nil {
		parts = append(parts, fmt.Sprintf("%d", *i.metadata.Year))
	}
	if sf, ok := i.metadata.AdditionalProperties["subformat"].(string); ok && sf != "" {
		parts = append(parts, sf)
	}
	if i.metadata.LeafCount != nil {
		parts = append(parts, fmt.Sprintf("%d tracks", *i.metadata.LeafCount))
	}
	return strings.Join(parts, " • ")
}

func (i albumItem) FilterValue() string { return i.metadata.Title }

type ArtistDetailView struct {
	DetailBase
	children      []components.Metadata
	albumList     list.Model
	selectedAlbum *AlbumDetailView
}

func NewArtistDetailView(ratingKey string, theme tint.Tint) *ArtistDetailView {
	return &ArtistDetailView{
		DetailBase: NewDetailBase(ratingKey, theme),
		albumList:  createBaseDetailList(),
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
	if v.albumList.SelectedItem() != nil {
		meta := v.albumList.SelectedItem().(albumItem).metadata
		return &meta
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

	switch msg := msg.(type) {
	case detailDataMsg:
		v.children = msg.children
		var items []list.Item
		for _, child := range v.children {
			items = append(items, albumItem{metadata: child})
		}
		v.albumList.SetItems(items)
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			if v.albumList.SelectedItem() != nil {
				child := v.albumList.SelectedItem().(albumItem).metadata
				v.selectedAlbum = NewAlbumDetailView(*child.RatingKey, v.Theme)
				return v, v.selectedAlbum.Init()
			}
		case "esc", "backspace":
			return v, func() tea.Msg { return BackMsg{} }
		}
	}

	cmd := v.DetailBase.Update(msg)
	cmds = append(cmds, cmd)

	var listCmd tea.Cmd
	v.albumList, listCmd = v.albumList.Update(msg)
	cmds = append(cmds, listCmd)

	v.albumList.SetSize(ui.GetLayout().InnerWidth()-4, ui.GetLayout().ContentHeight()/2)

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
		{Key: "esc", Desc: "Back"},
		{Key: "j/up", Desc: "Move Up / Scroll"},
		{Key: "k/down", Desc: "Move Down / Scroll"},
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
		Render(v.albumList.View())

	return lipgloss.NewStyle().Padding(1, 2).Render(lipgloss.JoinVertical(lipgloss.Left, mainLayout, listContent)) +
		"\n\n " + lipgloss.NewStyle().Foreground(v.Theme.BrightBlack()).Render("[enter] Select Album | [p] Play Album | [esc] Back")
}
