package pages

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/titusmd/vaultmount/internal/config"
	"github.com/titusmd/vaultmount/internal/tui/components"
)

type ConnectSavedMsg struct{ Conn config.Connection }
type ConnectCancelMsg struct{}

var (
	connectTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("33")).Padding(0, 1)
	sectionStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).MarginTop(1)
	connectHintStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	validStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("46"))
	connectErrStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
)

var connectionFields = []components.FieldDef{
	{Key: "name", Label: "Name", Placeholder: "My Server", Required: true},
	{Key: "host", Label: "Host / IP", Placeholder: "192.168.1.1 or hostname", Required: true},
	{Key: "port", Label: "Port", Placeholder: "22", Default: "22"},
	{Key: "user", Label: "Username", Placeholder: "ubuntu", Required: true},
	{Key: "remote_path", Label: "Remote Path", Placeholder: "/home/user/data", Required: true},
	{Key: "mount_point", Label: "Local Mount", Placeholder: "/Users/you/Vaults/server", Required: true},
	{Key: "ssh_key", Label: "SSH Key Path", Placeholder: "~/.ssh/id_ed25519"},
	{Key: "auto_mount", Label: "Auto-mount", Placeholder: "y/n (default: n)", Default: "n"},
	{Key: "compression", Label: "Compression", Placeholder: "y/n (default: n)", Default: "n"},
}

// ConnectModel is the model for page 2 — add/edit a connection.
type ConnectModel struct {
	form      components.Form
	editID    string
	width     int
	height    int
	statusMsg string
	isError   bool
}

func NewConnect() ConnectModel {
	return ConnectModel{
		form:   components.NewForm(connectionFields),
		width:  80,
		height: 24,
	}
}

// LoadConnection pre-fills the form for editing an existing connection.
func (m *ConnectModel) LoadConnection(c config.Connection) {
	m.editID = c.ID
	m.form.SetValue("name", c.Name)
	m.form.SetValue("host", c.Host)
	m.form.SetValue("port", strconv.Itoa(c.Port))
	m.form.SetValue("user", c.User)
	m.form.SetValue("remote_path", c.RemotePath)
	m.form.SetValue("mount_point", c.MountPoint)
	m.form.SetValue("ssh_key", c.SSHKeyPath)
	m.form.SetValue("auto_mount", boolToYN(c.AutoMount))
	m.form.SetValue("compression", boolToYN(c.Compression))
}

func (m ConnectModel) Init() tea.Cmd { return nil }

func (m ConnectModel) Update(msg tea.Msg) (ConnectModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return m, func() tea.Msg { return ConnectCancelMsg{} }
		case "ctrl+s", "f10":
			return m, m.submit()
		}
	}
	var cmd tea.Cmd
	m.form, cmd = m.form.Update(msg)
	return m, cmd
}

func (m *ConnectModel) submit() tea.Cmd {
	errs := m.form.Validate()
	if len(errs) > 0 {
		m.statusMsg = strings.Join(errs, " · ")
		m.isError = true
		return nil
	}
	vals := m.form.Values()
	conn, err := buildConnection(m.editID, vals)
	if err != nil {
		m.statusMsg = err.Error()
		m.isError = true
		return nil
	}
	m.statusMsg = "Saved."
	m.isError = false
	saved := conn
	return func() tea.Msg { return ConnectSavedMsg{Conn: saved} }
}

func buildConnection(editID string, vals map[string]string) (config.Connection, error) {
	port, err := strconv.Atoi(vals["port"])
	if err != nil || port < 1 || port > 65535 {
		return config.Connection{}, fmt.Errorf("port must be 1–65535")
	}
	remote := vals["remote_path"]
	if remote != "" && !strings.HasPrefix(remote, "/") {
		return config.Connection{}, fmt.Errorf("remote path must be absolute (start with /)")
	}
	mp := vals["mount_point"]
	if mp != "" && !strings.HasPrefix(mp, "/") {
		return config.Connection{}, fmt.Errorf("local mount point must be absolute (start with /)")
	}
	return config.Connection{
		ID:          editID,
		Name:        vals["name"],
		Host:        vals["host"],
		Port:        port,
		User:        vals["user"],
		RemotePath:  remote,
		MountPoint:  mp,
		SSHKeyPath:  vals["ssh_key"],
		AutoMount:   ynToBool(vals["auto_mount"]),
		Compression: ynToBool(vals["compression"]),
	}, nil
}

func (m ConnectModel) View() string {
	var sb strings.Builder

	action := "Add Connection"
	if m.editID != "" {
		action = "Edit Connection"
	}
	sb.WriteString(connectTitleStyle.Render("⬡ VaultMount — "+action) + "\n")
	sb.WriteString(connectHintStyle.Render("tab/↑↓=navigate  ctrl+s=save  esc=cancel") + "\n\n")

	sb.WriteString(sectionStyle.Render("── Connection Details ──────────────────────────────────") + "\n\n")
	sb.WriteString(m.form.View())

	if m.statusMsg != "" {
		sb.WriteString("\n")
		if m.isError {
			sb.WriteString(connectErrStyle.Render("  ✗ "+m.statusMsg) + "\n")
		} else {
			sb.WriteString(validStyle.Render("  ✓ "+m.statusMsg) + "\n")
		}
	}

	return sb.String()
}

func (m *ConnectModel) SetSize(w, h int) { m.width = w; m.height = h }

func (m *ConnectModel) Reset() {
	m.form = components.NewForm(connectionFields)
	m.editID = ""
	m.statusMsg = ""
	m.isError = false
}

func boolToYN(b bool) string {
	if b {
		return "y"
	}
	return "n"
}

func ynToBool(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "y" || s == "yes" || s == "true" || s == "1"
}
