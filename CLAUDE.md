# CLAUDE.md

## Build & test
- All Go commands need `GOFLAGS=-mod=vendor` (deps are vendored, including the `jesseduffield/gocui` fork and the Docker SDK).
- Unit tests: `GOFLAGS=-mod=vendor go test ./...`
- Without a local Go toolchain, run them in Docker: `docker run --rm -v "<repo>:/src" -w /src -e GOFLAGS=-mod=vendor golang:1.24 go test ./...` (add `GOOS=windows go build -o lazydocker-dev.exe .` for a Windows binary).
- `vendor/github.com/jesseduffield/gocui/view.go` carries a local patch (search for "lazydocker patch"): views only re-wrap the lines that changed instead of the whole buffer. `go mod vendor` overwrites it, so re-apply it after re-vendoring.
- Only vendored packages can be imported: `testify/assert` is vendored, `testify/require` is not.
