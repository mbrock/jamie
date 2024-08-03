package tts

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type model struct {
	viewport    viewport.Model
	messages    [][]TranscriptWord
	currentLine []TranscriptWord
	ready       bool
	transcripts chan TranscriptMessage
}

func initialModel(transcripts chan TranscriptMessage) model {
	return model{
		messages:    [][]TranscriptWord{},
		currentLine: []TranscriptWord{},
		ready:       false,
		transcripts: transcripts,
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if k := msg.String(); k == "ctrl+c" || k == "q" || k == "esc" {
			return m, tea.Quit
		}

	case tea.WindowSizeMsg:
		headerHeight := lipgloss.Height(m.headerView())
		footerHeight := lipgloss.Height(m.footerView())
		verticalMarginHeight := headerHeight + footerHeight

		if !m.ready {
			m.viewport = viewport.New(msg.Width, msg.Height-verticalMarginHeight)
			m.viewport.YPosition = headerHeight
			m.viewport.SetContent(m.contentView())
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = msg.Height - verticalMarginHeight
		}

	case transcriptMsg:
		if msg.IsPartial {
			m.currentLine = msg.Words
		} else if msg.AttachesTo == "previous" {
			m = m.updatePreviousLine(msg.Words)
		} else {
			// For final transcripts, update the current line and add it to messages
			m.currentLine = msg.Words
			m.messages = append(m.messages, m.currentLine)
			// Start a new empty current line
			m.currentLine = []TranscriptWord{}
		}
		m.viewport.SetContent(m.contentView())
		m.viewport.GotoBottom()
	}

	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m model) View() string {
	if !m.ready {
		return "\n  Initializing..."
	}
	return fmt.Sprintf(
		"%s\n%s\n%s",
		m.headerView(),
		m.viewport.View(),
		m.footerView(),
	)
}

func (m model) headerView() string {
	title := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFDF5")).
		Background(lipgloss.Color("#25A065")).
		Padding(0, 1).
		Render("Real-time Transcription")
	line := strings.Repeat(
		"─",
		max(0, m.viewport.Width-lipgloss.Width(title)),
	)
	return lipgloss.JoinHorizontal(lipgloss.Center, title, line)
}

func (m model) footerView() string {
	info := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFDF5")).
		Background(lipgloss.Color("#25A065")).
		Padding(0, 1).
		Render("Press q to quit")
	line := strings.Repeat("─", max(0, m.viewport.Width-lipgloss.Width(info)))
	return lipgloss.JoinHorizontal(lipgloss.Center, line, info)
}

func (m model) contentView() string {
	var content strings.Builder
	for _, line := range m.messages {
		content.WriteString(formatWords(line))
		content.WriteString("\n")
	}
	if len(m.currentLine) > 0 {
		content.WriteString(formatWords(m.currentLine))
	}
	return content.String()
}

func formatWords(words []TranscriptWord) string {
	var line strings.Builder
	for _, word := range words {
		color := getConfidenceColor(word.Confidence)
		line.WriteString(lipgloss.NewStyle().Foreground(color).Render(word.Content))
		line.WriteString(" ")
	}
	return strings.TrimSpace(line.String())
}

func getConfidenceColor(confidence float64) lipgloss.Color {
	switch {
	case confidence >= 0.9:
		return lipgloss.Color("#00FF00") // Green
	case confidence >= 0.7:
		return lipgloss.Color("#FFFF00") // Yellow
	default:
		return lipgloss.Color("#FF0000") // Red
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (m model) updatePreviousLine(words []TranscriptWord) model {
	if len(m.messages) > 0 {
		lastIndex := len(m.messages) - 1
		m.messages[lastIndex] = words
	} else {
		// If there are no previous messages, treat it as a new message
		m.messages = append(m.messages, words)
	}
	return m
}

type transcriptMsg TranscriptMessage

func listenForTranscripts(transcripts chan TranscriptMessage) tea.Cmd {
	return func() tea.Msg {
		return transcriptMsg(<-transcripts)
	}
}

func StartUI(transcripts chan TranscriptMessage) error {
	p := tea.NewProgram(
		initialModel(transcripts),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	_, err := p.Run()
	return err
}
