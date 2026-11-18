#!/usr/bin/env bash
# Native release tags must match the version in the tagged source.
# Release CI enforces the same rule before publishing; this catches it locally.
# prek runs pre-push hooks for one pushed ref only, so a tag pushed together
# with other refs may be left to release CI.
set -euo pipefail

case "${PRE_COMMIT_REMOTE_BRANCH:-}" in
  refs/tags/v[0-9]*) ;;
  *) exit 0 ;;
esac
candidate="${PRE_COMMIT_REMOTE_BRANCH#refs/tags/}"
if ! version=$(git show "$PRE_COMMIT_TO_REF:internal/version/VERSION" 2>/dev/null); then
  echo "pre-push: tagged source has no native version file" >&2
  exit 1
fi
if [ "$candidate" != "v$version" ]; then
  echo "pre-push: $candidate does not match native version $version" >&2
  exit 1
fi
