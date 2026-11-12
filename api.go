package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/milinkovicluka/gator/internal/database"
)

// handlerServe runs the HTTP API until Ctrl+C.
func handlerServe(s *state, cmd command) error {
	fs := flag.NewFlagSet(cmd.name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	addr := fs.String("addr", "127.0.0.1:8080", "address to listen on")
	if err := fs.Parse(cmd.args); err != nil || fs.NArg() > 0 {
		return fmt.Errorf("usage: %s [--addr 127.0.0.1:8080]", cmd.name)
	}

	api := &apiServer{db: s.db}
	srv := &http.Server{
		Addr:              *addr,
		Handler:           logRequests(api.routes()),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       time.Minute,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	fmt.Printf("Gator API listening on http://%s (Ctrl+C to stop)\n", *addr)

	select {
	case err := <-errCh:
		return fmt.Errorf("server error: %w", err)
	case <-ctx.Done():
	}

	// Let requests that are in progress finish, for up to 5 seconds.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("couldn't shut down cleanly: %w", err)
	}
	fmt.Println("\nServer stopped.")
	return nil
}

type apiServer struct {
	db *database.Queries
}

func (a *apiServer) routes() http.Handler {
	mux := http.NewServeMux()

	// Public
	mux.HandleFunc("GET /v1/healthz", a.handleHealthz)
	mux.HandleFunc("POST /v1/users", a.handleCreateUser)
	mux.HandleFunc("GET /v1/feeds", a.handleListFeeds)

	// Need an API key
	mux.HandleFunc("GET /v1/users/me", a.auth(a.handleGetMe))
	mux.HandleFunc("POST /v1/feeds", a.auth(a.handleCreateFeed))
	mux.HandleFunc("GET /v1/feed_follows", a.auth(a.handleListFollows))
	mux.HandleFunc("POST /v1/feed_follows", a.auth(a.handleFollow))
	mux.HandleFunc("DELETE /v1/feed_follows/{feedID}", a.auth(a.handleUnfollow))
	mux.HandleFunc("GET /v1/posts", a.auth(a.handleListPosts))
	mux.HandleFunc("GET /v1/posts/search", a.auth(a.handleSearchPosts))
	mux.HandleFunc("GET /v1/bookmarks", a.auth(a.handleListBookmarks))
	mux.HandleFunc("POST /v1/bookmarks", a.auth(a.handleBookmark))
	mux.HandleFunc("DELETE /v1/bookmarks/{postID}", a.auth(a.handleUnbookmark))

	return mux
}

// ----- authentication -----

type authedHandler func(w http.ResponseWriter, r *http.Request, user database.User)

// auth is the HTTP version of middlewareLoggedIn: it looks up the user from
// the "Authorization: Bearer <api key>" header and passes it to the handler.
func (a *apiServer) auth(next authedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || strings.TrimSpace(key) == "" {
			respondError(w, http.StatusUnauthorized, "missing API key: send 'Authorization: Bearer <key>'")
			return
		}
		user, err := a.db.GetUserByAPIKeyHash(r.Context(), sql.NullString{
			String: hashAPIKey(strings.TrimSpace(key)), Valid: true,
		})
		if errors.Is(err, sql.ErrNoRows) {
			respondError(w, http.StatusUnauthorized, "invalid API key")
			return
		}
		if err != nil {
			serverError(w, err)
			return
		}
		next(w, r, user)
	}
}

// ----- JSON shapes (never expose api_key_hash) -----

type userJSON struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type feedJSON struct {
	ID            uuid.UUID  `json:"id"`
	Name          string     `json:"name"`
	URL           string     `json:"url"`
	CreatedAt     time.Time  `json:"created_at"`
	LastFetchedAt *time.Time `json:"last_fetched_at"`
}

type postJSON struct {
	ID          uuid.UUID  `json:"id"`
	Title       string     `json:"title"`
	URL         string     `json:"url"`
	Description string     `json:"description,omitempty"`
	PublishedAt *time.Time `json:"published_at"`
	FeedID      uuid.UUID  `json:"feed_id"`
	FeedName    string     `json:"feed_name"`
}

func toUserJSON(u database.User) userJSON {
	return userJSON{ID: u.ID, Name: u.Name, CreatedAt: u.CreatedAt}
}

func toFeedJSON(f database.Feed) feedJSON {
	return feedJSON{ID: f.ID, Name: f.Name, URL: f.Url, CreatedAt: f.CreatedAt, LastFetchedAt: nullTime(f.LastFetchedAt)}
}

func toPostJSON(p database.GetPostsForUserRow) postJSON {
	return postJSON{
		ID: p.ID, Title: p.Title, URL: p.Url, Description: p.Description.String,
		PublishedAt: nullTime(p.PublishedAt), FeedID: p.FeedID, FeedName: p.FeedName,
	}
}

func nullTime(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}

// ----- handlers -----

func (a *apiServer) handleHealthz(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *apiServer) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		respondError(w, http.StatusBadRequest, "name is required")
		return
	}

	user, err := a.db.CreateUser(r.Context(), database.CreateUserParams{
		ID: uuid.New(), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(), Name: body.Name,
	})
	if isUniqueViolation(err) {
		respondError(w, http.StatusConflict, "a user with that name already exists")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	key, err := newAPIKey(r.Context(), a.db, user.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	respondJSON(w, http.StatusCreated, map[string]any{"user": toUserJSON(user), "api_key": key})
}

func (a *apiServer) handleGetMe(w http.ResponseWriter, r *http.Request, user database.User) {
	respondJSON(w, http.StatusOK, toUserJSON(user))
}

func (a *apiServer) handleListFeeds(w http.ResponseWriter, r *http.Request) {
	feeds, err := a.db.GetFeeds(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	type row struct {
		Name    string `json:"name"`
		URL     string `json:"url"`
		AddedBy string `json:"added_by"`
	}
	out := make([]row, 0, len(feeds))
	for _, f := range feeds {
		out = append(out, row{Name: f.Name, URL: f.Url, AddedBy: f.UserName})
	}
	respondJSON(w, http.StatusOK, out)
}

func (a *apiServer) handleCreateFeed(w http.ResponseWriter, r *http.Request, user database.User) {
	var body struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Name == "" || !strings.HasPrefix(body.URL, "http") {
		respondError(w, http.StatusBadRequest, "name and a http(s) url are required")
		return
	}

	feed, err := a.db.CreateFeed(r.Context(), database.CreateFeedParams{
		ID: uuid.New(), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		Name: body.Name, Url: body.URL, UserID: user.ID,
	})
	if isUniqueViolation(err) {
		respondError(w, http.StatusConflict, "that feed already exists: follow it with POST /v1/feed_follows")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	if _, err := a.db.CreateFeedFollow(r.Context(), database.CreateFeedFollowParams{
		ID: uuid.New(), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		UserID: user.ID, FeedID: feed.ID,
	}); err != nil {
		serverError(w, err)
		return
	}
	respondJSON(w, http.StatusCreated, toFeedJSON(feed))
}

func (a *apiServer) handleListFollows(w http.ResponseWriter, r *http.Request, user database.User) {
	follows, err := a.db.GetFeedFollowsForUser(r.Context(), user.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	type row struct {
		FeedID     uuid.UUID `json:"feed_id"`
		FeedName   string    `json:"feed_name"`
		FollowedAt time.Time `json:"followed_at"`
	}
	out := make([]row, 0, len(follows))
	for _, f := range follows {
		out = append(out, row{FeedID: f.FeedID, FeedName: f.FeedName, FollowedAt: f.CreatedAt})
	}
	respondJSON(w, http.StatusOK, out)
}

func (a *apiServer) handleFollow(w http.ResponseWriter, r *http.Request, user database.User) {
	var body struct {
		FeedURL string `json:"feed_url"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	feed, err := a.db.GetFeedByURL(r.Context(), body.FeedURL)
	if errors.Is(err, sql.ErrNoRows) {
		respondError(w, http.StatusNotFound, "no feed with that url: add it with POST /v1/feeds")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	_, err = a.db.CreateFeedFollow(r.Context(), database.CreateFeedFollowParams{
		ID: uuid.New(), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		UserID: user.ID, FeedID: feed.ID,
	})
	if isUniqueViolation(err) {
		respondError(w, http.StatusConflict, "you already follow that feed")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	respondJSON(w, http.StatusCreated, toFeedJSON(feed))
}

func (a *apiServer) handleUnfollow(w http.ResponseWriter, r *http.Request, user database.User) {
	feedID, ok := pathUUID(w, r, "feedID")
	if !ok {
		return
	}
	// DeleteFeedFollow is :exec, so we can't tell "not following" apart;
	// deleting something that isn't there is fine for DELETE.
	if err := a.db.DeleteFeedFollow(r.Context(), database.DeleteFeedFollowParams{
		UserID: user.ID, FeedID: feedID,
	}); err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *apiServer) handleListPosts(w http.ResponseWriter, r *http.Request, user database.User) {
	limit, offset, ok := pagination(w, r)
	if !ok {
		return
	}
	sort := r.URL.Query().Get("sort")
	if sort == "" {
		sort = "newest"
	}
	if sort != "newest" && sort != "oldest" {
		respondError(w, http.StatusBadRequest, "sort must be 'newest' or 'oldest'")
		return
	}

	rows, err := a.db.GetPostsForUser(r.Context(), database.GetPostsForUserParams{
		UserID: user.ID, FeedName: r.URL.Query().Get("feed"), SortOrder: sort,
		Limit: limit, Offset: offset,
	})
	if err != nil {
		serverError(w, err)
		return
	}
	out := make([]postJSON, 0, len(rows))
	for _, p := range rows {
		out = append(out, toPostJSON(p))
	}
	respondJSON(w, http.StatusOK, out)
}

func (a *apiServer) handleSearchPosts(w http.ResponseWriter, r *http.Request, user database.User) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		respondError(w, http.StatusBadRequest, "q is required, e.g. /v1/posts/search?q=golang")
		return
	}
	limit, offset, ok := pagination(w, r)
	if !ok {
		return
	}
	rows, err := a.db.SearchPostsForUser(r.Context(), database.SearchPostsForUserParams{
		UserID: user.ID, Query: q, Limit: limit, Offset: offset,
	})
	if err != nil {
		serverError(w, err)
		return
	}
	out := make([]postJSON, 0, len(rows))
	for _, p := range rows {
		out = append(out, toPostJSON(database.GetPostsForUserRow(p)))
	}
	respondJSON(w, http.StatusOK, out)
}

func (a *apiServer) handleListBookmarks(w http.ResponseWriter, r *http.Request, user database.User) {
	limit, offset, ok := pagination(w, r)
	if !ok {
		return
	}
	rows, err := a.db.GetBookmarksForUser(r.Context(), database.GetBookmarksForUserParams{
		UserID: user.ID, Limit: limit, Offset: offset,
	})
	if err != nil {
		serverError(w, err)
		return
	}
	out := make([]postJSON, 0, len(rows))
	for _, p := range rows {
		out = append(out, toPostJSON(database.GetPostsForUserRow(p)))
	}
	respondJSON(w, http.StatusOK, out)
}

func (a *apiServer) handleBookmark(w http.ResponseWriter, r *http.Request, user database.User) {
	var body struct {
		PostID uuid.UUID `json:"post_id"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	added, err := a.db.CreateBookmark(r.Context(), database.CreateBookmarkParams{
		ID: uuid.New(), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		UserID: user.ID, PostID: body.PostID,
	})
	if isForeignKeyViolation(err) {
		respondError(w, http.StatusNotFound, "no post with that id")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	if added == 0 {
		respondJSON(w, http.StatusOK, map[string]string{"status": "already bookmarked"})
		return
	}
	respondJSON(w, http.StatusCreated, map[string]string{"status": "bookmarked"})
}

func (a *apiServer) handleUnbookmark(w http.ResponseWriter, r *http.Request, user database.User) {
	postID, ok := pathUUID(w, r, "postID")
	if !ok {
		return
	}
	removed, err := a.db.DeleteBookmark(r.Context(), database.DeleteBookmarkParams{
		UserID: user.ID, PostID: postID,
	})
	if err != nil {
		serverError(w, err)
		return
	}
	if removed == 0 {
		respondError(w, http.StatusNotFound, "that post isn't bookmarked")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ----- helpers -----

func respondJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false) // send "<p>" as-is, not "<p>"
	_ = enc.Encode(v)
}

func respondError(w http.ResponseWriter, status int, msg string) {
	respondJSON(w, status, map[string]string{"error": msg})
}

// serverError logs the real error but only tells the client "internal error".
func serverError(w http.ResponseWriter, err error) {
	log.Printf("internal error: %v", err)
	respondError(w, http.StatusInternalServerError, "internal server error")
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB is plenty
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		respondError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

func pathUUID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue(name))
	if err != nil {
		respondError(w, http.StatusBadRequest, name+" must be a UUID")
		return uuid.Nil, false
	}
	return id, true
}

// pagination reads ?limit= (default 20, max 100) and ?page= (default 1).
func pagination(w http.ResponseWriter, r *http.Request) (limit, offset int32, ok bool) {
	l, p := 20, 1
	var err error
	if v := r.URL.Query().Get("limit"); v != "" {
		if l, err = strconv.Atoi(v); err != nil || l < 1 || l > 100 {
			respondError(w, http.StatusBadRequest, "limit must be between 1 and 100")
			return 0, 0, false
		}
	}
	if v := r.URL.Query().Get("page"); v != "" {
		if p, err = strconv.Atoi(v); err != nil || p < 1 {
			respondError(w, http.StatusBadRequest, "page must be at least 1")
			return 0, 0, false
		}
	}
	return int32(l), int32((p - 1) * l), true
}

func isForeignKeyViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23503"
}

// statusRecorder remembers the status code so we can log it.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Printf("%s %s %d (%s)", r.Method, r.URL.Path, rec.status, time.Since(start).Round(time.Millisecond))
	})
}
