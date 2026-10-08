#!/bin/sh
# Records docs/demo.gif from docs/demo/demo.tape with VHS in Docker. The
# demo runs in the container's own network namespace, so peek only sees the
# servers setup.sh stages there, nothing from your machine.
set -e
cd "$(dirname "$0")/../.."
bin=$(mktemp -d)
trap 'rm -rf "$bin"' EXIT
CGO_ENABLED=0 GOOS=linux go build -o "$bin/peek" ./cmd/peek
CGO_ENABLED=0 GOOS=linux go build -o "$bin/fakeserver" ./docs/demo/fakeserver
cp docs/demo/setup.sh "$bin/setup.sh"
docker run --rm -v "$PWD:/vhs" -v "$bin:/demo/bin:ro" -v "$bin/setup.sh:/demo/setup.sh:ro" \
	ghcr.io/charmbracelet/vhs docs/demo/demo.tape
# The container runs as root; give the GIF back to the current user.
docker run --rm -v "$PWD/docs:/docs" --entrypoint chown ghcr.io/charmbracelet/vhs "$(id -u):$(id -g)" /docs/demo.gif
