#!/bin/sh
# Generates shell completion scripts into completions/ for the release
# archives. GoReleaser runs it before building.
set -e
rm -rf completions
mkdir completions
for sh in bash zsh fish; do
	go run ./cmd/peek completion "$sh" >"completions/peek.$sh"
done
