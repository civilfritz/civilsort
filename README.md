# civilsort

A web application for ranked-choice voting with real-time results. Uses the Schulze method (Condorcet completion) to compute fair, cycle-resistant rankings from collaborative ballots. Built in Go with WebSocket updates.

## Features

- **No login required** - Users are identified by secure browser cookies
- **Unique ballot URLs** - Each ballot gets a shareable URL
- **Ballot access control** - Open/Closed toggle to control new participant access; authorized participants can always re-join
- **Copy URL to clipboard** - One-click copy button next to the share URL
- **Collaborative item management** - Any participant can add items; users can delete items they added
- **Delete confirmation** - Prompts "Are you sure?" before deleting items
- **Drag-and-drop ranking** - Intuitive interface using SortableJS
- **Real-time results** - Results update live across all connected browsers via WebSockets
- **Dynamic updates** - Items added/deleted by other users update in-place without losing scroll position or input text
- **Dark/light mode** - Toggle with system preference detection and localStorage persistence
- **Schulze algorithm** - Implements the Schulze method for computing the final ranking
- **Hidden stats page** - De-identified aggregate statistics at `/stats`
- **Clean, responsive UI** - Works on desktop and mobile

## Quick Start

1. **Build and run:**
   ```bash
   go build -o civilsort
   ./civilsort
   ```

2. **Open in browser:**
   ```
   http://localhost:8080
   ```

3. **Create a ballot**, add items, and share the URL with participants!

## Architecture

### Technology Stack
- **Backend**: Go 1.25+ with standard library (`net/http`, `html/template`, `embed`)
- **Database**: SQLite with `modernc.org/sqlite` (pure Go, no CGO)
- **WebSockets**: `github.com/coder/websocket`
- **Frontend**: Vanilla JavaScript with SortableJS (loaded from CDN)
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
│   ├── handler/                  # HTTP handlers and middleware
│   └── hub/                      # WebSocket broadcast hub
├── templates/
│   ├── home.html                 # Landing page
│   ├── ballot.html               # Main ballot interface
│   ├── about.html                # About page with usage help
│   ├── stats.html                # Aggregate statistics
│   └── closed.html               # "Ballot is closed" error page
├── static/                       # CSS and JavaScript
└── civilsort.db                  # SQLite database (created at runtime)
```

## How It Works

### User Flow
1. Visit the homepage to create a new ballot (optionally with a title)
2. Share the unique ballot URL with participants (click the 📋 button to copy)
3. Control access with the **Open/Closed** toggle:
   - **Open**: New visitors can join and become participants
   - **Closed**: Only existing participants can access the ballot
   - The share URL and copy button are hidden when the ballot is closed
4. Each participant can:
   - Add items to the ballot
   - Remove items they added (with confirmation prompt)
   - Rank all items via drag-and-drop
5. The Results tab shows the live Schulze ranking, updated in real-time
6. Toggle between light/dark mode with the theme button (top-right)

### Cookie-Based Authentication
- First visit generates a UUID v4 stored in an HttpOnly, SameSite=Lax cookie
- Users can "reset" by clearing cookies to get a new identity
- No passwords or email required - perfect for casual voting

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
1. Open the ballot in **multiple browser windows** (or different browsers)
2. Add items and rank them differently in each window
3. Switch to the Results tab - you'll see the combined ranking update in real-time

### Unit Tests
Run all tests across the project:
```bash
go test ./... -v
```

Test coverage includes:
- **Schulze algorithm**: Basic elections, Condorcet winners, cycle resolution (A>B, B>C, C>A), incomplete rankings, ties and edge cases
- **HTTP handlers**: Ballot creation, item management, ranking saves, authorization checks
- **Database layer**: Schema validation, WAL mode, foreign keys
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

```sql
CREATE TABLE users (
    id           TEXT PRIMARY KEY,   -- UUID v4 from cookie
    display_name TEXT NOT NULL DEFAULT '',
    created_at   DATETIME DEFAULT (datetime('now'))
);

CREATE TABLE ballots (
    id         TEXT PRIMARY KEY,   -- 8-char base62 (URL-friendly)
    title      TEXT DEFAULT '',
    is_open    INTEGER NOT NULL DEFAULT 1,  -- 1 = open, 0 = closed
    created_by TEXT REFERENCES users(id),
    created_at DATETIME DEFAULT (datetime('now'))
);

CREATE TABLE ballot_participants (
    ballot_id      TEXT REFERENCES ballots(id) ON DELETE CASCADE,
    user_id        TEXT REFERENCES users(id),
    participant_id TEXT NOT NULL DEFAULT '',
    display_name   TEXT NOT NULL DEFAULT '',
    created_at     DATETIME DEFAULT (datetime('now')),
    PRIMARY KEY (ballot_id, user_id)
);

CREATE UNIQUE INDEX idx_bp_participant ON ballot_participants(ballot_id, participant_id);

CREATE TABLE items (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    ballot_id  TEXT REFERENCES ballots(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    added_by   TEXT REFERENCES users(id),
    created_at DATETIME DEFAULT (datetime('now')),
    UNIQUE(ballot_id, name)
);

CREATE TABLE rankings (
    ballot_id TEXT    REFERENCES ballots(id) ON DELETE CASCADE,
    user_id   TEXT    REFERENCES users(id),
    item_id   INTEGER REFERENCES items(id) ON DELETE CASCADE,
    position  INTEGER NOT NULL,  -- 1 = most preferred
    PRIMARY KEY (ballot_id, user_id, item_id)
);
```

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
