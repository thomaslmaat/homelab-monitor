# Game Server Manager

The manager uses explicitly registered Kubernetes Deployments. Each game server is a separate entry with a unique ID, so multiple Minecraft, Satisfactory, Factorio or future game instances can run side by side.

## Configuration

Set `GAME_SERVERS_JSON` on the Core container. Example:

```json
[
  {"id":"minecraft-survival","name":"Minecraft Survival","game":"minecraft","namespace":"games","workload":"minecraft-survival","workload_type":"deployment"},
  {"id":"minecraft-creative","name":"Minecraft Creative","game":"minecraft","namespace":"games","workload":"minecraft-creative","workload_type":"deployment"},
  {"id":"satisfactory-main","name":"Satisfactory Main","game":"satisfactory","namespace":"satisfactory","workload":"satisfactory-main","workload_type":"deployment"},
  {"id":"factorio-main","name":"Factorio","game":"factorio","namespace":"factorio","workload":"factorio-server","workload_type":"deployment"}
]
```

Every ID must be unique. Namespace and Deployment name are exact Kubernetes references. Multiple entries may use the same game type, but each must reference its own Deployment. Only Deployments are supported by this version.

## Controls

- **Start:** sets the Deployment replica count to 1.
- **Stop:** sets the Deployment replica count to 0. PersistentVolumeClaims and game data are not deleted.
- **Restart:** requests a rolling restart by changing a pod-template annotation. Kubernetes replaces the pod and retains its mounted persistent data.

Actions are exposed through authenticated `POST /api/v1/game-servers/{id}/actions` requests with `{"action":"start|stop|restart"}`. IDs are resolved only against the explicit allowlist in `GAME_SERVERS_JSON`; arbitrary namespace, workload or Kubernetes paths cannot be supplied by the browser. Actions are written to the Core application log with the authenticated actor context.

## Required Kubernetes permissions

The Core ServiceAccount needs read access to Deployments plus the following write permissions:

```yaml
- apiGroups: ["apps"]
  resources: ["deployments"]
  verbs: ["get", "list", "watch", "patch"]
- apiGroups: ["apps"]
  resources: ["deployments/scale"]
  verbs: ["get", "patch", "update"]
```

These permissions are cluster-wide when attached through a ClusterRole. Keep the dashboard private and authenticated. The application allowlist restricts which workloads can be controlled, but Kubernetes RBAC itself does not restrict the ServiceAccount to those configured names. For stricter isolation, use namespace-scoped Roles and RoleBindings for each game namespace.

## No game-server agent required

Start, stop, restart and basic workload status use the Kubernetes API. No software or agent needs to be installed inside Minecraft, Satisfactory or Factorio containers. Player counts, in-game console and chat are not included in this generic control layer; those require optional game-specific query/RCON configuration and are not needed for lifecycle management.
