package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/milinkovicluka/gator/internal/database"
)

func handlerBookmark(s *state, cmd command, user database.User) error {
	if len(cmd.args) != 1 {
		return fmt.Errorf("usage: %s <post_url>", cmd.name)
	}

	post, err := getPostByURL(s, cmd.args[0])
	if err != nil {
		return err
	}

	added, err := s.db.CreateBookmark(context.Background(), database.CreateBookmarkParams{
		ID:        uuid.New(),
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		UserID:    user.ID,
		PostID:    post.ID,
	})
	if err != nil {
		return fmt.Errorf("couldn't bookmark post: %w", err)
	}

	if added == 0 {
		fmt.Printf("Already bookmarked: %s\n", post.Title)
		return nil
	}
	fmt.Printf("Bookmarked: %s\n", post.Title)
	return nil
}

func handlerUnbookmark(s *state, cmd command, user database.User) error {
	if len(cmd.args) != 1 {
		return fmt.Errorf("usage: %s <post_url>", cmd.name)
	}

	post, err := getPostByURL(s, cmd.args[0])
	if err != nil {
		return err
	}

	removed, err := s.db.DeleteBookmark(context.Background(), database.DeleteBookmarkParams{
		UserID: user.ID,
		PostID: post.ID,
	})
	if err != nil {
		return fmt.Errorf("couldn't remove bookmark: %w", err)
	}

	if removed == 0 {
		return fmt.Errorf("post is not bookmarked: %s", post.Title)
	}
	fmt.Printf("Removed bookmark: %s\n", post.Title)
	return nil
}

func handlerListBookmarks(s *state, cmd command, user database.User) error {
	fs := flag.NewFlagSet(cmd.name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	limit := fs.Int("limit", 10, "bookmarks per page")
	page := fs.Int("page", 1, "page number")

	if err := fs.Parse(cmd.args); err != nil || fs.NArg() > 0 {
		return fmt.Errorf("usage: %s [--limit N] [--page N]", cmd.name)
	}
	if *limit < 1 {
		return fmt.Errorf("limit must be at least 1")
	}
	if *page < 1 {
		return fmt.Errorf("page must be at least 1")
	}

	bookmarks, err := s.db.GetBookmarksForUser(context.Background(), database.GetBookmarksForUserParams{
		UserID: user.ID,
		Limit:  int32(*limit),
		Offset: int32((*page - 1) * *limit),
	})
	if err != nil {
		return fmt.Errorf("couldn't get bookmarks: %w", err)
	}

	if len(bookmarks) == 0 {
		if *page > 1 {
			fmt.Printf("No bookmarks on page %d.\n", *page)
		} else {
			fmt.Println("No bookmarks yet. Add one with: gator bookmark <post_url>")
		}
		return nil
	}

	fmt.Printf("Bookmarks for %s (page %d):\n", user.Name, *page)
	for _, post := range bookmarks {
		// Same columns as browse, so the row types convert directly.
		printPost(database.GetPostsForUserRow(post))
	}

	if len(bookmarks) == *limit {
		fmt.Printf("\nNext page: gator bookmarks --limit %d --page %d\n", *limit, *page+1)
	}
	return nil
}

func getPostByURL(s *state, url string) (database.Post, error) {
	post, err := s.db.GetPostByURL(context.Background(), url)
	if errors.Is(err, sql.ErrNoRows) {
		return database.Post{}, fmt.Errorf("no post with URL %s (copy the Link from `gator browse`)", url)
	}
	if err != nil {
		return database.Post{}, fmt.Errorf("couldn't get post: %w", err)
	}
	return post, nil
}
