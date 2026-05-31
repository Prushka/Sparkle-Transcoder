#!/usr/bin/env bash
set -euo pipefail

IMAGE_NAME="${IMAGE_NAME:-meinya/sparkle-manager-frontend}"
PLATFORM="${PLATFORM:-linux/amd64}"
CONTEXT="${CONTEXT:-web}"
DOCKERFILE="${DOCKERFILE:-web/Dockerfile}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(git -C "$SCRIPT_DIR" rev-parse --show-toplevel)"
cd "$REPO_ROOT"

if ! command -v docker >/dev/null 2>&1; then
  echo "docker is required" >&2
  exit 1
fi

docker buildx version >/dev/null

commit_tag="$(git rev-parse HEAD)"
dirty=0
git diff --quiet --ignore-submodules=dirty || dirty=1
git diff --cached --quiet --ignore-submodules=dirty || dirty=1
if [ -n "$(git ls-files --others --exclude-standard)" ]; then
  dirty=1
fi
if [ "$dirty" -ne 0 ]; then
  commit_tag="${commit_tag}-dirty"
fi

cmd=(docker buildx build)
if [ -n "${BUILDER:-}" ]; then
  cmd+=(--builder "$BUILDER")
fi
cmd+=(
  --platform "$PLATFORM"
  -f "$DOCKERFILE"
  -t "${IMAGE_NAME}:latest"
  -t "${IMAGE_NAME}:${commit_tag}"
  --push
)
cmd+=("$CONTEXT")

printf 'Building and pushing %s with tags: latest, %s\n' "$IMAGE_NAME" "$commit_tag"
"${cmd[@]}"
