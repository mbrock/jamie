package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/trealla-prolog/go/trealla"
	"node.town/db"
)

type model struct {
	textInput textinput.Model
	viewport  viewport.Model
	prolog    trealla.Prolog
	history   []string
	err       error
	query     trealla.Query
	mode      string // "input" or "query"
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
			switch msg.Type {
			case tea.KeyEnter:
				queryStr := m.textInput.Value()
				m.query = m.prolog.Query(context.Background(), queryStr)
				m.history = append(m.history, fmt.Sprintf("Query: %s", queryStr))
				m.textInput.SetValue("")
				m.mode = "query"
				return m, m.iterateQuery()
			case tea.KeyCtrlC, tea.KeyEsc:
				return m, tea.Quit
			}
		case "query":
			switch msg.String() {
			case "n", "N":
				return m, m.iterateQuery()
			case "a", "A":
				m.query.Close()
				m.query = nil
				m.mode = "input"
			case "q", "Q":
				return m, tea.Quit
			}
		}

	case tea.WindowSizeMsg:
		m.viewport.Width = msg.Width
		m.viewport.Height = msg.Height - 3 // Reserve space for input or button menu
		m.textInput.Width = msg.Width - 2

	case error:
		m.err = msg
		return m, nil
	}

	if m.mode == "input" {
		m.textInput, cmd = m.textInput.Update(msg)
		cmds = append(cmds, cmd)
	}
	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func iterateQueryCmd(m model) tea.Msg {
	if m.query.Next(context.Background()) {
		solution := m.query.Current().Solution
		for k, v := range solution {
			m.history = append(m.history, fmt.Sprintf("  %v = %v", k, v))
		}
	} else {
		if err := m.query.Err(); err != nil {
			m.history = append(m.history, fmt.Sprintf("Error: %v", err))
		} else {
			m.history = append(m.history, "No more solutions.")
		}
		m.query.Close()
		m.query = nil
		m.mode = "input"
	}
	m.viewport.SetContent(strings.Join(m.history, "\n\n"))
	m.viewport.GotoBottom()
	return m
}

func (m model) iterateQuery() tea.Cmd {
	return func() tea.Msg {
		return iterateQueryCmd(m)
	}
}

func (m model) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v", m.err)
	}

	var footer string
	if m.mode == "input" {
		footer = m.textInput.View()
	} else {
		footer = "[ N ] Next solution  [ A ] Abort query  [ Q ] Quit"
	}

	return fmt.Sprintf(
		"%s\n\n%s",
		m.viewport.View(),
		footer,
	) + "\n"
}

func StartPrologREPL(queries *db.Queries) {
	p := tea.NewProgram(initialModel(queries))
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running program: %v", err)
	}
}
