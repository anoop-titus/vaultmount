package pages

import (
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/titusmd/vaultmount/internal/config"
)

var (
	logsTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("33")).Padding(0, 1)
	logsHintStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	logsSecStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Bold(true).MarginTop(1)
	logInfo        = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	logWarn        = lipgloss.NewStyle().Foreground(lipgloss.Color("226"))
	logError       = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	logFatal       = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	tabActiveStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("33")).Bold(true).Underline(true)
	tabStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
)

// LogsModel is the model for page 5 — scrollable log viewer.
type LogsModel struct {
	store    *config.Store
	tabs     []string // "all" + one per connection ID
	tabNames []string // display names
	selected int
	viewport viewport.Model
	width    int
	height   int
}

func NewLogs(store *config.Store) LogsModel {
	vp := viewport.New(80, 20)
	m := LogsModel{store: store, width: 80, height: 24, viewport: vp}
	m.rebuildTabs()
	m.refresh()
	return m
}

func (m *LogsModel) rebuildTabs() {
	conns := m.store.List()
	m.tabs = make([]string, 0, len(conns)+1)
	m.tabNames = make([]string, 0, len(conns)+1)
	m.tabs = append(m.tabs, "all")
	m.tabNames = append(m.tabNames, "All")
	for _, c := range conns {
		m.tabs = append(m.tabs, c.ID)
		m.tabNames = append(m.tabNames, c.Name)
	}
	if m.selected >= len(m.tabs) {
		m.selected = 0
	}
}

func (m *LogsModel) refresh() {
	content := m.loadLog()
	colored := colorizeLog(content)
	m.viewport.SetContent(colored)
	m.viewport.GotoBottom()
}

func (m *LogsModel) loadLog() string {
	if len(m.tabs) == 0 {
		return ""
	}
	tab := m.tabs[m.selected]
	if tab == "all" {
		return m.loadAllLogs()
	}
	conn, ok := m.store.Get(tab)
	if !ok {
		return ""
	}
	return readLogFile(conn.LogPath())
}

func (m *LogsModel) loadAllLogs() string {
	var sb strings.Builder
	for _, c := range m.store.List() {
		content := readLogFile(c.LogPath())
		if content != "" {
			sb.WriteString("=== " + c.Name + " ===\n")
			sb.WriteString(content)
			sb.WriteString("\n")
		}
	}
	if sb.Len() == 0 {
		return "(no logs yet)"
	}
	return sb.String()
}

func readLogFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func colorizeLog(content string) string {
	lines := strings.Split(content, "\n")
	var sb strings.Builder
	for _, line := range lines {
		switch {
		case strings.Contains(line, "[FATAL]"):
			sb.WriteString(logFatal.Render(line))
		case strings.Contains(line, "[ERROR]"):
			sb.WriteString(logError.Render(line))
		case strings.Contains(line, "[WARN ]"):
			sb.WriteString(logWarn.Render(line))
		default:
			sb.WriteString(logInfo.Render(line))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func (m LogsModel) Init() tea.Cmd { return nil }

func (m LogsModel) Update(msg tea.Msg) (LogsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return m, func() tea.Msg { return ConnectCancelMsg{} }
		case "tab":
			m.selected = (m.selected + 1) % len(m.tabs)
			m.refresh()
		case "shift+tab":
			m.selected = (m.selected - 1 + len(m.tabs)) % len(m.tabs)
			m.refresh()
		case "r", "R":
			m.rebuildTabs()
			m.refresh()
		case "c", "C":
			m.clearCurrentLog()
			m.refresh()
		default:
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

func (m *LogsModel) clearCurrentLog() {
	if len(m.tabs) == 0 {
		return
	}
	tab := m.tabs[m.selected]
	if tab == "all" {
		for _, c := range m.store.List() {
			_ = os.WriteFile(c.LogPath(), []byte{}, 0o644)
		}
		return
	}
	conn, ok := m.store.Get(tab)
	if ok {
		_ = os.WriteFile(conn.LogPath(), []byte{}, 0o644)
	}
}

func (m LogsModel) View() string {
	var sb strings.Builder

	sb.WriteString(logsTitleStyle.Render("⬡ VaultMount — Logs") + "\n")
	sb.WriteString(logsHintStyle.Render("tab=switch connection  r=refresh  c=clear  ↑↓/PgUp/PgDn=scroll  esc=back") + "\n\n")

	// Tab bar
	var tabBar strings.Builder
	for i, name := range m.tabNames {
		if i > 0 {
			tabBar.WriteString("  ")
		}
		if i == m.selected {
			tabBar.WriteString(tabActiveStyle.Render(name))
		} else {
			tabBar.WriteString(tabStyle.Render(name))
		}
	}
	sb.WriteString("  " + tabBar.String() + "\n")
	sb.WriteString(logsSecStyle.Render("  " + strings.Repeat("─", m.width-6)) + "\n\n")

	sb.WriteString(m.viewport.View())
	return sb.String()
}

func (m *LogsModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	m.viewport.Width = w - 2
	m.viewport.Height = h - 8
}

// logsTabRow is the page-relative Y of the tab bar.
// Layout: title(1) + hints(1) + blank(1) = row 3
const logsTabRow = 3

// HandleClick handles left-click tab switching and scroll-wheel log scrolling.
func (m LogsModel) HandleClick(x, pageY int, button tea.MouseButton) (LogsModel, tea.Cmd) {
	switch button {
	case tea.MouseButtonLeft:
		if pageY == logsTabRow && len(m.tabNames) > 0 {
			// Hit-test tab labels: "  Name1  Name2  ..."
			// Each tab is rendered as "  tabName" with 2-space prefix + 2-space separator.
			cursor := 2 // leading "  "
			for i, name := range m.tabNames {
				end := cursor + len([]rune(name))
				if x >= cursor && x < end {
					m.selected = i
					m.refresh()
					break
				}
				cursor = end + 2 // 2-space separator between tabs
			}
		}
	case tea.MouseButtonWheelUp:
		m.viewport.LineUp(3)
	case tea.MouseButtonWheelDown:
		m.viewport.LineDown(3)
	}
	return m, nil
}

