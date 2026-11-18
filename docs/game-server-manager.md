# Game Server Manager (Phase 2)

The first slice is an explicitly configured, read-only inventory. It does not infer game servers from arbitrary Kubernetes workloads and it does not perform lifecycle actions.

## Configuration

Set `GAME_SERVERS_JSON` on the Core container. Example:

```json
[
  {
    "id": "minecraft-main",
    "name": "Minecraft",
    "game": "minecraft",
    "namespace": "games",
    "workload": "minecraft",
    "workload_type": "deployment"
  },
  {
    "id": "satisfactory-main",
    "name": "Satisfactory",
    "game": "satisfactory",
    "namespace": "satisfactory",
    "workload": "satisfactory-server",
    "workload_type": "deployment"
  }
]
```

The workload reference is an exact Deployment name and namespace. IDs must be unique. Only `deployment` is supported in this first slice; unsupported workload types are rejected. An empty variable disables the inventory.

## API

`GET /api/v1/game-servers` returns configured entries with observed Deployment replica state. Possible states are `running`, `starting`, `stopped`, `not_found` and `unknown`. A missing Deployment is never represented as running.

## Safety boundary

This release is read-only. It does not expose start, stop, restart, exec, logs, or arbitrary Kubernetes operations. Do not reuse `HOMELAB_MONITOR_TOKEN` as browser authentication: that token protects Agent ingestion only. Lifecycle controls require a separate authenticated action API, role checks, narrow permissions, audit events, idempotency and graceful termination semantics before they are enabled.
