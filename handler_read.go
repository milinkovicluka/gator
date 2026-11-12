package main

import (
	"context"
	"flag"
	"fmt"
	"html"
	"io"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
	"github.com/milinkovicluka/gator/internal/database"
)

// handlerRead opens an interactive reader in the terminal.
func handlerRead(s *state, cmd command, user database.User) error {
	fs := flag.NewFlagSet(cmd.name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	limit := fs.Int("limit", 200, "how many posts to load")
	onlyBookmarks := fs.Bool("bookmarks", false, "only show bookmarked posts")
	if err := fs.Parse(cmd.args); err != nil || fs.NArg() > 0 {
		return fmt.Errorf("usage: %s [--limit N] [--bookmarks]", cmd.name)
	}
	if *limit < 1 {
		return fmt.Errorf("limit must be at least 1")
	}

	ctx := context.Background()
	var posts []database.GetPostsForUserRow
	if *onlyBookmarks {
		rows, err := s.db.GetBookmarksForUser(ctx, database.GetBookmarksForUserParams{
			UserID: user.ID, Limit: int32(*limit),
		})
		if err != nil {
			return fmt.Errorf("couldn't get bookmarks: %w", err)
		}
		for _, r := range rows {
			posts = append(posts, database.GetPostsForUserRow(r))
		}
	} else {
		var err error
		posts, err = s.db.GetPostsForUser(ctx, database.GetPostsForUserParams{
			UserID: user.ID, SortOrder: "newest", Limit: int32(*limit),
		})
		if err != nil {
			return fmt.Errorf("couldn't get posts: %w", err)
		}
	}

	if len(posts) == 0 {
		if *onlyBookmarks {
			fmt.Println("No bookmarks yet. Press b on a post in `gator read` to save it.")
		} else {
			fmt.Println("No posts found. Is `agg` running?")
		}
		return nil
	}

	bookmarkedIDs, err := s.db.GetBookmarkedPostIDs(ctx, user.ID)
	if err != nil {
		return fmt.Errorf("couldn't get bookmarks: %w", err)
	}
	bookmarked := make(map[uuid.UUID]bool, len(bookmarkedIDs))
	for _, id := range bookmarkedIDs {
		bookmarked[id] = true
	}

	items := make([]list.Item, len(posts))
	for i, p := range posts {
		items[i] = postItem{post: p, bookmarked: bookmarked[p.ID]}
	}

	title := fmt.Sprintf("Gator · %s · posts (%d)", user.Name, len(posts))
	if *onlyBookmarks {
		title = fmt.Sprintf("Gator · %s · bookmarks (%d)", user.Name, len(posts))
	}

	_, err = tea.NewProgram(newReadModel(s, user, items, title), tea.WithAltScreen()).Run()
	return err
}

// ----- list items -----

type postItem struct {
	post       database.GetPostsForUserRow
	bookmarked bool
}

func (p postItem) Title() string {
	if p.bookmarked {
		return "★ " + p.post.Title
	}
	return p.post.Title
}

func (p postItem) Description() string {
	return p.post.FeedName + " · " + formatPostDate(p.post)
}

// FilterValue is what `/` searches in.
func (p postItem) FilterValue() string { return p.post.Title + " " + p.post.FeedName }

// ----- messages from background work -----

type bookmarkToggledMsg struct {
	index      int
	bookmarked bool
	err        error
}

type browserOpenedMsg struct{ err error }

// ----- model -----

type readModel struct {
	s        *state
	user     database.User
	list     list.Model
	viewport viewport.Model
	reading  bool // true = showing one post, false = showing the list
	width    int
	height   int
}

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#04B575"))
	metaStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	linkStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#5DA9E9")).Underline(true)
	footerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#626262"))
)

func newReadModel(s *state, user database.User, items []list.Item, title string) readModel {
	l := list.New(items, list.NewDefaultDelegate(), 0, 0)
	l.Title = title
	l.AdditionalShortHelpKeys = listHelpKeys
	l.AdditionalFullHelpKeys = listHelpKeys
	return readModel{s: s, user: user, list: l, viewport: viewport.New(0, 0)}
}

func (m readModel) Init() tea.Cmd { return nil }

func (m readModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.list.SetSize(msg.Width, msg.Height)
		m.viewport.Width = msg.Width
		m.viewport.Height = msg.Height - 2 // room for the footer
		if m.reading {
			m.viewport.SetContent(m.renderPost())
		}
		return m, nil

	case bookmarkToggledMsg:
		if msg.err != nil {
			return m, m.list.NewStatusMessage("Error: " + msg.err.Error())
		}
		item := m.list.Items()[msg.index].(postItem)
		item.bookmarked = msg.bookmarked
		cmd := m.list.SetItem(msg.index, item)
		if m.reading {
			m.viewport.SetContent(m.renderPost())
		}
		status := "Removed bookmark"
		if msg.bookmarked {
			status = "Bookmarked ★"
		}
		return m, tea.Batch(cmd, m.list.NewStatusMessage(status))

	case browserOpenedMsg:
		if msg.err != nil {
			return m, m.list.NewStatusMessage("Couldn't open browser: " + msg.err.Error())
		}
		return m, m.list.NewStatusMessage("Opened in browser")

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.reading {
			return m.updateReading(msg)
		}
		// While typing a filter, every key goes to the filter box.
		if m.list.FilterState() != list.Filtering {
			switch msg.String() {
			case "enter":
				if _, ok := m.selected(); ok {
					m.reading = true
					m.viewport.SetContent(m.renderPost())
					m.viewport.GotoTop()
				}
				return m, nil
			case "o":
				return m, m.openSelected()
			case "b":
				return m, m.toggleBookmark()
			}
		}
	}

	if m.reading {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m readModel) updateReading(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "backspace", "left", "h":
		m.reading = false
		return m, nil
	case "q":
		return m, tea.Quit
	case "o":
		return m, m.openSelected()
	case "b":
		return m, m.toggleBookmark()
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m readModel) View() string {
	if !m.reading {
		return m.list.View()
	}
	footer := footerStyle.Render(fmt.Sprintf(
		" ↑/↓ scroll · o open in browser · b bookmark · esc back · q quit   %3.f%%",
		m.viewport.ScrollPercent()*100))
	return m.viewport.View() + "\n\n" + footer
}

// ----- helpers -----

func (m readModel) selected() (postItem, bool) {
	item, ok := m.list.SelectedItem().(postItem)
	return item, ok
}

func (m readModel) renderPost() string {
	item, ok := m.selected()
	if !ok {
		return ""
	}
	width := m.width - 4
	if width < 20 {
		width = 20
	}
	body := lipgloss.NewStyle().Width(width)
	p := item.post

	var b strings.Builder
	title := p.Title
	if item.bookmarked {
		title = "★ " + title
	}
	b.WriteString(body.Inherit(titleStyle).Render(title))
	b.WriteString("\n")
	b.WriteString(metaStyle.Render(p.FeedName + " · " + formatPostDate(p)))
	b.WriteString("\n\n")
	b.WriteString(linkStyle.Render(p.Url))
	b.WriteString("\n\n")

	text := ""
	if p.Description.Valid {
		text = cleanHTML(p.Description.String)
	}
	if text == "" {
		text = "(This feed doesn't include a summary. Press o to read the post in your browser.)"
	}
	b.WriteString(body.Render(text))

	return lipgloss.NewStyle().Padding(1, 2).Render(b.String())
}

// toggleBookmark saves or removes the selected post in the background.
func (m readModel) toggleBookmark() tea.Cmd {
	item, ok := m.selected()
	if !ok {
		return nil
	}
	index := m.list.GlobalIndex()
	s, userID := m.s, m.user.ID

	return func() tea.Msg {
		ctx := context.Background()
		if item.bookmarked {
			_, err := s.db.DeleteBookmark(ctx, database.DeleteBookmarkParams{
				UserID: userID, PostID: item.post.ID,
			})
			return bookmarkToggledMsg{index: index, bookmarked: err != nil, err: err}
		}
		_, err := s.db.CreateBookmark(ctx, database.CreateBookmarkParams{
			ID:        uuid.New(),
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
			UserID:    userID,
			PostID:    item.post.ID,
		})
		return bookmarkToggledMsg{index: index, bookmarked: err == nil, err: err}
	}
}

func (m readModel) openSelected() tea.Cmd {
	item, ok := m.selected()
	if !ok {
		return nil
	}
	return func() tea.Msg {
		return browserOpenedMsg{err: openInBrowser(item.post.Url)}
	}
}

// openInBrowser opens url in the default browser on Linux, WSL, macOS and Windows.
func openInBrowser(url string) error {
	var cmd *exec.Cmd
	switch {
	case runtime.GOOS == "darwin":
		cmd = exec.Command("open", url)
	case runtime.GOOS == "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case isWSL():
		if _, err := exec.LookPath("wslview"); err == nil {
			cmd = exec.Command("wslview", url)
		} else {
			cmd = exec.Command("explorer.exe", url)
		}
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	// explorer.exe reports failure even when it works, so don't check the
	// exit code; just wait in the background so the process is cleaned up.
	go cmd.Wait()
	return nil
}

func isWSL() bool {
	data, err := os.ReadFile("/proc/version")
	return err == nil && strings.Contains(strings.ToLower(string(data)), "microsoft")
}

var (
	htmlTagRe     = regexp.MustCompile(`(?s)<[^>]*>`)
	blockTagRe    = regexp.MustCompile(`(?i)<\s*(br|/p|/div|/li|/h[1-6])\s*/?>`)
	extraSpaceRe  = regexp.MustCompile(`[ \t]+`)
	extraBlanksRe = regexp.MustCompile(`\n\s*\n\s*\n+`)
)

// cleanHTML turns an HTML summary into plain text for the terminal.
func cleanHTML(s string) string {
	s = blockTagRe.ReplaceAllString(s, "\n")
	s = htmlTagRe.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = extraSpaceRe.ReplaceAllString(s, " ")
	s = extraBlanksRe.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

func formatPostDate(p database.GetPostsForUserRow) string {
	if !p.PublishedAt.Valid {
		return "unknown date"
	}
	return p.PublishedAt.Time.Local().Format("Mon Jan 2 2006 15:04")
}

func listHelpKeys() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "read")),
		key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open in browser")),
		key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "bookmark")),
	}
}
