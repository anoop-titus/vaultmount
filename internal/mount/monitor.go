package mount

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
)

// Sample holds a single bandwidth measurement.
type Sample struct {
	UpBytesPerSec   float64
	DownBytesPerSec float64
	Timestamp       time.Time
}

// Monitor tracks bandwidth for an SSHFS connection by sampling
// netstat interface byte counters.
type Monitor struct {
	remoteHost  string
	intervalMs  int
	mu          sync.RWMutex
	latest      Sample
	prevTxBytes int64
	prevRxBytes int64
	prevTime    time.Time
}

// NewMonitor creates a bandwidth monitor for the given remote host.
func NewMonitor(remoteHost string, intervalMs int) *Monitor {
	if intervalMs < 100 {
		intervalMs = 500
	}
	return &Monitor{
		remoteHost: remoteHost,
		intervalMs: intervalMs,
	}
}

// Start begins sampling in the background. Cancel ctx to stop.
func (m *Monitor) Start(ctx context.Context) {
	go m.loop(ctx)
}

// Latest returns the most recent bandwidth sample.
func (m *Monitor) Latest() Sample {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.latest
}

func (m *Monitor) loop(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(m.intervalMs) * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.sample()
		}
	}
}

func (m *Monitor) sample() {
	out, err := DefaultRunner("netstat", "-b", "-n", "-I", "en0")
	if err != nil {
		out, err = DefaultRunner("netstat", "-b", "-n", "-I", "en1")
		if err != nil {
			return
		}
	}

	tx, rx := parseNetstatBytes(string(out))
	now := time.Now()

	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.prevTime.IsZero() {
		dt := now.Sub(m.prevTime).Seconds()
		if dt > 0 {
			upBps := float64(tx-m.prevTxBytes) / dt
			downBps := float64(rx-m.prevRxBytes) / dt
			if upBps < 0 {
				upBps = 0
			}
			if downBps < 0 {
				downBps = 0
			}
			m.latest = Sample{
				UpBytesPerSec:   upBps,
				DownBytesPerSec: downBps,
				Timestamp:       now,
			}
		}
	}
	m.prevTxBytes = tx
	m.prevRxBytes = rx
	m.prevTime = now
}

// parseNetstatBytes extracts tx (obytes) and rx (ibytes) from netstat -b output.
// netstat -b -n -I en0 columns: Name Mtu Network Address Ipkts Ierrs Ibytes Opkts Oerrs Obytes Coll
func parseNetstatBytes(output string) (tx, rx int64) {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}
		if fields[0] == "Name" {
			continue
		}
		if strings.HasPrefix(fields[0], "en") {
			parseI64(fields[6], &rx)  // Ibytes
			parseI64(fields[9], &tx)  // Obytes
			return
		}
	}
	return 0, 0
}

func parseI64(s string, out *int64) {
	var v int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return
		}
		v = v*10 + int64(c-'0')
	}
	*out = v
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
