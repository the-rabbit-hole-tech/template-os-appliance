// Copyright 2026 The Example Authors
// SPDX-License-Identifier: Apache-2.0

package codes

import apperr "github.com/Bugs5382/go-apperr"

// The multi-node codes: 392x (internal/cluster, docs/multi-node.md).
const (
	ClusterSingleNode   = 3920
	ClusterQuorum       = 3921
	ClusterUpdateFailed = 3922
)

var clusterEntries = []apperr.Entry{
	{Code: ClusterSingleNode, Symbol: "CLUSTER_SINGLE_NODE", Title: "cluster", Cause: "this appliance is single-node: it doesn't issue join tokens or join another box"},
	{Code: ClusterQuorum, Symbol: "CLUSTER_QUORUM", Title: "cluster", Cause: "the change would leave the controllers without a ready majority, or they already have none; nothing was changed"},
	{Code: ClusterUpdateFailed, Symbol: "CLUSTER_UPDATE_FAILED", Title: "cluster", Cause: "a node's update failed; the rolling update stopped at that node and the nodes after it weren't touched"},
}

func init() { Entries = append(Entries, clusterEntries...) }
