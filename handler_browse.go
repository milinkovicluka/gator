package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strconv"

	"github.com/milinkovicluka/gator/internal/database"
)

func handlerBrowse(s *state, cmd command, user database.User) error {
	usage := fmt.Errorf("usage: %s [limit] [--limit N] [--page N]", cmd.name)

	args := cmd.args
	limit := 2

	// Keep the old form working: `gator browse 10`
	if len(args) > 0 {
		if n, err := strconv.Atoi(args[0]); err == nil {
			limit = n
			args = args[1:]
		}
	}

	fs := flag.NewFlagSet(cmd.name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.IntVar(&limit, "limit", limit, "posts per page")
	page := fs.Int("page", 1, "page number")

	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%v\n%w", err, usage)
	}
	if fs.NArg() > 0 {
		return usage
	}
	if limit < 1 {
		return fmt.Errorf("limit must be at least 1")
	}
	if *page < 1 {
		return fmt.Errorf("page must be at least 1")
	}

	posts, err := s.db.GetPostsForUser(context.Background(), database.GetPostsForUserParams{
		UserID: user.ID,
		Limit:  int32(limit),
		Offset: int32((*page - 1) * limit),
	})
	if err != nil {
		return fmt.Errorf("couldn't get posts: %w", err)
	}

	if len(posts) == 0 {
		if *page > 1 {
			fmt.Printf("No posts on page %d.\n", *page)
		} else {
			fmt.Println("No posts found. Is `agg` running?")
		}
		return nil
	}

	fmt.Printf("Page %d for user %s:\n", *page, user.Name)
	for _, post := range posts {
		printPost(post)
	}

	if len(posts) == limit {
		fmt.Printf("\nNext page: gator browse --limit %d --page %d\n", limit, *page+1)
	}
	return nil
}

func printPost(post database.GetPostsForUserRow) {
	date := "unknown date"
	if post.PublishedAt.Valid {
		date = post.PublishedAt.Time.Format("Mon Jan 2 2006 15:04")
	}
	fmt.Printf("\n%s from %s\n", date, post.FeedName)
	fmt.Printf("--- %s ---\n", post.Title)
	if post.Description.Valid {
		fmt.Printf("    %s\n", post.Description.String)
	}
	fmt.Printf("Link: %s\n", post.Url)
	fmt.Println("=====================================")
}
