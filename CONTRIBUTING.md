# Contributing to Starflux

Thanks for your interest in contributing.

## Development setup

Requires Go (see `go.mod` for the minimum version) and [Git LFS](https://git-lfs.com) (`testdata/fits/` holds ~85MB of reference FITS fixtures tracked via LFS).

```sh
git lfs install   # once per machine
git clone https://github.com/fikua/fikua-starflux.git

go build ./...
go vet ./...
go test ./...
gofmt -l .   # should print nothing
```

If you don't need the test fixtures (e.g. you're only reading code), skip downloading them with `GIT_LFS_SKIP_SMUDGE=1 git clone ...`.

## Making changes

1. Fork the repository and create a branch off `main`.
2. Keep commits focused and use clear, descriptive messages.
3. Add or update tests for any behavior change.
4. Ensure `go build ./...`, `go vet ./...`, `go test ./...`, and `gofmt -l .` are all clean before opening a pull request.
5. Open a pull request describing the change and its motivation.

## Reporting issues

Use [GitHub Issues](https://github.com/fikua/fikua-starflux/issues) for bug reports and feature requests. Include:

- What you expected to happen vs. what happened
- Steps to reproduce (a sample FITS file, if relevant)
- Your OS and the Starflux version

## Code style

- Standard Go formatting (`gofmt`); no linter-specific style beyond that.
- Comments explain *why*, not *what* — avoid restating what the code already says.
- Prefer small, focused packages under `internal/` over new abstractions.

## License

By contributing, you agree that your contributions will be licensed under the project's [Apache 2.0 License](LICENSE).
