package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type StorageOverview struct {
	Available         bool              `json:"available"`
	Error             string            `json:"error,omitempty"`
	Claims            []StorageClaim    `json:"claims"`
	PersistentVolumes []PersistentVolume `json:"persistent_volumes"`
	Nodes             []StorageNode     `json:"nodes"`
	Volumes           []LonghornVolume  `json:"volumes"`
}

type StorageNode struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

type StorageClaim struct {
	Name         string `json:"name"`
	Namespace    string `json:"namespace"`
	Status       string `json:"status"`
	Capacity     string `json:"capacity,omitempty"`
	StorageClass string `json:"storage_class,omitempty"`
	VolumeName   string `json:"volume_name,omitempty"`
}

type PersistentVolume struct {
	Name         string `json:"name"`
	Status       string `json:"status"`
	Capacity     string `json:"capacity,omitempty"`
	StorageClass string `json:"storage_class,omitempty"`
	Claim        string `json:"claim,omitempty"`
}

type LonghornReplica struct {
	Name       string `json:"name"`
	Node       string `json:"node,omitempty"`
	NodeStatus string `json:"node_status"`
	State      string `json:"state"`
	Active     bool   `json:"active"`
}

type LonghornVolume struct {
	Name             string            `json:"name"`
	State            string            `json:"state,omitempty"`
	Robustness       string            `json:"robustness,omitempty"`
	Capacity         int64             `json:"capacity"`
	ActualSize       int64             `json:"actual_size"`
	NumberOfReplicas int               `json:"number_of_replicas"`
	ActiveReplicas   int               `json:"active_replicas"`
	Replicas         []LonghornReplica `json:"replicas"`
}

func (c *Client) Storage(ctx context.Context) StorageOverview {
	out := StorageOverview{
		Claims: []StorageClaim{}, PersistentVolumes: []PersistentVolume{},
		Nodes: []StorageNode{}, Volumes: []LonghornVolume{},
	}
	var claims listMeta
	if err := c.get(ctx, "/api/v1/persistentvolumeclaims", &claims); err != nil {
		out.Error = fmt.Sprintf("PersistentVolumeClaims ophalen mislukt: %v", err)
		return out
	}
	for _, raw := range claims.Items {
		var x struct {
			Metadata struct {
				Name, Namespace string
				Annotations     map[string]string `json:"annotations"`
			} `json:"metadata"`
			Spec struct {
				StorageClassName string `json:"storageClassName"`
				VolumeName       string `json:"volumeName"`
			} `json:"spec"`
			Status struct {
				Phase    string            `json:"phase"`
				Capacity map[string]string `json:"capacity"`
			} `json:"status"`
		}
		if json.Unmarshal(raw, &x) != nil {
			continue
		}
		out.Claims = append(out.Claims, StorageClaim{
			Name: x.Metadata.Name, Namespace: x.Metadata.Namespace, Status: x.Status.Phase,
			Capacity: x.Status.Capacity["storage"], StorageClass: x.Spec.StorageClassName, VolumeName: x.Spec.VolumeName,
		})
	}

	var pvs listMeta
	if err := c.get(ctx, "/api/v1/persistentvolumes", &pvs); err != nil {
		out.Error = fmt.Sprintf("PersistentVolumes ophalen mislukt: %v", err)
		return out
	}
	for _, raw := range pvs.Items {
		var x struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Spec struct {
				Capacity        map[string]string `json:"capacity"`
				StorageClassName string            `json:"storageClassName"`
				ClaimRef         *struct {
					Namespace, Name string
				} `json:"claimRef"`
			} `json:"spec"`
			Status struct {
				Phase string `json:"phase"`
			} `json:"status"`
		}
		if json.Unmarshal(raw, &x) != nil {
			continue
		}
		claim := ""
		if x.Spec.ClaimRef != nil {
			claim = x.Spec.ClaimRef.Namespace + "/" + x.Spec.ClaimRef.Name
		}
		out.PersistentVolumes = append(out.PersistentVolumes, PersistentVolume{
			Name: x.Metadata.Name, Status: x.Status.Phase, Capacity: x.Spec.Capacity["storage"],
			StorageClass: x.Spec.StorageClassName, Claim: claim,
		})
	}

	// Use Kubernetes Node Ready conditions to avoid counting a stale "running"
	// replica on an offline node as an active replica.
	nodeReady := make(map[string]bool)
	var nodes listMeta
	if err := c.get(ctx, "/api/v1/nodes", &nodes); err == nil {
		for _, raw := range nodes.Items {
			var x struct {
				Metadata struct {
					Name string `json:"name"`
				} `json:"metadata"`
				Status struct {
					Conditions []struct {
						Type   string `json:"type"`
						Status string `json:"status"`
					} `json:"conditions"`
				} `json:"status"`
			}
			if json.Unmarshal(raw, &x) != nil || x.Metadata.Name == "" {
				continue
			}
			status := "Unknown"
			for _, condition := range x.Status.Conditions {
				if condition.Type == "Ready" {
					if condition.Status == "True" {
						status = "Ready"
					} else {
						status = "NotReady"
					}
					break
				}
			}
			nodeReady[x.Metadata.Name] = status == "Ready"
			out.Nodes = append(out.Nodes, StorageNode{Name: x.Metadata.Name, Status: status})
		}
	}

	var replicasByVolume = make(map[string][]LonghornReplica)
	var replicas listMeta
	replicaErr := c.get(ctx, "/apis/longhorn.io/v1beta2/replicas", &replicas)
	if replicaErr == nil {
		for _, raw := range replicas.Items {
			var x struct {
				Metadata struct {
					Name string `json:"name"`
				} `json:"metadata"`
				Spec struct {
					NodeID     string `json:"nodeID"`
					VolumeName string `json:"volumeName"`
				} `json:"spec"`
				Status struct {
					CurrentState string `json:"currentState"`
				} `json:"status"`
			}
			if json.Unmarshal(raw, &x) != nil || x.Spec.VolumeName == "" {
				continue
			}
			nodeStatus := "Unknown"
			ready, known := nodeReady[x.Spec.NodeID]
			if known {
				if ready {
					nodeStatus = "Ready"
				} else {
					nodeStatus = "Offline"
				}
			}
			active := strings.EqualFold(x.Status.CurrentState, "running") && known && ready
			replicasByVolume[x.Spec.VolumeName] = append(replicasByVolume[x.Spec.VolumeName], LonghornReplica{
				Name: x.Metadata.Name, Node: x.Spec.NodeID, NodeStatus: nodeStatus,
				State: x.Status.CurrentState, Active: active,
			})
		}
	}

	var lh listMeta
	if err := c.get(ctx, "/apis/longhorn.io/v1beta2/volumes", &lh); err == nil {
		for _, raw := range lh.Items {
			var x struct {
				Metadata struct {
					Name string `json:"name"`
				} `json:"metadata"`
				Spec struct {
					Size             string `json:"size"`
					NumberOfReplicas int    `json:"numberOfReplicas"`
				} `json:"spec"`
				Status struct {
					State      string `json:"state"`
					Robustness string `json:"robustness"`
					ActualSize string `json:"actualSize"`
				} `json:"status"`
			}
			if json.Unmarshal(raw, &x) != nil {
				continue
			}
			capacity, _ := strconv.ParseInt(strings.TrimSpace(x.Spec.Size), 10, 64)
			actual, _ := strconv.ParseInt(strings.TrimSpace(x.Status.ActualSize), 10, 64)
			volumeReplicas := replicasByVolume[x.Metadata.Name]
			active := 0
			for _, replica := range volumeReplicas {
				if replica.Active {
					active++
				}
			}
			out.Volumes = append(out.Volumes, LonghornVolume{
				Name: x.Metadata.Name, State: x.Status.State, Robustness: x.Status.Robustness,
				Capacity: capacity, ActualSize: actual, NumberOfReplicas: x.Spec.NumberOfReplicas,
				ActiveReplicas: active, Replicas: volumeReplicas,
			})
		}
	} else {
		out.Error = "Kubernetes PVC/PV-data beschikbaar; Longhorn API niet beschikbaar: " + err.Error()
	}
	if replicaErr != nil {
		out.Error = "Longhorn-replica's ophalen mislukt: " + replicaErr.Error()
	}
	out.Available = true
	return out
}
