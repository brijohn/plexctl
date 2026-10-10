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
	"github.com/ygelfand/plexctl/internal/config"
	"github.com/ygelfand/plexctl/internal/plex"
	"github.com/ygelfand/plexctl/internal/tui/player"
	"github.com/ygelfand/plexctl/internal/ui"
)

// trackItem renders a track row with its number and duration.
type trackItem struct {
	metadata components.Metadata
}

func (i trackItem) Title() string {
	if i.metadata.Index != nil {
		return fmt.Sprintf("%d. %s", *i.metadata.Index, i.metadata.Title)
	}
	return i.metadata.Title
}

func (i trackItem) Description() string {
	if i.metadata.Duration != nil {
		return ui.FormatDuration(int(*i.metadata.Duration))
	}
	return ""
}

func (i trackItem) FilterValue() string { return i.metadata.Title }

type AlbumDetailView struct {
	DetailBase
	children  []components.Metadata
	trackList list.Model
}

func NewAlbumDetailView(ratingKey string, theme tint.Tint) *AlbumDetailView {
	return &AlbumDetailView{
		DetailBase: NewDetailBase(ratingKey, theme),
		trackList:  createBaseDetailList(),
	}
}

func (v *AlbumDetailView) Init() tea.Cmd {
	return v.fetchData
}

func (v *AlbumDetailView) Refresh() tea.Cmd {
	v.Loading = true
	return func() tea.Msg {
		ctx := context.Background()
		meta, err := plex.GetMetadata(ctx, v.RatingKey, true)
		if err != nil {
			return err
		}
		children, _ := plex.GetChildren(ctx, v.RatingKey)
		return detailDataMsg{metadata: meta, children: children}
	}
}

func (v *AlbumDetailView) GetSelectedMetadata() *components.Metadata {
	if v.trackList.SelectedItem() != nil {
		meta := v.trackList.SelectedItem().(trackItem).metadata
		return &meta
	}
	return v.Metadata
}

func (v *AlbumDetailView) fetchData() tea.Msg {
	ctx := context.Background()
	meta, err := plex.GetMetadata(ctx, v.RatingKey, false)
	if err != nil {
		return err
	}

	children, _ := plex.GetChildren(ctx, v.RatingKey)
	return detailDataMsg{metadata: meta, children: children}
}

func (v *AlbumDetailView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case detailDataMsg:
		v.children = msg.children
		var items []list.Item
		for _, child := range v.children {
			items = append(items, trackItem{metadata: child})
		}
		v.trackList.SetItems(items)
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			if v.trackList.SelectedItem() != nil {
				track := v.trackList.SelectedItem().(trackItem).metadata
				if track.RatingKey != nil {
					rk := *track.RatingKey
					return v, func() tea.Msg {
						return ui.RequestPlayMsg{
							RatingKey: rk,
							Output:    player.ResolveOutput(config.Get().VideoOutput, false),
						}
					}
				}
			}
		case "esc", "backspace":
			return v, func() tea.Msg { return BackMsg{} }
		case "A":
			if v.Metadata != nil && v.Metadata.ParentRatingKey != nil {
				return v, func() tea.Msg {
					return ui.JumpToDetailMsg{
						RatingKey: *v.Metadata.ParentRatingKey,
						Type:      "artist",
					}
				}
			}
		}
	}

	cmd := v.DetailBase.Update(msg)
	cmds = append(cmds, cmd)

	var listCmd tea.Cmd
	v.trackList, listCmd = v.trackList.Update(msg)
	cmds = append(cmds, listCmd)

	v.trackList.SetSize(ui.GetLayout().InnerWidth()-4, ui.GetLayout().ContentHeight()/2)

	return v, tea.Batch(cmds...)
}

func (v *AlbumDetailView) IsAtRoot() bool {
	return true
}

func (v *AlbumDetailView) HelpKeys() []ui.HelpKey {
	return []ui.HelpKey{
		{Key: "enter", Desc: "Play Track"},
		{Key: "p", Desc: "Play Selected Track"},
		{Key: "A", Desc: "Go to Artist"},
		{Key: "esc", Desc: "Back"},
		{Key: "j/up", Desc: "Move Up / Scroll"},
		{Key: "k/down", Desc: "Move Down / Scroll"},
	}
}

func (v *AlbumDetailView) View() string {
	if v.Loading {
		return lipgloss.NewStyle().Padding(2).Render("Loading album details...")
	}
	if v.Metadata == nil {
		return "No metadata available"
	}

	rightWidth := v.SynopsisVP.Width

	var headerParts []string
	if v.Metadata.Year != nil {
		headerParts = append(headerParts, fmt.Sprintf("%d", *v.Metadata.Year))
	}
	if v.Metadata.LeafCount != nil {
		headerParts = append(headerParts, fmt.Sprintf("%d Tracks", *v.Metadata.LeafCount))
	} else if v.Metadata.ChildCount != nil {
		headerParts = append(headerParts, fmt.Sprintf("%d Tracks", *v.Metadata.ChildCount))
	}
	if v.Metadata.Duration != nil {
		headerParts = append(headerParts, ui.FormatDuration(int(*v.Metadata.Duration)))
	}
	headerInfo := strings.Join(headerParts, "  •  ")

	infoSection := lipgloss.JoinVertical(lipgloss.Left,
		v.RenderHeader(rightWidth),
		lipgloss.NewStyle().Foreground(v.Theme.BrightCyan()).MarginBottom(1).Width(rightWidth).Render(headerInfo),
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
		Render(v.trackList.View())

	return lipgloss.NewStyle().Padding(1, 2).Render(lipgloss.JoinVertical(lipgloss.Left, mainLayout, listContent)) +
		"\n\n " + lipgloss.NewStyle().Foreground(v.Theme.BrightBlack()).Render("[enter] Play Track | [A] Artist | [esc] Back")
}
