# civilsort

Ranked-choice voting web app: Go server (SQLite, WebSockets) computing live
Schulze rankings, with a TypeScript frontend in `web/src/`. See README.md for
features and architecture.

## Commands

- Build: `make build`. It bundles `web/src/` into `static/app.js` (gitignored)
  before `go build`; a bare `go build` embeds no JavaScript.
- Test: `go test -race ./...`
- Lint: `make lint` runs `golangci-lint run` (config in `.golangci.yml`),
  `go tool nilaway`, and `npm run typecheck`.
- Everything CI runs: `make check` (lint + tests).
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
