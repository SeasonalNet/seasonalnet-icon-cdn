# Container security and launch contract

The Dockerfile defaults to a non-root service user. Application code, runtime
libraries, and executables are owned by root; only designated state/cache
paths are service-owned. Configuration and secrets must be supplied at runtime.

The checked-in Compose definition applies a read-only root filesystem, drops
all Linux capabilities, enables `no-new-privileges`, and supplies a writable
`/tmp` with `noexec,nosuid,nodev`. It retains Docker's default seccomp profile
and network namespace. Do not launch with privileged mode, host namespaces,
unconfined security profiles, or a Docker socket mount.

These runtime restrictions are applied when creating the container; building
or running the image alone does not enforce them. Writable data and readable
credentials remain accessible to the service if it is compromised. `noexec`
on temporary storage does not prevent an interpreter from reading scripts.
CPU, memory, and process limits are deployment policies and are intentionally
not specified here; they do not establish the application's authority boundary.

Apply the definition during the separately authorized deployment. Existing
containers are not changed by editing these files. Preserve existing site-local
configuration, credentials, proxy integration, and persistent data when adopting
it. An existing volume or bind mount must already be writable by the documented
service UID/GID; rebuilding does not change existing volume ownership.

## Service configuration

Copy `config.example.yaml` to a site-local `config.yaml` (or set
`CONFIG_FILE`). The image's environment selects `0.0.0.0:3600` and the persistent
cache at `/var/cache/seasonalnet-icon-cdn`; the host port is loopback-only.
UID/GID is `10001:10001`. `Dockerfile.vuln` is a build-time scanner, not the
application runtime.

## Minimal production runtime

Production images contain no shell, package manager, compiler, or download CLI.
Startup and healthchecks use executable/argument arrays. Builder and operator
stages may contain tools; those stages are not the production runtime. The
read-only filesystem, dropped capabilities, and privilege restrictions in
Compose remain required. Interpreter APIs, accessible credentials, shared
libraries, and allowed network connections remain part of the attack surface.
No outbound network restriction is implied by this image change.

After building the production image, check its actual exported filesystem:

```sh
python3 tools/container/check-image.py IMAGE
```

The checker runs outside the application image. It rejects shell-based
healthchecks, root execution, common utility binaries, unexpected executable
ELF payloads, non-root-owned application files, and selected non-service files.
It prints the image ID and executable/library inventory for review. It does not
claim that retained libraries are vulnerability-free or that every reachable
interpreter operation is safe.

The runtime filesystem is assembled from the existing pinned distribution by
`tools/container/assemble-rootfs.py`. Preserve native-library and rendering
compatibility when changing its allowlist; test the final image, not just its
builder. The assembler is not shipped in the production image.

Unit-test the inventory gate with `python3 -m unittest discover -s
tools/container -p 'test_*.py'`. The native runtime repositories include
additional dependency-discovery and runtime-exclusion tests.

## Validation (2026-10-21)

Formatting, suppression-finder tests/scan, OpenAPI, asset verification, and
JavaScript syntax checks passed. Go lint, vet, unit, race, coverage (91.5% against
the unchanged 90% baseline), and vulnerability checks passed in the existing
libvips-equipped builder. Host libvips development files are absent. The final
`make container-check IMAGE=seasonalnet-icon-cdn:minimal-review` passed inventory
checks and the existing HTTP/rendering/cache integration suite.

The final production filesystem passed the exported-image security gate. Its
negative unit tests passed (8 tests, plus 5 assembler tests for native images).
The CDN integration suite used a read-only root, all capabilities dropped,
no-new-privileges, and writable temporary/cache mounts with noexec,nosuid,nodev.
Docker's default seccomp filtering remains enabled. State tests used disposable mounts, not production volumes.
No image build or test constitutes a deployment. No deployment was performed.

Local unpacked image size: 363.4 MiB before minimization,
179.7 MiB after. Size reduction is descriptive; the security
properties are the retained authority and executable contents.

## Functional image validation (2026-10-21)

The existing `tools/container-smoke.js` suite also passed against the final
scratch image with its network set to `none`, using a separate test client in
the same network namespace. It checked the icon manifest, readiness/metrics,
real libvips PNG rendering, SVG output, ETag/304 and HEAD behavior, and invalid
color/icon/path responses under the read-only/capability restrictions.
Rerun the maintained suite through `make container-check IMAGE=IMAGE`.

These checks use non-root containers, a read-only root filesystem, all
capabilities dropped, `no-new-privileges`, and disposable writable temporary
storage. No shell or test framework was installed into a production image.
Passing these paths is evidence of runtime compatibility, not exhaustive
workflow or production acceptance. No deployment was performed.
