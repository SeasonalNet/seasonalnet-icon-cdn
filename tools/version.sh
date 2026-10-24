#!/bin/sh
# Git tags are authoritative; this script needs only POSIX sh, Git and grep.
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
number='(0|[1-9][0-9]*)'
identifier='(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)'
semver="^v$number\\.$number\\.$number(-$identifier(\\.$identifier)*)?(\\+[0-9A-Za-z-]+(\\.[0-9A-Za-z-]+)*)?$"
valid_tag() { printf '%s\n' "$1" | grep -Eq "$semver"; }
fail() { printf 'version: %s\n' "$*" >&2; exit 1; }
git_cmd() { git -C "$root" "$@"; }
has_git=false
if [ "$(git_cmd rev-parse --show-toplevel 2>/dev/null || true)" = "$root" ]; then
    has_git=true
fi
mode=${1:---version}
case "$mode" in
    --image-tag|--stable)
        tag=${2:-}
        valid_tag "$tag" || fail "expected a v-prefixed SemVer tag, got: $tag"
        if [ "$mode" = --stable ]; then
            case "${tag%%+*}" in *-*) printf 'false\n';; *) printf 'true\n';; esac
        else
            image_tag=$(printf '%s' "$tag" | tr '+' '_')
            [ "${#image_tag}" -le 128 ] || fail 'release tag exceeds the OCI tag length limit'
            printf '%s\n' "$image_tag"
        fi
        exit
        ;;
    --revision)
        if "$has_git"; then git_cmd rev-parse HEAD; else printf 'unknown\n'; fi
        exit
        ;;
    --timestamp)
        if "$has_git"; then git_cmd show -s --format=%cI HEAD; else printf 'unknown\n'; fi
        exit
        ;;
    --release)
        tag=${2:-}
        valid_tag "$tag" || fail "expected a v-prefixed SemVer tag, got: $tag"
        "$has_git" || fail 'release validation requires a Git checkout'
        [ "$(git_cmd rev-parse --is-shallow-repository)" = false ] || fail 'release validation requires full history and tags'
        [ "$(git_cmd rev-parse --verify "refs/tags/$tag^{commit}")" = "$(git_cmd rev-parse HEAD)" ] || fail 'release tag does not identify HEAD'
        git_cmd diff --quiet HEAD -- || fail 'release checkout has tracked modifications'
        printf '%s\n' "${tag#v}"
        exit
        ;;
    --version) ;;
    *) fail "unknown option: $mode" ;;
esac
if ! "$has_git"; then
    printf '0.0.0+unknown\n'
    exit
fi
revision=$(git_cmd rev-parse --short=12 HEAD)
# Ignore unrelated/malformed tags when selecting the nearest release ancestor.
set --
for candidate in $(git_cmd tag --merged HEAD --list 'v[0-9]*'); do
    if valid_tag "$candidate"; then set -- "$@" "--match=$candidate"; fi
done
nearest=
if [ "$#" -gt 0 ]; then nearest=$(git_cmd describe --tags --abbrev=0 "$@" HEAD); fi
dirty=
if ! git_cmd diff --quiet HEAD --; then dirty=.dirty; fi
if [ -n "$nearest" ] && [ "$(git_cmd rev-parse "refs/tags/$nearest^{commit}")" = "$(git_cmd rev-parse HEAD)" ] && [ -z "$dirty" ]; then
    printf '%s\n' "${nearest#v}"
else
    base=0.0.0
    if [ -n "$nearest" ]; then
        base=${nearest#v}; base=${base%%+*}; base=${base%%-*}
        distance=$(git_cmd rev-list --count "$nearest..HEAD")
    else
        distance=$(git_cmd rev-list --count HEAD)
    fi
    printf '%s-dev.%s+g%s%s\n' "$base" "$distance" "$revision" "$dirty"
fi
