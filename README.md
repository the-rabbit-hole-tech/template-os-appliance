# template-os-appliance 🐹

> 🧭 A generic appliance template.

## 📐 What this is

A generic appliance template: the boot, first-boot console, setup, key custody, and A/B or
N-slot update-and-revert flow, built with generic images and generic names throughout. No
product ships from this repo. Each product that needs an appliance copies this template and
re-authors it as its own, so the flow is owned and dated in one place while each product's
internals stay its own.

Slot count and partition layout are a per-app design choice, not fixed by the template. The
flow also covers the multi-node case (first-node setup, join tokens, controller/worker roles,
quorum, rolling updates across nodes), with single-node as the N=1 case of the same flow. The
management surface is pluggable: a password-and-step-up web admin, or an mTLS-only API/CLI,
with every flow step working through either.

## 🧬 How a product gets its appliance

The template is filled in later, not now: once a product's appliance logic is fully coded and
proven for real, that working code is copied here as a literal copy, then everything
product-specific is stripped out (names, branding, product services, keys, lab values) until
only the generic flow is left. That makes this a remove-and-rebrand step, not a fresh build or
a port. Today this repo is scaffold only, with no code; the copy happens later, at a point its
owner picks.

## 📦 Status

Scaffold only: governance, CI and this README. No implementation yet.

## 🛠 Develop

```bash
task build    # go build ./...
task test     # go test ./...
task lint     # gofmt check + golangci-lint + yamllint
task license  # check Apache-2.0 headers (golic)
```

## ⚖️ License

Apache-2.0. See LICENSE and NOTICE; keep NOTICE in any copy or derivative.
