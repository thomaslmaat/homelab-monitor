# Integration adapter contract

## Design principles

- Adapters are compiled into the Core for the first releases. Do not use Go plugins or execute downloaded code.
- Each adapter owns one integration type and declares a stable ID, display name, version and supported capabilities.
- Read methods must be side-effect free. Write methods must be explicit typed operations, not generic command execution.
- Adapter configuration is validated at startup. Secret values are references to Kubernetes Secrets or environment-injected secret mounts and are never returned to the browser.
- Every network call has a timeout and bounded response size.
- An adapter failure is isolated and reported as degraded; it must not crash the Core or block other integrations.

## Conceptual Go interface

```go
type Integration interface {
    ID() string
    Version() string
    Capabilities() []Capability
    Validate(context.Context, Config) error
    Health(context.Context) Health
    Resources(context.Context) ([]Resource, error)
    Execute(context.Context, ActionRequest) (Operation, error)
    Close() error
}
```

This is a contract sketch, not a claim that this interface is already wired into the running application. Concrete types should use typed configuration and typed action payloads; avoid passing arbitrary maps to privileged operations.

## Capability examples

| Capability | Meaning |
|---|---|
| `resources.read` | List resources and status |
| `logs.read` | Read bounded logs |
| `server.start` | Start one configured game server |
| `server.stop` | Gracefully stop one configured game server |
| `server.restart` | Restart one configured game server |
| `vm.power` | Power action on an explicitly configured VM |
| `automation.execute` | Execute a saved automation |

Capabilities are declarations, not permissions by themselves. The Core must check the authenticated user's role, integration policy and target allow-list for every request.

## API shape (proposed)

- `GET /api/v1/integrations` — inventory, capabilities and health; never secrets.
- `POST /api/v1/integrations/{id}/validate` — validate non-secret settings and secret references.
- `POST /api/v1/operations` — submit one typed, authorized action.
- `GET /api/v1/operations/{id}` — inspect operation state.
- `GET /api/v1/audit` — administrator-only audit history.

Do not expose an endpoint that accepts arbitrary commands, arbitrary URLs, arbitrary Kubernetes objects or raw provider API requests.

## Game server configuration example

```yaml
apiVersion: homelab-monitor.io/v1alpha1
kind: GameServer
metadata:
  id: minecraft-main
  name: Minecraft
spec:
  adapter: kubernetes-workload
  target:
    namespace: games
    kind: StatefulSet
    name: minecraft
  connection:
    address: play.example.invalid
    port: 25565
  actions:
    start: true
    gracefulStop: true
    restart: true
    logs: true
  shutdown:
    gracePeriodSeconds: 90
  playerQuery:
    enabled: false
```

The example is illustrative configuration only. The Core must validate namespace, workload kind/name and allowed actions against administrator-defined policy. The example hostname is deliberately non-routable. Player query is disabled until a protocol-specific implementation exists.

## Kubernetes permission model

Start with read-only access. Add narrowly scoped write permissions only for the exact workload types and namespaces used by configured game servers. Prefer a dedicated action ServiceAccount or a narrowly scoped authorization path over expanding the monitoring ServiceAccount. Do not grant cluster-wide wildcard update/patch/delete rights.

## Required security tests

- Missing or invalid authentication cannot submit actions.
- A viewer cannot submit operator/admin actions.
- Unknown integration IDs and unknown capabilities are rejected.
- A target outside the configured allow-list is rejected.
- Secret values never appear in API responses, audit logs or error messages.
- A timeout or provider error returns a failed operation without leaking credentials.
- Duplicate operation IDs/requests do not repeat a non-idempotent action.
- Graceful stop never deletes the workload's PVC or persistent world data.
