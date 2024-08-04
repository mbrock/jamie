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
	focusedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	blurredStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	cursorStyle  = focusedStyle.Copy()
	noStyle      = lipgloss.NewStyle()

	focusedButton = focusedStyle.Copy().Render("[ Submit ]")
	blurredButton = fmt.Sprintf("[ %s ]", blurredStyle.Render("Submit"))
)

type model struct {
	textInput textinput.Model
	viewport  viewport.Model
	prolog    trealla.Prolog
	history   []string
	err       error
	query     trealla.Query
}

func initialModel(queries *db.Queries) model {
	ti := textinput.New()
	ti.Placeholder = "Enter Prolog query..."
	ti.Focus()
	ti.CharLimit = 156
	ti.Width = 80

	vp := viewport.New(80, 20)
	vp.SetContent("Welcome to the Prolog REPL!\nEnter your queries below.")

	prolog, err := trealla.New()
	if err != nil {
		return model{err: fmt.Errorf("failed to initialize Prolog: %w", err)}
	}

	RegisterDBQuery(prolog, context.Background(), queries)

	return model{
		textInput: ti,
		viewport:  vp,
		prolog:    prolog,
		history:   []string{},
		err:       nil,
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
		switch msg.Type {
		case tea.KeyEnter:
			queryStr := m.textInput.Value()
			m.query, err = m.prolog.Query(context.Background(), queryStr)
			if err != nil {
				m.history = append(m.history, fmt.Sprintf("Error: %v", err))
			} else {
				m.history = append(m.history, fmt.Sprintf("Query: %s", queryStr))
				m.iterateQuery()
			}
			m.textInput.SetValue("")
			m.viewport.SetContent(strings.Join(m.history, "\n\n"))
			m.viewport.GotoBottom()
		case tea.KeyComma:
			if m.query != nil {
				m.iterateQuery()
				m.viewport.SetContent(strings.Join(m.history, "\n\n"))
				m.viewport.GotoBottom()
			}
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		}

	case tea.WindowSizeMsg:
		m.viewport.Width = msg.Width
		m.viewport.Height = msg.Height - 3 // Reserve space for input
		m.textInput.Width = msg.Width - 2

	case error:
		m.err = msg
		return m, nil
	}

	m.textInput, cmd = m.textInput.Update(msg)
	cmds = append(cmds, cmd)
	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m *model) iterateQuery() {
	if m.query.Next(context.Background()) {
		solution := m.query.Current()
		m.history = append(m.history, fmt.Sprintf("Result: %v", solution))
	} else {
		if err := m.query.Err(); err != nil {
			m.history = append(m.history, fmt.Sprintf("Error: %v", err))
		} else {
			m.history = append(m.history, "No more solutions.")
		}
		m.query.Close()
		m.query = nil
	}
}

func (m model) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v", m.err)
	}

	return fmt.Sprintf(
		"%s\n\n%s",
		m.viewport.View(),
		m.textInput.View(),
	) + "\n"
}

func StartPrologREPL(queries *db.Queries) {
	p := tea.NewProgram(initialModel(queries))
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running program: %v", err)
	}
}
