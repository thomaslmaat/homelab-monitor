//go:build linux

package collector

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/thomaslmaat/homelab-monitor/internal/model"
)

type Linux struct {
	ProcRoot string
	SysRoot  string
	RootFS   string
	lastCPU  uint64
	lastIdle uint64
	lastAt   time.Time
}

func NewLinux() *Linux {
	return &Linux{ProcRoot: envOr("PROC_ROOT", "/proc"), SysRoot: envOr("SYS_ROOT", "/sys"), RootFS: envOr("ROOT_FS", "/")}
}

func (c *Linux) Collect() (model.HostMetrics, error) {
	host := os.Getenv("NODE_NAME")
	if host == "" {
		var err error
		host, err = os.Hostname()
		if err != nil {
			return model.HostMetrics{}, err
		}
	}
	cpu, err := c.cpuPercent()
	if err != nil {
		return model.HostMetrics{}, err
	}
	memTotal, memUsed, err := c.memory()
	if err != nil {
		return model.HostMetrics{}, err
	}
	diskTotal, diskUsed, err := c.disk()
	if err != nil {
		return model.HostMetrics{}, err
	}
	rx, tx, err := c.network()
	if err != nil {
		return model.HostMetrics{}, err
	}
	uptime, err := c.uptime()
	if err != nil {
		return model.HostMetrics{}, err
	}
	return model.HostMetrics{SchemaVersion: 1, Hostname: host, CollectedAt: time.Now().UTC(), CPUPercent: cpu, MemoryTotal: memTotal, MemoryUsed: memUsed, DiskTotal: diskTotal, DiskUsed: diskUsed, NetworkRxBytes: rx, NetworkTxBytes: tx, UptimeSeconds: uptime, TemperatureC: c.temperature()}, nil
}

func (c *Linux) cpuPercent() (float64, error) {
	f, err := os.Open(filepath.Join(c.ProcRoot, "stat"))
	if err != nil {
		return 0, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	if !s.Scan() {
		return 0, errors.New("missing cpu line in proc/stat")
	}
	fields := strings.Fields(s.Text())
	if len(fields) < 5 {
		return 0, errors.New("invalid cpu line in proc/stat")
	}
	var total, idle uint64
	for i, raw := range fields[1:] {
		n, e := strconv.ParseUint(raw, 10, 64)
		if e != nil {
			return 0, e
		}
		total += n
		if i == 3 || i == 4 {
			idle += n
		}
	}
	defer func() { c.lastCPU, c.lastIdle, c.lastAt = total, idle, time.Now() }()
	if c.lastAt.IsZero() || total <= c.lastCPU {
		return 0, nil
	}
	deltaTotal, deltaIdle := total-c.lastCPU, idle-c.lastIdle
	if deltaTotal == 0 {
		return 0, nil
	}
	return 100 * (1 - float64(deltaIdle)/float64(deltaTotal)), nil
}

func (c *Linux) memory() (uint64, uint64, error) {
	f, err := os.Open(filepath.Join(c.ProcRoot, "meminfo"))
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	values := map[string]uint64{}
	s := bufio.NewScanner(f)
	for s.Scan() {
		p := strings.Fields(s.Text())
		if len(p) >= 2 && (p[0] == "MemTotal:" || p[0] == "MemAvailable:") {
			n, e := strconv.ParseUint(p[1], 10, 64)
			if e != nil {
				return 0, 0, e
			}
			values[strings.TrimSuffix(p[0], ":")] = n * 1024
		}
	}
	if err := s.Err(); err != nil {
		return 0, 0, err
	}
	total, available := values["MemTotal"], values["MemAvailable"]
	if total == 0 {
		return 0, 0, errors.New("MemTotal missing")
	}
	if available > total {
		available = total
	}
	return total, total - available, nil
}

func (c *Linux) disk() (uint64, uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(c.RootFS, &st); err != nil {
		return 0, 0, err
	}
	total := st.Blocks * uint64(st.Bsize)
	free := st.Bavail * uint64(st.Bsize)
	if free > total {
		free = total
	}
	return total, total - free, nil
}

func (c *Linux) network() (uint64, uint64, error) {
	f, err := os.Open(filepath.Join(c.ProcRoot, "net/dev"))
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	var rx, tx uint64
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if !strings.Contains(line, ":") {
			continue
		}
		p := strings.SplitN(line, ":", 2)
		name := strings.TrimSpace(p[0])
		if name == "lo" {
			continue
		}
		fields := strings.Fields(p[1])
		if len(fields) < 9 {
			continue
		}
		a, e1 := strconv.ParseUint(fields[0], 10, 64)
		b, e2 := strconv.ParseUint(fields[8], 10, 64)
		if e1 == nil && e2 == nil {
			rx += a
			tx += b
		}
	}
	return rx, tx, s.Err()
}

func (c *Linux) uptime() (uint64, error) {
	b, err := os.ReadFile(filepath.Join(c.ProcRoot, "uptime"))
	if err != nil {
		return 0, err
	}
	p := strings.Fields(string(b))
	if len(p) == 0 {
		return 0, fmt.Errorf("invalid proc/uptime")
	}
	n, err := strconv.ParseFloat(p[0], 64)
	return uint64(n), err
}

func (c *Linux) temperature() *float64 {
	matches, _ := filepath.Glob(filepath.Join(c.SysRoot, "class/thermal/thermal_zone*/temp"))
	for _, path := range matches {
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		n, err := strconv.ParseFloat(strings.TrimSpace(string(b)), 64)
		if err != nil {
			continue
		}
		if n > 1000 {
			n /= 1000
		}
		if n >= -20 && n <= 150 {
			return &n
		}
	}
	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
