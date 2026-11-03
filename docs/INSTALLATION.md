# Installation

1. Ensure the cluster has a `longhorn` StorageClass, or edit the Core PVC.
2. Build and publish the Core and Agent images for your target architectures.
3. Apply the manifests: `kubectl apply -k deploy/kubernetes`.
4. Wait for the Core and DaemonSet: `kubectl -n homelab-monitor get pods -o wide`.
5. Port-forward the dashboard: `kubectl -n homelab-monitor port-forward svc/homelab-monitor 8080:8080`.
6. Open `http://localhost:8080`.

To verify automatic node coverage, compare `kubectl get nodes` with `kubectl -n homelab-monitor get pods -l app.kubernetes.io/name=homelab-monitor-agent -o wide`. When a new Linux node joins, the DaemonSet controller schedules its Agent automatically.
