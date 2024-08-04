package tts

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/yourusername/yourproject/db"
)

type lineInfo struct {
	content   string
	startTime float64
}

type WordBuilder struct {
	lines            []lineInfo
	currentLine      strings.Builder
	currentStartTime float64
	lastWasEOS       bool
}

func (wb *WordBuilder) WriteWord(word TranscriptWord, isPartial bool) {
	if !wb.lastWasEOS && word.AttachesTo != "previous" {
		wb.currentLine.WriteString(" ")
	}

	style := lipgloss.NewStyle()
	if isPartial {
		style = style.Foreground(lipgloss.Color("240"))
	} else {
		style = style.Foreground(getConfidenceColor(word.Confidence))
	}

	wb.currentLine.WriteString(style.Render(word.Content))

	wb.lastWasEOS = word.IsEOS

	if wb.currentStartTime == -1 {
		wb.currentStartTime = word.StartTime
	}

	if word.IsEOS {
		wb.lines = append(wb.lines, lineInfo{
			content:   wb.currentLine.String(),
			startTime: wb.currentStartTime,
		})

		wb.currentLine.Reset()
		wb.currentStartTime = -1
	}
}

func (wb *WordBuilder) AppendWords(words []TranscriptWord, isPartial bool) {
	for _, word := range words {
		wb.WriteWord(word, isPartial)
	}

}

func (wb *WordBuilder) GetLines() []lineInfo {
	lines := wb.lines
	if wb.currentLine.Len() > 0 {
		lines = append(lines, lineInfo{
			content:   wb.currentLine.String(),
			startTime: wb.currentStartTime,
		})
	}
	return lines
}

type SessionTranscript struct {
	FinalTranscripts  [][]TranscriptWord
	CurrentTranscript []TranscriptWord
	LastStartTime     float64
}

type model struct {
	viewport    viewport.Model
	sessions    map[int64]*SessionTranscript
	logEntries  []string
	ready       bool
	transcripts chan TranscriptMessage
	showLog     bool
	dbQueries   *db.Queries
}

func initialModel(transcripts chan TranscriptMessage, dbQueries *db.Queries) model {
	m := model{
		sessions:    make(map[int64]*SessionTranscript),
		logEntries:  []string{},
		ready:       false,
		transcripts: transcripts,
		showLog:     false,
		dbQueries:   dbQueries,
	}
	
	// Load past hour's transcripts
	pastTranscripts, err := m.loadPastHourTranscripts()
	if err != nil {
		m.logEntries = append(m.logEntries, fmt.Sprintf("Error loading past transcripts: %v", err))
	} else {
		for _, transcript := range pastTranscripts {
			m.updateTranscript(transcript)
		}
	}
	
	return m
}

func (m *model) loadPastHourTranscripts() ([]TranscriptMessage, error) {
	oneHourAgo := time.Now().Add(-1 * time.Hour)
	
	// Fetch transcripts from the database
	segments, err := m.dbQueries.GetTranscriptsFromLastHour(context.Background(), oneHourAgo)
	if err != nil {
		return nil, err
	}
	
	var transcripts []TranscriptMessage
	for _, segment := range segments {
		words := make([]TranscriptWord, 0)
		for _, word := range segment.Words {
			words = append(words, TranscriptWord{
				Content:    word.Content,
				Confidence: word.Confidence,
				StartTime:  word.StartTime.Seconds(),
				EndTime:    word.StartTime.Seconds() + word.Duration.Seconds(),
				IsEOS:      word.IsEos,
				AttachesTo: word.AttachesTo.String,
			})
		}
		
		transcripts = append(transcripts, TranscriptMessage{
			SessionID: segment.SessionID,
			Words:     words,
			IsPartial: false,
		})
	}
	
	return transcripts, nil
}

func (m *model) updateTranscript(msg TranscriptMessage) {
	session, ok := m.sessions[msg.SessionID]
	if !ok {
		session = &SessionTranscript{}
		m.sessions[msg.SessionID] = session
	}

	if msg.IsPartial {
		session.CurrentTranscript = msg.Words
	} else {
		session.FinalTranscripts = append(session.FinalTranscripts, msg.Words)
		session.CurrentTranscript = []TranscriptWord{}
	}

	if len(msg.Words) > 0 {
		session.LastStartTime = msg.Words[0].StartTime
	}
}

func (m model) Init() tea.Cmd {
	return waitForTranscript(m.transcripts)
}

func waitForTranscript(transcripts chan TranscriptMessage) tea.Cmd {
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

	case TranscriptMessage:
		m.updateTranscript(msg)
		m.viewport.SetContent(m.contentView())
		m.viewport.GotoBottom()

		// Add log entry
		prefix := getLogPrefix(msg.IsPartial)
		logEntry := fmt.Sprintf("%s Session %d: %d words \"%s\"",
			prefix,
			msg.SessionID,
			len(msg.Words),
			formatTranscriptWords(msg.Words))
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
	var allLines []lineInfo
	for _, session := range m.sessions {
		wb := &WordBuilder{lastWasEOS: true, currentStartTime: -1}

		for _, transcript := range session.FinalTranscripts {
			wb.AppendWords(transcript, false)
		}

		if len(session.CurrentTranscript) > 0 {
			wb.AppendWords(session.CurrentTranscript, true)
		}

		sessionLines := wb.GetLines()
		allLines = append(allLines, sessionLines...)
	}

	// Sort lines by start time
	sort.Slice(allLines, func(i, j int) bool {
		return allLines[i].startTime < allLines[j].startTime
	})

	var result strings.Builder
	for _, line := range allLines {
		result.WriteString(line.content)
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

func getLogPrefix(isPartial bool) string {
	if isPartial {
		return "TMP"
	}
	return "FIN"
}
