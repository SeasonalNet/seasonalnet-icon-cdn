#!/bin/sh
# Exercise the real resolver against isolated Git histories; no project refs change.
set -eu
source_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
fixture=$(mktemp -d)
trap 'rm -rf "$fixture"' EXIT HUP INT TERM
mkdir -p "$fixture/repo/tools"
cp "$source_root/tools/version.sh" "$fixture/repo/tools/version.sh"
version() { sh "$fixture/repo/tools/version.sh" "$@"; }
assert_equal() {
    [ "$1" = "$2" ] || { printf 'expected %s, got %s\n' "$2" "$1" >&2; exit 1; }
}
assert_rejected() {
    if version "$@" > /dev/null 2>&1; then printf 'unexpectedly accepted: %s\n' "$*" >&2; exit 1; fi
}
assert_equal "$(version)" '0.0.0+unknown'
git init -q "$fixture/repo"
git -C "$fixture/repo" config user.name 'Version test'
git -C "$fixture/repo" config user.email 'version-test@example.invalid'
git -C "$fixture/repo" config commit.gpgSign false
git -C "$fixture/repo" config tag.gpgSign false
git -C "$fixture/repo" add tools
git -C "$fixture/repo" commit -qm initial
mkdir -p "$fixture/repo/export/tools"
cp "$source_root/tools/version.sh" "$fixture/repo/export/tools/version.sh"
assert_equal "$(sh "$fixture/repo/export/tools/version.sh")" '0.0.0+unknown'
revision=$(git -C "$fixture/repo" rev-parse --short=12 HEAD)
assert_equal "$(version)" "0.0.0-dev.1+g$revision"
git -C "$fixture/repo" tag v1.2.3
assert_equal "$(version)" '1.2.3'
assert_equal "$(version --release v1.2.3)" '1.2.3'
assert_equal "$(version --stable v1.2.3)" true
assert_equal "$(version --revision)" "$(git -C "$fixture/repo" rev-parse HEAD)"
printf '# modified\n' >> "$fixture/repo/tools/version.sh"
assert_equal "$(version)" "1.2.3-dev.0+g$revision.dirty"
assert_rejected --release v1.2.3
git -C "$fixture/repo" add tools
git -C "$fixture/repo" commit -qm change
revision=$(git -C "$fixture/repo" rev-parse --short=12 HEAD)
assert_equal "$(version)" "1.2.3-dev.1+g$revision"
assert_rejected --release v1.2.3
git -C "$fixture/repo" tag -a v2.0.0-rc.1 -m candidate
assert_equal "$(version --release v2.0.0-rc.1)" '2.0.0-rc.1'
assert_equal "$(version --stable v2.0.0-rc.1)" false
assert_equal "$(version --image-tag v2.0.0-rc.1+build.5)" 'v2.0.0-rc.1_build.5'
git -C "$fixture/repo" tag v2.0.0+build.5
assert_equal "$(version --release v2.0.0+build.5)" '2.0.0+build.5'
assert_equal "$(version --stable v2.0.0+build.5)" true
for tag in v01.2.3 v1.2 v1.2.3-01 v1.2.3+ v1.2.3- 1.2.3; do
    assert_rejected --release "$tag"
    assert_rejected --image-tag "$tag"
done
git clone -q --depth 1 "file://$fixture/repo" "$fixture/shallow"
if sh "$fixture/shallow/tools/version.sh" --release v2.0.0+build.5 > /dev/null 2>&1; then
    printf 'unexpectedly accepted a shallow release checkout\n' >&2; exit 1
fi
printf 'version tests: archive, snapshots, clean/dirty releases, tag identity, prereleases, metadata, malformed tags and shallow clones passed\n'

# Verify that the image builder forwards the resolved identity and caller options.
mkdir -p "$fixture/fake-bin"
cat > "$fixture/fake-bin/docker" <<'DOCKER'
#!/bin/sh
printf '%s\n' "$@" > "$BUILD_TRACE"
DOCKER
chmod +x "$fixture/fake-bin/docker"
BUILD_TRACE="$fixture/build-args" PATH="$fixture/fake-bin:$PATH" \
    APP_VERSION=3.4.5-rc.1 APP_REVISION=abc123 APP_BUILD_DATE=2026-01-01T00:00:00Z \
    sh "$source_root/tools/build-image.sh" --load --tag example:dev .
grep -Fxq 'APP_VERSION=3.4.5-rc.1' "$fixture/build-args"
grep -Fxq 'APP_REVISION=abc123' "$fixture/build-args"
grep -Fxq 'org.opencontainers.image.version=3.4.5-rc.1' "$fixture/build-args"
grep -Fxq 'org.opencontainers.image.revision=abc123' "$fixture/build-args"
grep -Fxq 'org.opencontainers.image.created=2026-01-01T00:00:00Z' "$fixture/build-args"
grep -Fxq 'example:dev' "$fixture/build-args"
printf 'image metadata forwarding passed\n'

if BUILD_TRACE="$fixture/build-args" PATH="$fixture/fake-bin:$PATH" \
    APP_VERSION=invalid-version sh "$source_root/tools/build-image.sh" --load . > /dev/null 2>&1; then
    printf 'unexpectedly accepted invalid build metadata\n' >&2; exit 1
fi
