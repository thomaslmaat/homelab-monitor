package gameservers

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/thomaslmaat/homelab-monitor/internal/kubernetes"
)

// Config explicitly maps a game server to a Kubernetes Deployment.
// Workloads are never auto-detected from names or labels.
type Config struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Game          string `json:"game"`
	Namespace     string `json:"namespace"`
	Workload      string `json:"workload"`
	WorkloadType  string `json:"workload_type"`
}

type View struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Game         string `json:"game"`
	Namespace    string `json:"namespace"`
	Workload     string `json:"workload"`
	WorkloadType string `json:"workload_type"`
	State        string `json:"state"`
	Ready        int    `json:"ready"`
	Desired      int    `json:"desired"`
}

func Parse(raw string) ([]Config, error) {
	if strings.TrimSpace(raw) == "" { return []Config{}, nil }
	var configs []Config
	if err := json.Unmarshal([]byte(raw), &configs); err != nil { return nil, fmt.Errorf("GAME_SERVERS_JSON must be a JSON array: %w", err) }
	seen := make(map[string]bool, len(configs))
	for i := range configs {
		c := &configs[i]
		c.ID, c.Name, c.Game = strings.TrimSpace(c.ID), strings.TrimSpace(c.Name), strings.TrimSpace(c.Game)
		c.Namespace, c.Workload, c.WorkloadType = strings.TrimSpace(c.Namespace), strings.TrimSpace(c.Workload), strings.ToLower(strings.TrimSpace(c.WorkloadType))
		if c.ID == "" || c.Name == "" || c.Game == "" || c.Namespace == "" || c.Workload == "" { return nil, fmt.Errorf("game server at index %d requires id, name, game, namespace and workload", i) }
		if seen[c.ID] { return nil, fmt.Errorf("duplicate game server id %q", c.ID) }
		seen[c.ID] = true
		if c.WorkloadType == "" { c.WorkloadType = "deployment" }
		if c.WorkloadType != "deployment" { return nil, fmt.Errorf("game server %q uses unsupported workload_type %q; only deployment is supported in this release", c.ID, c.WorkloadType) }
	}
	return configs, nil
}

func Unknown(configs []Config) []View {
	out := make([]View, 0, len(configs))
	for _, c := range configs { out = append(out, View{ID:c.ID, Name:c.Name, Game:c.Game, Namespace:c.Namespace, Workload:c.Workload, WorkloadType:c.WorkloadType, State:"unknown"}) }
	return out
}

func Resolve(configs []Config, deployments []kubernetes.Deployment) []View {
	out := make([]View, 0, len(configs))
	for _, c := range configs {
		v := View{ID:c.ID, Name:c.Name, Game:c.Game, Namespace:c.Namespace, Workload:c.Workload, WorkloadType:c.WorkloadType, State:"not_found"}
		for _, d := range deployments {
			if d.Namespace == c.Namespace && d.Name == c.Workload {
				v.Ready, v.Desired = d.Ready, d.Desired
				switch { case d.Desired == 0: v.State = "stopped"; case d.Ready > 0: v.State = "running"; default: v.State = "starting" }
				break
			}
		}
		out = append(out, v)
	}
	return out
}
