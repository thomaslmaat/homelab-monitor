# Homelab Control Center — product roadmap

This document turns Homelab Monitor into a Kubernetes-native control plane for a self-hosted homelab. The roadmap is intentionally capability-based: an item is only considered implemented when its adapter, API, permissions, UI, tests and operational documentation are present. A visual card alone is never treated as a working integration.

## Product pillars

1. **Monitor** — collect and visualize health, capacity, history and events.
2. **Manage** — perform explicitly supported actions on workloads and connected services.
3. **Automate** — execute user-defined, auditable workflows from events, schedules and conditions.

## Target architecture

```text
Browser UI
   |
   v
Core API ── Authentication / RBAC ── Audit log
   |
   +── Read models / metrics / history
   |
   +── Integration registry
   |      +── Kubernetes adapter
   |      +── Longhorn adapter
   |      +── Game server adapters
   |      +── Proxmox adapter
   |      +── Home Assistant adapter
   |      +── Docker adapter
   |      +── Uptime Kuma / DNS adapters
   |
   +── Action service ── policy checks ── confirmations
   |
   +── Automation engine ── triggers / conditions / actions
   |
   +── Notification dispatcher
   |
   +── SQLite initially; migration path to PostgreSQL
```

The browser must never receive Kubernetes, Proxmox, Docker or third-party credentials. All actions go through the Core API and a narrowly scoped adapter. Adapters declare their capabilities; the UI only renders actions that the adapter actually supports.

## Phase 1 — Integration and plugin foundation

**Goal:** a consistent contract for first-party and future integrations.

- Define integration identity, type, version, health, configuration schema and declared capabilities.
- Provide lifecycle methods: validate configuration, connect, health check, read resources, execute an explicitly declared action and close.
- Keep secrets server-side; redact them from API responses, logs and audit events.
- Expose integration inventory and health through a read-only API.
- Support enabled/disabled state and clear degraded/error states.
- Start with built-in adapters; do not load arbitrary executable plugins into the Core process.
- Version the adapter contract and reject unsupported versions.

**Acceptance:** an integration can be registered, validated, health-checked, listed and disabled without changing dashboard code. Tests cover invalid config, secret redaction, timeouts and adapter errors.

## Phase 2 — Game Server Manager

**Goal:** one place to see and manage dedicated game servers.

Initial target: Minecraft and Satisfactory, followed by Valheim, Project Zomboid and other servers.

A game server record contains:
- stable ID, display name, game type and adapter;
- Kubernetes namespace and workload reference, or an explicitly configured external endpoint;
- desired state and observed state;
- player count when a supported query protocol is available;
- address/port, version, uptime, resource usage and last health check;
- backup/update policy and maintenance status.

The first Kubernetes adapter should support Deployments and StatefulSets only when their mapping is explicitly configured. It must not infer that every workload is a game server.

Supported actions: start, graceful stop, restart, status and logs, subject to adapter capability. Stop must use a graceful shutdown path where the server supports it and must respect a configurable termination grace period. Do not delete PVCs or world data when stopping a server.

**Acceptance:** a configured Minecraft or Satisfactory workload can be discovered, shown with truthful status and safely started/stopped/restarted. Actions are idempotent, audited and covered by tests. Player counts are omitted when unavailable rather than fabricated.

## Phase 3 — Safe dashboard actions

- Add an action API with typed requests, target validation, authorization and timeouts.
- Require confirmation for destructive or disruptive actions.
- Use allow-listed resource references; never accept arbitrary shell commands, raw Kubernetes manifests or arbitrary URLs from the browser.
- Return an operation ID and track pending/running/succeeded/failed state.
- Prevent duplicate clicks from creating duplicate operations.
- Add per-action audit entries with actor, target, outcome and timestamp.

**Acceptance:** actions are not available to anonymous users; unauthorized, malformed and duplicate requests are rejected; action outcomes are visible in the UI.

## Phase 4 — Cluster topology and infrastructure

- Interactive node topology with node readiness, agent heartbeat, CPU, memory, disk, temperature and network.
- Workload-to-node placement and pod/deployment health.
- Longhorn volume, replica, node and capacity health.
- Drill-down from a node or volume to its related workloads.
- Historical charts with explicit data freshness and retention indicators.

**Acceptance:** topology is generated from live cluster data; missing metrics are clearly marked; no sample values are presented as live values.

## Phase 5 — Alerts and notifications

- Rule definitions with severity, threshold, evaluation window, cooldown and recovery behavior.
- Initial rules: node unavailable, agent stale, workload unhealthy, PVC capacity threshold and Longhorn volume degraded.
- In-app notification inbox with acknowledgement and history.
- Optional Discord, Telegram and email delivery adapters.
- Deduplication and rate limiting to avoid notification storms.

**Acceptance:** alerts are derived from observed data, have clear lifecycle state and are tested for firing, recovery, cooldown and duplicate suppression.

## Phase 6 — Automation engine

- Event, schedule and threshold triggers.
- Conditions and explicitly allow-listed actions.
- Dry-run/preview, enable/disable, execution history and per-step results.
- Initial recipes: notify when a node goes offline; back up before an update; stop an idle game server only when player count is known to be zero; start a selected server on a schedule.
- Concurrency limits, retry policy, timeouts and loop prevention.

**Acceptance:** automations cannot execute arbitrary scripts by default; risky actions require explicit opt-in and can be disabled globally.

## Phase 7 — External integrations

Planned adapters, in this order:
1. Proxmox VE — VM/CT inventory and controlled power actions.
2. Home Assistant — entity/status view and explicitly configured service actions.
3. Docker Engine — container inventory and controlled lifecycle actions.
4. Uptime Kuma — monitor status and incidents.
5. Pi-hole / AdGuard Home — DNS statistics and safe controls.
6. GitHub — repository, workflow and deployment status.

Each adapter requires its own endpoint, credentials, health check, permissions and tests. Integration cards remain disconnected until configuration and a successful health check are complete.

## Phase 8 — Personalization, access control and operations

- User accounts and role-based permissions (viewer, operator, administrator).
- Dashboard layouts saved per user and synchronized across browsers.
- Widget catalog, drag/drop layouts, themes and per-widget refresh intervals.
- Global search / command palette with permission-aware results.
- API token rotation, secret management, audit log export and backup/restore.
- Configurable metric retention and database migration strategy.
- Accessibility, responsive layout, localization and operational health page.

**Acceptance:** users only see and execute actions allowed by their role; preferences persist across sessions; backups can be restored; migrations are tested.

## Delivery and safety rules

- Keep the existing monitoring API read-only until a specific action adapter is implemented and reviewed.
- Use least-privilege Kubernetes RBAC per action and resource type. Never grant wildcard verbs/resources.
- Do not expose the Core directly to the public internet. Put it behind a trusted ingress/VPN and authentication.
- No default credentials, no secrets in ConfigMaps, frontend bundles, logs or screenshots.
- Every integration must clearly distinguish connected, degraded, disconnected and not configured.
- Feature flags should keep unfinished action surfaces disabled.
- A phase is not complete until tests, docs and a live-cluster verification checklist exist.

## Current baseline

The current application already has the Go Core, SQLite metrics history, Kubernetes inventory, host Agents, Longhorn/PVC read model, customizable dashboard and a futuristic frontend branch. This roadmap does not claim that the future integrations or control actions are already implemented. The next engineering slice is Phase 1's typed integration contract and read-only registry, followed by the Kubernetes game-server adapter.
