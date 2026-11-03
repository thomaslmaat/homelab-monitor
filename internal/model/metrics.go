package model

import "time"

// HostMetrics is the versioned payload sent by a Homelab Monitor agent.
type HostMetrics struct {
	SchemaVersion  int       `json:"schema_version"`
	Hostname       string    `json:"hostname"`
	CollectedAt    time.Time `json:"collected_at"`
	CPUPercent     float64   `json:"cpu_percent"`
	MemoryTotal    uint64    `json:"memory_total_bytes"`
	MemoryUsed     uint64    `json:"memory_used_bytes"`
	DiskTotal      uint64    `json:"disk_total_bytes"`
	DiskUsed       uint64    `json:"disk_used_bytes"`
	NetworkRxBytes uint64    `json:"network_rx_bytes"`
	NetworkTxBytes uint64    `json:"network_tx_bytes"`
	UptimeSeconds  uint64    `json:"uptime_seconds"`
	TemperatureC   *float64  `json:"temperature_c,omitempty"`
}
