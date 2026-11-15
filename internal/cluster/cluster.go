// Copyright 2026 The Example Authors
// SPDX-License-Identifier: Apache-2.0

// Package cluster is where a multi-node appliance plugs in: the node's
// role, joining more nodes, the cluster's membership, quorum and the order
// a rolling update takes (docs/multi-node.md). The base is single-node, so
// what it ships here are the extension points and their N=1 defaults:
// Single is a membership of one node that is both controller and worker,
// and NoJoin refuses to issue or redeem a join token. A product that runs
// more than one node fills in Membership and Joiner with its own (k0s's
// join tokens, say) and keeps Quorum, UpdateOrder and Roll, which are the
// same rules at N=1 and at N=5.
package cluster

import (
	"context"
	"fmt"
	"sort"
	"time"

	"example.org/appliance/internal/codes"
)

// Role is what a node does in the cluster.
type Role int

const (
	// ControllerWorker runs the control plane and workloads: every node of
	// a single-node box, and any node a small cluster doesn't split.
	ControllerWorker Role = iota
	// Controller runs the control plane only.
	Controller
	// Worker runs workloads only.
	Worker
)

var roleNames = map[Role]string{
	ControllerWorker: "controller+worker",
	Controller:       "controller",
	Worker:           "worker",
}

func (r Role) String() string {
	if s, ok := roleNames[r]; ok {
		return s
	}
	return fmt.Sprintf("Role(%d)", int(r))
}

// Controls reports whether the role counts towards quorum.
func (r Role) Controls() bool { return r == Controller || r == ControllerWorker }

// Runs reports whether the role runs workloads.
func (r Role) Runs() bool { return r == Worker || r == ControllerWorker }

// ParseRole reads a role's String form.
func ParseRole(s string) (Role, error) {
	for r, n := range roleNames {
		if n == s {
			return r, nil
		}
	}
	return 0, fmt.Errorf("cluster: %q isn't a role (controller, worker or controller+worker)", s)
}

// Node is one member of the cluster.
type Node struct {
	Name    string
	Role    Role
	Version string
	// Ready: the node is up and, for a controller, a working member of the
	// control plane.
	Ready bool
}

// Membership is the cluster as this node sees it.
type Membership interface {
	// Self is this node.
	Self(ctx context.Context) (Node, error)
	// Nodes is every member, this node among them.
	Nodes(ctx context.Context) ([]Node, error)
}

// Single is the base's membership: the box is the whole cluster.
type Single struct {
	// Name is the box's node name.
	Name string
	// Version is the release it runs.
	Version string
}

// Self is the box, both roles, ready: if this code runs, the box is up.
func (s Single) Self(context.Context) (Node, error) {
	return Node{Name: s.Name, Role: ControllerWorker, Version: s.Version, Ready: true}, nil
}

// Nodes is the box alone.
func (s Single) Nodes(ctx context.Context) ([]Node, error) {
	n, err := s.Self(ctx)
	return []Node{n}, err
}

// Token is a one-time join token: it lets one new node join with one role,
// until it expires or is used. A Joiner records every token it issues and
// every use of one in the OS audit log.
type Token struct {
	ID      string
	Role    Role
	Expires time.Time
	// Secret is what the new node is given; it is never logged.
	Secret string
}

// Joiner grows the cluster.
type Joiner interface {
	// IssueToken makes a one-time token for a node of role to join with,
	// valid for ttl.
	IssueToken(ctx context.Context, role Role, ttl time.Duration) (Token, error)
	// Join makes this box a member of the cluster that issued secret,
	// using it up.
	Join(ctx context.Context, secret string) error
}

// NoJoin is the base's Joiner: a single-node box neither issues nor
// redeems join tokens.
type NoJoin struct{}

// IssueToken refuses with ClusterSingleNode.
func (NoJoin) IssueToken(context.Context, Role, time.Duration) (Token, error) {
	return Token{}, codes.New(codes.ClusterSingleNode, "this appliance is single-node: no join tokens")
}

// Join refuses with ClusterSingleNode.
func (NoJoin) Join(context.Context, string) error {
	return codes.New(codes.ClusterSingleNode, "this appliance is single-node: it can't join another box")
}

// Quorum reports whether ready of controllers is a majority.
func Quorum(controllers, ready int) bool {
	return controllers > 0 && ready > controllers/2
}

// UpdateOrder is the order a rolling update takes the nodes in: the
// controllers first, then the workers, each by name. It refuses
// (ClusterQuorum) a cluster whose controllers have no ready majority now,
// or would lose it with any one of them down. A single controller, alone or
// with workers, is the N=1 control plane: its update is the outage a
// single-node box always has, and is allowed.
func UpdateOrder(nodes []Node) ([]Node, error) {
	if len(nodes) == 0 {
		return nil, fmt.Errorf("cluster: no nodes to update")
	}
	controllers, ready := 0, 0
	for _, n := range nodes {
		if n.Role.Controls() {
			controllers++
			if n.Ready {
				ready++
			}
		}
	}
	if !Quorum(controllers, ready) {
		return nil, codes.New(codes.ClusterQuorum, "%d of %d controllers are ready", ready, controllers)
	}
	if controllers > 1 && !Quorum(controllers, ready-1) {
		return nil, codes.New(codes.ClusterQuorum, "with one of %d ready controllers down, %d of %d is no majority", ready, ready-1, controllers)
	}
	out := append([]Node(nil), nodes...)
	sort.SliceStable(out, func(i, j int) bool {
		ci, cj := out[i].Role.Controls(), out[j].Role.Controls()
		if ci != cj {
			return ci
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// NodeFailed is the error Roll stops with: Node's update returned Err.
type NodeFailed struct {
	Node string
	Err  error
}

func (e *NodeFailed) Error() string { return fmt.Sprintf("cluster: %s: %v", e.Node, e.Err) }

func (e *NodeFailed) Unwrap() error { return e.Err }

// Roll updates the nodes one at a time in UpdateOrder, calling update for
// each, and stops at the first that fails (ClusterUpdateFailed, wrapping a
// NodeFailed) or when ctx is done; the nodes after it aren't touched. done
// is every node it finished, in order.
func Roll(ctx context.Context, nodes []Node, update func(context.Context, Node) error) (done []string, err error) {
	order, err := UpdateOrder(nodes)
	if err != nil {
		return nil, err
	}
	for _, n := range order {
		if err := ctx.Err(); err != nil {
			return done, err
		}
		if err := update(ctx, n); err != nil {
			return done, codes.Wrap(codes.ClusterUpdateFailed, &NodeFailed{Node: n.Name, Err: err})
		}
		done = append(done, n.Name)
	}
	return done, nil
}
