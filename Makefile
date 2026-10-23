.PHONY: build run dev app build-all install test clean

# The mygo CLI is a Go tool dependency (go.mod `tool`): no install step.
# Everything is pure Go (no cgo), so any machine builds every platform.

# ── Development ───────────────────────────────────────────────────────────────

# Run a development app bundle, rebuilt and restarted as files change
dev:
	go tool mygo dev

# Run directly, without bundling or live reload
run:
	go run ./cmd/desktop/

# Build a plain binary for the current OS/arch (no bundling)
build:
	go build -o us3ui ./cmd/desktop/

test:
	go test ./...

# ── Packaged apps (into build/<os>-<arch>/) ───────────────────────────────────

# Package for the current platform (.app on macOS, .exe on Windows, binary + .deb on Linux)
app:
	go tool mygo build -skip-dmg

# Package for every platform
build-all:
	go tool mygo build -skip-dmg -platform darwin/arm64,darwin/amd64,windows/amd64,windows/arm64,linux/amd64

# Install into /Applications (ad-hoc signed by mygo build, no Apple certificate required)
install: app
	$(eval ARCH := $(shell go env GOARCH))
	rm -rf "/Applications/Universal S3 GUI.app"
	cp -R "build/darwin-$(ARCH)/Universal S3 GUI.app" "/Applications/Universal S3 GUI.app"
	xattr -cr "/Applications/Universal S3 GUI.app"

# ── Cleanup ───────────────────────────────────────────────────────────────────
clean:
	rm -rf us3ui build .mygo
