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
	sentences   [][]TranscriptWord
	currentSentence []TranscriptWord
	logEntries  []string
	ready       bool
	transcripts chan TranscriptMessage
	showLog     bool
}

func initialModel(transcripts chan TranscriptMessage) model {
	return model{
		sentences:       [][]TranscriptWord{},
		currentSentence: []TranscriptWord{},
		logEntries:      []string{},
		ready:           false,
		transcripts:     transcripts,
		showLog:         false,
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
			m.currentSentence = msg.Words
		} else {
			// For final transcripts
			if msg.AttachesTo == "previous" && len(m.sentences) > 0 {
				// Update the previous sentence
				lastIndex := len(m.sentences) - 1
				m.sentences[lastIndex] = append(m.sentences[lastIndex], msg.Words...)
			} else {
				// Update the current sentence and add it to sentences
				m.currentSentence = append(m.currentSentence, msg.Words...)
			}
			
			// Check for end of sentence
			for i, word := range m.currentSentence {
				if word.IsEOS {
					m.sentences = append(m.sentences, m.currentSentence[:i+1])
					if i+1 < len(m.currentSentence) {
						m.currentSentence = m.currentSentence[i+1:]
					} else {
						m.currentSentence = []TranscriptWord{}
					}
					break
				}
			}
		}
		m.viewport.SetContent(m.contentView())
		m.viewport.GotoBottom()

		// Add log entry
		prefix := getLogPrefix(msg.IsPartial)
		if msg.AttachesTo != "" {
			prefix += fmt.Sprintf(" (%s)", msg.AttachesTo)
		}
		logEntry := fmt.Sprintf("%s %d \"%s\"",
			prefix,
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
	for _, sentence := range m.sentences {
		content.WriteString(formatWords(sentence))
		content.WriteString("\n")
	}
	if len(m.currentSentence) > 0 {
		content.WriteString(formatWords(m.currentSentence))
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
	for i, word := range words {
		color := getConfidenceColor(word.Confidence)
		if i > 0 && word.Type == "word" {
			line.WriteString(" ")
		}
		line.WriteString(
			lipgloss.NewStyle().Foreground(color).Render(word.Content),
		)
		if word.IsEOS {
			line.WriteString(" ")
		}
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
