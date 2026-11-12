package main

import (
	"database/sql"
	"log"
	"os"

	"github.com/milinkovicluka/gator/internal/config"
	"github.com/milinkovicluka/gator/internal/database"

	_ "github.com/lib/pq"
)

type state struct {
	db  *database.Queries
	cfg *config.Config
}

func main() {
	cfg, err := config.Read()
	if err != nil {
		log.Fatalf("error reading config: %v", err)
	}

	db, err := sql.Open("postgres", cfg.DBURL)
	if err != nil {
		log.Fatalf("error connecting to db: %v", err)
	}
	defer db.Close()

	programState := &state{
		db:  database.New(db),
		cfg: &cfg,
	}

	cmds := commands{
		registeredCommands: make(map[string]func(*state, command) error),
	}
	cmds.register("login", handlerLogin)
	cmds.register("register", handlerRegister)
	cmds.register("reset", handlerReset)
	cmds.register("users", handlerListUsers)
	cmds.register("agg", handlerAgg)
	cmds.register("addfeed", middlewareLoggedIn(handlerAddFeed))
	cmds.register("feeds", handlerListFeeds)
	cmds.register("follow", middlewareLoggedIn(handlerFollow))
	cmds.register("following", middlewareLoggedIn(handlerListFeedFollows))
	cmds.register("unfollow", middlewareLoggedIn(handlerUnfollow))
	cmds.register("browse", middlewareLoggedIn(handlerBrowse))
	cmds.register("search", middlewareLoggedIn(handlerSearch))
	cmds.register("bookmark", middlewareLoggedIn(handlerBookmark))
	cmds.register("unbookmark", middlewareLoggedIn(handlerUnbookmark))
	cmds.register("bookmarks", middlewareLoggedIn(handlerListBookmarks))
	cmds.register("read", middlewareLoggedIn(handlerRead))
	cmds.register("service", handlerService)
	cmds.register("apikey", middlewareLoggedIn(handlerAPIKey))
	cmds.register("serve", handlerServe)

	if len(os.Args) < 2 {
		log.Fatal("Usage: cli <command> [args...]")
	}

	cmd := command{
		name: os.Args[1],
		args: os.Args[2:],
	}

	if err := cmds.run(programState, cmd); err != nil {
		log.Fatal(err)
	}
}
