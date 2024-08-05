package discord

import (
	"encoding/json"
	"fmt"
	"node.town/db"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"node.town/snd"
)

var (
	titleStyle        = lipgloss.NewStyle().MarginLeft(2)
	itemStyle         = lipgloss.NewStyle().PaddingLeft(4)
	selectedItemStyle = lipgloss.NewStyle().PaddingLeft(2).Foreground(lipgloss.Color("170"))
	paginationStyle   = list.DefaultStyles().PaginationStyle.PaddingLeft(4)
	helpStyle         = list.DefaultStyles().HelpStyle.PaddingLeft(4).PaddingBottom(1)
	quitTextStyle     = lipgloss.NewStyle().Margin(1, 0, 2, 4)
)

type eventItem struct {
	event snd.DiscordEventNotification
}

func (i eventItem) Title() string {
	return fmt.Sprintf("%s", i.event.Type)
}

func (i eventItem) Description() string {
	return fmt.Sprintf("Op: %d | ID: %d | Sequence: %v | Created: %s",
		i.event.Operation,
		i.event.ID,
		i.event.Sequence.Int32,
		i.event.CreatedAt.Format("15:04:05"))
}

func (i eventItem) FilterValue() string {
	return i.event.Type
}

type Model struct {
	list                list.Model
	events              <-chan snd.DiscordEventNotification
	quitting            bool
	showingJSON         bool
	jsonViewport        viewport.Model
	selectedItem        eventItem
	showingParsedEvents bool
	parsedEvents        []ParsedEvent
}

type ParsedEvent struct {
	Type      string
	Content   string
	Timestamp time.Time
}

func NewEventUI(events <-chan snd.DiscordEventNotification, existingEvents []db.DiscordEvent) *Model {
	m := &Model{events: events}

	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = selectedItemStyle
	delegate.Styles.SelectedDesc = selectedItemStyle
	delegate.Styles.NormalTitle = itemStyle
	delegate.Styles.NormalDesc = itemStyle.Copy().Foreground(lipgloss.Color("240"))

	items := make([]list.Item, len(existingEvents))
	for i, event := range existingEvents {
		convertedEvent := snd.ConvertDiscordEvent(event).(snd.DiscordEventNotification)
		items[i] = eventItem{event: convertedEvent}
		m.parsedEvents = append(m.parsedEvents, parseEvent(convertedEvent))
	}
	m.list = list.New(items, delegate, 0, 0)
	m.list.Title = "Discord Events"
	m.list.SetShowStatusBar(false)
	m.list.SetFilteringEnabled(false)
	m.list.Styles.Title = titleStyle
	m.list.Styles.PaginationStyle = paginationStyle
	m.list.Styles.HelpStyle = helpStyle

	m.jsonViewport = viewport.New(0, 0)
	m.jsonViewport.Style = lipgloss.NewStyle().Padding(1, 2)

	return m
}

func parseEvent(event snd.DiscordEventNotification) ParsedEvent {
	parsedEvent := ParsedEvent{
		Type:      event.Type,
		Timestamp: event.CreatedAt,
	}

	switch event.Type {
	case "MESSAGE_CREATE":
		var messageData struct {
			Content string `json:"content"`
			Author  struct {
				Username string `json:"username"`
			} `json:"author"`
		}
		json.Unmarshal(event.RawData, &messageData)
		parsedEvent.Content = fmt.Sprintf("%s: %s", messageData.Author.Username, messageData.Content)
	case "GUILD_CREATE":
		var guildData struct {
			Name string `json:"name"`
			ID   string `json:"id"`
		}
		json.Unmarshal(event.RawData, &guildData)
		parsedEvent.Content = fmt.Sprintf("Joined guild: %s (ID: %s)", guildData.Name, guildData.ID)
	default:
		parsedEvent.Content = "Unhandled event type"
	}

	return parsedEvent
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		waitForEvent(m.events),
		tea.EnterAltScreen,
	)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			m.quitting = true
			return m, tea.Quit
		case "enter":
			if !m.showingJSON && !m.showingParsedEvents {
				m.showingJSON = true
				m.selectedItem = m.list.SelectedItem().(eventItem)
				var rawData interface{}
				err := json.Unmarshal(m.selectedItem.event.RawData, &rawData)
				if err != nil {
					m.jsonViewport.SetContent(fmt.Sprintf("Error unmarshalling JSON: %v", err))
				} else {
					renderedJSON := RenderJSON(rawData)
					m.jsonViewport.SetContent(renderedJSON)
				}
			} else {
				m.showingJSON = false
				m.showingParsedEvents = false
			}
		case "tab":
			m.showingParsedEvents = !m.showingParsedEvents
			m.showingJSON = false
		case "esc":
			if m.showingJSON || m.showingParsedEvents {
				m.showingJSON = false
				m.showingParsedEvents = false
			}
		}

	case tea.WindowSizeMsg:
		h, v := docStyle.GetFrameSize()

		m.jsonViewport.Width = msg.Width - h
		m.jsonViewport.Height = msg.Height - v

		m.list.SetSize(msg.Width-h, msg.Height-v)

	case snd.DiscordEventNotification:
		m.list.InsertItem(0, eventItem{event: msg})
		if len(m.list.Items()) > 1000 {
			m.list.RemoveItem(len(m.list.Items()) - 1)
		}
		m.parsedEvents = append([]ParsedEvent{parseEvent(msg)}, m.parsedEvents...)
		if len(m.parsedEvents) > 1000 {
			m.parsedEvents = m.parsedEvents[:1000]
		}
		return m, waitForEvent(m.events)
	}

	if m.showingJSON {
		m.jsonViewport, cmd = m.jsonViewport.Update(msg)
	} else if m.showingParsedEvents {
		m.jsonViewport, cmd = m.jsonViewport.Update(msg)
	} else {
		m.list, cmd = m.list.Update(msg)
	}
	return m, cmd
}

func (m Model) View() string {
	if m.quitting {
		return quitTextStyle.Render("Goodbye!")
	}
	if m.showingJSON {
		return docStyle.Render(m.jsonViewport.View())
	}
	if m.showingParsedEvents {
		return docStyle.Render(m.renderParsedEvents())
	}
	return m.list.View()
}

func waitForEvent(events <-chan snd.DiscordEventNotification) tea.Cmd {
	return func() tea.Msg {
		return <-events
	}
}

func (m *Model) renderParsedEvents() string {
	var content strings.Builder
	content.WriteString("Parsed Events (focusing on MESSAGE_CREATE and GUILD_CREATE):\n\n")

	for _, event := range m.parsedEvents {
		if event.Type == "MESSAGE_CREATE" || event.Type == "GUILD_CREATE" {
			content.WriteString(fmt.Sprintf("[%s] %s\n%s\n\n",
				event.Timestamp.Format("2006-01-02 15:04:05"),
				event.Type,
				event.Content))
		}
	}

	m.jsonViewport.SetContent(content.String())
	return m.jsonViewport.View()
}

var docStyle = lipgloss.NewStyle().Margin(1, 2)
