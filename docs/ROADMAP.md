# Roadmap

## v0.1 - distributed collection foundation
- [x] Go Linux host agent
- [x] DaemonSet auto-scheduling on Linux nodes
- [x] Central Core and SQLite persistence
- [x] Kubernetes nodes, pods and deployment inventory
- [x] Initial live dashboard and heartbeat/offline detection

## v0.2 - richer metrics
- [ ] Kubelet/cAdvisor metrics for per-container CPU and memory
- [ ] Disk I/O and per-interface network rates
- [ ] Longhorn volume and replica health
- [ ] Historical charts and configurable retention

## v0.3 - operations
- [ ] Alert rules, notification channels and alert history
- [ ] Dashboard layouts, saved dashboards and configurable widgets
- [ ] Authentication and user/role management
- [ ] Helm chart and full install/upgrade documentation

## Later - scale and resilience
- [ ] Replace single-writer SQLite with an optional HA storage backend
- [ ] Separate ingestion from query/API services where required
- [ ] Agent buffering and backpressure under extended Core outages
- [ ] Multi-cluster support
