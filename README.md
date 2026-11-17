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

## 🧬 How this got filled in

A product's own appliance logic, once fully coded and proven for real, was copied here as a
literal copy; everything product-specific (names, branding, product services, keys, lab values)
was then stripped out until only the generic flow was left. That made it a remove-and-rebrand
step, not a fresh build or a port. The one example product under `build/lab/` (`hello`) is a
stand-in that exercises the flow; it is not a real product.

## 📦 Status

The flow is implemented: boot, key custody, the console, Base Web, the update format and the k0s
engine, with product backup sets, the data move, telemetry settings, the support bundle and the
release guards a product's pipeline runs. Multi-node is extension points with single-node
defaults, not a working cluster yet (`docs/multi-node.md`). Nothing here is released as a binary; see `AGENTS.md` for how a product forks this into
its own appliance, and `docs/` for how each piece works. `TEMPLATE.md` says how to fill in the product's own module path, registry, owner and licence holder, and `CRYPTO.md` what cryptography the template uses and what a product reviews for itself. Commits are signed off (`docs/dco.md`).

## 🤝 Contributing

Issues and pull requests are welcome. Commits need a DCO sign-off (`git commit -s`, see
`docs/dco.md`), and titles follow Conventional Commits. See `CONTRIBUTING.md` for how to
contribute and `SECURITY.md` for how to report a vulnerability.

## 🛠 Develop

```bash
task build    # go build ./...
task test     # go test ./...
task lint     # gofmt check + golangci-lint + yamllint
task license  # check Apache-2.0 headers (golic)
```

## ⚖️ License

Apache-2.0, copyright 2026 the rabbit hole tech, llc. See LICENSE and NOTICE; keep NOTICE in any
copy or derivative. NOTICE also carries the MIT credit for the password lists behind the breached-password check.
