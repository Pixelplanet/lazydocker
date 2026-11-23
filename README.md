# lazydocker (Pixelplanet fork)

A fork of [jesseduffield/lazydocker](https://github.com/jesseduffield/lazydocker), the terminal UI for docker and docker compose. Everything upstream does still works the same way; see the [upstream README](https://github.com/jesseduffield/lazydocker#readme) for general usage, and [docs/Config.md](docs/Config.md) and [docs/keybindings](docs/keybindings) for configuration and keys.

This page covers what the fork changes and how to install or update it on Ubuntu.

## Install or update on Ubuntu

### From a release

```sh
curl -fsSL https://raw.githubusercontent.com/Pixelplanet/lazydocker/master/scripts/install_update_linux.sh | bash
```

Run the same command again to update. The script downloads the latest release for your architecture (amd64, arm64 or armv7), verifies its checksum and installs it.

- If `lazydocker` is already on your `PATH`, that copy is replaced, using `sudo` if its directory is not writable.
- Otherwise it is installed to `~/.local/bin`.
- Set `DIR` to install somewhere else, or `VERSION` to install a specific release:

```sh
curl -fsSL https://raw.githubusercontent.com/Pixelplanet/lazydocker/master/scripts/install_update_linux.sh | DIR=/usr/local/bin VERSION=v1.0.0 bash
```

The script needs a published release. To publish one, push a version tag and the [release workflow](.github/workflows/release.yml) builds the binaries:

```sh
git tag v1.0.0
git push origin v1.0.0
```

### From source

This builds the current `master` using Docker, so Go does not need to be installed. Run it again to update.

```sh
git clone https://github.com/Pixelplanet/lazydocker.git ~/lazydocker-src 2>/dev/null || git -C ~/lazydocker-src pull
cd ~/lazydocker-src
docker run --rm -v "$PWD":/src -w /src -e GOFLAGS=-mod=vendor -e CGO_ENABLED=0 golang:1.24 \
  go build -buildvcs=false -ldflags "-X main.version=$(git rev-parse --short HEAD)" -o lazydocker .
sudo install -m 755 lazydocker /usr/local/bin/lazydocker
```

The version in the bottom-right corner of lazydocker shows which build you are running.

## What this fork changes

### Container actions (`R` on a container)

- **Update to latest image and recreate**: pulls the image, then recreates the container with the same name and configuration (environment, volumes, ports, networks, labels, restart policy).
- **Recreate (keep current image)**: the same without the pull.
- **Recreate from compose file**: recreates a compose-managed container through `docker compose`, so changes made to its compose files are applied. The prompt says whether the service's configuration has changed since the container was created. Compose decides the new container's name; if it differs from the old one you are offered to rename it back.
- **Edit environment variables and recreate**: for standalone containers.
- **Start**, **rename** and **change restart policy** (applied without recreating).

If creating or starting the replacement fails, the original container is put back and restarted.

Every confirmation names the container it is about to act on, and bulk actions list the containers they will touch.

### Config tab

- The container Config tab starts with a summary in sections: General, Network (mode, each network's IP, gateway, MAC and aliases, published ports, DNS), Mounts, Runtime (restart policy, limits, health), Compose and Labels. The full `docker inspect` output follows below.
- `g` opens a menu of those sections, and of the blocks inside the full output, and scrolls straight to the one you pick.

### Other additions

- **Copy to clipboard** (`y` on a container): container ID, name, image, a single environment variable, or all of them exported to a `.env` file. `y` in the main panel copies its content, or opens the environment variable list on the Env tab. Works over SSH through OSC52 and locally through `xclip`, `xsel`, `pbcopy` or `clip.exe`.
- **Pull an image** (`p` on an image).
- **Update all containers** (`b` on the containers panel): pulls and recreates every running standalone container.
- Images are pulled through the docker CLI, so `docker login` credentials and credential helpers are used. Images that only exist locally are handled instead of failing the pull.
- Volumes and networks lists refresh after removing or pruning.

### Performance

- Following the logs of a busy container no longer gets more expensive the more log is on screen.
- Container details and stats are only read for the containers on screen, not for every container on the host.
- Refreshes triggered by docker events are throttled, and volumes, networks and images are reloaded less often.
- `gui.refreshInterval` sets how often container details are re-read in the background, in seconds (default `5`, `0` to disable). See [docs/Config.md](docs/Config.md).

### Removed or changed

- The UI is English only; the `gui.language` setting is ignored.
- The donate link is removed.
- The version in the bottom-right corner is labelled `ver:`.

## Staying in sync with upstream

```sh
git remote add upstream https://github.com/jesseduffield/lazydocker.git
git fetch upstream
git merge upstream/master
```

The `gocui` UI library is built from [third_party/gocui](third_party/gocui), a copy of the upstream version with one local patch in `view.go` (search for "lazydocker patch"). `go.mod` points at that directory, so `go mod vendor` keeps the patch. If upstream moves to a newer gocui, copy the new version into `third_party/gocui`, re-apply the patch and run that directory's tests.

## Development

Dependencies are vendored, so Go commands need `GOFLAGS=-mod=vendor`:

```sh
GOFLAGS=-mod=vendor go test ./...
GOFLAGS=-mod=vendor go build .
```

## License

MIT, as upstream. See [LICENSE](LICENSE).
