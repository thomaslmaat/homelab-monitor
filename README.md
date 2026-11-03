# Homelab Monitor

Homelab Monitor is an open-source, Kubernetes-native monitoring application for homelabs. A central Core collects host metrics from one lightweight Agent per Linux node, while the Core also reads cluster state from the Kubernetes API. The first release is intentionally small and self-contained: Go, SQLite and a built-in web dashboard. It does not require Prometheus, Grafana or Node Exporter.

## Architecture

- **Core**: one Kubernetes Deployment, a ClusterIP Service, read-only Kubernetes API access and a Longhorn-backed SQLite PVC.
- **Agent**: a DaemonSet. Kubernetes schedules one Agent on every Linux node, including control-plane nodes with scheduling taints. A newly joined Linux node is picked up automatically by the DaemonSet.
- **Dashboard**: served by the Core at `/`; polls `/api/v1/overview` every 10 seconds.
- **Data flow**: Agent -> Core HTTP ingest API -> SQLite. Core -> Kubernetes API -> cluster inventory.

The Core stays at one replica in v0.1 because SQLite is a single-writer embedded database. The Agent tier scales with the number of Linux nodes. The Core can be made highly available in a later release by moving storage to a multi-writer-capable backend and separating ingestion from query/API workloads.

## First-release metrics

Host agent: CPU utilization, memory usage, root filesystem capacity/usage, network RX/TX counters, uptime and the first available Linux thermal-zone reading. Core: Kubernetes node readiness, node IP and kubelet version, pod phase/readiness/restarts and deployment desired/ready replicas.

## Install with Kustomize

The published images must exist before applying these manifests. For a source checkout:

```bash
git clone https://github.com/thomaslmaat/homelab-monitor.git
cd homelab-monitor
kubectl apply -k deploy/kubernetes
```

Or reference the remote base from your own GitOps repository:

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - https://github.com/thomaslmaat/homelab-monitor//deploy/kubernetes?ref=v0.1.0
```

The default PVC uses the `longhorn` StorageClass. Change `storageClassName` in `deploy/kubernetes/core.yaml` if your cluster uses another class.

Open the dashboard temporarily:

```bash
kubectl -n homelab-monitor port-forward svc/homelab-monitor 8080:8080
```

Then visit http://localhost:8080.

## Build locally

```bash
go test ./...
CGO_ENABLED=0 go build ./cmd/core
CGO_ENABLED=0 go build ./cmd/agent
```

Build container images from the repository root:

```bash
docker build --build-arg COMPONENT=core -t homelab-monitor:dev .
docker build --build-arg COMPONENT=agent -t homelab-monitor-agent:dev .
```

## API

- `GET /healthz`
- `GET /api/v1/overview`
- `GET /api/v1/hosts`
- `GET /api/v1/history?host=NODE_NAME`
- `POST /api/v1/metrics` (Agent ingestion)

The overview read endpoints are read-only. Keep the dashboard behind a trusted ingress/VPN or add ingress authentication before exposing it publicly. Do not expose the Core directly to the public internet.

## Current limitations

- Linux nodes only; Windows nodes are ignored by the DaemonSet node selector.
- CPU temperature depends on available `/sys/class/thermal` sensors and may be absent on some hardware.
- Container CPU/memory usage is not yet collected; v0.1 reports pod state and host-level resource usage. Kubelet/cAdvisor stats integration is planned.
- Longhorn volume/replica health, alerts, dashboard editing and configurable retention are planned. The current Core prunes metric history older than seven days.
- SQLite is single-writer and Core runs one replica. Agent pods are distributed and automatically scale with Linux node count.

## License

MIT. See [LICENSE](LICENSE).
