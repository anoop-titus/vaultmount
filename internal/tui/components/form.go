package components

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	focusedLabelStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("33")).Bold(true).Width(18)
	blurredLabelStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Width(18)
	errorStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	requiredMark       = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("*")
	optionalMark       = lipgloss.NewStyle().Foreground(lipgloss.Color("238")).Render(" ")
)

// FieldDef describes a form field's configuration.
type FieldDef struct {
	Key         string
	Label       string
	Placeholder string
	Required    bool
	Secret      bool
	Default     string
}

// Form is a multi-field text input form.
type Form struct {
	fields  []FieldDef
	inputs  []textinput.Model
	focused int
	Width   int
	errors  []string
}

// NewForm creates a Form from field definitions.
func NewForm(fields []FieldDef) Form {
	inputs := make([]textinput.Model, len(fields))
	for i, f := range fields {
		ti := textinput.New()
		ti.Placeholder = f.Placeholder
		ti.CharLimit = 512
		if f.Secret {
			ti.EchoMode = textinput.EchoPassword
		}
		if f.Default != "" {
			ti.SetValue(f.Default)
		}
		inputs[i] = ti
	}
	if len(inputs) > 0 {
		inputs[0].Focus()
	}
	return Form{fields: fields, inputs: inputs, focused: 0, Width: 80}
}

// SetValue sets the value of the field with the given key.
func (f *Form) SetValue(key, value string) {
	for i, fd := range f.fields {
		if fd.Key == key {
			f.inputs[i].SetValue(value)
			return
		}
	}
}

// Update handles keyboard events for the form.
func (f Form) Update(msg tea.Msg) (Form, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyTab, tea.KeyDown:
			f.inputs[f.focused].Blur()
			f.focused = (f.focused + 1) % len(f.inputs)
			f.inputs[f.focused].Focus()
			return f, nil
		case tea.KeyShiftTab, tea.KeyUp:
			f.inputs[f.focused].Blur()
			f.focused = (f.focused - 1 + len(f.inputs)) % len(f.inputs)
			f.inputs[f.focused].Focus()
			return f, nil
		}
	}
	var cmd tea.Cmd
	f.inputs[f.focused], cmd = f.inputs[f.focused].Update(msg)
	return f, cmd
}

// View renders the form.
func (f Form) View() string {
	var sb strings.Builder
	for i, fd := range f.fields {
		mark := optionalMark
		if fd.Required {
			mark = requiredMark
		}
		var lbl string
		if i == f.focused {
			lbl = focusedLabelStyle.Render(fd.Label) + mark
		} else {
			lbl = blurredLabelStyle.Render(fd.Label) + mark
		}
		sb.WriteString(lbl + "  " + f.inputs[i].View() + "\n")
	}
	if len(f.errors) > 0 {
		sb.WriteString("\n")
		for _, e := range f.errors {
			sb.WriteString(errorStyle.Render("  ✗ "+e) + "\n")
		}
	}
	return sb.String()
}

// Values returns a map of key → current input value.
func (f Form) Values() map[string]string {
	m := make(map[string]string, len(f.fields))
	for i, fd := range f.fields {
		m[fd.Key] = strings.TrimSpace(f.inputs[i].Value())
	}
	return m
}

// Validate checks required fields and returns error messages.
func (f *Form) Validate() []string {
	f.errors = nil
	vals := f.Values()
	for _, fd := range f.fields {
		if fd.Required && strings.TrimSpace(vals[fd.Key]) == "" {
			f.errors = append(f.errors, fd.Label+" is required")
		}
	}
	return f.errors
}

// FocusedIndex returns the index of the currently focused field.
func (f Form) FocusedIndex() int { return f.focused }

// Len returns the number of fields.
func (f Form) Len() int { return len(f.fields) }
