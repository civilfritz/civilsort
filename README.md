# civilsort

A web application for ranked-choice voting with real-time results. Uses the Schulze method (Condorcet completion) to compute fair, cycle-resistant rankings from collaborative ballots. Built in Go with WebSocket updates.

## Features

- **No login required** - Each ballot remembers its participants with a cookie scoped to that ballot
- **Unique ballot addresses** - Anyone with a ballot's address can watch it live; joining takes a name
- **Creator controls** - The creator opens or closes the ballot to new participants, edits the title, recovers or resets participants' links, removes participants, and can delete the ballot
- **Private rankings** - The creator can hide individual rankings so only combined results are shown, from the top of the participant list (eye for visible, incognito for private)
- **Add device** - Each participant can show a personal link, as a QR code or a link to copy, that signs another browser in as them
- **Collaborative item management** - Any participant can add items and remove their own until someone else ranks them
- **Remove confirmation** - Prompts "Are you sure?" before removing items
- **Drag-and-drop ranking** - Intuitive interface using SortableJS
- **Real-time results** - Results update live across all connected browsers via WebSockets
- **Dynamic updates** - Items added/removed by other users update in-place without losing scroll position or input text
- **Dark/light mode** - Toggle with system preference detection and localStorage persistence
- **Schulze algorithm** - Implements the Schulze method for computing the final ranking
- **Hidden stats page** - De-identified aggregate statistics at `/stats`
- **Clean, responsive UI** - Works on desktop and mobile

## Quick Start

1. **Build and run** (requires Go and Node.js):
   ```bash
   make build
   ./civilsort
   ```

2. **Open in browser:**
   ```
   http://localhost:8080
   ```

3. **Create a ballot**, add items, and share its address with participants!

## Architecture

### Technology Stack
- **Backend**: Go 1.26+ with standard library (`net/http`, `html/template`, `embed`)
- **Database**: SQLite with `modernc.org/sqlite` (pure Go, no CGO)
- **WebSockets**: `github.com/coder/websocket`
- **Frontend**: TypeScript bundled with esbuild, SortableJS
- **Deployment**: Single binary with embedded templates and static files

### Project Structure
```
civilfritz-voting/
├── main.go                       # Entry point
├── internal/
│   ├── cli/                      # CLI command implementations
│   ├── cmd/                      # Cobra command wiring
│   ├── db/                       # SQLite database layer
│   ├── model/                    # Domain types (Ballot, Item, Ranking)
│   ├── schulze/                  # Pure Schulze algorithm with tests
│   ├── handler/                  # HTTP handlers, identity, and roles
│   ├── hub/                      # WebSocket broadcast hub
│   └── protocol/                 # JSON message types shared with the frontend
├── templates/
│   ├── home.html                 # Landing page
│   ├── ballot.html               # Main ballot interface
│   ├── about.html                # About page with usage help
│   ├── stats.html                # Aggregate statistics
├── web/src/                      # TypeScript sources (bundled to static/app.js)
├── static/                       # CSS and built JavaScript
└── civilsort.db                  # SQLite database (created at runtime)
```

## How It Works

### User Flow
1. Create a ballot from the homepage with your name and an optional title. You're its creator.
2. Share the ballot's address from the address bar, or with **Share** in the menu (☰), which shows it as a QR code for others to scan and as a link to copy.
3. Anyone with the address sees the ballot as a **viewer**: items, live results, and who's participating. While the ballot is **Open**, viewers can **Join** by entering a name.
4. **Participants** add items, drag them into the Ranked zone (or tap **+** to add one to the bottom; **−** unranks it), and rename themselves. They can remove items they added until someone else ranks them.
5. The Results tab shows the live Schulze ranking.

### Roles
| | Viewer | Participant | Creator |
|---|---|---|---|
| See items, results, participant list | ✓ | ✓ | ✓ |
| Join (while open) | ✓ | | |
| Add items, rank, rename self | | ✓ | ✓ |
| View others' rankings (unless private) | | ✓ | ✓ |
| Remove items | | own, until someone else ranks them | any |
| Lock/unlock joining (the padlock), edit title, make rankings private | | | ✓ |
| Copy, reset, or remove participants; delete ballot | | | ✓ |

Closing a ballot stops new people joining; it stays viewable. Making rankings
private can't be undone once anyone other than the creator has ranked.

### Identity
- There are no accounts. Joining a ballot creates a participant with a secret,
  stored in an HttpOnly, SameSite=Lax cookie scoped to `/ballot/{id}`, so each
  ballot only sees its own cookie.
- **Add device** shows a `/ballot/{id}/join/{secret}` link, as a QR code or to copy, that signs
  another browser in as you. The creator can copy or reset anyone's link, for
  someone who lost theirs or whose link leaked.
- Browsers from before per-ballot identity (with a `voter_id` cookie) are
  recognized on ballots they had joined.
- Cross-origin POSTs are rejected, and cookies are `Secure` over HTTPS
  (including behind a proxy that sets `X-Forwarded-Proto`).

### Schulze Algorithm
The [Schulze method](https://en.wikipedia.org/wiki/Schulze_method) is a Condorcet completion method:

1. **Build pairwise preference matrix**: Count how many voters prefer each candidate over each other candidate
2. **Initialize strongest path matrix**: Start with direct wins
3. **Floyd-Warshall variant**: Compute strongest paths between all candidates
4. **Determine ranking**: Candidate `i` beats `j` if the strongest path from `i` to `j` is stronger than from `j` to `i`

**Incomplete rankings**: Ranked items are preferred over unranked items; unranked items are tied with each other.

**Time complexity**: O(V × C² + C³) where V = voters, C = candidates

### Real-Time Updates
- Each ballot has a WebSocket hub that manages connected clients
- When any user saves a ranking, adds/removes an item, or toggles the ballot state:
  1. The server recomputes the Schulze results (if needed)
  2. The hub broadcasts updates to all connected clients
  3. The UI updates instantly without page refresh using DOM reconciliation
- Updates preserve scroll position and input text - you won't lose what you're typing

## Testing

### Manual Testing
1. Open the ballot in **different browsers or private windows** (windows of one browser share its cookies) and join each with a different name
2. Add items and rank them differently in each window
3. Switch to the Results tab - you'll see the combined ranking update in real-time

### Unit Tests
Run all tests across the project:
```bash
go test ./... -v
```

### Checks
`make check` runs everything CI runs: `golangci-lint`, `nilaway`, the TypeScript
type checker, and `go test -race`. It needs
[golangci-lint](https://golangci-lint.run/) installed; `nilaway` runs via `go tool`.

The WebSocket and JSON API message types live in `internal/protocol`. After
changing them, run `make generate` to regenerate `web/src/protocol.gen.ts`; CI
fails if the generated file is stale.

Test coverage includes:
- **Schulze algorithm**: Basic elections, Condorcet winners, cycle resolution (A>B, B>C, C>A), incomplete rankings, ties and edge cases
- **HTTP handlers**: Ballot creation, viewing and joining, per-ballot cookies and join links, creator-only actions, rankings visibility, item and ranking validation
- **Database layer**: Schema, WAL mode, foreign keys on every connection, migration from earlier schemas
- **WebSocket hub**: Hub manager lifecycle and broadcast safety
- **CLI commands**: List ballots, items, and participants with error handling

## Configuration

### Server

Start the web server with command-line flags:
```bash
./civilsort --addr :8080 --db civilsort.db
```

| Flag | Default | Env Var | Description |
|------|---------|---------|-------------|
| `--addr` | `:8080` | `CIVILSORT_ADDR` | Listen address |
| `--db` | `civilsort.db` | `CIVILSORT_DB` | SQLite database path |

Environment variables are overridden by command-line flags.

### CLI Commands

List all ballots:
```bash
./civilsort list
```

List items for a specific ballot:
```bash
./civilsort list items <ballot-id>
```

List participants for a specific ballot:
```bash
./civilsort list participants <ballot-id>
```

## Debian Package

Build a `.deb` package:
```bash
make deb
```

Install:
```bash
sudo dpkg -i dist/civilsort_*.deb
sudo systemctl enable --now civilsort
```

Configuration: `/etc/default/civilsort`
Database: `/var/lib/civilsort/civilsort.db`

## Reverse Proxy with Caddy

Example Caddyfile configuration for HTTPS deployment:

```caddy
vote.example.com {
    reverse_proxy localhost:8080
}
```

For a more complete configuration with logging and WebSocket support:

```caddy
vote.example.com {
    # Reverse proxy to civilsort
    reverse_proxy localhost:8080 {
        # WebSocket support (automatically detected by Caddy)
        flush_interval -1
    }

    # Access logging
    log {
        output file /var/log/caddy/civilsort.log
        format json
    }

    # Security headers
    header {
        # Enable HSTS
        Strict-Transport-Security "max-age=31536000; includeSubDomains; preload"
        # Prevent clickjacking
        X-Frame-Options "SAMEORIGIN"
        # XSS protection
        X-Content-Type-Options "nosniff"
        # Remove server info
        -Server
    }
}
```

Caddy automatically handles:
- HTTPS certificate provisioning via Let's Encrypt
- HTTP to HTTPS redirects
- WebSocket upgrades
- HTTP/2 and HTTP/3 support

## Database Schema

The schema is defined in [`internal/db/db.go`](internal/db/db.go) and versioned
with `PRAGMA user_version`; older databases are migrated on startup. In brief:

- `ballots`: title, whether it's open to new participants, and whether
  rankings are private.
- `participants`: one row per person per ballot, keyed by `(ballot_id, id)`.
  `id` is shown to others; `secret` is the credential in their cookie and join
  link. `is_creator` marks the admin, and removed participants keep their row
  with `removed_at` set and no secret.
- `items` and `rankings` reference participants by ID.

## Example Use Cases

- **Team lunch decisions** - "Where should we eat Friday?"
- **Feature prioritization** - "Which features should we build next?"
- **Book/movie club voting** - "What should we read/watch this month?"
- **Event planning** - "Which date works best for everyone?"

## License

This project is licensed under the BSD 3-Clause License - see the [LICENSE](LICENSE) file for details.

Copyright © 2026 Jonathon Anderson

## Credits

Built with:
- [Go](https://go.dev)
- [SortableJS](https://sortablejs.github.io/Sortable/)
- [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite)
- [coder/websocket](https://github.com/coder/websocket)
