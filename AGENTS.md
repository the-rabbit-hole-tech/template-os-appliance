# AGENTS.md - template-os-appliance

Guide for AI agents working in this repository. Pair with `CLAUDE.md` (the working agreement and
hook-enforced rules). Keep this file current when the build, layout, or public API changes.

## What this is

A reusable template of an appliance's FLOW: the boot path, first-boot console, key custody, the
closed admin surface and the signed, versioned update-and-revert mechanism a single- or
multi-node box needs, with every product name, brand and key stripped out. It is **never
released as a binary and is not a working product**: nothing here boots on real hardware until a
product adds its own overlay, keys and build. See [Releases](#releases) for what a release of
this repo is.

## The flow, end to end

1. **UKI boot.** A single signed UEFI executable (kernel + initrd + cmdline) carries the
   dm-verity root hash; Secure Boot chains to it. See [docs/boot.md](docs/boot.md) and
   [docs/secure-boot.md](docs/secure-boot.md).
2. **A/B slots.** Two read-only, dm-verity-protected root filesystem slots; an update stages into
   the unused slot and switches on success, with an automatic revert if it doesn't come up. See
   [docs/disk-layout.md](docs/disk-layout.md) and [docs/upgrades.md](docs/upgrades.md).
3. **Key custody.** The LUKS2 key for the writable state partition is sealed to the TPM when one
   is present, or kept in a key file otherwise; [docs/key-custody.md](docs/key-custody.md) and
   [docs/factory-reset.md](docs/factory-reset.md).
4. **The closed first-boot console.** A text UI on the local screen and serial line drives setup
   before anything is reachable over the network: [docs/console.md](docs/console.md) and
   [docs/init.md](docs/init.md).
5. **:8443 Base Web.** The admin surface after setup: sign-in, network, admins, keys, TLS,
   updates, the closed SSH shell. [docs/osadmin-api.md](docs/osadmin-api.md),
   [docs/ssh-and-elevation.md](docs/ssh-and-elevation.md), [docs/access.md](docs/access.md).
6. **The signed, encrypted update file.** A `.bin` header (signed) plus an age-encrypted payload,
   one per channel key; [docs/formats.md](docs/formats.md), [docs/artifact.md](docs/artifact.md),
   [docs/update-mirror.md](docs/update-mirror.md).
7. **The k0s product bundle.** The installed product is a signed bundle of container images and
   k0s manifests, applied phase by phase; [docs/k0s.md](docs/k0s.md), [docs/import.md](docs/import.md).
8. **Update and revert.** Staging, applying, and the automatic rollback on a failed boot or a
   bad health check; [docs/upgrades.md](docs/upgrades.md). Before it seals the state key to a new
   release's predicted PCR 11, the stage checks the predictor against this boot (the running UKI's
   prediction must match the PCR 11 the TPM holds) and refuses rather than risk a box that can't
   unseal.

**Also in the base:** the product's backup sets ([docs/backups.md](docs/backups.md)), the one-time
data move to a new database cluster during an update ([docs/data-move.md](docs/data-move.md)), the
product's telemetry settings from :8443 ([docs/product-telemetry.md](docs/product-telemetry.md)),
the support bundle ([docs/support-bundle.md](docs/support-bundle.md)), network changes staged and
confirmed on the console or over SSH ([docs/network.md](docs/network.md)), the qcow2 image and the
QEMU guest agent ([docs/formats.md](docs/formats.md), [docs/static-tools.md](docs/static-tools.md)),
the release guards a product's pipeline runs ([docs/release.md](docs/release.md#release-guards)),
and the multi-node extension points, which are **stubs** with single-node defaults
([docs/multi-node.md](docs/multi-node.md)).

## Base vs. overlay

**Stays generic in the base** (this repo): the boot path, slot and verity mechanics, key custody,
the console, Base Web's pages and API, the update format and the k0s phase/switch engine, the
`appliance` namespace and its reserved stacks (`appliance-exposed`, `appliance-secrets`), and the
neutral default look (plain-text "Appliance" wordmark, greyscale palette, no logo).

**A product supplies, in its own fork:**

- its own `build/product/<name>/` overlay: `product.yaml` (phases, switches, exposed and box
  values/secrets), the k0s manifests for its own stacks, and its own import job template if it
  takes one (see [docs/release.md](docs/release.md), [docs/import.md](docs/import.md));
- a brand overlay: colour tokens and a logo file (`internal/brand`,
  [docs/artifact.md](docs/artifact.md)) and its own `consoleui.Name` (set at link time, see
  below);
- its own build profile: which switches it ships, Secure Boot and TPM requirements per
  target (`docs/secure-boot.md`);
- its own keys, generated fresh for that product, never copied from another's or from a lab;
- its own release process: tags, CI, signing, publishing a binary artifact. This repo's
  release-drafter tags are source only.

## Releases

Releases of this repo are source-only tags that track the template's features. Each release lists
what the template gained (the base, the init tool, the release guards, the docs), taken from
release-drafter's notes, so a product knows what it picks up by moving to a newer template. A
release never carries a binary, image or bundle: a product builds and ships those under its own
release process.

## Making a product from this template

1. Fork or copy the repo. Build it, install it and run through setup with the bundled example
   product (`example/hello/`, `build/lab/build.sh`) once, so you watch every base feature work end
   to end against a trivial product first: a phased apply, a switch, the Email page reaching a
   page, an import, an update and a revert ([example/hello/README.md](example/hello/README.md)).
   Then strip it out: delete `example/hello/`, drop its two pins from `build/lab/images.txt` (the
   `hello` busybox pin and the `migrate` job pin that reuses it), and remove its references through
   `build/lab/` (`build.sh`, `units.sh`, `unphased.sh`, `product-unphased.yaml`, the `lab-hook`
   overlay service). Nothing here is a module, hook or extension point to build against; it's
   source to copy and change. `product.yaml`'s exposed values, switches, phases, email and import
   are the base's real, already-built features, not a promise of more.
2. Fill the template's four placeholders (module path, image registry, release-repo owner,
   licence holder) with `go run ./build/tools/initproduct`, as [TEMPLATE.md](TEMPLATE.md) describes.
   Then rename the generic `appliance` identifiers to the product's own name: the Go
   module path (already done by the tool), the `cmd/appliance-*` binaries, the `/usr/libexec`, `/usr/lib`, `/var/lib` and
   `/run` paths, the `appl0` bridge, the `APPLIANCE_` env prefix, the proto package and
   `gen/go/appliance/...`, and the license header. (This repo went through the same rename from
   the product it was first built in, so the list above is every identifier that moves.)
3. Add the product's overlay: `build/product/<name>/`, its brand tokens and logo, its switches.
4. Generate the product's own keys (lab keys for lab builds, fresh production keys for
   production; never the reverse) and wire them into its own build.
5. Set up the product's own release CI: tags, binary builds, signing, publishing.

## Building and testing locally

- `task build` - `go build ./...`
- `task test` - `go test ./...` (Go tests that touch real resources are gated behind build tags;
  see [docs/testing.md](docs/testing.md) for which need root or real hardware/VMs, and run large
  suites under a memory cap: `systemd-run --user --scope -p MemoryMax=6G -p MemorySwapMax=0 go
  test ./...`)
- `task lint` - gofmt check + golangci-lint + yamllint
- `task license` - Apache-2.0 header check (golic, dry run)
- `task proto` - regenerate `gen/go` from `proto/` with the pinned buf and protoc plugins; never
  hand-edit generated code
- **Unsigned lab builds:** the lab build path (`build/lab/build.sh`) builds and boots with Secure
  Boot off and no keys of any kind committed to this repo; see
  [docs/building.md](docs/building.md) for the unsigned build path and where a product's own keys
  plug in.
- Never run image builds or boot a QEMU/VM image as part of routine work in this repo; the image
  suite (`test/image/...`, build-tagged `image`) is its own, separate workflow
  ([docs/testing.md](docs/testing.md)).

## Repo layout

- `cmd/` - the appliance's binaries (`appliance-init`, `appliance-osadmin`, `appliance-shell`,
  and so on), one `main` package each.
- `internal/` - the base's own packages: boot, key custody, the console, Base Web, update
  packaging, the k0s phase/switch engine, brand, backup sets (`backupset`), the data move
  (`datamove`), the support bundle (`supportbundle`), and the multi-node extension points
  (`cluster`, stubs).
- `os/` - what goes into the root filesystem and the k0s node config: `rootfs/`, `kernel/`,
  `loader/`, `rootshell/`, `k0s/`.
- `build/` - the build scripts: `lab/` (the kept, unsigned local build path and its example
  product), `product/` (the generic bundler a product's overlay plugs into), `root/`, `kernel/`,
  `uki/`, `static/`, `tools/` (generic build-time tools), `keys/` (lab key generation only), `ci/`
  (pinned tool versions), `release/` (the release guards a product's pipeline runs: the pin guard,
  the reproducible check and the unsigned check; no release workflow and no keys).
- `proto/`, `gen/` - the API: proto sources, and generated Go code (never hand-edited).
  `scripts/proto-generate.sh` rebuilds `gen/`.
- `docs/` - the detailed design and operational docs this file links to; `test/` - the image
  suite and kit-level fixtures.

## The rules

- **No release of a binary artifact from this repo**, ever; no image, ISO, OVA, `.bin` or bundle
  build or publish job in this repo's CI. Source tags (release-drafter) are the only release.
- **No keys of any kind committed here**, lab or production; the lab build generates its own
  disposable lab keys at build time (`build/keys/lab-keys.sh`) and nothing is checked in.
- **No product names, brands or other real identifiers in the base.** A `git grep` for the name
  of whatever product this flow was last imported from must stay empty; the one example product
  (the `hello` stand-in, `build/lab/`) is a generic placeholder, not a real product.
- **Light CI only**: lint, unit tests and the scrub/hooks checks run on every PR; no image build,
  no e2e image job, no release job runs here.
- **Keep the base current with its source product.** The base was last re-synced from the
  product it was first built in. When that product ships fixes to the generic flow, compare
  against the commits named in the pull request that did the last sync, port what's generic and
  skip what's product-only (its extra features, production keys and signing, image-build CI, its
  product overlay and product-specific data moves).
- **Base fixes land here first.** A fix to the boot path, console, key custody, update format or
  k0s engine belongs in this template, not only in a product's fork; a product rebases onto a
  template release to pick it up.

## Conventions and gotchas

- See `CLAUDE.md` for the branch/commit/PR rules; they are enforced by the git hooks in
  `.claude/hooks` (run `bash .claude/hooks/install.sh` once per clone).
- Open every PR as a draft and mark it ready when the work is finished; CI skips drafts. See
  CLAUDE.md "CI and Actions minutes".
- This repo is Linux-only (`.claude/linux-only`): most packages build and test only on Linux, so
  there is no local pre-push build/lint/test gate and CI on GitHub-hosted runners is the gate. Run
  what can run on macOS, and the rest in a Linux container, before pushing once.
- No emoji anywhere in this repo except README.md's lite treatment; none in code, comments,
  console or UI strings, docs, commit messages or PR text.
