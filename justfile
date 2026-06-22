# kbrd task runner — run `just` to list recipes.

# Show available recipes.
default:
    @just --list

# Build the binary into ./kbrd.
build:
    go build -o kbrd ./

# Install kbrd into $GOBIN (or $GOPATH/bin, i.e. ~/go/bin) so it's on your PATH.
# Stamps model.Version from git (like the release's goreleaser ldflags) so the
# installed binary reports its real version, e.g. v0.11.0 or v0.11.0-1-gabc1234.
install:
    go install -ldflags "-X kbrd/model.Version=$(git describe --tags --always)" ./
    @echo "installed kbrd to $(go env GOBIN GOPATH | awk 'NR==1{b=$0} NR==2{p=$0} END{print (b!="" ? b : p"/bin")"/kbrd"}')"

# Run the test suite.
test:
    go test ./...

# Run go vet.
vet:
    go vet ./...

# Format all Go sources.
fmt:
    gofmt -w .

# Tidy module dependencies.
tidy:
    go mod tidy

# Install git hooks via prek (pre-commit + pre-push).
hooks:
    prek install --hook-type pre-commit --hook-type pre-push

# Run kbrd in the current directory (pass flags after --, e.g. `just run -- --no-mcp`).
run *args:
    go run . {{ args }}

# Build the docker image for the headless web server (kbrd serve).
docker-build:
    docker build --build-arg VERSION=$(git describe --tags --always) -t kbrd .

# Validate the GoReleaser config.
check:
    goreleaser check

# Build a local release into ./dist without publishing (dry run).
snapshot:
    goreleaser release --snapshot --clean

# Tag and push a release, triggering the GitHub Actions release workflow.
# Usage: just release 0.2.0   (creates and pushes tag v0.2.0)
release version:
    #!/usr/bin/env bash
    set -euo pipefail
    version="{{ version }}"
    version="${version#v}"
    tag="v${version}"
    if [ -n "$(git status --porcelain)" ]; then
        echo "error: working tree is not clean; commit or stash changes first" >&2
        exit 1
    fi
    if git rev-parse "$tag" >/dev/null 2>&1; then
        echo "error: tag $tag already exists" >&2
        exit 1
    fi
    echo "Running pre-release checks..."
    go test ./...
    goreleaser check
    echo "Tagging and pushing $tag..."
    git tag -a "$tag" -m "Release $tag"
    git push origin "$tag"
    echo "Pushed $tag — the release workflow will now build and publish it."

# Seed demo data and capture all README screenshots with VHS (requires vhs installed).
# Depends on `build` so the tape never captures a stale ./kbrd binary.
screenshots: build
    demo/seed.sh
    vhs demo/screenshots.tape
