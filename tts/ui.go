package tts

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type model struct {
	leftViewport  viewport.Model
	rightViewport viewport.Model
	messages      [][]TranscriptWord
	currentLine   []TranscriptWord
	logEntries    []string
	ready         bool
	transcripts   chan TranscriptMessage
}

func initialModel(transcripts chan TranscriptMessage) model {
	return model{
		messages:    [][]TranscriptWord{},
		currentLine: []TranscriptWord{},
		logEntries:  []string{},
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
			halfWidth := msg.Width / 2
			m.leftViewport = viewport.New(halfWidth, msg.Height-verticalMarginHeight)
			m.rightViewport = viewport.New(msg.Width-halfWidth, msg.Height-verticalMarginHeight)
			m.leftViewport.YPosition = headerHeight
			m.rightViewport.YPosition = headerHeight
			m.leftViewport.SetContent(m.leftContentView())
			m.rightViewport.SetContent(m.rightContentView())
			m.ready = true
		} else {
			halfWidth := msg.Width / 2
			m.leftViewport.Width = halfWidth
			m.rightViewport.Width = msg.Width - halfWidth
			m.leftViewport.Height = msg.Height - verticalMarginHeight
			m.rightViewport.Height = msg.Height - verticalMarginHeight
		}

	case transcriptMsg:
		if msg.IsPartial {
			m.currentLine = msg.Words
		} else {
			// For final transcripts
			if msg.AttachesTo == "previous" && len(m.messages) > 0 {
				lastIndex := len(m.messages) - 1
				m.messages[lastIndex] = append(m.messages[lastIndex], TranscriptWord{Content: "[ATTACHED]"})
				m.messages[lastIndex] = append(m.messages[lastIndex], msg.Words...)
			} else {
				// Update the current line and add it to messages
				m.currentLine = msg.Words
				m.messages = append(m.messages, m.currentLine)
				// Start a new empty current line
				m.currentLine = []TranscriptWord{}
			}
		}
		m.leftViewport.SetContent(m.leftContentView())
		m.leftViewport.GotoBottom()

		// Add log entry
		logEntry := fmt.Sprintf("Transcript: %d words, Partial: %v", len(msg.Words), msg.IsPartial)
		m.logEntries = append(m.logEntries, logEntry)
		m.rightViewport.SetContent(m.rightContentView())
		m.rightViewport.GotoBottom()
	}

	m.leftViewport, cmd = m.leftViewport.Update(msg)
	cmds = append(cmds, cmd)
	m.rightViewport, cmd = m.rightViewport.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m model) View() string {
	if !m.ready {
		return "\n  Initializing..."
	}
	return fmt.Sprintf(
		"%s\n%s%s\n%s",
		m.headerView(),
		m.leftViewport.View(),
		m.rightViewport.View(),
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

func (m model) leftContentView() string {
	var content strings.Builder
	for _, msg := range m.messages {
		content.WriteString(formatWords(msg))
		content.WriteString("\n")
	}
	if len(m.currentLine) > 0 {
		content.WriteString("[CUR] ")
		content.WriteString(formatWords(m.currentLine))
	}
	return content.String()
}

func (m model) rightContentView() string {
	var content strings.Builder
	for _, entry := range m.logEntries {
		content.WriteString(entry)
		content.WriteString("\n")
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
