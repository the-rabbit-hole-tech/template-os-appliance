# Using the template

How a product turns this template into its own appliance repository. The template is the base
every appliance shares and carries no product's name. A product fills in four values once, then does
the rest of the work in `AGENTS.md` ("Making a product from this template").

## The placeholders

Four fixed strings stand in for the product's values. They are valid Go and valid paths, so the
template builds, vets and tests as it stands, and `build/tools/initproduct` swaps them all in one
pass. None is a real organisation or registry, and the tool refuses to finish while one is left.

| Placeholder in the tree  | Flag         | What it is                                            | Where it lands                                                                                          |
| ------------------------ | ------------ | ----------------------------------------------------- | ------------------------------------------------------------------------------------------------------- |
| `example.org/appliance`  | `--module`   | The product's Go module path                          | `go.mod`, every import, the proto `go_package`, `build/lab/build.sh`, the `-ldflags -X` examples in docs |
| `registry.example.org/`  | `--registry` | The image registry and owner, with the trailing slash | Every container image path: the lab stacks, `os/k0s/containerd.toml`, the docs, the test fixtures        |
| `example-owner`          | `--owner`    | The GitHub owner of the product's release repos       | Release-repo slugs and the release and issue URLs in the Proxmox guide and the update docs             |
| `The Example Authors`    | `--holder`   | Who the licence header names                          | The `Copyright 2026 ...` line of every source file, `COMMON_LIC` in the Taskfile, `.golic.yaml`         |

A value may hold letters, digits and the punctuation its place needs; the tool checks each one
before it writes anything, and says which flag is wrong. A value may not be the placeholder itself.

## Start a product, in order

1. Create the product's repository from this template (or copy it) and clone it.
2. Have `buf`, `protoc-gen-go` and `protoc-gen-connect-go` on `PATH`. The two plugins are the
   versions `go.mod` pins:

   ```bash
   go install google.golang.org/protobuf/cmd/protoc-gen-go connectrpc.com/connect/cmd/protoc-gen-connect-go
   ```

3. See what would change, then fill the placeholders:

   ```bash
   go run ./build/tools/initproduct --dry-run \
     --module=github.com/acme/acme-appliance --registry=ghcr.io/acme/ \
     --owner=acme --holder="The Acme Authors"
   go run ./build/tools/initproduct \
     --module=github.com/acme/acme-appliance --registry=ghcr.io/acme/ \
     --owner=acme --holder="The Acme Authors"
   ```

   The tool rewrites the text files, puts the rewritten imports back in gofmt order, rebuilds
   `gen/` with `scripts/proto-generate.sh` and then lists any file that still holds a placeholder
   (a non-zero exit). `gen/` is rebuilt rather than rewritten because the protobuf descriptors
   embed the module path with its length. `--skip-generate` leaves `gen/` stale.

4. Remove what only the template needs: the tool itself (`build/tools/initproduct/`) and the check
   that the template names no real organisation (`build/ci/org_name_test.sh`, plus its step in
   `.github/workflows/job-shell-tests.yaml`).
5. Put the product's own name in `NOTICE`, `.github/CODEOWNERS`, `FUNDING.yml`, `SUPPORT.md`,
   `CODE_OF_CONDUCT.md` and `CLAUDE.md`. The tool leaves them alone: they describe the template's
   own repository, and `NOTICE` carries the original-author attribution Apache-2.0 section 4(d)
   asks every copy to keep.
6. Carry on with `AGENTS.md`: rename the binaries and on-box paths, add the product overlay and
   its keys, and set up its release CI.

## What the tool never touches

`gen/` (rebuilt), `go.sum`, `NOTICE`, `LICENSE`, this guide, the tool's own folder, and
`build/ci/org_name_test.sh`. Binary files are skipped.

## How CI proves it

The `InitFixture` workflow fills a checkout with a fictional product (`--fixture`: Acme, on
`github.com/acme-example` and `ghcr.io/acme-example/`), removes the tool as step 4 does, and then
builds, vets, formats, lints the protos and checks the licence headers on the result. The filled
template is checked the way a product uses it, so a change that leaves a path the tool cannot fill
(or fills wrongly) fails there.

Other things the guard scripts hold:

- `build/ci/org_name_test.sh`: the template names no real organisation outside `.github/`,
  `CLAUDE.md` and `NOTICE`.
- A real GitHub organisation called `example-org` exists, which is why the placeholders are
  `example.org/...` and `example-owner`, not `github.com/example-org/...`.
