Gator: Everyday Use
A quick cheat sheet for running Gator day to day.
---
1. Every time you open WSL
Postgres doesn't start on its own in WSL. Start it first:
```bash
sudo service postgresql start
```
That's it. Your users, feeds, posts and bookmarks are all still saved.
---
2. Collect new posts
Open terminal 1 and leave it running:
```bash
gator agg 1m
```
It fetches one feed per minute and loops through all of them.
Press Ctrl+C to stop it when you're done.
3. Read
In terminal 2:
```bash
gator browse 10                    # 10 newest posts
gator browse 10 --page 2           # the next 10
gator browse 10 --feed hacker      # only one feed
gator search golang                # find posts by keyword (small typos are OK)
```
Open a post in your Windows browser:
```bash
explorer.exe "PASTE_LINK_HERE"
```
4. Save posts for later
```bash
gator bookmark "PASTE_LINK_HERE"
gator bookmarks
gator unbookmark "PASTE_LINK_HERE"
```
---
Coming back after a few days
```bash
sudo service postgresql start      # 1. start the database
gator agg 1m                       # 2. terminal 1: let it run 5-10 minutes
gator browse 20                    # 3. terminal 2: catch up on what's new
```
Each feed only keeps its latest items (often 20-30), so on a busy feed like
Hacker News, posts from while you were away may already be gone. To miss less,
run `agg` more often.
---
Managing feeds
```bash
gator following                         # feeds you follow
gator feeds                             # all feeds in the database
gator addfeed "Name" "https://site/rss" # add a new feed (and follow it)
gator follow "https://site/rss"         # follow a feed that already exists
gator unfollow "https://site/rss"       # stop following
```
If `addfeed` says `duplicate key`, the feed already exists: use `follow` instead.
---
After changing the code (or pulling a new version)
From the project folder:
```bash
cd ~/workspace/blog_aggregator
cd sql/schema && goose postgres 'postgres://postgres:postgres@localhost:5432/gator' up && cd ../..
sqlc generate
go install .
```
`goose ... up` only adds new tables or indexes. It never deletes your data.
`go install .` updates the `gator` command you run from anywhere.
---
⚠️ Don't run these (they delete everything)
Command	What it deletes
`gator reset`	All users, feeds, posts and bookmarks
`goose ... reset`	All tables (empty again after `up`)
`goose ... down`	The most recent table or index
---
Troubleshooting
You see	Fix
`connection refused`	Postgres isn't running: `sudo service postgresql start`
`gator: command not found`	Run `go install .` in the project folder, and check `~/go/bin` is on your PATH
`unknown command: ...`	Your installed `gator` is out of date: `go install .`
`No posts found. Is agg running?`	Run `gator agg 1m` for a few minutes
`couldn't get current user`	Log in: `gator login luka`
`agg`: `no rows in result set`	There are no feeds yet: add some with `addfeed`
`relation "..." does not exist`	A migration hasn't been run yet: run the `goose ... up` command above
