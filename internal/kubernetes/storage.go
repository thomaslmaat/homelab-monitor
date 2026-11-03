package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type StorageOverview struct {
	Available       bool            `json:"available"`
	Error           string          `json:"error,omitempty"`
	Claims          []StorageClaim  `json:"claims"`
	PersistentVolumes []PersistentVolume `json:"persistent_volumes"`
	Volumes         []LonghornVolume `json:"volumes"`
}
type StorageClaim struct {
	Name        string `json:"name"`
	Namespace   string `json:"namespace"`
	Status      string `json:"status"`
	Capacity    string `json:"capacity,omitempty"`
	StorageClass string `json:"storage_class,omitempty"`
	VolumeName  string `json:"volume_name,omitempty"`
}
type PersistentVolume struct {
	Name string `json:"name"`
	Status string `json:"status"`
	Capacity string `json:"capacity,omitempty"`
	StorageClass string `json:"storage_class,omitempty"`
	Claim string `json:"claim,omitempty"`
}
type LonghornVolume struct {
	Name string `json:"name"`
	State string `json:"state,omitempty"`
	Robustness string `json:"robustness,omitempty"`
	Capacity int64 `json:"capacity"`
	ActualSize int64 `json:"actual_size"`
	NumberOfReplicas int `json:"number_of_replicas"`
}

func (c *Client) Storage(ctx context.Context) StorageOverview {
	out := StorageOverview{Claims: []StorageClaim{}, PersistentVolumes: []PersistentVolume{}, Volumes: []LonghornVolume{}}
	var claims listMeta
	if err := c.get(ctx, "/api/v1/persistentvolumeclaims", &claims); err != nil {
		out.Error = fmt.Sprintf("PersistentVolumeClaims ophalen mislukt: %v", err)
		return out
	}
	for _, raw := range claims.Items {
		var x struct {
			Metadata struct { Name, Namespace string; Annotations map[string]string `json:"annotations"` } `json:"metadata"`
			Spec struct { StorageClassName string `json:"storageClassName"`; VolumeName string `json:"volumeName"` } `json:"spec"`
			Status struct { Phase string `json:"phase"`; Capacity map[string]string `json:"capacity"` } `json:"status"`
		}
		if json.Unmarshal(raw, &x) != nil { continue }
		out.Claims = append(out.Claims, StorageClaim{Name:x.Metadata.Name, Namespace:x.Metadata.Namespace, Status:x.Status.Phase, Capacity:x.Status.Capacity["storage"], StorageClass:x.Spec.StorageClassName, VolumeName:x.Spec.VolumeName})
	}
	var pvs listMeta
	if err := c.get(ctx, "/api/v1/persistentvolumes", &pvs); err != nil {
		out.Error = fmt.Sprintf("PersistentVolumes ophalen mislukt: %v", err)
		return out
	}
	for _, raw := range pvs.Items {
		var x struct {
			Metadata struct { Name string `json:"name"` } `json:"metadata"`
			Spec struct { Capacity map[string]string `json:"capacity"`; StorageClassName string `json:"storageClassName"`; ClaimRef *struct { Namespace, Name string } `json:"claimRef"` } `json:"spec"`
			Status struct { Phase string `json:"phase"` } `json:"status"`
		}
		if json.Unmarshal(raw, &x) != nil { continue }
		claim := ""
		if x.Spec.ClaimRef != nil { claim = x.Spec.ClaimRef.Namespace + "/" + x.Spec.ClaimRef.Name }
		out.PersistentVolumes = append(out.PersistentVolumes, PersistentVolume{Name:x.Metadata.Name, Status:x.Status.Phase, Capacity:x.Spec.Capacity["storage"], StorageClass:x.Spec.StorageClassName, Claim:claim})
	}
	var lh listMeta
	if err := c.get(ctx, "/apis/longhorn.io/v1beta2/volumes", &lh); err == nil {
		for _, raw := range lh.Items {
			var x struct {
				Metadata struct { Name string `json:"name"` } `json:"metadata"`
				Spec struct { Size string `json:"size"`; NumberOfReplicas int `json:"numberOfReplicas"` } `json:"spec"`
				Status struct { State, Robustness string; ActualSize string `json:"actualSize"` } `json:"status"`
			}
			if json.Unmarshal(raw, &x) != nil { continue }
			capacity, _ := strconv.ParseInt(strings.TrimSpace(x.Spec.Size), 10, 64)
			actual, _ := strconv.ParseInt(strings.TrimSpace(x.Status.ActualSize), 10, 64)
			out.Volumes = append(out.Volumes, LonghornVolume{Name:x.Metadata.Name, State:x.Status.State, Robustness:x.Status.Robustness, Capacity:capacity, ActualSize:actual, NumberOfReplicas:x.Spec.NumberOfReplicas})
		}
	} else {
		out.Error = "Kubernetes PVC/PV-data beschikbaar; Longhorn API niet beschikbaar: " + err.Error()
	}
	out.Available = true
	return out
}
