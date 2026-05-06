package pages

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	sshpkg "github.com/titusmd/vaultmount/internal/ssh"
	"github.com/titusmd/vaultmount/internal/tui/components"
)

type sshAction int

const (
	sshActionNone sshAction = iota
	sshActionGenerate
	sshActionCopy
	sshActionTest
)

var (
	sshTitleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("33")).Padding(0, 1)
	sshHintStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	sshSuccessStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("46"))
	sshErrStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	keyListStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	keySelStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("33")).Bold(true)
	sshSecStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Bold(true).MarginTop(1)
)

var generateFields = []components.FieldDef{
	{Key: "path", Label: "Key Path", Placeholder: "~/.ssh/id_ed25519", Required: true, Default: "~/.ssh/id_ed25519"},
	{Key: "comment", Label: "Comment", Placeholder: "user@host"},
}

var copyFields = []components.FieldDef{
	{Key: "host", Label: "Host", Placeholder: "192.168.1.1", Required: true},
	{Key: "port", Label: "Port", Placeholder: "22", Default: "22"},
	{Key: "user", Label: "Username", Placeholder: "ubuntu", Required: true},
}

var testFields = []components.FieldDef{
	{Key: "host", Label: "Host", Placeholder: "192.168.1.1", Required: true},
	{Key: "port", Label: "Port", Placeholder: "22", Default: "22"},
	{Key: "user", Label: "Username", Placeholder: "ubuntu", Required: true},
}

// SSHSetupModel is the model for page 3 — SSH key management.
type SSHSetupModel struct {
	keys        []string
	selectedKey int
	action      sshAction
	form        components.Form
	statusMsg   string
	isError     bool
	width       int
	height      int
}

func NewSSHSetup() SSHSetupModel {
	m := SSHSetupModel{width: 80, height: 24}
	m.refreshKeys()
	return m
}

func (m *SSHSetupModel) refreshKeys() {
	keys, _ := sshpkg.ListKeys(sshpkg.DefaultSSHDir())
	m.keys = keys
}

func (m SSHSetupModel) Init() tea.Cmd { return nil }

func (m SSHSetupModel) Update(msg tea.Msg) (SSHSetupModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.action != sshActionNone {
			return m.updateForm(msg)
		}
		switch msg.String() {
		case "esc":
			return m, func() tea.Msg { return ConnectCancelMsg{} }
		case "g", "G":
			m.action = sshActionGenerate
			m.form = components.NewForm(generateFields)
			m.statusMsg = ""
		case "c", "C":
			if len(m.keys) > 0 {
				m.action = sshActionCopy
				m.form = components.NewForm(copyFields)
				m.statusMsg = ""
			}
		case "t", "T":
			m.action = sshActionTest
			m.form = components.NewForm(testFields)
			m.statusMsg = ""
		case "r", "R":
			m.refreshKeys()
			m.statusMsg = "Keys refreshed"
		case "up", "k":
			if m.selectedKey > 0 {
				m.selectedKey--
			}
		case "down", "j":
			if m.selectedKey < len(m.keys)-1 {
				m.selectedKey++
			}
		}
	}
	return m, nil
}

func (m SSHSetupModel) updateForm(msg tea.KeyMsg) (SSHSetupModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.action = sshActionNone
		m.statusMsg = ""
		return m, nil
	case "ctrl+s":
		return m, m.submitForm()
	}
	var cmd tea.Cmd
	m.form, cmd = m.form.Update(msg)
	return m, cmd
}

func (m *SSHSetupModel) submitForm() tea.Cmd {
	errs := m.form.Validate()
	if len(errs) > 0 {
		m.statusMsg = strings.Join(errs, " · ")
		m.isError = true
		return nil
	}
	vals := m.form.Values()

	switch m.action {
	case sshActionGenerate:
		path := expandTilde(vals["path"])
		if err := sshpkg.GenerateKey(path, vals["comment"]); err != nil {
			m.statusMsg = "Error: " + err.Error()
			m.isError = true
		} else {
			m.statusMsg = fmt.Sprintf("Key generated: %s", path)
			m.isError = false
			m.refreshKeys()
			m.action = sshActionNone
		}

	case sshActionCopy:
		if len(m.keys) == 0 {
			m.statusMsg = "No key selected"
			m.isError = true
			return nil
		}
		pubKey := m.keys[m.selectedKey] + ".pub"
		port := parsePortStr(vals["port"])
		if err := sshpkg.CopyKey(pubKey, vals["user"], vals["host"], port); err != nil {
			m.statusMsg = "Error: " + err.Error()
			m.isError = true
		} else {
			m.statusMsg = fmt.Sprintf("Key copied to %s@%s", vals["user"], vals["host"])
			m.isError = false
			m.action = sshActionNone
		}

	case sshActionTest:
		port := parsePortStr(vals["port"])
		keyPath := ""
		if len(m.keys) > 0 {
			keyPath = m.keys[m.selectedKey]
		}
		ok, result := sshpkg.TestConnection(vals["user"], vals["host"], keyPath, port)
		if ok {
			m.statusMsg = fmt.Sprintf("SSH OK: %s", result)
			m.isError = false
		} else {
			m.statusMsg = fmt.Sprintf("SSH failed: %s", result)
			m.isError = true
		}
		m.action = sshActionNone
	}
	return nil
}

func (m SSHSetupModel) View() string {
	var sb strings.Builder

	sb.WriteString(sshTitleStyle.Render("⬡ VaultMount — SSH Setup") + "\n")

	if m.action != sshActionNone {
		return sb.String() + m.viewForm()
	}

	sb.WriteString(sshHintStyle.Render("g=generate  c=copy to server  t=test  r=refresh  esc=back") + "\n\n")
	sb.WriteString(sshSecStyle.Render("── SSH Keys in ~/.ssh/ ──────────────────────────────────") + "\n\n")

	if len(m.keys) == 0 {
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Italic(true).
			Render("  No SSH key pairs found. Press g to generate one.") + "\n")
	} else {
		for i, k := range m.keys {
			prefix := "  "
			style := keyListStyle
			if i == m.selectedKey {
				prefix = "▶ "
				style = keySelStyle
			}
			sb.WriteString(style.Render(prefix+k) + "\n")
		}
	}

	if m.statusMsg != "" {
		sb.WriteString("\n")
		if m.isError {
			sb.WriteString(sshErrStyle.Render("  ✗ "+m.statusMsg) + "\n")
		} else {
			sb.WriteString(sshSuccessStyle.Render("  ✓ "+m.statusMsg) + "\n")
		}
	}

	sb.WriteString("\n" + sshSecStyle.Render("── Actions ──────────────────────────────────────────────") + "\n\n")
	sb.WriteString(sshHintStyle.Render("  [G] Generate new ed25519 key pair\n"))
	sb.WriteString(sshHintStyle.Render("  [C] Copy selected key to server (ssh-copy-id)\n"))
	sb.WriteString(sshHintStyle.Render("  [T] Test SSH connection to server\n"))
	sb.WriteString(sshHintStyle.Render("  [R] Refresh key list\n"))

	return sb.String()
}

func (m SSHSetupModel) viewForm() string {
	var sb strings.Builder
	var title string
	switch m.action {
	case sshActionGenerate:
		title = "Generate SSH Key"
		sb.WriteString(sshHintStyle.Render("ctrl+s=generate  esc=cancel") + "\n\n")
	case sshActionCopy:
		title = "Copy Key to Server"
		if len(m.keys) > 0 {
			sb.WriteString(sshHintStyle.Render("Copying: "+m.keys[m.selectedKey]) + "\n")
		}
		sb.WriteString(sshHintStyle.Render("ctrl+s=copy  esc=cancel") + "\n\n")
	case sshActionTest:
		title = "Test SSH Connection"
		sb.WriteString(sshHintStyle.Render("ctrl+s=test  esc=cancel") + "\n\n")
	}
	sb.WriteString(sshSecStyle.Render("── "+title+" ──────────────────────────────────") + "\n\n")
	sb.WriteString(m.form.View())

	if m.statusMsg != "" {
		if m.isError {
			sb.WriteString(sshErrStyle.Render("  ✗ "+m.statusMsg) + "\n")
		} else {
			sb.WriteString(sshSuccessStyle.Render("  ✓ "+m.statusMsg) + "\n")
		}
	}
	return sb.String()
}

func (m *SSHSetupModel) SetSize(w, h int) { m.width = w; m.height = h }

// sshKeyRowStart is the page-relative Y of the first key row.
// Layout: title(1) + hints(1) + blank(1) + section(1) + blank(1) = 5
const sshKeyRowStart = 5

// HandleClick processes a left-click in the SSH setup page.
func (m SSHSetupModel) HandleClick(x, pageY int) SSHSetupModel {
	if m.action != sshActionNone {
		return m
	}
	if pageY < sshKeyRowStart || len(m.keys) == 0 {
		return m
	}
	idx := pageY - sshKeyRowStart
	if idx >= 0 && idx < len(m.keys) {
		m.selectedKey = idx
	}
	return m
}

func expandTilde(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return home + path[1:]
	}
	return path
}

func parsePortStr(s string) int {
	p := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			p = p*10 + int(c-'0')
		}
	}
	if p < 1 || p > 65535 {
		return 22
	}
	return p
}
