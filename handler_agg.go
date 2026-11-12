package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/milinkovicluka/gator/internal/database"
)

func handlerAgg(s *state, cmd command) error {
	usage := fmt.Errorf("usage: %s <time_between_reqs> [--feeds N]", cmd.name)
	if len(cmd.args) < 1 {
		return usage
	}

	timeBetweenRequests, err := time.ParseDuration(cmd.args[0])
	if err != nil {
		return fmt.Errorf("invalid duration: %w", err)
	}
	if timeBetweenRequests < time.Second {
		return fmt.Errorf("time_between_reqs must be at least 1s, so we don't flood the sites we fetch from")
	}

	fs := flag.NewFlagSet(cmd.name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	feedsPerTick := fs.Int("feeds", 3, "how many feeds to fetch at the same time")
	if err := fs.Parse(cmd.args[1:]); err != nil || fs.NArg() > 0 {
		return usage
	}
	if *feedsPerTick < 1 || *feedsPerTick > 20 {
		return fmt.Errorf("--feeds must be between 1 and 20")
	}

	// Ctrl+C cancels ctx: in-flight requests stop and agg exits cleanly.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Printf("Collecting up to %d feeds every %s (Ctrl+C to stop)\n", *feedsPerTick, timeBetweenRequests)

	ticker := time.NewTicker(timeBetweenRequests)
	defer ticker.Stop()
	for {
		scrapeFeeds(ctx, s, *feedsPerTick)

		select {
		case <-ctx.Done():
			fmt.Println("\nStopped collecting feeds.")
			return nil
		case <-ticker.C:
		}
	}
}

// scrapeFeeds fetches the next batch of feeds at the same time,
// one goroutine per feed, and waits for all of them to finish.
func scrapeFeeds(ctx context.Context, s *state, limit int) {
	feeds, err := s.db.GetNextFeedsToFetch(ctx, int32(limit))
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("couldn't get feeds to fetch: %v", err)
		}
		return
	}
	if len(feeds) == 0 {
		log.Println("No feeds to fetch yet. Add one with: gator addfeed <name> <url>")
		return
	}

	var wg sync.WaitGroup
	for _, feed := range feeds {
		wg.Add(1)
		go func(feed database.Feed) {
			defer wg.Done()
			scrapeFeed(ctx, s, feed)
		}(feed)
	}
	wg.Wait()
}

func scrapeFeed(ctx context.Context, s *state, feed database.Feed) {
	start := time.Now()

	rssFeed, err := fetchFeed(ctx, feed.Url)
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("%-20s failed: %v", feed.Name, err)
		}
		return
	}

	saved := 0
	for _, item := range rssFeed.Channel.Item {
		_, err := s.db.CreatePost(ctx, database.CreatePostParams{
			ID:        uuid.New(),
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
			Title:     item.Title,
			Url:       item.Link,
			Description: sql.NullString{
				String: item.Description,
				Valid:  item.Description != "",
			},
			PublishedAt: parsePubDate(item.PubDate),
			FeedID:      feed.ID,
		})
		if err != nil {
			if isUniqueViolation(err) {
				continue
			}
			if ctx.Err() != nil {
				return
			}
			log.Printf("%-20s couldn't save post %q: %v", feed.Name, item.Title, err)
			continue
		}
		saved++
	}

	log.Printf("%-20s %3d items, %3d new  (%s)",
		feed.Name, len(rssFeed.Channel.Item), saved, time.Since(start).Round(time.Millisecond))
}

func isUniqueViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23505"
}

var pubDateLayouts = []string{
	time.RFC1123Z,
	time.RFC1123,
	time.RFC3339,
	time.RFC822Z,
	time.RFC822,
	"Mon, 2 Jan 2006 15:04:05 -0700",
	"Mon, 2 Jan 2006 15:04:05 MST",
	"2006-01-02 15:04:05",
}

func parsePubDate(s string) sql.NullTime {
	for _, layout := range pubDateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return sql.NullTime{Time: t.UTC(), Valid: true}
		}
	}
	return sql.NullTime{}
}
