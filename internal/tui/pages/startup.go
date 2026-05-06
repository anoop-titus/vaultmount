package pages

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"

	"github.com/titusmd/vaultmount/internal/config"
	"github.com/titusmd/vaultmount/internal/launchd"
)

const sshfsBinDefault = "/opt/homebrew/bin/sshfs"

var (
	startupTitleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("33")).Padding(0, 1)
	startupHintStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	startupSecStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Bold(true).MarginTop(1)
	installedStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("46")).Bold(true)
	notInstalledStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	startupErrStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	startupOkStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("46"))
	previewStyle       = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("237")).Padding(0, 1)
)

type startupView int

const (
	startupViewList startupView = iota
	startupViewScript
	startupViewPlist
)

// StartupModel is the model for page 4 — launchd / startup manager.
type StartupModel struct {
	store     *config.Store
	selected  int
	view      startupView
	viewport  viewport.Model
	statusMsg string
	isError   bool
	width     int
	height    int
	sshfsBin  string
}

func NewStartup(store *config.Store, sshfsBin string) StartupModel {
	vp := viewport.New(80, 20)
	return StartupModel{
		store:    store,
		width:    80,
		height:   24,
		sshfsBin: sshfsBin,
		viewport: vp,
	}
}

func (m StartupModel) Init() tea.Cmd { return nil }

func (m StartupModel) Update(msg tea.Msg) (StartupModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.view != startupViewList {
			switch msg.String() {
			case "esc", "q":
				m.view = startupViewList
				return m, nil
			}
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}
		return m.handleListKey(msg)
	}
	return m, nil
}

func (m StartupModel) handleListKey(msg tea.KeyMsg) (StartupModel, tea.Cmd) {
	conns := m.store.List()
	switch msg.String() {
	case "esc":
		return m, func() tea.Msg { return ConnectCancelMsg{} }
	case "up", "k":
		if m.selected > 0 {
			m.selected--
		}
	case "down", "j":
		if m.selected < len(conns)-1 {
			m.selected++
		}
	case "i", "I":
		if m.selected < len(conns) {
			m.installSelected(conns[m.selected])
		}
	case "u", "U":
		if m.selected < len(conns) {
			m.uninstallSelected(conns[m.selected])
		}
	case "v", "V":
		if m.selected < len(conns) {
			m.showScript(conns[m.selected])
		}
	case "p", "P":
		if m.selected < len(conns) {
			m.showPlist(conns[m.selected])
		}
	case "r", "R":
		m.statusMsg = "Reloaded"
		m.isError = false
	}
	return m, nil
}

func (m *StartupModel) installSelected(c config.Connection) {
	home, _ := os.UserHomeDir()
	scriptDir := filepath.Join(home, ".local", "bin")
	scriptPath := filepath.Join(scriptDir, c.ScriptName())
	logPath := c.LogPath()

	// Write script
	if err := os.MkdirAll(scriptDir, 0o755); err != nil {
		m.statusMsg = "Error creating script dir: " + err.Error()
		m.isError = true
		return
	}
	script, err := launchd.GenerateMountScript(c, m.sshfsBin, logPath)
	if err != nil {
		m.statusMsg = "Script error: " + err.Error()
		m.isError = true
		return
	}
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		m.statusMsg = "Write script error: " + err.Error()
		m.isError = true
		return
	}

	// Install plist
	plistCfg := launchd.PlistConfig{
		Label:      c.Label(),
		ScriptPath: scriptPath,
		Interval:   60,
		LogPath:    logPath,
	}
	if err := launchd.Install(plistCfg); err != nil {
		m.statusMsg = "Install error: " + err.Error()
		m.isError = true
		return
	}
	m.statusMsg = fmt.Sprintf("Installed %s", c.Label())
	m.isError = false
}

func (m *StartupModel) uninstallSelected(c config.Connection) {
	if err := launchd.Uninstall(c.Label()); err != nil {
		m.statusMsg = "Uninstall error: " + err.Error()
		m.isError = true
		return
	}
	// Remove script
	home, _ := os.UserHomeDir()
	scriptPath := filepath.Join(home, ".local", "bin", c.ScriptName())
	_ = os.Remove(scriptPath)
	m.statusMsg = fmt.Sprintf("Uninstalled %s", c.Label())
	m.isError = false
}

func (m *StartupModel) showScript(c config.Connection) {
	script, err := launchd.GenerateMountScript(c, m.sshfsBin, c.LogPath())
	if err != nil {
		m.statusMsg = err.Error()
		m.isError = true
		return
	}
	m.viewport.SetContent(script)
	m.viewport.GotoTop()
	m.view = startupViewScript
}

func (m *StartupModel) showPlist(c config.Connection) {
	home, _ := os.UserHomeDir()
	scriptPath := filepath.Join(home, ".local", "bin", c.ScriptName())
	plistCfg := launchd.PlistConfig{
		Label:      c.Label(),
		ScriptPath: scriptPath,
		Interval:   60,
		LogPath:    c.LogPath(),
	}
	content, err := launchd.Generate(plistCfg)
	if err != nil {
		m.statusMsg = err.Error()
		m.isError = true
		return
	}
	m.viewport.SetContent(content)
	m.viewport.GotoTop()
	m.view = startupViewPlist
}

func (m StartupModel) View() string {
	var sb strings.Builder
	sb.WriteString(startupTitleStyle.Render("⬡ VaultMount — Startup Manager") + "\n")

	if m.view == startupViewScript || m.view == startupViewPlist {
		title := "Mount Script"
		if m.view == startupViewPlist {
			title = "LaunchD Plist"
		}
		sb.WriteString(startupHintStyle.Render("↑↓/PgUp/PgDn=scroll  esc=back") + "\n\n")
		sb.WriteString(startupSecStyle.Render("── "+title+" ──────────────────────────────────") + "\n\n")
		sb.WriteString(previewStyle.Render(m.viewport.View()) + "\n")
		return sb.String()
	}

	sb.WriteString(startupHintStyle.Render("i=install  u=uninstall  v=view script  p=view plist  esc=back") + "\n\n")
	sb.WriteString(startupSecStyle.Render("── Connections ──────────────────────────────────────────") + "\n\n")

	conns := m.store.List()
	if len(conns) == 0 {
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Italic(true).
			Render("  No connections. Add one from the dashboard (n).") + "\n")
	} else {
		for i, c := range conns {
			installed := launchd.IsInstalled(c.Label())
			loaded, pid, _ := launchd.Status(c.Label())

			prefix := "  "
			if i == m.selected {
				prefix = "▶ "
			}

			status := notInstalledStyle.Render("NOT INSTALLED")
			if installed && loaded {
				pidStr := ""
				if pid > 0 {
					pidStr = fmt.Sprintf(" (pid %d)", pid)
				}
				status = installedStyle.Render("LOADED" + pidStr)
			} else if installed {
				status = installedStyle.Render("INSTALLED")
			}

			line := fmt.Sprintf("%s%-22s  %s  %s", prefix,
				truncate(c.Name, 22), truncate(c.Label(), 36), status)
			if i == m.selected {
				sb.WriteString(selectedRow.Render(line) + "\n")
			} else {
				sb.WriteString(line + "\n")
			}
		}
	}

	if m.statusMsg != "" {
		sb.WriteString("\n")
		if m.isError {
			sb.WriteString(startupErrStyle.Render("  ✗ "+m.statusMsg) + "\n")
		} else {
			sb.WriteString(startupOkStyle.Render("  ✓ "+m.statusMsg) + "\n")
		}
	}

	return sb.String()
}

func (m *StartupModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	m.viewport.Width = w - 4
	m.viewport.Height = h - 8
}

// startupRowStart is the page-relative Y of the first connection row.
// Layout: title(1) + hints(1) + blank(1) + section(1) + blank(1) = 5
const startupRowStart = 5

// HandleClick selects the clicked connection row.
func (m StartupModel) HandleClick(x, pageY int) StartupModel {
	if m.view != startupViewList {
		return m
	}
	if pageY < startupRowStart {
		return m
	}
	conns := m.store.List()
	idx := pageY - startupRowStart
	if idx >= 0 && idx < len(conns) {
		m.selected = idx
	}
	return m
}
