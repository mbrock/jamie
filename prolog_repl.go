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
	baseStyle = lipgloss.NewStyle()

	titleStyle = baseStyle.Copy().
			Foreground(lipgloss.Color("#FFFF00")).
			Background(lipgloss.Color("#000088")).
			Bold(true).
			Padding(0, 2).
			Margin(0, 0).
			AlignHorizontal(lipgloss.Center)

	errorStyle = baseStyle.Copy().
			Foreground(lipgloss.Color("#FF0000")).
			Bold(true)

	promptStyle = baseStyle.Copy().
			Bold(true).
			Padding(0, 2)

	viewportStyle = lipgloss.NewStyle().
			Padding(1, 1).Border(lipgloss.RoundedBorder(), true)
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
	helpText := `Welcome to Jamie Prolog.

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.
`
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
				m.history = append(m.history, fmt.Sprintf("  ⎆ %s", queryStr))
				m.textInput.SetValue("")
				m.mode = "query"
				m.solutions = []map[string]trealla.Term{}
				m.iterateQuery(context.Background())

			case "ctrl+c", "esc":
				return m, tea.Quit

			default:
				m.textInput, cmd = m.textInput.Update(msg)
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
		m.viewport.Width = msg.Width - 1
		m.viewport.Height = msg.Height - 2
		m.textInput.Width = msg.Width - 1

	case error:
		m.err = msg
		m.history = append(m.history, fmt.Sprintf("Error: %v", msg))
		m.viewport.SetContent(lipgloss.JoinVertical(lipgloss.Left, m.history...))
		m.viewport.GotoBottom()
		return m, nil
	case string:
		m.history = append(m.history, msg)
		m.viewport.SetContent(lipgloss.JoinVertical(lipgloss.Left, m.history...))
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
		footer = promptStyle.Render(
			lipgloss.JoinHorizontal(
				lipgloss.Center,
				lipgloss.NewStyle().
					Bold(true).
					Underline(true).
					Foreground(lipgloss.Color("#888888")).
					Background(lipgloss.Color("#FFFFFF")).
					Render("n"),
				" Next  ",
				lipgloss.NewStyle().
					Bold(true).
					Underline(true).
					Foreground(lipgloss.Color("#888888")).
					Background(lipgloss.Color("#FFFFFF")).
					Render("a"),
				" Abort  ",
				lipgloss.NewStyle().
					Bold(true).
					Underline(true).
					Foreground(lipgloss.Color("#888888")).
					Background(lipgloss.Color("#FFFFFF")).
					Render("q"),
				" Quit",
			),
		)
	}

	content := m.viewport.View()

	titleWidth := m.viewport.Width
	leftText := "Jamie Prolog v3"
	rightText := "Unauthorized"
	spacing := titleWidth - lipgloss.Width(
		leftText,
	) - lipgloss.Width(
		rightText,
	) - 4

	title := titleStyle.Render(
		lipgloss.JoinHorizontal(
			lipgloss.Center,
			leftText,
			strings.Repeat(" ", spacing),
			rightText,
		),
	)

	return lipgloss.JoinVertical(
		lipgloss.Left,
		title,
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
			if !strings.HasPrefix(k, "_") {
				solutionStr += fmt.Sprintf("  ⦿ %v = %v", k, v)
			}
		}
		m.history = append(m.history, solutionStr)
	} else {
		if err := m.query.Err(); err != nil {
			m.history = append(m.history, errorStyle.Render(fmt.Sprintf("Error: %v", err)))
		} else {
			m.history = append(m.history, "  ◼︎")
		}
		m.query.Close()
		m.query = nil
		m.mode = "input"
	}
	m.viewport.SetContent(
		lipgloss.JoinVertical(lipgloss.Left, m.history...),
	)
	m.viewport.GotoBottom()
}
