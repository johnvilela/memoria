package main

import (
	"fmt"
	"os"
	"strings"
	"time"

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
// chosen option's value. Var so tests can stub the picker.
var selectOption = func(title string, opts []option) (string, error) {
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

type multiModel struct {
	title   string
	opts    []option
	cursor  int
	checked []bool
	aborted bool
}

func (m multiModel) Init() tea.Cmd { return nil }

func (m multiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		case " ":
			m.checked[m.cursor] = !m.checked[m.cursor]
		case "enter":
			return m, tea.Quit
		case "esc", "ctrl+c", "q":
			m.aborted = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m multiModel) View() string {
	var b strings.Builder
	b.WriteString(tuiTitle.Render(m.title) + "\n\n")
	for i, o := range m.opts {
		cursor := "  "
		if i == m.cursor {
			cursor = tuiCursor.Render("> ")
		}
		box := "[ ] "
		if m.checked[i] {
			box = "[x] "
		}
		line := o.label
		if o.desc != "" {
			line += " " + tuiFaint.Render("— "+o.desc)
		}
		b.WriteString(cursor + box + line + "\n")
	}
	b.WriteString(tuiFaint.Render("\n↑/↓ move · space toggle · enter confirm · esc cancel") + "\n")
	return b.String()
}

// selectMulti runs an interactive multi-choice select and returns the checked
// values in option order (nil when nothing was checked). Var so tests can
// stub the picker.
var selectMulti = func(title string, opts []option) ([]string, error) {
	res, err := tea.NewProgram(multiModel{title: title, opts: opts, checked: make([]bool, len(opts))}).Run()
	if err != nil {
		return nil, err
	}
	m := res.(multiModel)
	if m.aborted {
		return nil, fmt.Errorf("aborted")
	}
	var vals []string
	for i, o := range m.opts {
		if m.checked[i] {
			vals = append(vals, o.value)
		}
	}
	return vals, nil
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

// withSpinner runs fn while animating a spinner on stderr, clearing the line
// when fn returns. ponytail: plain goroutine, no bubbletea program needed.
func withSpinner(msg string, fn func() error) error {
	done := make(chan struct{})
	cleared := make(chan struct{})
	go func() {
		defer close(cleared)
		frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		for i := 0; ; i++ {
			select {
			case <-done:
				fmt.Fprint(os.Stderr, "\r\033[K")
				return
			case <-time.After(100 * time.Millisecond):
				fmt.Fprintf(os.Stderr, "\r%s %s", tuiCursor.Render(frames[i%len(frames)]), msg)
			}
		}
	}()
	err := fn()
	close(done)
	<-cleared
	return err
}

// promptSecret asks for a masked value (API keys).
func promptSecret(title string) (string, error) {
	return promptInput(title, textinput.EchoPassword)
}

// promptText asks for a plain-echo value (cron expressions).
func promptText(title string) (string, error) {
	return promptInput(title, textinput.EchoNormal)
}

func promptInput(title string, echo textinput.EchoMode) (string, error) {
	in := textinput.New()
	in.Prompt = title + ": "
	in.EchoMode = echo
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
