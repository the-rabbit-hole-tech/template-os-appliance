#!/usr/bin/env bash
# Copyright 2026 The Example Authors
# SPDX-License-Identifier: Apache-2.0
#
# unphased.sh STACKS OUT: LAB ONLY. The lab stacks as a product without
# phases had them (build/lab/product-unphased.yaml): the phases' workloads
# go into the hello stack, and no workload carries a phase label.
set -euo pipefail
in="${1:?the lab stacks}"
out="${2:?the output directory}"
strip() { grep -v 'appliance/phase' "$1"; }
mkdir -p "$out/hello" "$out/edge"
{
  strip "$in/hello/hello.yaml"
  for f in "$in/lab-data/lab-data.yaml" "$in/lab-front/lab-front.yaml"; do
    echo "---"
    strip "$f"
  done
} > "$out/hello/hello.yaml"
cp "$in/edge/edge.yaml" "$out/edge/edge.yaml"
