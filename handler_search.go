package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/milinkovicluka/gator/internal/database"
)

func handlerSearch(s *state, cmd command, user database.User) error {
	usage := fmt.Errorf("usage: %s <words...> [--limit N] [--page N]", cmd.name)

	fs := flag.NewFlagSet(cmd.name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	limit := fs.Int("limit", 5, "results per page")
	page := fs.Int("page", 1, "page number")

	// Flags may come before, between or after the search words, so keep
	// parsing until every argument is used up.
	var words []string
	args := cmd.args
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return fmt.Errorf("%v\n%w", err, usage)
		}
		if fs.NArg() == 0 {
			break
		}
		words = append(words, fs.Arg(0))
		args = fs.Args()[1:]
	}

	query := strings.TrimSpace(strings.Join(words, " "))
	if query == "" {
		return usage
	}
	if *limit < 1 {
		return fmt.Errorf("limit must be at least 1")
	}
	if *page < 1 {
		return fmt.Errorf("page must be at least 1")
	}

	results, err := s.db.SearchPostsForUser(context.Background(), database.SearchPostsForUserParams{
		UserID: user.ID,
		Query:  query,
		Limit:  int32(*limit),
		Offset: int32((*page - 1) * *limit),
	})
	if err != nil {
		return fmt.Errorf("couldn't search posts: %w", err)
	}

	if len(results) == 0 {
		if *page > 1 {
			fmt.Printf("No more results for %q.\n", query)
		} else {
			fmt.Printf("No posts found matching %q.\n", query)
		}
		return nil
	}

	fmt.Printf("Results for %q (page %d):\n", query, *page)
	for _, post := range results {
		// Same columns as browse, so the row types convert directly.
		printPost(database.GetPostsForUserRow(post))
	}

	if len(results) == *limit {
		fmt.Printf("\nNext page: gator search %q --limit %d --page %d\n", query, *limit, *page+1)
	}
	return nil
}
