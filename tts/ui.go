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
	messages    []string
	currentLine string
	ready       bool
	transcripts chan TranscriptMessage
}

func initialModel(transcripts chan TranscriptMessage) model {
	return model{
		messages:    []string{},
		currentLine: "",
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
			m.currentLine = msg.Text
		} else {
			if m.currentLine != "" {
				m.messages = append(m.messages, m.currentLine)
			}
			m.messages = append(m.messages, msg.Text)
			m.currentLine = ""
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
	content := strings.Join(m.messages, "\n")
	if m.currentLine != "" {
		content += "\n" + m.currentLine
	}
	return content
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
