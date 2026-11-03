package httpapi

import (
	"context"
	"encoding/json"
	"github.com/thomaslmaat/homelab-monitor/internal/dashboard"
	"github.com/thomaslmaat/homelab-monitor/internal/kubernetes"
	"github.com/thomaslmaat/homelab-monitor/internal/model"
	"github.com/thomaslmaat/homelab-monitor/internal/storage"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type Server struct {
	store *storage.Store
	token string
	kube  *kubernetes.Client
}
type HostView struct {
	model.HostMetrics
	Online     bool  `json:"online"`
	AgeSeconds int64 `json:"age_seconds"`
}

func New(store *storage.Store, kube *kubernetes.Client) *Server {
	return &Server{store: store, token: os.Getenv("HOMELAB_MONITOR_TOKEN"), kube: kube}
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("POST /api/v1/metrics", s.ingest)
	mux.HandleFunc("GET /api/v1/hosts", s.hosts)
	mux.HandleFunc("GET /api/v1/history", s.history)
	mux.HandleFunc("GET /api/v1/overview", s.overview)
	mux.HandleFunc("GET /api/v1/storage", s.storage)
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
