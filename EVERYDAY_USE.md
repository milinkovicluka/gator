# Gator: Everyday Use

A quick cheat sheet for running Gator day to day.

---

## 1. Every time you open WSL

Postgres doesn't start on its own in WSL. Start it first:

```bash
sudo service postgresql start
```

Then make sure the background collector is running:

```bash
gator service start
```

If it says `already running`, that's fine. Your users, feeds, posts and
bookmarks are always still saved.

---

## 2. Read

The easiest way is the interactive reader:

```bash
gator read
```

| Key | What it does |
| --- | --- |
| `↑` `↓` | Move through posts (or scroll while reading) |
| `enter` | Read the selected post |
| `/` | Filter, then type part of a title or feed name |
| `o` | Open the post in your Windows browser |
| `b` | Bookmark it (★), or press again to remove the bookmark |
| `esc` | Back to the list / clear the filter |
| `q` | Quit |

Only your bookmarks:

```bash
gator read --bookmarks
```

Prefer plain text output? These print and exit:

```bash
gator browse 10                    # 10 newest posts
gator browse 10 --page 2           # the next 10
gator browse 10 --feed hacker      # only one feed
gator search golang                # find posts by keyword (small typos are OK)
gator bookmarks                    # your saved posts
```

---

## 3. Is the collector working?

```bash
gator service status               # running? since when? any crashes?
gator service logs                 # what it fetched recently
gator service logs -f              # watch it live (Ctrl+C stops watching, not the service)
```

Change how often it fetches, or stop it:

```bash
gator service restart --interval 10m
gator service stop
```

---

## Coming back after a few days

```bash
sudo service postgresql start      # 1. start the database
gator service start                # 2. start collecting
gator read                         # 3. wait a few minutes, then catch up
```

Each feed only keeps its latest items (often 20-30), so on a busy feed like
Hacker News, posts from while you were away may already be gone. Keeping the
service running while WSL is open means you miss less.

Closing **every** WSL window can make Windows shut WSL down, which stops
Postgres and the service too. Just run the steps above next time.

---

## Managing feeds

```bash
gator following                         # feeds you follow
gator feeds                             # all feeds in the database
gator addfeed "Name" "https://site/rss" # add a new feed (and follow it)
gator follow "https://site/rss"         # follow a feed that already exists
gator unfollow "https://site/rss"       # stop following
```

If `addfeed` says `duplicate key`, the feed already exists: use `follow` instead.

To find a site's feed, try adding `/feed`, `/rss`, `/index.xml` or `/feed.xml` to
its address.

---

## Using the HTTP API

```bash
gator apikey                       # create your key (shown only once, so save it)
gator serve                        # start the API (leave this terminal open)
```

In another terminal (replace `gtr_YOUR_KEY` with your key):

```bash
curl -H "Authorization: Bearer gtr_YOUR_KEY" "http://localhost:8080/v1/posts?limit=5"
```

Lost your key? Run `gator apikey` again to get a new one (the old one stops working).
All endpoints are listed in the [README](README.md#http-api-1).

---

## After changing the code (or pulling a new version)

From the project folder:

```bash
cd ~/workspace/blog_aggregator
cd sql/schema && goose postgres 'postgres://postgres:postgres@localhost:5432/gator' up && cd ../..
sqlc generate
go install .
gator service restart
```

- `goose ... up` only adds new tables or indexes. It never deletes your data.
- `go install .` updates the `gator` command you run from anywhere.
- `gator service restart` makes the background collector use the new version.

---

## Where things are stored

| What | Where |
| --- | --- |
| Users, feeds, posts, bookmarks | The Postgres database `gator` |
| Settings (database URL, current user) | `~/.gatorconfig.json` |
| Service log | `~/.gator/agg.log` |
| Service PID | `~/.gator/service.pid` |

In Windows File Explorer: `\\wsl$\Ubuntu\home\lukamilinkovic\.gator`

---

## ⚠️ Don't run these (they delete everything)

| Command | What it deletes |
| --- | --- |
| `gator reset` | All users, feeds, posts and bookmarks |
| `goose ... reset` | All tables (empty again after `up`) |
| `goose ... down` | The most recent table, column or index |

---

## Troubleshooting

| You see | Fix |
| --- | --- |
| `connection refused` | Postgres isn't running: `sudo service postgresql start` |
| `gator: command not found` | Run `go install .` in the project folder, and check `~/go/bin` is on your PATH |
| `unknown command: ...` | Your installed `gator` is out of date: `go install .` |
| `No posts found. Is agg running?` | `gator service start`, then wait a few minutes |
| `couldn't get current user` | Log in: `gator login luka` |
| `No feeds to fetch yet` in the service log | Add some feeds with `addfeed` |
| `relation "..." does not exist` | A migration hasn't been run yet: run the `goose ... up` command above |
| `gator service is not running` | Start it: `gator service start` |
| `o` in `gator read` doesn't open a browser | Copy the link and run `explorer.exe "LINK"` |
| API says `401 invalid API key` | Check the `Authorization: Bearer gtr_...` header, or make a new key with `gator apikey` |
| `bind: address already in use` from `gator serve` | It's already running in another terminal, or use `gator serve --addr 127.0.0.1:8090` |
