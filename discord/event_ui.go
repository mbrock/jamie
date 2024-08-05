package discord

import (
	"fmt"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"node.town/snd"
)

type eventItem struct {
	event snd.DiscordEventNotification
}

func (i eventItem) Title() string {
	return fmt.Sprintf("%s (Op: %d)", i.event.Type, i.event.Operation)
}

func (i eventItem) Description() string {
	return fmt.Sprintf("ID: %d, Sequence: %v, Created: %s", i.event.ID, i.event.Sequence.Int32, i.event.CreatedAt.Format("15:04:05"))
}

func (i eventItem) FilterValue() string {
	return i.event.Type
}

type Model struct {
	list     list.Model
	events   <-chan snd.DiscordEventNotification
	quitting bool
}

func NewEventUI(events <-chan snd.DiscordEventNotification, existingEvents []db.DiscordEvent) *Model {
	m := &Model{events: events}

	delegate := list.NewDefaultDelegate()
	items := make([]list.Item, len(existingEvents))
	for i, event := range existingEvents {
		items[i] = eventItem{event: snd.DiscordEventNotification{
			ID:        event.ID,
			Operation: event.Operation,
			Sequence:  event.Sequence,
			Type:      event.Type,
			RawData:   event.RawData,
			BotToken:  event.BotToken,
			CreatedAt: event.CreatedAt,
		}}
	}
	m.list = list.New(items, delegate, 0, 0)
	m.list.Title = "Discord Events"
	m.list.SetShowStatusBar(false)
	m.list.SetFilteringEnabled(false)
	m.list.Styles.Title = lipgloss.NewStyle().MarginLeft(2)
	m.list.Styles.PaginationStyle = list.DefaultStyles().PaginationStyle.PaddingLeft(4)
	m.list.Styles.HelpStyle = list.DefaultStyles().HelpStyle.PaddingLeft(4).PaddingBottom(1)

	return m
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		waitForEvent(m.events),
		tea.EnterAltScreen,
	)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" || msg.String() == "q" {
			m.quitting = true
			return m, tea.Quit
		}

	case tea.WindowSizeMsg:
		h, v := docStyle.GetFrameSize()
		m.list.SetSize(msg.Width-h, msg.Height-v)

	case snd.DiscordEventNotification:
		m.list.InsertItem(0, eventItem{event: msg})
		if len(m.list.Items()) > 1000 {
			m.list.RemoveItem(len(m.list.Items()) - 1)
		}
		return m, waitForEvent(m.events)
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m Model) View() string {
	if m.quitting {
		return "Goodbye!\n"
	}
	return docStyle.Render(m.list.View())
}

func waitForEvent(events <-chan snd.DiscordEventNotification) tea.Cmd {
	return func() tea.Msg {
		return <-events
	}
}

var docStyle = lipgloss.NewStyle().Margin(1, 2)
