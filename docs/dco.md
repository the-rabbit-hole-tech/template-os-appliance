# Developer Certificate of Origin

Every commit in a pull request carries a sign-off line:

```
Signed-off-by: Ada Dev <ada@example.org>
```

It is your statement that you wrote the change, or have the right to submit it, under this
project's licence (Apache-2.0). The certificate you are agreeing to is the root [`DCO`](../DCO)
file, version 1.1 of the Linux Foundation text. The project takes it as the record that a
product copying from this base is copying code that was offered for that.

## What CI checks

The `DCO sign-off` step of the PR Checks job runs `build/ci/dco.sh` over the commits the pull
request adds. A commit fails when it has:

- no `Signed-off-by: Name <email>` line, or
- a sign-off whose address is neither the commit's author address nor its committer address.

Merge commits and bot commits (an author address containing `[bot]`) are skipped. The check
names each failing commit and says why. The same script is what `build/ci/dco_test.sh` tests, so
the rule has one definition.

## Signing as you commit

`git commit -s` adds the line from your git identity. To have it added to every commit without
remembering the flag, install the hook once per clone:

```
scripts/install-dco-hook.sh
```

It installs `scripts/hooks/prepare-commit-msg`, which adds the line when the message has none. It
is its own hook, so it sits beside the governance hooks (`.claude/hooks/install.sh`) and changes
none of them. An existing `prepare-commit-msg` that is not this one is left alone.

## Fixing a pull request that fails

Add the line to the commits already pushed, then push again:

```
git rebase --signoff origin/main
git push --force-with-lease
```

For a single last commit, `git commit --amend -s --no-edit` does it. Squash merges keep the
sign-offs of the pull request's commits in the merged message.

## Scope

The check applies from the commit that adds it. History before that is not rewritten.
