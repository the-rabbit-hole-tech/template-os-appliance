// Copyright 2026 The Example Authors
// SPDX-License-Identifier: Apache-2.0

package cluster_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"example.org/appliance/internal/cluster"
	"example.org/appliance/internal/codes"
)

var ctx = context.Background()

// A box with nothing filled in is the N=1 case: one node that is both the
// controller and the worker, the box itself.
func TestTheDefaultIsOneNodeThatIsBothRoles(t *testing.T) {
	m := cluster.Single{Name: "box1"}
	self, err := m.Self(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if self.Name != "box1" || self.Role != cluster.ControllerWorker || !self.Ready {
		t.Fatalf("self: %+v", self)
	}
	nodes, err := m.Nodes(ctx)
	if err != nil || len(nodes) != 1 || nodes[0] != self {
		t.Fatalf("nodes: %+v %v", nodes, err)
	}
	if !self.Role.Controls() || !self.Role.Runs() {
		t.Fatalf("a single node must control and run workloads: %v", self.Role)
	}
}

func TestRolesSayWhatTheyDo(t *testing.T) {
	for role, want := range map[cluster.Role][2]bool{
		cluster.Controller:       {true, false},
		cluster.Worker:           {false, true},
		cluster.ControllerWorker: {true, true},
	} {
		if got := [2]bool{role.Controls(), role.Runs()}; got != want {
			t.Errorf("%s: controls, runs = %v, want %v", role, got, want)
		}
	}
	for _, s := range []string{"controller", "worker", "controller+worker"} {
		r, err := cluster.ParseRole(s)
		if err != nil || r.String() != s {
			t.Errorf("%q: %v %v", s, r, err)
		}
	}
	if _, err := cluster.ParseRole("leader"); err == nil {
		t.Error("an unknown role parsed")
	}
}

// A single-node box can't hand out join tokens or join another box; it
// says so with its own code rather than pretending.
func TestASingleNodeBoxRefusesToJoin(t *testing.T) {
	var j cluster.Joiner = cluster.NoJoin{}
	if _, err := j.IssueToken(ctx, cluster.Worker, time.Hour); !codes.Is(err, codes.ClusterSingleNode) {
		t.Fatalf("issue: %v", err)
	}
	if err := j.Join(ctx, "a-token"); !codes.Is(err, codes.ClusterSingleNode) {
		t.Fatalf("join: %v", err)
	}
}

func TestQuorumIsAMajorityOfTheControllers(t *testing.T) {
	for _, c := range []struct {
		controllers, ready int
		want               bool
	}{
		{1, 1, true}, {1, 0, false},
		{2, 2, true}, {2, 1, false},
		{3, 2, true}, {3, 1, false},
		{5, 3, true}, {5, 2, false},
		{0, 0, false},
	} {
		if got := cluster.Quorum(c.controllers, c.ready); got != c.want {
			t.Errorf("Quorum(%d, %d) = %v, want %v", c.controllers, c.ready, got, c.want)
		}
	}
}

func names(ns []cluster.Node) []string {
	var out []string
	for _, n := range ns {
		out = append(out, n.Name)
	}
	return out
}

// Controllers go first, one at a time, then the workers; within a role the
// order is by name, so two runs over the same cluster agree.
func TestTheUpdateOrderIsControllersThenWorkersByName(t *testing.T) {
	nodes := []cluster.Node{
		{Name: "w2", Role: cluster.Worker, Ready: true},
		{Name: "c2", Role: cluster.Controller, Ready: true},
		{Name: "w1", Role: cluster.Worker, Ready: true},
		{Name: "c1", Role: cluster.Controller, Ready: true},
		{Name: "c3", Role: cluster.ControllerWorker, Ready: true},
	}
	order, err := cluster.UpdateOrder(nodes)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := names(order), []string{"c1", "c2", "c3", "w1", "w2"}; !slices.Equal(got, want) {
		t.Fatalf("order %v, want %v", got, want)
	}
}

// One node is the N=1 case: its update is the box's own update, with the
// outage a single-node box always has, so it's allowed.
func TestASingleNodeUpdateIsAllowed(t *testing.T) {
	order, err := cluster.UpdateOrder([]cluster.Node{{Name: "box1", Role: cluster.ControllerWorker, Ready: true}})
	if err != nil || len(order) != 1 {
		t.Fatalf("%v %v", order, err)
	}
}

// With two controllers, taking either down leaves one of two: no quorum.
// A cluster already short of quorum isn't touched either.
func TestAnUpdateThatWouldLoseQuorumIsRefused(t *testing.T) {
	two := []cluster.Node{
		{Name: "c1", Role: cluster.Controller, Ready: true},
		{Name: "c2", Role: cluster.Controller, Ready: true},
	}
	if _, err := cluster.UpdateOrder(two); !codes.Is(err, codes.ClusterQuorum) {
		t.Fatalf("two controllers: %v", err)
	}
	short := []cluster.Node{
		{Name: "c1", Role: cluster.Controller, Ready: true},
		{Name: "c2", Role: cluster.Controller, Ready: false},
		{Name: "c3", Role: cluster.Controller, Ready: false},
	}
	if _, err := cluster.UpdateOrder(short); !codes.Is(err, codes.ClusterQuorum) {
		t.Fatalf("short of quorum: %v", err)
	}
	if _, err := cluster.UpdateOrder(nil); err == nil {
		t.Fatal("no nodes at all was accepted")
	}
}

// The rolling update runs the order one node at a time and stops at the
// first node that fails: the nodes after it are never touched, and the
// error names the node.
func TestARollingUpdateStopsAtTheFirstFailedNode(t *testing.T) {
	nodes := []cluster.Node{
		{Name: "c1", Role: cluster.Controller, Ready: true},
		{Name: "c2", Role: cluster.Controller, Ready: true},
		{Name: "c3", Role: cluster.Controller, Ready: true},
		{Name: "w1", Role: cluster.Worker, Ready: true},
	}
	var ran []string
	broken := errors.New("the new slot didn't come up")
	done, err := cluster.Roll(ctx, nodes, func(_ context.Context, n cluster.Node) error {
		ran = append(ran, n.Name)
		if n.Name == "c2" {
			return broken
		}
		return nil
	})
	if !codes.Is(err, codes.ClusterUpdateFailed) || !errors.Is(err, broken) {
		t.Fatalf("err: %v", err)
	}
	if !slices.Equal(ran, []string{"c1", "c2"}) || !slices.Equal(done, []string{"c1"}) {
		t.Fatalf("ran %v done %v", ran, done)
	}
	var nf *cluster.NodeFailed
	if !errors.As(err, &nf) || nf.Node != "c2" {
		t.Fatalf("the error doesn't name c2: %v", err)
	}
}

func TestARollingUpdateRunsEveryNodeInOrder(t *testing.T) {
	nodes := []cluster.Node{
		{Name: "w1", Role: cluster.Worker, Ready: true},
		{Name: "box1", Role: cluster.ControllerWorker, Ready: true},
	}
	var ran []string
	done, err := cluster.Roll(ctx, nodes, func(_ context.Context, n cluster.Node) error {
		ran = append(ran, n.Name)
		return nil
	})
	if err != nil || !slices.Equal(ran, []string{"box1", "w1"}) || !slices.Equal(done, ran) {
		t.Fatalf("ran %v done %v err %v", ran, done, err)
	}
}

func TestARollingUpdateStopsWhenCancelled(t *testing.T) {
	c, cancel := context.WithCancel(ctx)
	nodes := []cluster.Node{
		{Name: "c1", Role: cluster.Controller, Ready: true},
		{Name: "c2", Role: cluster.Controller, Ready: true},
		{Name: "c3", Role: cluster.Controller, Ready: true},
	}
	var ran []string
	_, err := cluster.Roll(c, nodes, func(_ context.Context, n cluster.Node) error {
		ran = append(ran, n.Name)
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) || !slices.Equal(ran, []string{"c1"}) {
		t.Fatalf("ran %v err %v", ran, err)
	}
}
