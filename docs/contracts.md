# Workspace and investigation contracts

The Go provider owns Kubernetes transport, authorization-aware collection reads,
sanitization, and bounded retention. Services compute investigations; REST and MCP
adapters expose those services. The frontend never supplies upstream URLs or arbitrary
API paths.

## Workspace replacement

Runtime responses carry `X-Timone-Workspace` as an opaque process epoch and revision.
A changed header replaces the browser workspace even on a failed switch request:
another tab may already have switched. The request layer emits one change event for
each observed revision transition; the shell cancels queries, clears data, returns
to the overview, and reloads. Late retired responses cannot roll the revision back.
The switch form closes on success and does not independently reset the shell.

Session and workspace wire fields are defined in `internal/api/contracts.go` and
`web/src/types/index.ts`. The frontend contract test assigns actual Go-generated
fixtures to these interfaces, so missing or incompatible fields fail typechecking.
Changes to wire fields require updating both contracts and their fixture checks.

## Namespace visibility

A configured namespace ceiling restricts **namespaced** resource reads. It is not
a tenant-isolation boundary for cluster-scoped objects. Readable Nodes, ClusterRoles,
CRDs, and the generic Namespace inventory remain cluster scoped and subject to the
backend identity's Kubernetes RBAC. Browser authentication does not create an
individual Kubernetes identity.

Overview snapshots and global search filter Namespace objects to the selected
namespace for contextual counts and results. `/api/v1/namespaces` and generic
Namespace lists retain the readable cluster inventory. Changing this behavior is
an API policy change, not a pagination fix.

## REST and MCP capabilities

| Capability | REST/UI | MCP |
| --- | --- | --- |
| Overview, resources, diagnostics, events, topology | Yes | Yes |
| Applications, observed changes, integrations | Yes | Yes |
| Metrics and bounded historical metrics | Yes | Yes |
| Helm history and comparisons | Yes | Yes |
| Enabled custom resources and ServiceAccount grant inventory | Yes | Yes |
| Live authorization review | Yes | No |
| Desired-versus-live GitOps drift | Yes | No |
| Local workspace replacement and browser sessions | Yes | No |

Authorization reviews create nonpersistent Kubernetes review requests; they grant
no permissions. MCP grant inventory explains observed bindings and does not perform
live authorization reviews. See `internal/mcp/server.go` for the tool catalog.

## Query state and recovery

Pagination is derived from validated URL integers: offset 0–5000, limit 1–200.
Global search uses `goffset`; other paginated investigations use `offset`. Changing
search filters or namespace clears the offset. Unsupported resource detail tabs
normalize to Overview without discarding the namespace or version selection.
