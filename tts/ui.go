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
	messages    []struct {
		Words      []TranscriptWord
		AttachesTo string
	}
	currentLine struct {
		Words      []TranscriptWord
		AttachesTo string
	}
	logEntries  []string
	ready       bool
	transcripts chan TranscriptMessage
	showLog     bool
}

func initialModel(transcripts chan TranscriptMessage) model {
	return model{
		messages:    [][]TranscriptWord{},
		currentLine: []TranscriptWord{},
		logEntries:  []string{},
		ready:       false,
		transcripts: transcripts,
		showLog:     false,
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
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			return m, tea.Quit
		case "tab":
			m.showLog = !m.showLog
			m.viewport.SetContent(m.contentView())
			return m, nil
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
			m.currentLine.Words = msg.Words
			m.currentLine.AttachesTo = msg.AttachesTo
		} else {
			// For final transcripts
			// Update the current line and add it to messages
			m.currentLine.Words = msg.Words
			m.currentLine.AttachesTo = msg.AttachesTo
			m.messages = append(m.messages, m.currentLine)
			// Start a new empty current line
			m.currentLine = struct {
				Words      []TranscriptWord
				AttachesTo string
			}{}
		}
		m.viewport.SetContent(m.contentView())
		m.viewport.GotoBottom()

		// Add log entry
		logEntry := fmt.Sprintf("%s %d \"%s\"",
			getLogPrefix(msg.IsPartial),
			len(msg.Words),
			formatTranscriptWords(msg.Words))
		m.logEntries = append(m.logEntries, logEntry)
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
		Render("Press q to quit, Tab to switch views")
	line := strings.Repeat("─", max(0, m.viewport.Width-lipgloss.Width(info)))
	return lipgloss.JoinHorizontal(lipgloss.Center, line, info)
}

func (m model) contentView() string {
	if m.showLog {
		return m.logView()
	}
	return m.transcriptView()
}

func (m model) transcriptView() string {
	var content strings.Builder
	for _, msg := range m.messages {
		content.WriteString(fmt.Sprintf("[SSRC %s] ", msg.AttachesTo))
		content.WriteString(formatWords(msg.Words))
		content.WriteString("\n")
	}
	if len(m.currentLine.Words) > 0 {
		content.WriteString(fmt.Sprintf("[CUR SSRC %s] ", m.currentLine.AttachesTo))
		content.WriteString(formatWords(m.currentLine.Words))
	}
	return content.String()
}

func (m model) logView() string {
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
		line.WriteString(
			lipgloss.NewStyle().Foreground(color).Render(word.Content),
		)
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

func getLogPrefix(isPartial bool) string {
	if isPartial {
		return "TMP"
	}
	return "FIN"
}
