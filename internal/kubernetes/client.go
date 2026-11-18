package kubernetes

import (
	"context"
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	baseURL, token string
	http           *http.Client
}
type Overview struct {
	Available   bool         `json:"available"`
	Error       string       `json:"error,omitempty"`
	Nodes       []Node       `json:"nodes"`
	Pods        []Pod        `json:"pods"`
	Deployments []Deployment `json:"deployments"`
}
type Node struct {
	Name       string    `json:"name"`
	Status     string    `json:"status"`
	Roles      []string  `json:"roles"`
	Version    string    `json:"version"`
	InternalIP string    `json:"internal_ip,omitempty"`
	CreatedAt  time.Time `json:"created_at,omitempty"`
}
type Pod struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Node      string `json:"node"`
	Phase     string `json:"phase"`
	Ready     int    `json:"ready"`
	Total     int    `json:"total"`
	Restarts  int    `json:"restarts"`
	Reason    string `json:"reason,omitempty"`
}
type Deployment struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Ready     int    `json:"ready"`
	Desired   int    `json:"desired"`
}
type listMeta struct {
	Items []json.RawMessage `json:"items"`
}

func NewInCluster() (*Client, error) {
	host := os.Getenv("KUBERNETES_SERVICE_HOST")
	port := os.Getenv("KUBERNETES_SERVICE_PORT")
	if host == "" {
		return nil, fmt.Errorf("KUBERNETES_SERVICE_HOST is not set")
	}
	token, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token")
	if err != nil {
		return nil, err
	}
	ca, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/ca.crt")
	if err != nil {
		return nil, err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, fmt.Errorf("could not load Kubernetes CA")
	}
	tr := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}
	if port == "" {
		port = "443"
	}
	return &Client{baseURL: "https://" + host + ":" + port, token: strings.TrimSpace(string(token)), http: &http.Client{Transport: tr, Timeout: 5 * time.Second}}, nil
}
func (c *Client) Overview(ctx context.Context) Overview {
	out := Overview{Nodes: []Node{}, Pods: []Pod{}, Deployments: []Deployment{}}
	var nodes listMeta
	if err := c.get(ctx, "/api/v1/nodes", &nodes); err != nil {
		out.Error = err.Error()
		return out
	}
	for _, raw := range nodes.Items {
		var x struct {
			Metadata struct {
				Name              string            `json:"name"`
				Labels            map[string]string `json:"labels"`
				CreationTimestamp time.Time         `json:"creationTimestamp"`
			} `json:"metadata"`
			Status struct {
				NodeInfo struct {
					KubeletVersion string `json:"kubeletVersion"`
				} `json:"nodeInfo"`
				Addresses  []struct{ Type, Address string } `json:"addresses"`
				Conditions []struct{ Type, Status string }  `json:"conditions"`
			} `json:"status"`
		}
		if json.Unmarshal(raw, &x) != nil {
			continue
		}
		n := Node{Name: x.Metadata.Name, Version: x.Status.NodeInfo.KubeletVersion, CreatedAt: x.Metadata.CreationTimestamp}
		for k, v := range x.Metadata.Labels {
			if strings.HasPrefix(k, "node-role.kubernetes.io/") && v != "" {
				n.Roles = append(n.Roles, v)
			} else if strings.HasPrefix(k, "node-role.kubernetes.io/") {
				n.Roles = append(n.Roles, strings.TrimPrefix(k, "node-role.kubernetes.io/"))
			}
		}
		for _, a := range x.Status.Addresses {
			if a.Type == "InternalIP" {
				n.InternalIP = a.Address
				break
			}
		}
		n.Status = "Unknown"
		for _, co := range x.Status.Conditions {
			if co.Type == "Ready" {
				if co.Status == "True" {
					n.Status = "Ready"
				} else {
					n.Status = "NotReady"
				}
			}
		}
		out.Nodes = append(out.Nodes, n)
	}
	var pods listMeta
	if err := c.get(ctx, "/api/v1/pods", &pods); err != nil {
		out.Error = err.Error()
		return out
	}
	for _, raw := range pods.Items {
		var x struct {
			Metadata struct{ Name, Namespace string } `json:"metadata"`
			Spec     struct {
				NodeName string `json:"nodeName"`
			} `json:"spec"`
			Status struct {
				Phase             string `json:"phase"`
				ContainerStatuses []struct {
					Ready        bool `json:"ready"`
					RestartCount int  `json:"restartCount"`
					State        struct {
						Waiting *struct {
							Reason string `json:"reason"`
						} `json:"waiting"`
					} `json:"state"`
				} `json:"containerStatuses"`
			} `json:"status"`
		}
		if json.Unmarshal(raw, &x) != nil {
			continue
		}
		p := Pod{Name: x.Metadata.Name, Namespace: x.Metadata.Namespace, Node: x.Spec.NodeName, Phase: x.Status.Phase}
		for _, cs := range x.Status.ContainerStatuses {
			p.Total++
			if cs.Ready {
				p.Ready++
			}
			p.Restarts += cs.RestartCount
			if cs.State.Waiting != nil && p.Reason == "" {
				p.Reason = cs.State.Waiting.Reason
			}
		}
		out.Pods = append(out.Pods, p)
	}
	var deps listMeta
	if err := c.get(ctx, "/apis/apps/v1/deployments", &deps); err == nil {
		for _, raw := range deps.Items {
			var x struct {
				Metadata struct{ Name, Namespace string } `json:"metadata"`
				Spec     struct {
					Replicas *int `json:"replicas"`
				} `json:"spec"`
				Status struct {
					ReadyReplicas int `json:"readyReplicas"`
				} `json:"status"`
			}
			if json.Unmarshal(raw, &x) != nil {
				continue
			}
			desired := 1
			if x.Spec.Replicas != nil {
				desired = *x.Spec.Replicas
			}
			out.Deployments = append(out.Deployments, Deployment{Name: x.Metadata.Name, Namespace: x.Metadata.Namespace, Ready: x.Status.ReadyReplicas, Desired: desired})
		}
	}
	out.Available = true
	return out
}
// ScaleDeployment changes only the replica count of an explicitly configured game Deployment.
func (c *Client) ScaleDeployment(ctx context.Context, namespace, name string, replicas int) error {
	if replicas < 0 || replicas > 1 {
		return fmt.Errorf("game server replicas must be 0 or 1")
	}
	path := "/apis/apps/v1/namespaces/" + url.PathEscape(namespace) + "/deployments/" + url.PathEscape(name) + "/scale"
	return c.patch(ctx, path, map[string]any{"spec": map[string]int{"replicas": replicas}})
}

// RestartDeployment requests a rolling restart without deleting pods or persistent data.
func (c *Client) RestartDeployment(ctx context.Context, namespace, name string) error {
	path := "/apis/apps/v1/namespaces/" + url.PathEscape(namespace) + "/deployments/" + url.PathEscape(name)
	patch := map[string]any{"spec": map[string]any{"template": map[string]any{"metadata": map[string]any{"annotations": map[string]string{"homelab-monitor.io/restarted-at": time.Now().UTC().Format(time.RFC3339Nano)}}}}}
	return c.patch(ctx, path, patch)
}

func (c *Client) patch(ctx context.Context, path string, value any) error {
	body, err := json.Marshal(value)
	if err != nil { return err }
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, c.baseURL+path, bytes.NewReader(body))
	if err != nil { return err }
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/strategic-merge-patch+json")
	resp, err := c.http.Do(req)
	if err != nil { return err }
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Kubernetes API returned %s for %s", resp.Status, path)
	}
	return nil
}

// CreateResource creates a Kubernetes API object from a validated, server-generated manifest.
func (c *Client) CreateResource(ctx context.Context, path string, value any) error {
	body, err := json.Marshal(value)
	if err != nil { return err }
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil { return err }
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil { return err }
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Kubernetes API returned %s creating resource", resp.Status)
	}
	return nil
}

func (c *Client) get(ctx context.Context, path string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("Kubernetes API returned %s for %s", resp.Status, path)
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}
