package main

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbles/textinput"
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
	prolog    trealla.Prolog
	output    string
	err       error
}

func initialModel(queries *db.Queries) model {
	ti := textinput.New()
	ti.Placeholder = "Enter Prolog query..."
	ti.Focus()
	ti.CharLimit = 156
	ti.Width = 80

	prolog, err := trealla.New()
	if err != nil {
		return model{err: fmt.Errorf("failed to initialize Prolog: %w", err)}
	}

	RegisterDBQuery(prolog, context.Background(), queries)

	return model{
		textInput: ti,
		prolog:    prolog,
		err:       nil,
	}
}

func (m model) Init() tea.Cmd {
	return textinput.Blink
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEnter:
			query := m.textInput.Value()
			answer, err := m.prolog.QueryOnce(context.Background(), query)
			if err != nil {
				m.output = fmt.Sprintf("Error: %v", err)
			} else {
				m.output = fmt.Sprintf("Result: %v", answer.Solution)
			}
			m.textInput.SetValue("")
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		}

	// We handle errors just like any other message
	case error:
		m.err = msg
		return m, nil
	}

	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m model) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v", m.err)
	}

	return fmt.Sprintf(
		"Enter your Prolog query:\n\n%s\n\n%s\n\n%s",
		m.textInput.View(),
		focusedButton,
		m.output,
	) + "\n"
}

func StartPrologREPL(queries *db.Queries) {
	p := tea.NewProgram(initialModel(queries))
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running program: %v", err)
	}
}
