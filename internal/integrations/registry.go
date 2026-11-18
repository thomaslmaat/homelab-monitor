package integrations

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Health is deliberately small and contains no credentials or provider response bodies.
type Health struct {
	Status      string    `json:"status"`
	Message     string    `json:"message,omitempty"`
	CheckedAt   time.Time `json:"checked_at"`
}

// Capability describes a supported operation. A capability is descriptive only;
// authorization must still be enforced by the API handling each operation.
type Capability struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

// Adapter is the built-in integration contract. Adapters are compiled into the
// Core; arbitrary downloaded code is never loaded at runtime.
type Adapter interface {
	ID() string
	Name() string
	Kind() string
	Version() string
	Capabilities() []Capability
	Check(context.Context) Health
}

type Snapshot struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	Kind         string       `json:"kind"`
	Version      string       `json:"version"`
	Capabilities []Capability `json:"capabilities"`
	Health       Health       `json:"health"`
}

type Registry struct {
	mu         sync.RWMutex
	adapters   map[string]Adapter
	cached     []Snapshot
	cacheUntil time.Time
}

const healthCacheTTL = 30 * time.Second

func NewRegistry() *Registry { return &Registry{adapters: make(map[string]Adapter)} }

func (r *Registry) Register(a Adapter) error {
	if a == nil { return fmt.Errorf("adapter is required") }
	id := strings.TrimSpace(a.ID())
	r.mu.Lock()
	defer r.mu.Unlock()
	if id == "" { return fmt.Errorf("adapter ID is required") }
	if strings.TrimSpace(a.Name()) == "" || strings.TrimSpace(a.Kind()) == "" || strings.TrimSpace(a.Version()) == "" {
		return fmt.Errorf("adapter %q must define name, kind and version", id)
	}
	if _, exists := r.adapters[id]; exists { return fmt.Errorf("adapter %q is already registered", id) }
	r.adapters[id] = a
	r.cacheUntil = time.Time{}
	return nil
}

func cloneSnapshots(in []Snapshot) []Snapshot {
	out := make([]Snapshot, len(in))
	for i, item := range in {
		out[i] = item
		out[i].Capabilities = append([]Capability(nil), item.Capabilities...)
	}
	return out
}

// List caches adapter health checks briefly. This avoids repeatedly querying the
// Kubernetes and Longhorn APIs on every dashboard refresh.
func (r *Registry) List(ctx context.Context) []Snapshot {
	now := time.Now()
	r.mu.RLock()
	if now.Before(r.cacheUntil) {
		out := cloneSnapshots(r.cached)
		r.mu.RUnlock()
		return out
	}
	adapters := make([]Adapter, 0, len(r.adapters))
	for _, a := range r.adapters {
		adapters = append(adapters, a)
	}
	r.mu.RUnlock()

	out := make([]Snapshot, 0, len(adapters))
	for _, a := range adapters {
		health := a.Check(ctx)
		if health.Status == "" { health.Status = "unknown" }
		if health.CheckedAt.IsZero() { health.CheckedAt = time.Now().UTC() }
		caps := append([]Capability(nil), a.Capabilities()...)
		out = append(out, Snapshot{ID:a.ID(), Name:a.Name(), Kind:a.Kind(), Version:a.Version(), Capabilities:caps, Health:health})
	}
	sort.Slice(out, func(i,j int) bool { return out[i].ID < out[j].ID })

	r.mu.Lock()
	r.cached = cloneSnapshots(out)
	r.cacheUntil = time.Now().Add(healthCacheTTL)
	r.mu.Unlock()
	return out
}

type FuncAdapter struct {
	AdapterID string
	DisplayName string
	AdapterKind string
	AdapterVersion string
	Supported []Capability
	HealthCheck func(context.Context) Health
}
func (a FuncAdapter) ID() string { return a.AdapterID }
func (a FuncAdapter) Name() string { return a.DisplayName }
func (a FuncAdapter) Kind() string { return a.AdapterKind }
func (a FuncAdapter) Version() string { return a.AdapterVersion }
func (a FuncAdapter) Capabilities() []Capability { return append([]Capability(nil), a.Supported...) }
func (a FuncAdapter) Check(ctx context.Context) Health {
	if a.HealthCheck == nil { return Health{Status:"unknown", Message:"No health check configured"} }
	return a.HealthCheck(ctx)
}
