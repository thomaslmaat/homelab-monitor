# Architecture

Homelab Monitor uses a hub-and-agent model native to Kubernetes.

1. A DaemonSet schedules one Agent on every Linux node. `nodeSelector: kubernetes.io/os=linux` excludes non-Linux nodes, while an `Exists` toleration includes tainted control-plane nodes.
2. Each Agent reads host `/proc`, `/sys` and filesystem capacity through read-only hostPath mounts. It identifies itself with the Downward API `spec.nodeName` and posts a versioned metrics payload every 15 seconds.
3. The Core receives metrics over a ClusterIP Service and stores them in SQLite on a Longhorn PVC.
4. The Core uses a narrowly scoped ClusterRole to list nodes, pods and workloads. The dashboard combines Kubernetes state with agent heartbeats.
5. A heartbeat younger than 45 seconds is considered online. A missing agent is still visible in the Kubernetes node inventory and appears offline in the host view.

## Scaling

Adding a Linux node requires no Homelab Monitor configuration change: Kubernetes creates a DaemonSet pod on that node automatically. Removing a node causes its Agent pod to disappear; the Core marks the host offline when its last heartbeat ages out. The Agent tier scales linearly with node count. The Core is intentionally single-replica in this SQLite release; do not increase its replica count while using the embedded database.

## Security

Agents have no Kubernetes API token and no Linux capabilities. They run with a read-only root filesystem and read-only host mounts. The Core has a service account with read-only `get/list/watch` permissions for nodes, pods, namespaces, events and common workload controllers. The Core's dashboard API is read-only; restrict access with a private network or authenticated ingress.
