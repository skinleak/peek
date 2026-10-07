# peek

[![CI](https://github.com/aaron03EM/peek/actions/workflows/ci.yml/badge.svg)](https://github.com/aaron03EM/peek/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/aaron03EM/peek.svg)](https://pkg.go.dev/github.com/aaron03EM/peek)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

**See what's listening on your ports, and free them up.**

`peek` is a fast, zero-config CLI that replaces the
`lsof -i :3000 | grep LISTEN | awk '{print $2}' | xargs kill` incantation everyone keeps googling.

<p align="center">
  <img src="docs/demo.svg" alt="peek listing listening ports with process, PID, address, working directory and uptime, then killing the process on port 3000" width="720">
</p>

## Features

- **One command, no flags to remember:** `peek` lists everything that's listening.
- **Answers "who's on port 3000?"** with the process, PID, bind address, working directory and uptime.
- **Frees a port safely:** `peek kill 3000` asks first, sends SIGTERM and checks that the process actually exited.
- **Knows about Docker:** ports published by containers show the container name, and `peek kill` stops the container instead of breaking Docker's proxy.
- **Shows exposure at a glance:** binds reachable from other machines (`0.0.0.0`, `::`) are highlighted differently from local-only ones (`127.0.0.1`, `::1`).
- **Scriptable:** `--json` output and meaningful exit codes.
- **Tiny and instant:** a single static binary for Linux and macOS, with no config files and no runtime dependencies.

## Install

**Homebrew** (macOS and Linux):

```sh
brew install aaron03EM/tap/peek
```

**Install script** (macOS and Linux). It downloads the latest release, verifies its checksum and installs to `/usr/local/bin`, or `~/.local/bin` if that isn't writable:

```sh
curl -fsSL https://raw.githubusercontent.com/aaron03EM/peek/main/install.sh | sh
```

**Go** (1.26 or newer):

```sh
go install github.com/aaron03EM/peek/cmd/peek@latest
```

Or download a binary from the [releases page](https://github.com/aaron03EM/peek/releases).

## Usage

```sh
peek                    # everything listening
peek 3000               # who's on port 3000
peek 3000-3999          # a port range
peek 22 8000-8100       # several ports and ranges

peek kill 3000          # stop the process on port 3000 (asks for confirmation)
peek kill 3000 --yes    # don't ask
peek kill 3000 --force  # send SIGKILL instead of SIGTERM

peek --json             # machine-readable output
peek --version
peek --help
```

Flags can go before or after the ports, so `peek kill 3000 -f -y` works too.

### Reading the output

| Column  | Meaning |
|---------|---------|
| PORT    | The listening TCP port |
| PROCESS | Process name (`-` if it belongs to another user and you aren't root) |
| PID     | Process ID |
| ADDRESS | Bind address: amber when exposed to the network, green when local-only |
| CWD     | The process's working directory, with your home shown as `~` |
| UPTIME  | How long the process has been running |

A socket shared by several processes (for example a pre-forking web server) shows one row per process.

Colors adapt to light and dark terminals. They're switched off automatically when output isn't a terminal or when [`NO_COLOR`](https://no-color.org) is set.

### Docker containers

When a port is published by a Docker container, `peek` shows the container name instead of `docker-proxy`:

```
 PORT  PROCESS             PID  ADDRESS  CWD  UPTIME
 5432  webapp-db (docker)    -  0.0.0.0  -    -
```

`peek kill 5432` then stops the container through the Docker API (`docker stop`, or `docker kill` with `--force`) rather than killing the proxy process, which would free the port but leave Docker in a broken state.

This needs access to the Docker socket (on Linux, being in the `docker` group). It works with `/var/run/docker.sock`, Docker Desktop, rootless Docker and `DOCKER_HOST=unix://...`. If Docker isn't available, `peek` works as usual.

### Seeing other users' processes

Operating systems only let you inspect your own processes without root. Run `sudo peek` to see everything.

- **Linux:** ports owned by other users still show up, but with the process and PID as `-`.
- **macOS:** ports owned by other users don't show up at all, the same as `lsof`.

### JSON

```sh
$ peek 3000 --json
[
  {
    "port": 3000,
    "protocol": "tcp",
    "address": "127.0.0.1",
    "pid": 48213,
    "process": "node",
    "cwd": "/home/dev/code/webapp",
    "start_time": "2026-10-07T13:01:42+02:00",
    "user": "dev"
  }
]
```

Fields that couldn't be read (such as `pid` for another user's process) are left out. Ports published by Docker containers also have `container` and `container_id`. When nothing matches, the output is `[]`.

### Exit codes

| Code | Meaning |
|------|---------|
| 0    | Success |
| 1    | Nothing is listening on the requested port(s), the kill was declined, or something failed |
| 2    | Usage error, such as an invalid port or unknown flag |

This makes `peek` handy in scripts:

```sh
peek 5432 >/dev/null || echo "start the database first"
```

## Platform support

| OS      | Status |
|---------|--------|
| Linux   | Supported (amd64, arm64) |
| macOS   | Supported (Apple Silicon and Intel) |
| Windows | Planned |

## How it works

`peek` doesn't shell out to `lsof`, `ss` or `netstat`; it asks the kernel directly.

- **Linux:** it reads `/proc/net/tcp` and `/proc/net/tcp6` for sockets in the LISTEN state, maps each socket's inode to processes by scanning `/proc/<pid>/fd`, and reads each process's name, working directory and start time from `/proc/<pid>`.
- **macOS:** it uses libproc (`proc_pidinfo` and `proc_pidfdinfo`), the same interface `lsof` uses. It calls libproc without cgo, so the binary stays static.

## Roadmap

- `peek --watch`: a live-refreshing view
- Windows support
- UDP sockets

## Contributing

Contributions are welcome! See [CONTRIBUTING.md](CONTRIBUTING.md) to get started.

## License

[MIT](LICENSE)
