package main

import (
	"context"
	"fmt"
	"strconv"

	"github.com/lukamilinkovic/gator/internal/database"
)

func handlerBrowse(s *state, cmd command, user database.User) error {
	limit := 2
	if len(cmd.args) == 1 {
		parsed, err := strconv.Atoi(cmd.args[0])
		if err != nil || parsed < 1 {
			return fmt.Errorf("invalid limit: %s", cmd.args[0])
		}
		limit = parsed
	} else if len(cmd.args) > 1 {
		return fmt.Errorf("usage: %s [limit]", cmd.name)
	}

	posts, err := s.db.GetPostsForUser(context.Background(), database.GetPostsForUserParams{
		UserID: user.ID,
		Limit:  int32(limit),
	})
	if err != nil {
		return fmt.Errorf("couldn't get posts: %w", err)
	}

	if len(posts) == 0 {
		fmt.Println("No posts found. Is `agg` running?")
		return nil
	}

	fmt.Printf("Found %d posts for user %s:\n", len(posts), user.Name)
	for _, post := range posts {
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
	return nil
}
