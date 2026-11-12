# Gator 🐊

Gator is a multi-user RSS feed aggregator written in Go. Add the feeds you like,
let Gator collect new posts in the background, then read, search and bookmark
them from your terminal or through a JSON HTTP API.

> 📖 Day-to-day commands: see [EVERYDAY_USE.md](EVERYDAY_USE.md)

## Features

- **Multi-user:** each user follows their own feeds and keeps their own bookmarks
- **Background collector:** fetches several feeds at once and restarts itself if it crashes
- **Interactive reader:** a full-screen terminal UI to browse, read and bookmark posts
- **Fuzzy search:** finds posts even when you make a small typo
- **Pagination, sorting and filtering** on every list
- **JSON HTTP API** with API-key authentication

## Requirements

- [Go](https://go.dev/doc/install) 1.27 or later, to install the CLI
- [PostgreSQL](https://www.postgresql.org/download/) 15 or later, with the
  `postgresql-contrib` package (it provides the `pg_trgm` extension used by search)
- [Goose](https://github.com/pressly/goose), to run the database migrations:

  ```bash
  go install github.com/pressly/goose/v3/cmd/goose@latest
  ```

The background service (`gator service`) works on Linux, macOS and WSL. Everything
else also works on Windows.

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

If this fails with `permission denied to create extension "pg_trgm"`, run this once
as the Postgres admin, then run the migrations again:

```bash
sudo -u postgres psql gator -c 'CREATE EXTENSION IF NOT EXISTS pg_trgm;'
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

## Quick start

```bash
gator register alice
gator addfeed "Hacker News" "https://news.ycombinator.com/rss"
gator addfeed "TechCrunch" "https://techcrunch.com/feed/"

gator service start      # collect posts in the background
gator read               # read them in the interactive reader
```

## Commands

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
| `gator addfeed <name> <url>` | Add a feed and follow it |
| `gator feeds` | List all feeds and who added them |
| `gator follow <url>` | Follow a feed someone else added |
| `gator unfollow <url>` | Stop following a feed |
| `gator following` | List the feeds you follow |

### Reading

| Command | Description |
| --- | --- |
| `gator read [--bookmarks] [--limit N]` | Interactive reader (see the keys below) |
| `gator browse [limit] [--page N] [--sort newest\|oldest] [--feed NAME]` | Print posts from feeds you follow (default 2 per page). `--feed` matches part of a feed name, ignoring case |
| `gator search <words> [--limit N] [--page N]` | Search titles and summaries; handles small typos (default 5 per page) |

Keys in `gator read`:

| Key | In the list | While reading a post |
| --- | --- | --- |
| `↑` `↓` / `j` `k` | Move | Scroll |
| `enter` | Read the selected post | |
| `/` | Filter by title or feed name | |
| `o` | Open in your browser | Open in your browser |
| `b` | Bookmark / remove bookmark (★) | Bookmark / remove bookmark |
| `esc` | Clear the filter | Back to the list |
| `q` | Quit | Quit |

### Bookmarks

| Command | Description |
| --- | --- |
| `gator bookmark <post_url>` | Save a post (copy the link from `browse` or `search`) |
| `gator unbookmark <post_url>` | Remove a saved post |
| `gator bookmarks [--limit N] [--page N]` | List your saved posts, most recently saved first (default 10 per page) |

### Collecting posts

| Command | Description |
| --- | --- |
| `gator service start [--interval 1m] [--feeds 3]` | Collect posts in the background, and restart the collector if it crashes |
| `gator service status` | Is it running, for how long, and how many restarts? |
| `gator service logs [--lines N] [-f]` | Show the log (`-f` to follow it live) |
| `gator service restart [--interval 1m] [--feeds 3]` | Restart, for example after updating Gator or to change settings |
| `gator service stop` | Stop collecting |
| `gator agg <interval> [--feeds N]` | Collect in the foreground instead: up to N feeds at the same time (default 3) every interval (min `1s`). Stop with Ctrl+C |

The service keeps its PID file and log in `~/.gator/`. Please use a sensible
interval (a few minutes is plenty) so you don't flood the sites you fetch from.

### HTTP API

| Command | Description |
| --- | --- |
| `gator apikey` | Create (or replace) your API key. It's shown once; only a hash is stored |
| `gator serve [--addr 127.0.0.1:8080]` | Run the JSON HTTP API (Ctrl+C to stop) |

See [HTTP API](#http-api-1) below for the endpoints.

### Danger zone

| Command | Description |
| --- | --- |
| `gator reset` | ⚠️ Delete **all** users, feeds, posts and bookmarks |

## HTTP API

Start the server and create a key:

```bash
gator apikey
gator serve
```

Send the key in the `Authorization` header:

```bash
curl -H "Authorization: Bearer gtr_YOUR_KEY" "http://localhost:8080/v1/posts?limit=5"
```

| Method & path | Key? | Description |
| --- | --- | --- |
| `GET /v1/healthz` | | Health check |
| `POST /v1/users` `{"name": "ana"}` | | Create a user; the response includes their API key (shown once) |
| `GET /v1/feeds` | | All feeds and who added them |
| `GET /v1/users/me` | ✅ | The user this key belongs to |
| `POST /v1/feeds` `{"name": "...", "url": "..."}` | ✅ | Add a feed and follow it |
| `GET /v1/feed_follows` | ✅ | Feeds you follow |
| `POST /v1/feed_follows` `{"feed_url": "..."}` | ✅ | Follow a feed |
| `DELETE /v1/feed_follows/{feedID}` | ✅ | Unfollow a feed |
| `GET /v1/posts?limit=&page=&sort=&feed=` | ✅ | Posts from feeds you follow |
| `GET /v1/posts/search?q=&limit=&page=` | ✅ | Search posts |
| `GET /v1/bookmarks?limit=&page=` | ✅ | Your bookmarks |
| `POST /v1/bookmarks` `{"post_id": "..."}` | ✅ | Bookmark a post |
| `DELETE /v1/bookmarks/{postID}` | ✅ | Remove a bookmark |

- `limit` defaults to 20 (max 100), `page` to 1 and `sort` to `newest`.
- Errors come back as `{"error": "..."}` with a matching status code
  (`400`, `401`, `404`, `409`, ...).
- By default the server only listens on `127.0.0.1`, so only your own computer can
  reach it. Use `--addr 0.0.0.0:8080` to allow other devices, but only on a network
  you trust: it's plain HTTP, so keys are sent unencrypted.

## Development

```bash
go run . <command> [args...]
```

Queries live in `sql/queries` and are compiled to Go with [sqlc](https://sqlc.dev/):

```bash
sqlc generate
```

New tables and indexes go in a new numbered file in `sql/schema`, applied with
`goose ... up`.
