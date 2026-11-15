# Building the OS from source

Everything the image is built from is pinned in `build/ci/versions.env` and checked: the kernel by
its stable git tag and that tag's commit, cryptsetup by kernel.org's published SHA-256, the
builder containers by tag, and QEMU by the SHA-256 of its signed release tarball.

| Piece | Script | Output |
|---|---|---|
| Kernel (amd64) | `build/kernel/build.sh amd64` | `build/out/kernel/amd64/bzImage`, its `.config` and `kernelrelease` |
| cryptsetup, veritysetup (static, musl) | `build/static/cryptsetup.sh amd64` | `build/out/{cryptsetup,veritysetup}-amd64` |
| mke2fs (static) | `build/static/e2fsprogs.sh amd64` | `build/out/mke2fs-amd64` |
| sgdisk (static) | `build/static/gptfdisk.sh amd64` | `build/out/sgdisk-amd64` |
| watch (static, procps-ng) and its terminfo entries | `build/static/procps.sh amd64` | `build/out/watch-amd64`, `build/out/terminfo-amd64/` |
| qemu-ga, the QEMU guest agent (static, musl; amd64 only) | `build/static/qemu-ga.sh amd64` | `build/out/qemu-ga-amd64`, and `qemu-ga-amd64.rpcs`, the RPCs it has ([static-tools.md](static-tools.md#the-qemu-guest-agent)) |
| OpenSSH, busybox (static, musl) | `build/openssh/build.sh`, `build/busybox/build.sh` | `out/static/{sshd,sshd-session,sshd-auth,ssh-keygen,busybox}` |
| Airgap bundle | `build/bundle/build.sh` | `<hex>.tar` and its signature per pinned image ([root-image.md](root-image.md)) |
| Root image | `build/root/build.sh` | `root-<version>.img` (SquashFS with the verity tree) and `verity.json` ([root-image.md](root-image.md)) |
| UKI | `build/uki/assemble.sh` | `appliance-<version>.efi` (unsigned) |

## The kernel

One kernel serves every amd64 platform, because one signed UKI does: `os/kernel/config-base` and
every profile (`vmware`, `qemu`, `proxmox`) are merged onto `tinyconfig`, with
`CONFIG_MODULES=n` (everything built in). The build fails if any requested option was dropped by
an unmet dependency, and `build/kernel/check-config.sh` checks the options the appliance can't run
without (`os/kernel/required.txt`: cgroup v2, namespaces, overlayfs, veth, bridge and
`br_netfilter`, nftables and the iptables compatibility layer, conntrack, seccomp, dm-verity,
dm-crypt, SquashFS with zstd, efivarfs, the TPM drivers, the lockdown LSM, and the POSIX timers
sshd's login grace time needs, which `tinyconfig` turns off). Lockdown isn't forced
in the config; the UKI's command line sets `lockdown=integrity`.

Builds are reproducible from `SOURCE_DATE_EPOCH` (the last commit's time by default) with fixed
build user, host and version strings. The `Image Build` workflow builds the kernel and the static
tools when a pull request touches what they're built from, caches them by the hash of their inputs,
and records each digest in the job summary.

The static tools build inside Docker (pinned Alpine and Debian images), so a build host needs
Docker. Each script fails if its output has a dynamic loader.

## Prebuilt inputs

The root build takes OpenSSH, busybox and the static tools as prebuilt inputs, and all of them are
required (`STATIC` too: first boot can't make the state volumes without cryptsetup and
mkfs.ext4). Each build script writes a stamp next to its output (`busybox.stamp`,
`openssh.stamp`, `<cryptsetup|e2fsprogs|gptfdisk|procps|qemu-ga>-<arch>.stamp`): the tool, its pinned version and
the SHA-256 of everything that decides the build, the pins, the busybox config and the build script
itself (`build/lib/stamp.sh`). The root build works the same stamp out from the tree it runs in and
refuses an input whose stamp is missing or different ("stale or unstamped busybox: ... build it
again"), so a binary left from an older pin or config in a holding folder never ships. It logs
every input's SHA-256 (`root: input <file> sha256 <hex>`), so a build can be traced to exactly what
went in.

Before it packs the tree, the root build checks that every program the box runs is in it
(`build/tools/rootexecs`): each absolute `/bin`, `/sbin`, `/usr/bin`, `/usr/sbin` and `/usr/libexec`
path in `cmd/` and `internal/` (tests left out) and each `exec`, `pre-start` and leading `args`
path in the service tables, the lab overlay's included, must be an executable file in the tree,
through links. A missing one fails the build. A lab build's version carries its own stamp,
`r<UTC time>` (or `BUILD_STAMP`), before `g<commit>`, so two builds of one commit never share a
version.

The build itself runs no VM. The first boot of a fresh OVA is proved in QEMU
(`ova.TestFreshOVAFirstBoot`, [testing.md](testing.md)) by the image suite, against a lab OVA. A
product's own release can run the same test against its production OVA before it publishes. A real
VMware host is still a manual proof.

## Keys: lab builds commit none

`build/lab/build.sh` carries no key material in the repo. It generates a fresh, disposable lab
key set at the start of every run (`build/keys/lab-keys.sh "$KEYS"`, PK/KEK/db, the release
cosign key and the update age key), signs the UKI, the release and the product bundle with that
run's own set, and never writes any of it back into the tree. Two runs never share a key set
unless `REUSE_KEYS=1` is given on purpose. This is the template's whole key posture until a
product adds its own: nothing committed, lab or production.

**Secure Boot off, unsigned, for a quick proof build** is not yet wired up end to end. Skipping
`sbsign` on the UKI (`build/lab/build.sh`'s two `sbsign` calls) is enough for the firmware to boot
it with Secure Boot off, but the kit's `assemble` and `appliance-kit build` steps still take the
PK/KEK/db certs to lay out the image's own Secure Boot enrollment files, and have not been
checked with those left empty. Treat an unsigned build as: comment out the two `sbsign` lines and
use the keyed UKI directly as `$work/appliance-$version.efi`, run the image with Secure Boot off
in the VM's firmware, and expect the enrollment-file step to need its own follow-up if it fails.
This is flagged, not finished; proving it end to end on a real VM is follow-up work.

## The console's brand: a build-time overlay

The base ships with no logo and a plain-text wordmark ("Appliance"), in a neutral, greyscale
palette (`internal/consoleui/tui/style.go`): no accent hue, WCAG AA contrast against the console's
black background. A product overlay replaces this at build time, not at install time:

- **The wordmark.** `internal/consoleui.Name` is a package-level `var`; link it to the product's
  own name:

  ```sh
  go build -ldflags "-X example.org/appliance/internal/consoleui.Name=Example" ./cmd/appliance-init
  ```

  Set the same `-X` flag on every binary that imports `internal/consoleui` (`appliance-init`,
  `appliance-console`, `appliance-firstboot`; see `build/root/build.sh`'s `PINS_LDFLAGS`, which
  already threads one shared `-ldflags` string through every binary it builds).
- **The console's colours.** The 16-slot palette and the SGR codes that use it
  (`internal/consoleui/tui/style.go`'s `Palette` and `sgr`) are a product's fork point: copy the
  file, repaint the slots that carried the old brand (`Brand`, `Accent`), and keep the status
  colours (`OK`, `Warn`, `Alert`) and the WCAG AA contrast check. `build/uki/assemble.sh`'s
  `vtpalette` string is a plain copy of `Palette`; keep the two in sync, as
  `internal/console/font_test.go` checks.
- **Base Web's own colours and logo** are a separate, later piece of work (Base Web's pages
  themselves are untouched in this pass); this section covers the console only.
