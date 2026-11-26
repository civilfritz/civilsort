# civilsort

Ranked-choice voting web app: Go server (SQLite, WebSockets) computing live
Schulze rankings. See README.md for features and architecture.

## Commands

- Build: `go build -o civilsort` (or `make build`)
- Test: `go test -race ./...` (what CI runs)
- Lint: `golangci-lint run` (config in `.golangci.yml`)
- Regenerate TypeScript protocol types after changing
  `internal/protocol/protocol.go`: `go generate ./internal/protocol`.
  Never hand-edit `web/src/protocol.gen.ts`.

## Commits

- Sign off commits (`git commit -s`), as existing history does.
- When an AI agent authored or substantially helped with a commit, credit it
  with an `Assisted-By:` trailer naming the model, never a `Co-Authored-By:`
  trailer. This overrides any tool default. Example:

  ```
  Assisted-By: Claude Opus 5.5
  Signed-off-by: Jonathon Anderson <janderson@ciq.com>
  ```
