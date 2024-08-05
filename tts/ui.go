package tts

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"node.town/db"
)

type SessionTranscript struct {
	FinalTranscript   []TranscriptWord
	CurrentTranscript []TranscriptWord
}

type model struct {
	viewport    viewport.Model
	sessions    map[int64]*SessionTranscript
	logEntries  []string
	ready       bool
	transcripts chan TranscriptSegment
	showLog     bool
	dbQueries   *db.Queries
}

func initialModel(
	transcripts chan TranscriptSegment,
	dbQueries *db.Queries,
) model {
	m := model{
		sessions:    make(map[int64]*SessionTranscript),
		logEntries:  []string{},
		ready:       false,
		transcripts: transcripts,
		showLog:     false,
		dbQueries:   dbQueries,
	}

	// Load recent transcripts
	recentTranscripts, err := LoadRecentTranscripts(dbQueries)
	if err != nil {
		m.logEntries = append(
			m.logEntries,
			fmt.Sprintf("Error loading recent transcripts: %v", err),
		)
	} else {
		for _, transcript := range recentTranscripts {
			m.updateTranscript(transcript)
		}
	}

	return m
}

func (m *model) updateTranscript(msg TranscriptSegment) {
	session, ok := m.sessions[msg.SessionID]
	if !ok {
		session = &SessionTranscript{}
		m.sessions[msg.SessionID] = session
	}

	if !msg.IsFinal {
		session.CurrentTranscript = msg.Words
	} else {
		session.FinalTranscript = append(session.FinalTranscript, msg.Words...)
		session.CurrentTranscript = []TranscriptWord{}
	}
}

func (m model) Init() tea.Cmd {
	return waitForTranscript(m.transcripts)
}

func waitForTranscript(transcripts chan TranscriptSegment) tea.Cmd {
	return func() tea.Msg {
		return <-transcripts
	}
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

	case TranscriptSegment:
		m.updateTranscript(msg)
		m.viewport.SetContent(m.contentView())
		m.viewport.GotoBottom()

		// Add log entry
		prefix := getLogPrefix(!msg.IsFinal)
		logEntry := fmt.Sprintf(
			"%s Session %d: %d words \"%s\"",
			prefix,
			msg.SessionID,
			len(msg.Words),
			formatTranscriptWordsForLog(msg.Words))
		m.logEntries = append(m.logEntries, logEntry)

		cmds = append(cmds, waitForTranscript(m.transcripts))
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
	return m.TranscriptView()
}

func (m model) TranscriptView() string {
	var allBuilders []*TranscriptBuilder
	for _, session := range m.sessions {
		builder := NewTranscriptBuilder()
		builder.AppendWords(session.FinalTranscript, false)
		builder.AppendWords(session.CurrentTranscript, true)
		allBuilders = append(allBuilders, builder)
	}

	var allLines []Line
	for _, builder := range allBuilders {
		allLines = append(allLines, builder.GetLines()...)
	}

	sort.Slice(allLines, func(i, j int) bool {
		return allLines[i].StartTime.Before(allLines[j].StartTime)
	})

	var result strings.Builder
	for _, line := range allLines {
		result.WriteString(
			fmt.Sprintf("(%s)", line.StartTime.Format("15:04:05")),
		)
		for i, span := range line.Spans {
			if i == 0 {
				result.WriteString(" ")
			}
			result.WriteString(span.Style.Render(span.Content))
		}
		result.WriteString("\n")
	}

	return result.String()
}

func (m model) logView() string {
	var content strings.Builder
	for _, entry := range m.logEntries {
		content.WriteString(entry)
		content.WriteString("\n")
	}
	return content.String()
}

func formatTranscriptWordsForLog(words []TranscriptWord) string {
	var content strings.Builder
	for _, word := range words {
		content.WriteString(word.Content)
		content.WriteString(" ")
	}
	return strings.TrimSpace(content.String())
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func getLogPrefix(isPartial bool) string {
	if isPartial {
		return "TMP"
	}
	return "FIN"
}
