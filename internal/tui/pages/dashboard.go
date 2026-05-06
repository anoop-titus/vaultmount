package pages

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/titusmd/vaultmount/internal/config"
	"github.com/titusmd/vaultmount/internal/mount"
	"github.com/titusmd/vaultmount/internal/tui/components"
)

// MountStatus tracks the live state of one connection.
type MountStatus struct {
	Conn    config.Connection
	State   mount.State
	Message string
	Sample  mount.Sample
	Monitor *mount.Monitor
	Cancel  context.CancelFunc
}

type TickMsg time.Time
type MountEventMsg struct {
	ConnID string
	Event  mount.Event
}

var (
	dashTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("33")).Padding(0, 1)
	headerStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Bold(true)
	selectedRow    = lipgloss.NewStyle().Background(lipgloss.Color("236")).Foreground(lipgloss.Color("255"))
	normalRow      = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	statusColors   = map[mount.State]lipgloss.Style{
		mount.StateMounted:    lipgloss.NewStyle().Foreground(lipgloss.Color("46")).Bold(true),
		mount.StateMounting:   lipgloss.NewStyle().Foreground(lipgloss.Color("226")),
		mount.StateRetrying:   lipgloss.NewStyle().Foreground(lipgloss.Color("214")),
		mount.StateDiagnosing: lipgloss.NewStyle().Foreground(lipgloss.Color("33")),
		mount.StateError:      lipgloss.NewStyle().Foreground(lipgloss.Color("196")),
		mount.StateFatalError: lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true),
		mount.StateIdle:       lipgloss.NewStyle().Foreground(lipgloss.Color("238")),
	}
	dashHintStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
)

// DashboardModel is the model for page 1 — the main connection dashboard.
type DashboardModel struct {
	Statuses []MountStatus
	Selected int
	Width    int
	Height   int
	store    *config.Store
	sshfsBin string
}

func NewDashboard(store *config.Store, sshfsBin string) DashboardModel {
	m := DashboardModel{store: store, sshfsBin: sshfsBin, Width: 80, Height: 24}
	m.Reload()
	return m
}

func (m *DashboardModel) Reload() {
	conns := m.store.List()
	existing := map[string]MountStatus{}
	for _, s := range m.Statuses {
		existing[s.Conn.ID] = s
	}
	statuses := make([]MountStatus, len(conns))
	for i, c := range conns {
		if s, ok := existing[c.ID]; ok {
			s.Conn = c
			statuses[i] = s
		} else {
			statuses[i] = MountStatus{Conn: c, State: mount.StateIdle}
		}
	}
	m.Statuses = statuses
	if m.Selected >= len(m.Statuses) {
		m.Selected = maxInt(0, len(m.Statuses)-1)
	}
}

func (m DashboardModel) Init() tea.Cmd {
	return Tick()
}

func Tick() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}

func (m DashboardModel) Update(msg tea.Msg) (DashboardModel, tea.Cmd) {
	switch msg := msg.(type) {
	case TickMsg:
		for i := range m.Statuses {
			if m.Statuses[i].Monitor != nil {
				m.Statuses[i].Sample = m.Statuses[i].Monitor.Latest()
			}
		}
		return m, Tick()

	case MountEventMsg:
		for i := range m.Statuses {
			if m.Statuses[i].Conn.ID == msg.ConnID {
				m.Statuses[i].State = msg.Event.State
				m.Statuses[i].Message = msg.Event.Message
			}
		}
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.Selected > 0 {
				m.Selected--
			}
		case "down", "j":
			if m.Selected < len(m.Statuses)-1 {
				m.Selected++
			}
		case "enter", " ":
			return m, m.toggleMount()
		case "r":
			m.Reload()
		}
	}
	return m, nil
}

func (m *DashboardModel) toggleMount() tea.Cmd {
	if len(m.Statuses) == 0 {
		return nil
	}
	s := &m.Statuses[m.Selected]
	if s.State == mount.StateMounted || s.State == mount.StateMounting || s.State == mount.StateDiagnosing {
		return m.disconnect(m.Selected)
	}
	return m.connect(m.Selected)
}

func (m *DashboardModel) connect(idx int) tea.Cmd {
	s := &m.Statuses[idx]
	if s.Cancel != nil {
		s.Cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.Cancel = cancel

	mon := mount.NewMonitor(s.Conn.Host, 500)
	mon.Start(ctx)
	s.Monitor = mon

	connID := s.Conn.ID
	conn := s.Conn
	sshfsBin := m.sshfsBin
	events := make(chan mount.Event, 16)

	go func() {
		cfg := mount.MachineConfig{
			Connection: conn,
			SSHFSBin:   sshfsBin,
			MaxRetries: 3,
			RetryDelay: 3 * time.Second,
		}
		mount.Run(ctx, cfg, events)
		close(events)
	}()

	return func() tea.Msg {
		for e := range events {
			return MountEventMsg{ConnID: connID, Event: e}
		}
		return nil
	}
}

func (m *DashboardModel) disconnect(idx int) tea.Cmd {
	s := &m.Statuses[idx]
	if s.Cancel != nil {
		s.Cancel()
		s.Cancel = nil
	}
	s.Monitor = nil
	s.State = mount.StateIdle
	s.Message = "disconnected"
	s.Sample = mount.Sample{}
	mp := s.Conn.MountPoint
	go func() {
		_ = exec.Command("diskutil", "unmount", "force", mp).Run()
	}()
	return nil
}

func (m DashboardModel) View() string {
	var sb strings.Builder

	sb.WriteString(dashTitleStyle.Render("⬡ VaultMount — Dashboard") + "\n")
	sb.WriteString(dashHintStyle.Render("enter=connect/disconnect  n=new  d=delete  s=startup  h=ssh  l=logs  q=quit") + "\n\n")

	if len(m.Statuses) == 0 {
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Italic(true).
			Render("  No connections configured. Press n to add one.") + "\n")
		return sb.String()
	}

	cols := fmt.Sprintf("  %-22s  %-20s  %-12s  %-28s",
		"NAME", "HOST", "STATUS", "MOUNT POINT")
	sb.WriteString(headerStyle.Render(cols) + "\n")
	sb.WriteString(headerStyle.Render("  " + strings.Repeat("─", m.Width-6)) + "\n")

	for i, s := range m.Statuses {
		stateLabel := strings.ToUpper(s.State.String())
		stateStyle, ok := statusColors[s.State]
		if !ok {
			stateStyle = normalRow
		}

		host := s.Conn.Host
		if s.Conn.Port != 0 && s.Conn.Port != 22 {
			host = fmt.Sprintf("%s:%d", host, s.Conn.Port)
		}

		row := fmt.Sprintf("  %-22s  %-20s  %-12s  %-28s",
			truncate(s.Conn.Name, 22),
			truncate(host, 20),
			stateLabel,
			truncate(s.Conn.MountPoint, 28),
		)

		if i == m.Selected {
			sb.WriteString(selectedRow.Render(row) + "\n")
		} else {
			sb.WriteString(stateStyle.Render(row) + "\n")
		}
	}

	// Detail panel for selected
	if m.Selected < len(m.Statuses) {
		s := m.Statuses[m.Selected]
		sb.WriteString("\n" + "  " + strings.Repeat("─", m.Width-6) + "\n")
		if s.Message != "" {
			sb.WriteString(dashHintStyle.Render("  "+s.Message) + "\n")
		}
		if s.State == mount.StateMounted {
			sb.WriteString("\n")
			sb.WriteString("  " + components.SpeedBarPair(s.Sample.UpBytesPerSec, s.Sample.DownBytesPerSec, m.Width-8) + "\n")
		}
	}

	return sb.String()
}

func (m *DashboardModel) SetSize(w, h int) {
	m.Width = w
	m.Height = h
}

// dashContentRowStart is the page-relative Y of the first connection row.
// Layout: title(1) + hints(1) + blank(1) + header(1) + divider(1) = 5 rows before data.
const dashContentRowStart = 5

// HandleClick processes a left-click or scroll-wheel event in the dashboard.
// pageY is Y relative to the page content area (nav bar already subtracted).
func (m DashboardModel) HandleClick(x, pageY int, button tea.MouseButton) (DashboardModel, tea.Cmd) {
	switch button {
	case tea.MouseButtonLeft:
		if pageY < dashContentRowStart || len(m.Statuses) == 0 {
			return m, nil
		}
		idx := pageY - dashContentRowStart
		if idx < 0 || idx >= len(m.Statuses) {
			return m, nil
		}
		if idx == m.Selected {
			// Second click on already-selected row → toggle connect
			return m, m.toggleMount()
		}
		m.Selected = idx

	case tea.MouseButtonWheelUp:
		if m.Selected > 0 {
			m.Selected--
		}
	case tea.MouseButtonWheelDown:
		if m.Selected < len(m.Statuses)-1 {
			m.Selected++
		}
	}
	return m, nil
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return string(runes)
	}
	return string(runes[:n-1]) + "…"
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
