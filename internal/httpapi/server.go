package httpapi

import (
	"context"
	"encoding/json"
	"github.com/thomaslmaat/homelab-monitor/internal/dashboard"
	"github.com/thomaslmaat/homelab-monitor/internal/gameservers"
	"github.com/thomaslmaat/homelab-monitor/internal/integrations"
	"github.com/thomaslmaat/homelab-monitor/internal/kubernetes"
	"github.com/thomaslmaat/homelab-monitor/internal/model"
	"github.com/thomaslmaat/homelab-monitor/internal/storage"
	"log"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

type Server struct {
	store *storage.Store
	token string
	kube  *kubernetes.Client
	registry *integrations.Registry
	gameServers []gameservers.Config
}
type HostView struct {
	model.HostMetrics
	Online     bool  `json:"online"`
	AgeSeconds int64 `json:"age_seconds"`
}

func New(store *storage.Store, kube *kubernetes.Client) *Server {
	s := &Server{store: store, token: os.Getenv("HOMELAB_MONITOR_TOKEN"), kube: kube, registry: integrations.NewRegistry()}
	configured, err := gameservers.Parse(os.Getenv("GAME_SERVERS_JSON"))
	if err != nil { log.Printf("game server configuration rejected: %v", err) } else { s.gameServers = configured }
	_ = s.registry.Register(integrations.FuncAdapter{AdapterID:"kubernetes", DisplayName:"Kubernetes API", AdapterKind:"infrastructure", AdapterVersion:"built-in", Supported:[]integrations.Capability{{ID:"resources.read",Description:"Read cluster nodes, pods and deployments"}}, HealthCheck:func(ctx context.Context) integrations.Health {
		if s.kube == nil { return integrations.Health{Status:"not_configured",Message:"In-cluster Kubernetes client is unavailable"} }
		if !s.kube.Overview(ctx).Available { return integrations.Health{Status:"degraded",Message:"Kubernetes API could not return cluster inventory"} }
		return integrations.Health{Status:"connected",Message:"Cluster inventory is readable"}
	}})
	_ = s.registry.Register(integrations.FuncAdapter{AdapterID:"longhorn", DisplayName:"Longhorn", AdapterKind:"storage", AdapterVersion:"built-in", Supported:[]integrations.Capability{{ID:"resources.read",Description:"Read PVC, PV and Longhorn volume health"}}, HealthCheck:func(ctx context.Context) integrations.Health {
		if s.kube == nil { return integrations.Health{Status:"not_configured",Message:"Kubernetes client is unavailable"} }
		v := s.kube.Storage(ctx)
		if !v.Available { return integrations.Health{Status:"degraded",Message:"Storage API unavailable; check PVC/PV and Longhorn read permissions"} }
		return integrations.Health{Status:"connected",Message:"Storage resources are readable"}
	}})
	_ = s.registry.Register(integrations.FuncAdapter{AdapterID:"homelab-agents", DisplayName:"Homelab Agents", AdapterKind:"monitoring", AdapterVersion:"built-in", Supported:[]integrations.Capability{{ID:"metrics.read",Description:"Read host metrics and agent heartbeat"}}, HealthCheck:func(ctx context.Context) integrations.Health {
		hosts, err := s.store.Latest(ctx)
		if err != nil { return integrations.Health{Status:"degraded",Message:"Host metrics could not be read"} }
		if len(hosts) == 0 { return integrations.Health{Status:"degraded",Message:"No agent heartbeat received yet"} }
		return integrations.Health{Status:"connected",Message:fmt.Sprintf("%d agent(s) reporting",len(hosts))}
	}})
	return s
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("POST /api/v1/metrics", s.ingest)
	mux.HandleFunc("GET /api/v1/hosts", s.hosts)
	mux.HandleFunc("GET /api/v1/history", s.history)
	mux.HandleFunc("GET /api/v1/overview", s.overview)
	mux.HandleFunc("GET /api/v1/storage", s.storage)
	mux.HandleFunc("GET /api/v1/integrations", s.integrations)
	mux.HandleFunc("GET /api/v1/game-servers", s.gameServersInventory)
	mux.Handle("/", dashboard.Handler())
	return securityHeaders(mux)
}
func (s *Server) authorized(r *http.Request) bool {
	return s.token == "" || strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")) == s.token
}
func (s *Server) ingest(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeJSON(w, 401, map[string]string{"error": "unauthorized"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	defer r.Body.Close()
	var m model.HostMetrics
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid metrics payload"})
		return
	}
	if m.SchemaVersion != 1 || strings.TrimSpace(m.Hostname) == "" || m.CollectedAt.IsZero() || m.CPUPercent < 0 || m.CPUPercent > 100 || m.MemoryUsed > m.MemoryTotal || m.DiskUsed > m.DiskTotal {
		writeJSON(w, 400, map[string]string{"error": "missing or invalid required metric fields"})
		return
	}
	if m.CollectedAt.After(time.Now().Add(5 * time.Minute)) {
		writeJSON(w, 400, map[string]string{"error": "collected_at is too far in the future"})
		return
	}
	if err := s.store.Insert(r.Context(), m); err != nil {
		log.Printf("store metrics: %v", err)
		writeJSON(w, 500, map[string]string{"error": "could not store metrics"})
		return
	}
	writeJSON(w, 202, map[string]string{"status": "accepted"})
}
func (s *Server) hosts(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	hosts, err := s.store.Latest(ctx)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "could not read hosts"})
		return
	}
	writeJSON(w, 200, map[string]any{"hosts": hostViews(hosts)})
}
func (s *Server) history(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	rows, err := s.store.History(ctx, r.URL.Query().Get("host"), 120)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "could not read history"})
		return
	}
	writeJSON(w, 200, map[string]any{"history": rows})
}
func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	hosts, err := s.store.Latest(ctx)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "could not read hosts"})
		return
	}
	var cluster kubernetes.Overview
	if s.kube != nil {
		cluster = s.kube.Overview(ctx)
	} else {
		cluster = kubernetes.Overview{Error: "Kubernetes client is not configured", Nodes: []kubernetes.Node{}, Pods: []kubernetes.Pod{}, Deployments: []kubernetes.Deployment{}}
	}
	writeJSON(w, 200, map[string]any{"hosts": hostViews(hosts), "cluster": cluster, "generated_at": time.Now().UTC()})
}
func (s *Server) storage(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	if s.kube == nil {
		writeJSON(w, 503, map[string]any{"available": false, "error": "Kubernetes client is not configured", "claims": []any{}, "persistent_volumes": []any{}, "volumes": []any{}})
		return
	}
	writeJSON(w, 200, s.kube.Storage(ctx))
}

func hostViews(hosts []model.HostMetrics) []HostView {
	out := make([]HostView, 0, len(hosts))
	now := time.Now()
	for _, m := range hosts {
		age := int64(now.Sub(m.CollectedAt).Seconds())
		if age < 0 {
			age = 0
		}
		out = append(out, HostView{HostMetrics: m, Online: age <= 45, AgeSeconds: age})
	}
	return out
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) integrations(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	writeJSON(w, http.StatusOK, map[string]any{"integrations": s.registry.List(ctx), "generated_at": time.Now().UTC()})
}

func (s *Server) gameServersInventory(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	if len(s.gameServers) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"configured": false, "game_servers": []gameservers.View{}, "message": "No game servers configured"})
		return
	}
	if s.kube == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"configured": true, "game_servers": gameservers.Unknown(s.gameServers), "error": "Kubernetes client is not configured"})
		return
	}
	cluster := s.kube.Overview(ctx)
	if !cluster.Available {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"configured": true, "game_servers": gameservers.Unknown(s.gameServers), "error": cluster.Error})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"configured": true, "game_servers": gameservers.Resolve(s.gameServers, cluster.Deployments)})
}
