package menu

import "github.com/ronigooja/Nagi/internal/i18n"

import "context"
import "strings"

// Page supplies a fresh view when opened or after an action.
type Page struct {
	ID   string // Stable navigation and presentation identity.
	Root bool

	Title string
	Load  func(context.Context, *Session) (PageView, error)
}

type PageView struct {
	ContextRaw    bool // Explicit sensitive content bypasses redaction and styles.
	Context       string
	Items         []Item // Resources, paginated ten at a time.
	Actions       []Item // Actions, visible after resources on every page.
	Empty         string
	ReadErrorHint string // Recovery guidance used instead of Empty after a failed read.
}

type Navigation struct {
	Next *Page
	Back bool
}

// Item opens a page, dispatches a command, or performs a custom flow.
type Item struct {
	ID string // Stable action identity; independent of translated labels.

	Selected bool // Explicit current selection; never inferred from resource text.
	Label    string
	Name     string // Resource identity.
	Args     []string
	Impact   string // Effect shown beside the action; never asks for confirmation.
	Next     func() *Page
	Action   func(context.Context, *Session) error
	Navigate func(context.Context, *Session) (Navigation, error)
}

const pageSize = 10

// numberedChoices is the single mapping used by rendering and dispatch.
type numberedChoice struct {
	Item      Item
	PageDelta int
}

func localizednumberedChoices(language i18n.Context, view PageView, index int) []numberedChoice {
	start := index * pageSize
	end := start + pageSize
	if end > len(view.Items) {
		end = len(view.Items)
	}
	choices := []numberedChoice{}
	for _, item := range view.Items[start:end] {
		if item.Name != "" {
			item.ID += "/" + item.Name
		}
		choices = append(choices, numberedChoice{Item: item})
	}
	for _, item := range view.Actions {
		if item.Name != "" {
			item.ID += "/" + item.Name
		}
		choices = append(choices, numberedChoice{Item: item})
	}
	if end < len(view.Items) {
		choices = append(choices, numberedChoice{Item: Item{ID: "page.numberedChoices.item1", Label: menuText(language, "menu.page.numbered_choices.next_page", "Next page")}, PageDelta: 1})
	}
	if index > 0 {
		choices = append(choices, numberedChoice{Item: Item{ID: "page.numberedChoices.item2", Label: menuText(language, "menu.page.numbered_choices.previous_page", "Previous page")}, PageDelta: -1})
	}
	return choices
}

func localizedactionPage(language i18n.Context, title string, items ...Item) *Page {
	id := "actions"
	if len(items) > 0 && items[0].ID != "" {
		id = items[0].ID + "/page"
	}
	return &Page{ID: id, Title: title, Load: func(context.Context, *Session) (PageView, error) {
		return PageView{Actions: items}, nil
	}}
}

// Settings reread their status on entry and after each operation.
func localizedstatePage(language i18n.Context, title string, command, fields []string, items ...Item) *Page {
	return &Page{ID: "state/" + strings.Join(command, "/"), Title: title, Load: func(ctx context.Context, s *Session) (PageView, error) {
		return PageView{Context: pageStatus(ctx, s, title, command, fields...), Actions: items}, nil
	}}
}

func localizedsubmenu(language i18n.Context, page *Page) Item {
	return Item{ID: "page.submenu.item1", Label: page.Title, Next: func() *Page { return page }}
}
func numberedChoices(
	view PageView, index int) []numberedChoice {
	return localizednumberedChoices(i18n.New(i18n.English), view, index)
}
func actionPage(title string, items ...Item) *Page {
	return localizedactionPage(i18n.New(i18n.English), title, items...)
}

func statePage(title string, command, fields []string, items ...Item) *Page {
	return localizedstatePage(i18n.New(i18n.English), title, command, fields, items...)
}
func submenu(page *Page) Item {
	return localizedsubmenu(i18n.New(i18n.English), page)
}
