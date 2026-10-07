# Contributing to peek

Thanks for your interest in improving `peek`! Bug reports, feature ideas and pull requests are all welcome.

By participating, you agree to follow the [Code of Conduct](CODE_OF_CONDUCT.md).

## Reporting bugs and requesting features

Open an [issue](https://github.com/aaron03EM/peek/issues/new/choose). For bugs, please include:

- your OS and version (`uname -a` on Linux)
- the output of `go version` if you built from source
- the exact command you ran, what you expected and what happened instead

If you found a security problem, please don't open a public issue. See [SECURITY.md](SECURITY.md) instead.

## Development setup

You need [Go](https://go.dev/dl/) 1.26 or newer.

```sh
git clone https://github.com/aaron03EM/peek.git
cd peek
go build ./...
go test ./...
```

Useful commands:

```sh
go run ./cmd/peek             # run without installing
go build -o peek ./cmd/peek   # build a local binary (ignored by git)
go test ./...                 # run all tests
go vet ./...                  # static checks
gofmt -l .                    # list badly formatted files (should print nothing)

# check that other platforms still compile
GOOS=darwin go vet ./...
GOOS=windows go vet ./...
```

## Project layout

```
cmd/peek/             entry point and argument parsing
internal/scan/        Scanner interface, Listener type and per-OS implementations
  scan.go             shared types and port-range parsing
  procfs.go           pure /proc parsers (tested on every OS)
  scan_linux.go       Linux scanner
  scan_darwin.go      stub, not implemented yet
  scan_windows.go     stub, not implemented yet
  testdata/           fixture files for the /proc parsers
internal/ui/          table and JSON rendering
internal/kill/        signalling, confirmation and waiting for exit
docs/demo.svg         terminal screenshot used in the README
```

## Guidelines

- **Keep it small.** `peek` aims for instant startup and a tiny binary. Prefer the standard library. The only third-party dependency is Lip Gloss (with termenv). Please open an issue to discuss before adding a new dependency.
- **Keep platform code behind `scan.Scanner`.** The UI and CLI must not know which OS they run on. A new platform is a new `scan_<os>.go` file with a build tag.
- **No panics for expected failures.** Permission errors, processes exiting mid-scan and missing files are normal. Skip them or mark fields as unknown, and return clear error messages for real failures.
- **Test with fixtures, not the live system.** Parsers take an `io.Reader` or a configurable proc root so tests can use files from `testdata/` or a fake `/proc` tree built in a temp directory.
- **Respect the exit codes:** 0 success, 1 nothing listening or failure, 2 usage error.
- **Match the existing style:** `gofmt`, small functions, comments that explain *why*.

## Pull requests

1. Fork the repo and create a branch from `main`.
2. Make your change and add or update tests.
3. Make sure `go vet ./...`, `go test ./...` and `gofmt -l .` are clean.
4. If you changed behavior or output, update the README (and `docs/demo.svg` if the table looks different).
5. Open a pull request describing what changed and why.

Keep pull requests focused: one feature or fix per PR is much easier to review.

## License

By contributing, you agree that your contributions will be licensed under the [MIT License](LICENSE).
