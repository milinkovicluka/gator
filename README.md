# Gator 🐊

Gator is a multi-user command-line RSS feed aggregator written in Go. It lets you
add RSS feeds, follow feeds added by other users, collect posts in the background,
and browse the latest posts right in your terminal.

## Requirements

You'll need these installed:

- [Go](https://go.dev/doc/install) 1.22 or later, to install the CLI
- [PostgreSQL](https://www.postgresql.org/download/) 15 or later, to store users, feeds and posts
- [Goose](https://github.com/pressly/goose), to run the database migrations:

  ```bash
  go install github.com/pressly/goose/v3/cmd/goose@latest
  ```

## Installation

```bash
go install github.com/milinkovicluka/gator@latest
```

This builds a statically compiled `gator` binary and puts it in `$(go env GOPATH)/bin`.
Make sure that directory is on your `PATH`. After that, you don't need the Go
toolchain to run `gator`.

## Setup

### 1. Create the database

Start Postgres, then create a database:

```bash
sudo -u postgres psql -c 'CREATE DATABASE gator;'
```

Your connection string will look like this:

```
postgres://username:password@localhost:5432/gator
```

### 2. Run the migrations

The migrations live in `sql/schema` in this repo:

```bash
git clone https://github.com/milinkovicluka/gator.git
cd gator/sql/schema
goose postgres "postgres://username:password@localhost:5432/gator" up
```

### 3. Create the config file

Create `~/.gatorconfig.json` in your home directory:

```json
{
  "db_url": "postgres://username:password@localhost:5432/gator?sslmode=disable"
}
```

Note the `?sslmode=disable` at the end. Gator fills in `current_user_name` itself
when you register or log in.

## Usage

```bash
gator <command> [args...]
```

### Users

| Command | Description |
| --- | --- |
| `gator register <name>` | Create a user and log in as them |
| `gator login <name>` | Switch to an existing user |
| `gator users` | List all users and show who is logged in |

### Feeds

| Command | Description |
| --- | --- |
| `gator addfeed <name> <url>` | Add a feed and follow it as the current user |
| `gator feeds` | List all feeds and who added them |
| `gator follow <url>` | Follow a feed that someone else added |
| `gator unfollow <url>` | Unfollow a feed |
| `gator following` | List the feeds the current user follows |

### Aggregating and reading posts

| Command | Description |
| --- | --- |
| `gator agg <interval>` | Fetch feeds in a loop, one feed per interval (e.g. `30s`, `1m`). Stop it with Ctrl+C |
| `gator browse [limit]` | Show the newest posts from the feeds you follow (default 2) |

`agg` is meant to run in the background. Start it in one terminal, then use
`browse` in another. Please use a sensible interval so you don't flood the sites
you fetch from.

### Development

| Command | Description |
| --- | --- |
| `gator reset` | ⚠️ Delete **all** users, feeds and posts |

## Example

```bash
gator register alice
gator addfeed "Hacker News" "https://news.ycombinator.com/rss"
gator addfeed "TechCrunch" "https://techcrunch.com/feed/"

# terminal 1
gator agg 1m

# terminal 2
gator browse 5
```

## Development

```bash
go run . <command> [args...]
```

Queries live in `sql/queries` and are compiled to Go with [sqlc](https://sqlc.dev/):

```bash
sqlc generate
```