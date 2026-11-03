# Development

1. Install Go 1.23 or newer.
2. Run `go mod download` and `go test ./...` from the repository root (requires access to the Go module proxy for the SQLite driver on a fresh machine).
3. The dashboard is embedded in the Core binary under `internal/dashboard/assets`; no separate frontend build is required.
4. For a local Core run, provide a writable SQLite path with `DATABASE_PATH`. Kubernetes API inventory is enabled automatically when the Core runs in-cluster with its service-account token.
5. For an Agent run, set `NODE_NAME`, `PROC_ROOT`, `SYS_ROOT`, `ROOT_FS` and `CORE_URL` as needed. In Kubernetes these are injected by the DaemonSet manifest.

Keep host collection, Kubernetes inventory, HTTP API and storage code isolated. Metric payload changes must be documented and covered by tests. Avoid collecting secrets, process command lines or user data; collect infrastructure telemetry only.
