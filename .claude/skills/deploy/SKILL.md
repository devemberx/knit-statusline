---
name: deploy
description: Deploy skill for knit-statusline. Cut a release by tagging main; GoReleaser publishes the GitHub Release and the npm packages. Triggers on `/deploy` or any release/version bump/tag request.
---

# Deploy Skill

Orchestrate production release. Be deliberate. Never skip hooks. Never push a tag without explicit user confirm.

**The tag is the version.** Nothing in the tree records it: the binary takes it from
`ldflags -X main.version={{.Version}}`, and `npm/scripts/prepare-packages.mjs` stamps it
into every `package.json` at build time (they sit at `0.0.0` in git on purpose). So a
release is *one* annotated tag — no bump commit, nothing to push to `main`.

**Nothing publishes without a human approval.** `publish.yml` runs in the `release`
GitHub environment. A tag push starts the run, then it waits for a required reviewer.
There is no npm token anywhere: the workflow authenticates over OIDC against a trusted
publisher configured per package on npmjs.com.

## Step 1 — Pre-flight checks (abort on any failure)

```bash
git status --porcelain                  # MUST be empty
git rev-parse --abbrev-ref HEAD         # MUST be 'main'
git fetch origin main --tags            # --tags so Step 2's check sees remote tags
git rev-list --count HEAD..origin/main  # MUST be 0
git rev-list --count origin/main..HEAD  # MUST be 0
git config core.hooksPath               # SHOULD be '.githooks'

# --workflow is not optional: ci is not the only workflow recording runs against
# main (CodeQL default setup does too), so an unfiltered --limit 1 can hand back
# someone else's green run while ci itself is red.
gh run list --branch main --workflow ci.yml --limit 1 \
  --json status,conclusion,headSha    # status 'completed', conclusion 'success'

gh api repos/:owner/:repo/environments -q '.environments[].name'  # MUST list 'release'
```

- Dirty tree → list offending files, stop. Do NOT auto-stash.
- Branch != main → ask user switch manually. Do NOT switch.
- Behind origin → tell user run `git pull --ff-only`.
- Ahead of origin → tell user open a PR for those commits. `publish.yml` refuses a tag whose
  commit is not reachable from `origin/main`, so this fails the release rather than shipping
  unreviewed code — catch it here instead.
- `core.hooksPath` ≠ `.githooks` → WARN, ask user run `git config core.hooksPath .githooks`.
  Nothing in this skill commits, so the hook does not gate *this* run — but restore it anyway
  (repo-wide net for every later commit).
- Latest `ci` run on main not green → stop. GoReleaser reruns `go test ./...` in its
  `before.hooks`, so a broken main fails the release *after* the tag is public, which is the
  expensive place to find out. `status` other than `completed` (queued, in_progress) is not
  green either — wait for it, do not tag mid-run. Empty result means ci never ran for that
  commit: stop and say so rather than reading it as a pass.
- Confirm the run's `headSha` is the commit about to be tagged (`git rev-parse HEAD`). A green
  run from an older commit says nothing about the one shipping.
- No `release` environment → stop. `publish.yml` names it, and GitHub would create one with no
  protection rules on first run — the approval gate would silently not exist.

### First release only (no tags yet)

The npm side has to be bootstrapped once by hand. Trusted publishing is configured **per
package** and npm requires the package to already exist on the registry, so a package that has
never been published cannot have a trusted publisher attached yet.

Confirm with the user that all six exist and are configured:

```bash
npm trust list @devemberx/knit-statusline          # and the five platform packages
```

The six are `@devemberx/knit-statusline` plus `@devemberx/knit-statusline-<target>` for
`darwin-arm64`, `darwin-x64`, `linux-arm64`, `linux-x64` and `win32-x64`. Each must point at
repository `devemberx/knit-statusline`, workflow `publish.yml`, environment `release`.

Unconfigured → stop, and give the user the bootstrap procedure:

```bash
npm install -g npm@12                # npm trust needs 11.15.0+
npm login                            # web auth + 2FA, no token created
for dir in npm/platforms/*/ npm/knit-statusline; do
  npm publish "./$dir" --access public --tag bootstrap
done
npm trust github <pkg> --repository devemberx/knit-statusline \
  --workflow publish.yml --environment release --allow-publish --yes
```

`--tag bootstrap` keeps the placeholder off `latest`, so the first real release claims it.
Leave two seconds between `npm trust` calls; the endpoint rate-limits.

Skip the bootstrap and GoReleaser still cuts the GitHub Release, then every `npm publish` fails
on auth — a released tag with no packages behind it.

## Step 2 — Determine new version (interactive)

```bash
PREV_TAG=$(git describe --tags --abbrev=0 2>/dev/null || echo "")
[ -n "$PREV_TAG" ] && git log "$PREV_TAG..HEAD" --oneline || git log --oneline
```

No `$PREV_TAG` → first release. Recommend **0.1.0** and say so; do not derive a bump.

Otherwise analyze commits since `$PREV_TAG`:

- `breaking` / `!:` marker → recommend **major**
- any `feat:` → recommend **minor**
- only `fix:` / `chore:` / `docs:` → recommend **patch**

Call **AskUserQuestion** with options `patch` / `minor` / `major` / `custom`, note recommendation
in question text. For `custom`, prompt exact `X.Y.Z`. Pre-1.0 the recommendation is advisory —
a `feat:` under `0.x` may still ship as a patch; let the user decide.

A prerelease version (`0.2.0-rc.1`) publishes under the npm `next` dist-tag, not `latest`, and
GoReleaser marks the GitHub Release as a prerelease. Say so if the user picks one.

Verify tag does not already exist (Step 1's `--tags` fetch catches remote tags too, not just local):

```bash
# -q --verify keeps a missing tag quiet; a bare `git rev-parse v1.2.3` echoes the
# argument back and still exits non-zero, which reads like a hit.
if git rev-parse -q --verify "refs/tags/v${NEW_VERSION}" >/dev/null; then
  echo "tag v${NEW_VERSION} already exists"
  exit 1
fi
```

If exists, abort, tell user pick different version. Remote tag means version already shipped
(or mid-publish) — never reuse; npm rejects republishing a version even after `npm unpublish`.
Local-only tag likely stale leftover from aborted prior run; user can delete manually
(`git tag -d v${NEW_VERSION}`) after confirming it never reached origin.

## Step 3 — Create annotated tag with curated highlights (interactive)

Tag message = single source of truth for this release's curated highlights — kept in the tag
itself, not a committed file, so repo never accumulates per-release notes. Line 1 = dated
marker; the rest is a curated bullet list that GoReleaser reads back as `{{ .TagBody }}` and
renders under a `## Highlights` heading (see `release.header` in `.goreleaser.yaml`), above the
change list it generates. (Change list is generated from `changelog:` in `.goreleaser.yaml` —
never hand-write it into the tag.)

From commits since `$PREV_TAG`, draft curated summary of what *matters* to user — few
plain-language bullets (or short prose), not restatement of PR titles. Show via
AskUserQuestion (`approve` / `edit` / `skip`). English only.

On `skip`, create date-only tag (release ships the generated change list alone):

```bash
RELEASE_DATE=$(date -u +%F)  # -u so the marker matches the UTC date GitHub stamps
git tag -a "v${NEW_VERSION}" -m "Release v${NEW_VERSION} (${RELEASE_DATE})"
```

Otherwise compose message as curated **bullet list only** — no `## Highlights` heading in the
tag (`release.header` adds it). Create tag with `-F`:

```bash
RELEASE_DATE=$(date -u +%F)
TAG_MSG=$(mktemp)
cat > "$TAG_MSG" <<EOF
Release v${NEW_VERSION} (${RELEASE_DATE})

- <plain-language summary of a major change>
- <another>
EOF
git tag -a "v${NEW_VERSION}" -F "$TAG_MSG"
rm -f "$TAG_MSG"
```

Verify either way, and confirm the tag is annotated (`object` type `tag`, not `commit`):

```bash
git cat-file -t "v${NEW_VERSION}"   # MUST print 'tag'
git show "v${NEW_VERSION}" --no-patch
```

## Step 4 — Push tag (confirm, with irreversibility warning)

AskUserQuestion with this exact warning text: **"Pushing tag v${NEW_VERSION} starts
`.github/workflows/publish.yml`, which waits for approval in the `release` environment. Once
approved: GoReleaser (build + draft GitHub Release) → npm publish of the platform packages,
then the launcher → the release goes public. The npm step is IRREVERSIBLE (npm blocks reusing a
published version). Continue?"** Options: `push` / `cancel`.

```bash
git push origin "v${NEW_VERSION}"
```

On failure: local tag still exists; user can retry with `git push origin v${NEW_VERSION}`
or delete locally via `git tag -d v${NEW_VERSION}`.

On `cancel`: nothing shipped and nothing to clean up — the tag exists only locally. Finish
later by pushing it, or delete it and re-run.

GitHub Release created by GoReleaser inside `publish.yml`. Do NOT create one by hand;
a manual `gh release create` collides with GoReleaser's own (`release already exists`).

## Step 5 — Post-push summary

```bash
OWNER_REPO=$(gh repo view --json nameWithOwner -q .nameWithOwner)
echo "Tagged v${NEW_VERSION}"
echo "Approve the run: https://github.com/${OWNER_REPO}/actions/workflows/publish.yml"
echo "Release notes:   https://github.com/${OWNER_REPO}/releases/tag/v${NEW_VERSION}"
```

Tell the user two things:

- **The run is waiting on them.** Nothing is built or published until the `release` environment
  approval goes through.
- The launcher is published last on purpose (it pins the platform packages exactly), so npm
  resolves the new version only once the whole job finishes.

## Recovering a partial publish

The job publishes six packages in sequence. If it dies partway — network, a revoked trusted
publisher, an npm outage — some are on the registry and some are not, and the GitHub Release is
still a draft.

Do not cut a new version to escape this. The run is designed to be repeated:

```bash
# 1. See how far it got.
for pkg in @devemberx/knit-statusline \
           @devemberx/knit-statusline-{darwin-arm64,darwin-x64,linux-arm64,linux-x64,win32-x64}; do
  printf '%s ' "$pkg"; npm view "${pkg}@${NEW_VERSION}" version 2>/dev/null || echo "(absent)"
done

# 2. Delete the draft release. GoReleaser refuses to recreate one that exists.
gh release delete "v${NEW_VERSION}" --yes

# 3. Re-run the workflow. It rebuilds, and the publish step skips every package
#    already on the registry.
gh run rerun <run-id> --failed
```

The release only flips public after all six succeed, so a half-finished run never fronts
packages that do not exist.

## Hard rules

- ALWAYS `git tag -a` (or `-F`). NEVER a lightweight tag — GoReleaser's `.TagBody` falls back
  to the *commit* message body, which would publish that commit's bullets as `## Highlights`.
- NEVER hand-write the categorized change list into the tag — GoReleaser generates it; the tag
  body holds only the dated marker (line 1) plus curated highlight bullets, no `## Highlights`
  heading (`release.header` adds it).
- NEVER hand-edit a `version` in `npm/**/package.json` — `prepare-packages.mjs` stamps every one
  of them from the tag, including the launcher's `optionalDependencies`, which must match exactly.
- NEVER `--no-verify`, `git push --force`, or a force-moved tag. Once pushed, a tag has published
  artifacts and npm versions hanging off it; move it and the two disagree forever.
- NEVER create an npm token to work around a failing publish. The pipeline is tokenless by
  design; a broken trusted publisher is fixed at npmjs.com, not by adding a secret.
- NEVER auto-retry a failed push.
- NEVER run `gh release create` by hand during deploy — GoReleaser owns it.
- ALWAYS confirm before the tag push.
- ALWAYS show the tag draft to user before creating it.
