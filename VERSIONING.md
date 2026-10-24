# Software versioning

Git release tags are the software version authority. Use `vMAJOR.MINOR.PATCH`
with optional [SemVer 2.0](https://semver.org/) prerelease and build metadata, such as `v1.2.3-rc.1`.
A clean tagged build reports `1.2.3-rc.1`. Untagged commits report
`MAJOR.MINOR.PATCH-dev.DISTANCE+gCOMMIT`; tracked modifications add `.dirty`.
A source export without Git or supplied build metadata reports `0.0.0+unknown`.
Package manifest versions are development metadata, not release authorities.

Both CI providers resolve identity from full Git history before application
checks. Release validation rejects malformed tags, shallow history, dirty
tracked files and tags that do not identify the checked-out commit. The resolved
`APP_VERSION`, `APP_REVISION` and `APP_BUILD_DATE` are shared by checks and builds.
No package-version bump is required to release a tagged commit.

Publishing runs only on `v*` tag pushes after its CI dependencies succeed.
Images receive the version tag and a commit tag; only stable releases update
`latest`. SemVer build metadata uses `+` in the software version and OCI label;
container tag names replace `+` with `_` to satisfy the registry tag grammar.
The build embeds the version into runtime metadata without including Git in the
runtime image. Version resolution never modifies project Git tags or commits.

## Local builds

```sh
sh tools/version.sh
sh tools/version.sh --revision
sh tools/test-version.sh
sh tools/build-image.sh --load --tag application:dev .
```

The image wrapper forwards ordinary Buildx options, including `--file`,
additional `--build-arg` options and the final build-context path.

For a Git-less source export, supply `APP_VERSION` and `APP_REVISION` explicitly
to the image wrapper. Raw `docker build` callers must pass those build arguments;
otherwise their image correctly reports an unknown development identity.

## Runtime identity

`make build` embeds the Git version and revision using Go linker flags. Query the binary with `icon-cdn --version`. `make container` forwards the same identity into the runtime binary and OCI labels. The Node and Python manifests in this repository describe asset/CI tooling, not application releases.
