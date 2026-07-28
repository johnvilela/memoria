package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// isTTY reports whether stdin is a terminal. Var so tests can force the
// non-interactive path.
var isTTY = func() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

type option struct{ value, label, desc string }

var (
	tuiTitle  = lipgloss.NewStyle().Bold(true)
	tuiCursor = lipgloss.NewStyle().Foreground(lipgloss.Color("212"))
	tuiFaint  = lipgloss.NewStyle().Faint(true)
)

type selectModel struct {
	title   string
	opts    []option
	cursor  int
	choice  string
	aborted bool
}

func (m selectModel) Init() tea.Cmd { return nil }

func (m selectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.opts)-1 {
				m.cursor++
			}
		case "enter":
			m.choice = m.opts[m.cursor].value
			return m, tea.Quit
		case "esc", "ctrl+c", "q":
			m.aborted = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m selectModel) View() string {
	var b strings.Builder
	b.WriteString(tuiTitle.Render(m.title) + "\n\n")
	for i, o := range m.opts {
		cursor := "  "
		if i == m.cursor {
			cursor = tuiCursor.Render("> ")
		}
		line := o.label
		if o.desc != "" {
			line += " " + tuiFaint.Render("— "+o.desc)
		}
		b.WriteString(cursor + line + "\n")
	}
	b.WriteString(tuiFaint.Render("\n↑/↓ move · enter select · esc cancel") + "\n")
	return b.String()
}

// selectOption runs an interactive single-choice select and returns the
// chosen option's value.
func selectOption(title string, opts []option) (string, error) {
	res, err := tea.NewProgram(selectModel{title: title, opts: opts}).Run()
	if err != nil {
		return "", err
	}
	m := res.(selectModel)
	if m.aborted {
		return "", fmt.Errorf("aborted")
	}
	return m.choice, nil
}

type secretModel struct {
	input   textinput.Model
	aborted bool
}

func (m secretModel) Init() tea.Cmd { return textinput.Blink }

func (m secretModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "enter":
			return m, tea.Quit
		case "esc", "ctrl+c":
			m.aborted = true
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m secretModel) View() string {
	return m.input.View() + tuiFaint.Render("\nenter confirm · esc cancel") + "\n"
}

// promptSecret asks for a masked value (API keys).
func promptSecret(title string) (string, error) {
	in := textinput.New()
	in.Prompt = title + ": "
	in.EchoMode = textinput.EchoPassword
	in.Focus()
	res, err := tea.NewProgram(secretModel{input: in}).Run()
	if err != nil {
		return "", err
	}
	m := res.(secretModel)
	if m.aborted {
		return "", fmt.Errorf("aborted")
	}
	return strings.TrimSpace(m.input.Value()), nil
}
