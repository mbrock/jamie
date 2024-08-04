package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/trealla-prolog/go/trealla"
	"node.town/db"
)

var (
	baseStyle = lipgloss.NewStyle().
		Background(lipgloss.Color("#0A0A1F")).
		Foreground(lipgloss.Color("#F0F0FF"))

	titleStyle = baseStyle.Copy().
		Foreground(lipgloss.Color("#FF00FF")).
		Background(lipgloss.Color("#000033")).
		Bold(true).
		Padding(0, 1).
		BorderStyle(lipgloss.DoubleBorder()).
		BorderForeground(lipgloss.Color("#00FFFF"))

	infoStyle = baseStyle.Copy().
		Foreground(lipgloss.Color("#00FFFF"))

	errorStyle = baseStyle.Copy().
		Foreground(lipgloss.Color("#FF0000")).
		Bold(true)

	promptStyle = baseStyle.Copy().
		Foreground(lipgloss.Color("#FF00FF")).
		Bold(true)

	solutionStyle = baseStyle.Copy().
		Foreground(lipgloss.Color("#00FF00"))

	viewportStyle = baseStyle.Copy().
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#FF00FF"))
)

type model struct {
	textInput textinput.Model
	viewport  viewport.Model
	prolog    trealla.Prolog
	history   []string
	err       error
	query     trealla.Query
	mode      string // "input" or "query"
	solutions []map[string]trealla.Term
}

func initialModel(queries *db.Queries) model {
	ti := textinput.New()
	ti.Placeholder = "Enter Prolog query..."
	ti.Focus()
	ti.CharLimit = 156
	ti.Width = 80

	vp := viewport.New(80, 20)
	helpText := `Welcome to the Prolog REPL!
Enter your queries below.

For Prolog queries, simply type them and press Enter.
Use 'N' to get the next solution when in query mode.`
	vp.SetContent(helpText)
	vp.Style = viewportStyle

	prolog, err := trealla.New()
	if err != nil {
		return model{err: fmt.Errorf("failed to initialize Prolog: %w", err)}
	}

	err = prolog.ConsultText(
		context.Background(),
		"user",
		"use_module(library(lists)).",
	)
	if err != nil {
		return model{err: fmt.Errorf("failed to consult text: %w", err)}
	}

	RegisterDBQuery(prolog, context.Background(), queries)

	return model{
		textInput: ti,
		viewport:  vp,
		prolog:    prolog,
		history:   []string{},
		err:       nil,
		mode:      "input",
	}
}

func (m model) Init() tea.Cmd {
	return textinput.Blink
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch m.mode {
		case "input":
			switch msg.String() {
			case "enter":
				queryStr := m.textInput.Value()
				m.query = m.prolog.Query(context.Background(), queryStr)
				m.history = append(m.history, promptStyle.Render(fmt.Sprintf("Query: %s", queryStr)))
				m.textInput.SetValue("")
				m.mode = "query"
				m.solutions = []map[string]trealla.Term{}
				m.iterateQuery(context.Background())

			case "ctrl+c", "esc":
				return m, tea.Quit

			default:
				_, cmd = m.textInput.Update(msg)
				cmds = append(cmds, cmd)
			}
		case "query":
			switch msg.String() {
			case "enter", "n":
				m.iterateQuery(context.Background())

			case "ctrl+c", "esc", "q":
				m.query.Close()
				m.query = nil
				m.mode = "input"

			case "ctrl+d":
				return m, tea.Quit
			}
		}

	case tea.WindowSizeMsg:
		m.viewport.Width = msg.Width
		m.viewport.Height = msg.Height - 3 // Reserve space for input or button menu
		m.textInput.Width = msg.Width - 2

	case error:
		m.err = msg
		m.history = append(m.history, fmt.Sprintf("Error: %v", msg))
		m.viewport.SetContent(strings.Join(m.history, "\n\n"))
		m.viewport.GotoBottom()
		return m, nil
	case string:
		m.history = append(m.history, msg)
		m.viewport.SetContent(strings.Join(m.history, "\n\n"))
		m.viewport.GotoBottom()
	}

	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

// func (m model) startQuery(queryStr string) tea.Cmd {
// 	return func() tea.Msg {
// 		m.query = m.prolog.Query(context.Background(), queryStr)
// 		m.history = append(m.history, fmt.Sprintf("Query: %s", queryStr))
// 		m.textInput.SetValue("")
// 		m.mode = "query"
// 		return m
// 	}
// }

func (m model) View() string {
	if m.err != nil {
		return errorStyle.Render(fmt.Sprintf("Error: %v", m.err))
	}

	var footer string
	if m.mode == "input" {
		footer = promptStyle.Render(m.textInput.View())
	} else {
		footer = infoStyle.Render("[ N ] Next solution  [ A ] Abort query  [ Q ] Quit")
	}

	content := viewportStyle.Render(m.viewport.View())

	return lipgloss.JoinVertical(
		lipgloss.Left,
		titleStyle.Render(" Prolog REPL "),
		content,
		footer,
	)
}

func StartPrologREPL(queries *db.Queries) {
	p := tea.NewProgram(initialModel(queries))
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running program: %v", err)
	}
}
func (m *model) iterateQuery(ctx context.Context) {
	if m.query.Next(ctx) {
		solution := m.query.Current().Solution
		m.solutions = append(m.solutions, solution)
		solutionStr := ""
		for k, v := range solution {
			solutionStr += fmt.Sprintf("  %v = %v\n", k, v)
		}
		m.history = append(m.history, solutionStyle.Render(solutionStr))
	} else {
		if err := m.query.Err(); err != nil {
			m.history = append(m.history, errorStyle.Render(fmt.Sprintf("Error: %v", err)))
		} else {
			m.history = append(m.history, infoStyle.Render("No more solutions."))
		}
		m.query.Close()
		m.query = nil
		m.mode = "input"
	}
	m.viewport.SetContent(strings.Join(m.history, "\n"))
	m.viewport.GotoBottom()
}
