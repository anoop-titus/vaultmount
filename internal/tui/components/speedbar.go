package components

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	uploadStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("33"))  // blue
	downloadStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("35"))  // cyan-green
	labelStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Width(12)
	speedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Width(10).Align(lipgloss.Right)
	barBg         = lipgloss.NewStyle().Foreground(lipgloss.Color("237"))
)

const barChars = " ▏▎▍▌▋▊▉█"

// SpeedBar renders an animated bandwidth bar.
type SpeedBar struct {
	Label      string
	Current    float64 // bytes/sec
	Max        float64 // bytes/sec scale (auto-scales if 0)
	Width      int
	IsUpload   bool
}

// Render returns the full rendered string for one speed bar row.
func (s SpeedBar) Render() string {
	if s.Width < 20 {
		s.Width = 40
	}
	if s.Max <= 0 {
		s.Max = autoMax(s.Current)
	}

	barWidth := s.Width - 24 // label(12) + space(2) + speed(10)
	if barWidth < 4 {
		barWidth = 4
	}

	filled := 0.0
	if s.Max > 0 {
		filled = math.Min(s.Current/s.Max, 1.0) * float64(barWidth)
	}

	bar := renderBar(filled, barWidth, s.IsUpload)
	speed := FormatSpeed(s.Current)

	label := labelStyle.Render(s.Label)
	speedStr := speedStyle.Render(speed)

	return label + "  " + bar + "  " + speedStr
}

func renderBar(filled float64, width int, isUpload bool) string {
	full := int(filled)
	frac := filled - float64(full)

	barRunes := []rune(barChars) // rune slice, not byte slice
	nFrac := len(barRunes)

	var sb strings.Builder
	// Full blocks
	for i := 0; i < full && i < width; i++ {
		sb.WriteRune('█')
	}
	// Fractional block
	if full < width {
		idx := int(frac * float64(nFrac-1))
		if idx < 0 {
			idx = 0
		}
		if idx >= nFrac {
			idx = nFrac - 1
		}
		sb.WriteRune(barRunes[idx])
		// Empty remainder
		for i := full + 1; i < width; i++ {
			sb.WriteString("░")
		}
	}

	raw := sb.String()
	if isUpload {
		return uploadStyle.Render(raw)
	}
	return downloadStyle.Render(raw)
}

// FormatSpeed formats bytes/sec into a human-readable string.
func FormatSpeed(bps float64) string {
	if math.IsNaN(bps) || bps < 0 {
		bps = 0
	}
	switch {
	case bps >= 1<<30:
		return fmt.Sprintf("%.1f GB/s", bps/float64(1<<30))
	case bps >= 1<<20:
		return fmt.Sprintf("%.1f MB/s", bps/float64(1<<20))
	case bps >= 1<<10:
		return fmt.Sprintf("%.0f KB/s", bps/float64(1<<10))
	default:
		return fmt.Sprintf("%.0f B/s", bps)
	}
}

// autoMax returns a reasonable scale ceiling for the given current speed.
func autoMax(current float64) float64 {
	scales := []float64{
		10 * 1024,          // 10 KB/s
		100 * 1024,         // 100 KB/s
		1024 * 1024,        // 1 MB/s
		10 * 1024 * 1024,   // 10 MB/s
		100 * 1024 * 1024,  // 100 MB/s
		1024 * 1024 * 1024, // 1 GB/s
	}
	for _, s := range scales {
		if current < s {
			return s
		}
	}
	return current * 2
}

// SpeedBarPair renders an upload and download bar as a two-line block.
func SpeedBarPair(upBps, downBps float64, width int) string {
	up := SpeedBar{
		Label:    "↑ Upload",
		Current:  upBps,
		Width:    width,
		IsUpload: true,
	}
	down := SpeedBar{
		Label:    "↓ Download",
		Current:  downBps,
		Width:    width,
		IsUpload: false,
	}
	return up.Render() + "\n" + down.Render()
}

// ZeroBar renders two idle speed bars.
func ZeroBar(width int) string {
	return SpeedBarPair(0, 0, width)
}

// Suppress unused import warning
var _ = barBg
