# Multi-node: the extension points

The base is single-node. What it ships for more than one node are **stubs**: the interfaces a
product fills in, their single-node defaults, and the rules that hold at any size. Nothing here
joins a second box yet; a product that needs a cluster builds that part in its own fork, against
these extension points, so the rest of the flow doesn't change shape when it does. The work to make
it real is tracked in the template's multi-node issue (join tokens, roles, quorum and rolling
updates).

Single-node is the N=1 case of the same flow, not a separate path: one node that is both the
controller and the worker, a quorum of one, and a rolling update of one node.

## What's in `internal/cluster`

| Piece | What it is | The base's default |
|---|---|---|
| `Role` | what a node does: `controller`, `worker` or `controller+worker` (`Controls`, `Runs`, `ParseRole`) | every node is `controller+worker` |
| `Membership` | the cluster as this node sees it: `Self` and `Nodes` | `Single`: the box, both roles, ready |
| `Joiner` | grows the cluster: `IssueToken` makes a one-time join token for a role, `Join` redeems one | `NoJoin`: both refuse with `CLUSTER_SINGLE_NODE` |
| `Token` | a one-time join token: an id, the role it joins with, its expiry and the secret the new node is given (never logged) | none issued |
| `Quorum(controllers, ready)` | whether the ready controllers are a majority | 1 of 1 |
| `UpdateOrder(nodes)` | the order a rolling update takes: the controllers first, then the workers, each by name; refuses (`CLUSTER_QUORUM`) a cluster with no ready majority, or one that would lose it with any one controller down | the box alone |
| `Roll(ctx, nodes, update)` | runs `update` for each node in that order, one at a time, and stops at the first that fails (`CLUSTER_UPDATE_FAILED`, naming the node) or when the context ends; the nodes after it aren't touched | the box's own update |

A single controller, alone or with workers, is the N=1 control plane: updating it is the outage a
single-node box always has, so `UpdateOrder` allows it. Two controllers can't keep a majority with
one down, so a rolling update over them is refused; three or more can, one at a time.

## How a product fills them in

- **Membership** reads the cluster's own view. With k0s that's the node list and each node's role
  label and Ready condition, read through the API server the box already talks to
  (`internal/kubeapi`).
- **Joiner** wraps the cluster's join mechanism. With k0s, `IssueToken` runs
  `k0s token create --role=<controller|worker> --expiry=<ttl>`, records the token's id (never its
  secret) in the OS audit log, and keeps it one-time by invalidating it once a node has joined;
  `Join` writes the token where the node's k0s service reads it on its next start. A product that
  runs Cilium in place of kube-router changes the node's network config, not this interface.
- **Rolling updates** use `Roll` with an `update` that drives one node's own A/B update
  ([upgrades.md](upgrades.md)): stage, apply, wait for the node to come back Ready on the new slot,
  and return an error if it reverts. `Roll` stopping at the first failed node is what keeps a bad
  release from reaching the rest of the cluster.
- **The admin surface** stays one surface for the cluster: the :8443 pages and the closed shell
  call `Membership` for the node list and `Joiner` for tokens, so a single-node box shows one node
  and answers `CLUSTER_SINGLE_NODE` for a token.

## What isn't decided here

Where controllers keep their state, how a node is removed, how a cluster recovers from a lost
quorum, and how the update units (Base OS, Base Web, Product) are ordered across nodes are a
product's design calls, made with its own topology. The stubs keep to what every multi-node
product needs, so filling them in doesn't mean undoing the base first.
