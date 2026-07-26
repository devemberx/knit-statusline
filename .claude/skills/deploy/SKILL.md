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
No npm credential exists in the repository or its secrets: the workflow authenticates over
OIDC against a trusted publisher configured per package on npmjs.com.

## Step 1 — Pre-flight checks (abort on any failure)

```bash
git status --porcelain                  # MUST be empty
git rev-parse --abbrev-ref HEAD         # MUST be 'main'
git fetch origin main --tags            # --tags so Step 2 check see remote tags
git rev-list --count HEAD..origin/main  # MUST be 0
git rev-list --count origin/main..HEAD  # MUST be 0
git config core.hooksPath               # SHOULD be '.githooks'

# --workflow not optional: CodeQL default setup also record runs against main, so
# unfiltered --limit 1 hand back its green run while ci itself red.
gh run list --branch main --workflow ci.yml --limit 1 \
  --json status,conclusion,headSha    # status 'completed', conclusion 'success'

# Environment existing prove nothing: zero protection rules = no gate.
gh api repos/:owner/:repo/environments/release \
  -q '.protection_rules[].type'       # MUST contain 'required_reviewers'
```

- Dirty tree → list offending files, stop. Do NOT auto-stash.
- Branch != main → ask user to switch manually. Do NOT switch.
- Behind origin → tell user to run `git pull --ff-only`.
- Ahead of origin → tell user to open a PR for those commits. `publish.yml` refuses a tag whose
  commit is not reachable from `origin/main`, so catching it here beats failing after the tag is
  public.
- `core.hooksPath` ≠ `.githooks` → WARN, ask user to run `git config core.hooksPath .githooks`.
  Nothing in this skill commits, so it does not gate *this* run — restore it anyway as the
  repo-wide net for every later commit.
- Latest `ci` run on main not green → stop. GoReleaser reruns `go test ./...` in its
  `before.hooks`, so a broken main fails the release *after* the tag is public. A `status` other
  than `completed` (queued, in_progress) is not green either — wait for it, do not tag mid-run.
  Empty result means ci never ran for that commit: stop and say so rather than reading it as a
  pass.
- Run's `headSha` ≠ the commit about to be tagged (`git rev-parse HEAD`) → stop. A green run from
  an older commit says nothing about the one shipping.
- No `release` environment (the `gh api` call 404s) **or** `protection_rules` without
  `required_reviewers` → stop, either way. `publish.yml` names the environment, so GitHub creates
  it on first run with no protection rules at all, and an environment with no required reviewer
  approves itself — the tag would publish six packages to npm with no human in the loop. Fix it
  at Settings → Environments → `release` → Required reviewers, then re-run the check.

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
repository `devemberx/knit-statusline`, workflow file `publish.yml`, environment `release`.

Unconfigured → stop, and give the user the bootstrap procedure. `npm trust` attaches one
publisher to one package, so it runs once per package — six times:

```bash
npm install -g npm@11                # npm trust need 11.15.0+; 11.18 run on node 22.9+
npm login                            # web auth + 2FA, token into ~/.npmrc
for dir in npm/platforms/*/ npm/knit-statusline; do
  npm publish "./${dir%/}" --access public --tag bootstrap
done
for pkg in @devemberx/knit-statusline \
           @devemberx/knit-statusline-{darwin-arm64,darwin-x64,linux-arm64,linux-x64,win32-x64}; do
  npm trust github "$pkg" --repository devemberx/knit-statusline \
    --file publish.yml --environment release --allow-publish --yes
  sleep 2                            # endpoint rate-limit
done
npm logout                           # local token dead weight, release run over OIDC
```

The workflow flag is `--file`, not `--workflow`.

`npm@11` not `npm@12` on purpose. Both carry `npm trust`, but npm 12 demands node
`^22.22.2 || ^24.15.0 || >=26.0.0` and fails `EBADENGINE` on an earlier node — a node upgrade
the operator does not need for a one-off bootstrap. `publish.yml` runs npm 12 because
`setup-node` gives it the latest 24.x.

The whole loop has to finish inside the five-minute window `npm login` opens before 2FA is asked
again.

Then verify the placeholders did not claim `latest`:

```bash
# --tag bootstrap unproven here: npm reported to set dist-tags.latest on a
# package's first publish whatever --tag say.
for pkg in @devemberx/knit-statusline \
           @devemberx/knit-statusline-{darwin-arm64,darwin-x64,linux-arm64,linux-x64,win32-x64}; do
  printf '%s ' "$pkg"; npm view "$pkg" dist-tags   # 'latest' MUST be absent
done
```

`latest` present → it points at the `0.0.0` placeholder, so `npm install
@devemberx/knit-statusline` serves a broken package until the first real release. Either drop it
with `npm dist-tag rm <pkg> latest` (once per affected package), or leave it knowingly — the
first real release publishes over it. If npm refuses to remove `latest`, take the second path and
tell the user the broken-install window is open until the release lands.

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
# -q --verify keep missing tag quiet; bare `git rev-parse v1.2.3` echo argument
# back and still exit non-zero, which read like a hit.
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

The tag message carries this release's curated highlights — in the tag itself, not a committed
file, so the repo never accumulates per-release notes. Line 1 is the dated marker; the rest is a
bullet list GoReleaser reads back as `{{ .TagBody }}` and renders under a `## Highlights` heading
(`release.header` in `.goreleaser.yaml`), above the change list it generates from `changelog:`.
Never hand-write that change list into the tag.

From commits since `$PREV_TAG`, draft curated summary of what *matters* to user — few
plain-language bullets (or short prose), not restatement of PR titles. Show via
AskUserQuestion (`approve` / `edit` / `skip`). English only.

On `skip`, create date-only tag (release ships the generated change list alone):

```bash
RELEASE_DATE=$(date -u +%F)  # -u so marker match UTC date GitHub stamp
git tag -a "v${NEW_VERSION}" -m "Release v${NEW_VERSION} (${RELEASE_DATE})"
```

Otherwise the message is the bullet list alone — no `## Highlights` heading in the tag,
`release.header` adds it. Create tag with `-F`:

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
# 1. See how far run got.
for pkg in @devemberx/knit-statusline \
           @devemberx/knit-statusline-{darwin-arm64,darwin-x64,linux-arm64,linux-x64,win32-x64}; do
  printf '%s ' "$pkg"; npm view "${pkg}@${NEW_VERSION}" version 2>/dev/null || echo "(absent)"
done

# 2. Rerun. replace_existing_draft in .goreleaser.yaml drop stale draft, and
#    publish step skip every package already on registry.
gh run rerun <run-id> --failed
```

Do not delete the draft by hand first. `replace_existing_draft` owns that, and a manual
`gh release delete` on a tag with several stacked drafts removes whichever one GitHub
answers with.

The release only flips public after all six succeed, so a half-finished run never fronts
packages that do not exist.

## Hard rules

- ALWAYS `git tag -a` (or `-F`). NEVER a lightweight tag — GoReleaser's `.TagBody` falls back
  to the *commit* message body, which would publish that commit's bullets as `## Highlights`.
- NEVER hand-write the categorized change list into the tag. The tag body holds the dated marker
  plus curated bullets, nothing else.
- NEVER hand-edit a `version` in `npm/**/package.json` — `prepare-packages.mjs` stamps every one
  of them from the tag, including the launcher's `optionalDependencies`, which must match exactly.
- NEVER `--no-verify`, `git push --force`, or a force-moved tag. A pushed tag has published
  artifacts and npm versions hanging off it; move it and the two disagree forever.
- NEVER create an npm token to work around a failing publish. The pipeline is tokenless by
  design; a broken trusted publisher is fixed at npmjs.com.
- NEVER auto-retry a failed push.
- NEVER run `gh release create` by hand during deploy — GoReleaser owns it.
- ALWAYS confirm before the tag push.
- ALWAYS show the tag draft to user before creating it.
