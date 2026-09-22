#!/usr/bin/env bash
#
# Cut a release: work out the next version from the conventional commits
# since the last tag, show what the release notes will say, then tag and
# push. The tag push is what triggers .github/workflows/release.yml.
#
#   ./scripts/release.sh            # version derived from commits
#   ./scripts/release.sh v0.4.0     # explicit override
#
set -euo pipefail

BRANCH=${RELEASE_BRANCH:-main}

die() {
  printf '%s\n' "$*" >&2
  exit 1
}

command -v git-cliff >/dev/null || die "git-cliff not installed"

# A tag must point at a commit that already exists on the remote, or the
# workflow checks out a ref the runner can't resolve.
[ -z "$(git status --porcelain)" ] || die "working tree is dirty"

current=$(git rev-parse --abbrev-ref HEAD)
[ "$current" = "$BRANCH" ] || die "on '$current', expected '$BRANCH'"

git fetch origin "$BRANCH" --tags || die "fetch failed"
[ "$(git rev-parse HEAD)" = "$(git rev-parse "origin/$BRANCH")" ] ||
  die "local $BRANCH is out of sync with origin"

# Explicit argument wins; otherwise let git-cliff read the commits.
if [ $# -gt 0 ]; then
  tag=$1
else
  tag=$(git cliff --bumped-version)
fi

[[ $tag =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "'$tag' is not a vX.Y.Z tag"
git rev-parse -q --verify "refs/tags/$tag" >/dev/null && die "$tag already exists"

prev=$(git describe --tags --abbrev=0 2>/dev/null || true)
if [ -n "$prev" ]; then
  notes=$(git cliff --strip header "$prev..HEAD")
  printf '\n%s -> %s\n\n' "$prev" "$tag"
else
  notes=$(git cliff --strip header --tag "$tag")
  printf '\nfirst release: %s\n\n' "$tag"
fi

[ -n "${notes//[[:space:]]/}" ] || die "no releasable commits since $prev"
printf '%s\n\n' "$notes"

read -rp "tag and push $tag? [y/N] " reply
[ "$reply" = "y" ] || die "aborted"

git tag -a "$tag" -m "$tag"
git push origin "$tag"

printf '\npushed %s — watch the build:\n  gh run watch\n' "$tag"
