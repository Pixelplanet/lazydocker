# CLAUDE.md

## Build & test
- All Go commands need `GOFLAGS=-mod=vendor` (deps are vendored, including the `jesseduffield/gocui` fork and the Docker SDK).
- Unit tests: `GOFLAGS=-mod=vendor go test ./...`
- Without a local Go toolchain, run them in Docker: `docker run --rm -v "<repo>:/src" -w /src -e GOFLAGS=-mod=vendor golang:1.24 go test ./...` (add `GOOS=windows go build -o lazydocker-dev.exe .` for a Windows binary).
- gocui is replaced by `third_party/gocui` in `go.mod`. It carries a local patch in `view.go` (search for "lazydocker patch"): views only re-wrap the lines that changed instead of the whole buffer. Edit it there, then run `go mod vendor`; its tests run with `cd third_party/gocui && go test ./...` (no `-mod=vendor`).
- CI also runs `golangci-lint run` (gofumpt formatting), `go run scripts/cheatsheet/main.go check` (regenerate with `generate` after changing keybindings) and `go mod vendor && git diff --exit-code`.
- Only vendored packages can be imported: `testify/assert` is vendored, `testify/require` is not.
