#!/bin/sh
# Resolve identity outside the Docker context, which intentionally excludes .git.
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
version=${APP_VERSION:-$(sh "$root/tools/version.sh")}
sh "$root/tools/version.sh" --image-tag "v$version" > /dev/null
revision=${APP_REVISION:-$(sh "$root/tools/version.sh" --revision)}
timestamp=${APP_BUILD_DATE:-$(sh "$root/tools/version.sh" --timestamp)}
if [ "$timestamp" != unknown ]; then
    set -- --label "org.opencontainers.image.created=$timestamp" "$@"
fi
exec docker buildx build \
    --build-arg "APP_VERSION=$version" \
    --build-arg "APP_REVISION=$revision" \
    --label "org.opencontainers.image.version=$version" \
    --label "org.opencontainers.image.revision=$revision" \
    "$@"
