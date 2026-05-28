# kbrd task runner — run `just` to list recipes.

# Show available recipes.
default:
    @just --list

# Build the binary into ./kbrd.
build:
    go build -o kbrd ./

# Install kbrd into $GOBIN (or $GOPATH/bin, i.e. ~/go/bin) so it's on your PATH.
install:
    go install ./
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

# Run kbrd in the current directory (pass flags after --, e.g. `just run -- --no-mcp`).
run *args:
    go run . {{ args }}

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
