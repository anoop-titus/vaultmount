package tui

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/titusmd/vaultmount/internal/config"
	"github.com/titusmd/vaultmount/internal/tui/pages"
)

// Page represents the active page.
type Page int

const (
	PageDashboard Page = iota
	PageConnect
	PageSSHSetup
	PageStartup
	PageLogs
)

var (
	navStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("238")).Background(lipgloss.Color("234")).Padding(0, 1)
	activeNav = lipgloss.NewStyle().Foreground(lipgloss.Color("33")).Background(lipgloss.Color("234")).Bold(true).Padding(0, 1)
	statusBar = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Background(lipgloss.Color("234")).Padding(0, 1)
)

const sshfsBin = "/opt/homebrew/bin/sshfs"

// navZone tracks the clickable X range of a nav item on row 0.
type navZone struct {
	page   Page
	xStart int
	xEnd   int
}

// AppModel is the root bubbletea model.
type AppModel struct {
	page      Page
	dashboard pages.DashboardModel
	connect   pages.ConnectModel
	sshSetup  pages.SSHSetupModel
	startup   pages.StartupModel
	logs      pages.LogsModel
	store     *config.Store
	width     int
	height    int
	statusMsg string
	navZones  []navZone // computed at render time, used for click detection
}

// NewAppModel creates the root application model.
func NewAppModel() AppModel {
	store, err := config.NewStore()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}
	return AppModel{
		page:      PageDashboard,
		dashboard: pages.NewDashboard(store, sshfsBin),
		connect:   pages.NewConnect(),
		sshSetup:  pages.NewSSHSetup(),
		startup:   pages.NewStartup(store, sshfsBin),
		logs:      pages.NewLogs(store),
		store:     store,
		width:     80,
		height:    24,
	}
}

func (m AppModel) Init() tea.Cmd {
	return m.dashboard.Init()
}

func (m AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.dashboard.SetSize(msg.Width, msg.Height-4)
		m.connect.SetSize(msg.Width, msg.Height-4)
		m.sshSetup.SetSize(msg.Width, msg.Height-4)
		m.startup.SetSize(msg.Width, msg.Height-4)
		m.logs.SetSize(msg.Width, msg.Height-4)
		return m, nil

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			if m.page == PageDashboard {
				return m, tea.Quit
			}
		case "1":
			m.page = PageDashboard
			return m, nil
		case "2":
			m.connect.Reset()
			m.page = PageConnect
			return m, nil
		case "3":
			m.page = PageSSHSetup
			return m, nil
		case "4":
			m.page = PageStartup
			return m, nil
		case "5":
			m.page = PageLogs
			return m, nil
		}

		if m.page == PageDashboard {
			switch msg.String() {
			case "n":
				m.connect.Reset()
				m.page = PageConnect
				return m, nil
			case "d":
				m.deleteSelected()
				return m, nil
			case "s":
				m.page = PageStartup
				return m, nil
			case "h":
				m.page = PageSSHSetup
				return m, nil
			case "l":
				m.page = PageLogs
				return m, nil
			case "q":
				return m, tea.Quit
			}
		}
	}

	return m.routeUpdate(msg)
}

// handleMouse dispatches mouse events to nav bar or active page.
func (m AppModel) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// Only act on left-button presses; pass everything else to the page.
	isLeftPress := msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft
	isScroll := msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown

	if isLeftPress && msg.Y == 0 {
		// Nav bar click
		for _, z := range m.navZones {
			if msg.X >= z.xStart && msg.X < z.xEnd {
				if z.page == PageConnect {
					m.connect.Reset()
				}
				m.page = z.page
				return m, nil
			}
		}
		return m, nil
	}

	// Content area is below the nav bar; translate Y to page-relative.
	pageY := msg.Y - 1 // row 0 of page content = global row 1

	if isLeftPress || isScroll {
		var cmd tea.Cmd
		switch m.page {
		case PageDashboard:
			m.dashboard, cmd = m.dashboard.HandleClick(msg.X, pageY, msg.Button)
		case PageSSHSetup:
			m.sshSetup = m.sshSetup.HandleClick(msg.X, pageY)
		case PageStartup:
			m.startup = m.startup.HandleClick(msg.X, pageY)
		case PageLogs:
			m.logs, cmd = m.logs.HandleClick(msg.X, pageY, msg.Button)
		}
		return m, cmd
	}

	return m, nil
}

func (m AppModel) routeUpdate(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg.(type) {
	case pages.ConnectCancelMsg:
		m.page = PageDashboard
		return m, nil
	case pages.ConnectSavedMsg:
		saved := msg.(pages.ConnectSavedMsg).Conn
		if err := m.store.Save(saved); err != nil {
			m.statusMsg = "Save error: " + err.Error()
		} else {
			m.statusMsg = "Saved: " + saved.Name
			m.dashboard.Reload()
		}
		m.page = PageDashboard
		return m, nil
	}

	switch m.page {
	case PageDashboard:
		m.dashboard, cmd = m.dashboard.Update(msg)
	case PageConnect:
		m.connect, cmd = m.connect.Update(msg)
	case PageSSHSetup:
		m.sshSetup, cmd = m.sshSetup.Update(msg)
	case PageStartup:
		m.startup, cmd = m.startup.Update(msg)
	case PageLogs:
		m.logs, cmd = m.logs.Update(msg)
	}
	return m, cmd
}

func (m *AppModel) deleteSelected() {
	if len(m.dashboard.Statuses) == 0 {
		return
	}
	sel := m.dashboard.Statuses[m.dashboard.Selected]
	_ = m.store.Delete(sel.Conn.ID)
	m.dashboard.Reload()
	m.statusMsg = "Deleted: " + sel.Conn.Name
}

// navItems describes the five nav tabs in order.
var navItems = []struct {
	key   string
	label string
	page  Page
}{
	{"1", "Dashboard", PageDashboard},
	{"2", "Add/Edit", PageConnect},
	{"3", "SSH", PageSSHSetup},
	{"4", "Startup", PageStartup},
	{"5", "Logs", PageLogs},
}

func (m AppModel) View() string {
	var sb strings.Builder

	// ── Nav bar (row 0) ───────────────────────────────────────────────────────
	var nav strings.Builder
	zones := make([]navZone, 0, len(navItems))
	x := 0
	for _, item := range navItems {
		label := fmt.Sprintf(" %s  %s ", item.key, item.label)
		var rendered string
		if m.page == item.page {
			rendered = activeNav.Render(label)
		} else {
			rendered = navStyle.Render(label)
		}
		w := lipgloss.Width(rendered)
		zones = append(zones, navZone{page: item.page, xStart: x, xEnd: x + w})
		x += w
		nav.WriteString(rendered)
	}
	// Store zones for next Update cycle (safe: single-threaded bubbletea loop).
	// We cast to pointer to mutate; this is the standard bubbletea pattern.
	m.navZones = zones

	sb.WriteString(nav.String() + "\n")

	// ── Page content ──────────────────────────────────────────────────────────
	switch m.page {
	case PageDashboard:
		sb.WriteString(m.dashboard.View())
	case PageConnect:
		sb.WriteString(m.connect.View())
	case PageSSHSetup:
		sb.WriteString(m.sshSetup.View())
	case PageStartup:
		sb.WriteString(m.startup.View())
	case PageLogs:
		sb.WriteString(m.logs.View())
	}

	// ── Status bar ────────────────────────────────────────────────────────────
	if m.statusMsg != "" {
		sb.WriteString("\n" + statusBar.Render(m.statusMsg))
	}

	return sb.String()
}
