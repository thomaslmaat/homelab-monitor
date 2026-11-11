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

## Customizable dashboard (v0.3)

The dashboard now supports a personal widget layout:
- Add and remove dashboard widgets.
- Drag widgets to reorder them.
- Layout is saved in the browser's local storage and survives refreshes on that browser.
- Built-in widgets: cluster status, node health, workloads, storage/Longhorn and agent status.
- Dedicated pages for Nodes, Workloads, Storage and Integrations.
- Responsive layout for desktop and mobile.

The current preference storage is browser-local (not yet synchronized between browsers or users). The integrations page reads the built-in adapter registry at `GET /api/v1/integrations` and reports live health for the Kubernetes API, Longhorn resources and Homelab Monitor agents. Longhorn data is read through the Kubernetes API using the Longhorn CRD; it does not use the Longhorn UI endpoint or credentials. The adapter registry is an internal, compiled-in extension point; it does not load arbitrary third-party code at runtime.

### Integrations and permissions

The Core uses read-only Kubernetes API access. At minimum, its ServiceAccount needs `get`, `list` and `watch` on nodes, pods, namespaces, events, deployments, daemonsets, statefulsets, replicasets, persistentvolumes, persistentvolumeclaims and the Longhorn `volumes.longhorn.io` resource. Do not grant write permissions for dashboard monitoring.

Only integrations that have a working API connector and the required RBAC/credentials can show live data. A plugin card is not a substitute for configuring that service. The initial registry exposes read-only capabilities only; it does not yet provide game-server power actions or third-party integrations.

## Current limitations

- Linux nodes only; Windows nodes are ignored by the DaemonSet node selector.
- CPU temperature depends on available `/sys/class/thermal` sensors and may be absent on some hardware.
- Container CPU/memory usage is not yet collected; v0.1 reports pod state and host-level resource usage. Kubelet/cAdvisor stats integration is planned.
- Longhorn volume/replica health, alerts, cross-browser dashboard sync, a user/account system, additional external-service connectors and configurable retention are planned. The current Core prunes metric history older than seven days.
- SQLite is single-writer and Core runs one replica. Agent pods are distributed and automatically scale with Linux node count.

## License

MIT. See [LICENSE](LICENSE).


## Homelab Control Center roadmap

Phase 1 now includes a typed, in-process adapter registry, three built-in read-only adapters and a live integration inventory API consumed by the dashboard. The long-term Control Center architecture and eight delivery phases are documented in [`docs/control-center-roadmap.md`](docs/control-center-roadmap.md). The secure integration adapter contract and proposed game-server configuration are in [`docs/integrations/adapter-contract.md`](docs/integrations/adapter-contract.md). These documents are the implementation plan; planned connectors and control actions are not enabled until their adapters, permissions, tests and UI are implemented.
