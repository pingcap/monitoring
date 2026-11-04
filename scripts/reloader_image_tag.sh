#!/usr/bin/env bash
set -euo pipefail

ref=${1:?expected a reloader/vX.Y.Z tag}
tag=${ref#reloader/}
if [[ "$ref" != reloader/* || ${#tag} -gt 128 || ! "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$ ]]; then
    echo "Invalid reloader release tag" >&2
    exit 1
fi
printf '%s\n' "$tag"
