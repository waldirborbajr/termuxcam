# Agent Instructions

## Project

- `termuxcam` is a Go service for periodic Android camera capture, Telegram uploads, and an optional HTTP API.
- The CLI entry point is `cmd/termuxcam`; implementation and tests live in `internal/termuxcam`.
- Camera and flashlight operations depend on Termux:API and cannot be fully exercised in the Linux development container.

## Development Checks

- Run `go test ./...` after code changes.
- Run `go vet ./...` for static checks; `just check` runs vet and tests.
- Run `go build ./cmd/termuxcam` to verify the CLI builds.
- Use `gofmt` on changed Go files.
- Add or update focused tests in `internal/termuxcam` for behavior changes.

## Implementation Guidance

- Keep changes scoped to the owning package and follow existing Go patterns.
- Preserve synchronization around capture operations; captures can be started by the daemon, API, and Telegram poller.
- Bound external commands and HTTP requests with timeouts, and avoid blocking the Telegram polling loop on long-running work.
- Escape user-visible Telegram content for its configured parse mode and never log bot tokens or other secrets.
- Do not assume camera IDs are the same across Android devices.
- Separate unit-testable logic from operations requiring an Android device or Termux:API; report when device-level validation was not possible.

## Running the Service

- `just start` invokes the built binary with `start` for manual background execution.
- For `termux-services`/runit supervision, run `termuxcam start` in the foreground so the supervisor can detect and restart process exits. Keep the service run script aligned with the README.
- Docker Compose also uses a foreground process with its restart policy enabled.
- Do not treat the daemon PID/status check as a substitute for the Termux service manager's `sv status`.

## Documentation and Configuration

- Keep operational setup, Termux permission requirements, and service recovery instructions consistent with `README.md`.
- Avoid adding real credentials to `termuxcam.toml`, tests, logs, or documentation.
- Do not commit generated binaries, camera captures, or runtime logs.