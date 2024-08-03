package tts

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type model struct {
	viewport          viewport.Model
	finalTranscripts  [][]TranscriptWord
	currentTranscript []TranscriptWord
	logEntries        []string
	ready             bool
	transcripts       chan TranscriptMessage
	showLog           bool
}

func initialModel(transcripts chan TranscriptMessage) model {
	return model{
		finalTranscripts:  [][]TranscriptWord{},
		currentTranscript: []TranscriptWord{},
		logEntries:        []string{},
		ready:             false,
		transcripts:       transcripts,
		showLog:           false,
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
			m.currentTranscript = msg.Words
		} else {
			// For final transcripts
			if msg.AttachesTo == "previous" && len(m.finalTranscripts) > 0 {
				// Update the previous transcript
				lastIndex := len(m.finalTranscripts) - 1
				m.finalTranscripts[lastIndex] = append(m.finalTranscripts[lastIndex], msg.Words...)
			} else {
				// Add a new final transcript
				m.finalTranscripts = append(m.finalTranscripts, msg.Words)
			}
			m.currentTranscript = []TranscriptWord{}
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
	for _, transcript := range m.finalTranscripts {
		content.WriteString(
			formatWords(transcript, lipgloss.Color("0")),
		) // No background for final transcripts
	}
	if len(m.currentTranscript) > 0 {
		s := content.String()
		if len(m.finalTranscripts) > 0 {
			matched, err := regexp.MatchString(`\w$`, s)
			if err != nil {
				panic(err)
			}

			if matched && len(m.currentTranscript) > 0 &&
				m.currentTranscript[0].Type == "word" {
				content.WriteString(" -- ")
			}
		}
		content.WriteString(
			formatWords(m.currentTranscript, lipgloss.Color("236")),
		) // Dark gray background for current transcript
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

func formatWords(words []TranscriptWord, bgColor lipgloss.Color) string {
	var line strings.Builder
	lastWasEOS := true
	for _, word := range words {
		color := getConfidenceColor(word.Confidence)
		if !lastWasEOS && word.Type == "word" {
			line.WriteString(" ")
		}
		style := lipgloss.NewStyle().
			Foreground(color).
			Background(bgColor)
		
		if word.AttachesTo == "previous" {
			style = style.Underline(true)
		}
		
		line.WriteString(style.Render(word.Content))
		
		if word.IsEOS {
			line.WriteString("\n")
			lastWasEOS = true
		} else {
			lastWasEOS = false
		}
	}
	return line.String()
}

func getConfidenceColor(confidence float64) lipgloss.Color {
	switch {
	case confidence >= 0.9:
		return lipgloss.Color("#FFFFFF")
	case confidence >= 0.8:
		return lipgloss.Color("#FFFF00")
	default:
		return lipgloss.Color("#FF0000")
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

func getLogPrefix(isPartial bool) string {
	if isPartial {
		return "TMP"
	}
	return "FIN"
}
