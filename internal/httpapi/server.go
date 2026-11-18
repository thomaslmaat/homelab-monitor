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
	"sync"
	"time"
)

type Server struct {
	store *storage.Store
	token string
	kube  *kubernetes.Client
	registry *integrations.Registry
	gameServers []gameservers.Config
	gameMu sync.Mutex
	authMu sync.Mutex
	sessions map[string]authSession
	loginAttempts map[string]loginAttempt
}
type HostView struct {
	model.HostMetrics
	Online     bool  `json:"online"`
	AgeSeconds int64 `json:"age_seconds"`
}

func New(store *storage.Store, kube *kubernetes.Client) *Server {
	s := &Server{store: store, token: os.Getenv("HOMELAB_MONITOR_TOKEN"), kube: kube, registry: integrations.NewRegistry(), sessions: make(map[string]authSession), loginAttempts: make(map[string]loginAttempt)}
	if store != nil { if err := s.bootstrapAdmin(); err != nil { log.Printf("dashboard admin bootstrap failed: %v", err) } }
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
		if v.Error != "" { return integrations.Health{Status:"degraded",Message:v.Error} }
		return integrations.Health{Status:"connected",Message:"PVC, PV and Longhorn volume resources are readable"}
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
	mux.HandleFunc("GET /api/v1/auth/status", s.authStatus)
	mux.HandleFunc("POST /api/v1/auth/login", s.login)
	mux.HandleFunc("POST /api/v1/auth/password", s.changePassword)
	mux.HandleFunc("POST /api/v1/auth/logout", s.logout)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("POST /api/v1/metrics", s.ingest)
	mux.HandleFunc("GET /api/v1/hosts", s.hosts)
	mux.HandleFunc("GET /api/v1/history", s.history)
	mux.HandleFunc("GET /api/v1/overview", s.overview)
	mux.HandleFunc("GET /api/v1/storage", s.storage)
	mux.HandleFunc("GET /api/v1/integrations", s.integrations)
	mux.HandleFunc("GET /api/v1/game-servers", s.managedGameInventory)
	mux.HandleFunc("GET /api/v1/game-servers/catalog", s.gameCatalog)
	mux.HandleFunc("POST /api/v1/game-servers", s.createManagedGame)
	mux.HandleFunc("PUT /api/v1/game-servers/{id}", s.updateManagedGame)
	mux.HandleFunc("GET /api/v1/game-servers/{id}/logs", s.gameServerLogs)
	mux.HandleFunc("DELETE /api/v1/game-servers/{id}", s.deleteManagedGame)
	mux.HandleFunc("POST /api/v1/game-servers/{id}/actions", s.gameServerAction)
	mux.Handle("/", dashboard.Handler())
	return securityHeaders(s.authMiddleware(mux))
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

type gameServerActionPayload struct { Action string `json:"action"` }

func (s *Server) gameServerAction(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if s.kube == nil { writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error":"Kubernetes client is unavailable"}); return }
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	rows, err := s.readManagedGames(ctx)
	if err != nil { writeJSON(w, http.StatusBadGateway, map[string]string{"error":"Could not read game server registry: "+err.Error()}); return }
	var target *managedGameServer
	for i := range rows { if rows[i].ID == id { target = &rows[i]; break } }
	if target == nil { writeJSON(w, http.StatusNotFound, map[string]string{"error":"Game server is not registered"}); return }
	r.Body = http.MaxBytesReader(w, r.Body, 4096); defer r.Body.Close()
	var payload gameServerActionPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil { writeJSON(w, http.StatusBadRequest, map[string]string{"error":"Invalid action request"}); return }
	var actionErr error
	switch payload.Action {
	case "start": actionErr = s.kube.ScaleDeployment(ctx, target.Namespace, target.Workload, 1)
	case "stop": actionErr = s.kube.ScaleDeployment(ctx, target.Namespace, target.Workload, 0)
	case "restart":
		cluster := s.kube.Overview(ctx)
		found, running := false, false
		for _, d := range cluster.Deployments { if d.Namespace == target.Namespace && d.Name == target.Workload { found=true; running=d.Desired>0; break } }
		if !cluster.Available { writeJSON(w,503,map[string]string{"error":"Could not verify game server status: "+cluster.Error}); return }
		if !found { writeJSON(w,404,map[string]string{"error":"Configured Deployment was not found"}); return }
		if !running { writeJSON(w,409,map[string]string{"error":"Start the stopped game server before restarting it"}); return }
		actionErr = s.kube.RestartDeployment(ctx, target.Namespace, target.Workload)
	default: writeJSON(w,400,map[string]string{"error":"Action must be start, stop or restart"}); return
	}
	if actionErr != nil { log.Printf("game server action failed: user=%q id=%q action=%q error=%v",s.sessionUser(r),id,payload.Action,actionErr); writeJSON(w,502,map[string]string{"error":"Kubernetes could not apply the action: "+actionErr.Error()}); return }
	log.Printf("game server action accepted: user=%q id=%q game=%q action=%q",sessionUser(r),id,target.Game,payload.Action)
	writeJSON(w,202,map[string]string{"status":"accepted","action":payload.Action,"id":id})
}

func (s *Server) sessionUser(r *http.Request) string {
	if username,ok:=s.sessionUsername(r);ok { return username }
	return "unknown"
}
