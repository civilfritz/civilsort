# Schulze Method Ranked-Choice Voting

A web application for conducting ranked-choice voting using the Schulze method (also known as the Condorcet method). Built in Go with real-time WebSocket updates.

## Features

- **No login required** - Users are identified by secure browser cookies
- **Unique ballot URLs** - Each ballot gets a shareable URL
- **Collaborative item management** - Any participant can add items; users can delete items they added
- **Drag-and-drop ranking** - Intuitive interface using SortableJS
- **Real-time results** - Results update live across all connected browsers via WebSockets
- **Schulze algorithm** - Implements the Schulze method for computing the final ranking
- **Clean, responsive UI** - Works on desktop and mobile

## Quick Start

1. **Build and run:**
   ```bash
   go build -o voting
   ./voting
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
│   ├── db/                       # SQLite database layer
│   ├── model/                    # Domain types (Ballot, Item, Ranking)
│   ├── schulze/                  # Pure Schulze algorithm with tests
│   ├── handler/                  # HTTP handlers and middleware
│   └── hub/                      # WebSocket broadcast hub
├── templates/                    # HTML templates
├── static/                       # CSS and JavaScript
└── voting.db                     # SQLite database (created at runtime)
```

## How It Works

### User Flow
1. Visit the homepage to create a new ballot (optionally with a title)
2. Share the unique ballot URL with participants
3. Each participant can:
   - Add items to the ballot
   - Remove items they added
   - Rank all items via drag-and-drop
4. The Results tab shows the live Schulze ranking, updated in real-time

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
- When any user saves a ranking or adds/removes an item:
  1. The server recomputes the Schulze results
  2. The hub broadcasts the new results to all connected clients
  3. The Results tab updates instantly without page refresh

## Testing

### Manual Testing
1. Open the ballot in **multiple browser windows** (or different browsers)
2. Add items and rank them differently in each window
3. Switch to the Results tab - you'll see the combined ranking update in real-time

### Unit Tests
The Schulze algorithm has comprehensive tests:
```bash
go test ./internal/schulze/... -v
```

Test cases include:
- Basic 3-candidate elections
- Condorcet winners
- Cycle resolution (A>B, B>C, C>A)
- Incomplete rankings
- Ties and edge cases

## Configuration

Command-line flags:
```bash
./voting -addr :8080 -db voting.db
```

- `-addr`: Listen address (default: `:8080`)
- `-db`: SQLite database path (default: `voting.db`)

## Database Schema

```sql
CREATE TABLE users (
    id         TEXT PRIMARY KEY,   -- UUID v4 from cookie
    created_at DATETIME DEFAULT (datetime('now'))
);

CREATE TABLE ballots (
    id         TEXT PRIMARY KEY,   -- 8-char base62 (URL-friendly)
    title      TEXT DEFAULT '',
    created_by TEXT REFERENCES users(id),
    created_at DATETIME DEFAULT (datetime('now'))
);

CREATE TABLE items (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    ballot_id  TEXT REFERENCES ballots(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    added_by   TEXT REFERENCES users(id),
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

This is a demonstration project. Use freely!

## Credits

Built with:
- [Go](https://go.dev)
- [SortableJS](https://sortablejs.github.io/Sortable/)
- [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite)
- [coder/websocket](https://github.com/coder/websocket)
