package menu

import "context"

// Page supplies a fresh view when opened or after an action.
type Page struct {
	Title string
	Load  func(context.Context, *Session) (PageView, error)
}

type PageView struct {
	Context string
	Items   []Item // Resources, paginated ten at a time.
	Actions []Item // Actions, visible after resources on every page.
	Empty   string
}

type Navigation struct {
	Next *Page
	Back bool
}

// Item opens a page, dispatches a command, or performs a custom flow.
type Item struct {
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

func numberedChoices(view PageView, index int) []numberedChoice {
	start := index * pageSize
	end := start + pageSize
	if end > len(view.Items) {
		end = len(view.Items)
	}
	choices := []numberedChoice{}
	for _, item := range view.Items[start:end] {
		choices = append(choices, numberedChoice{Item: item})
	}
	for _, item := range view.Actions {
		choices = append(choices, numberedChoice{Item: item})
	}
	if end < len(view.Items) {
		choices = append(choices, numberedChoice{Item: Item{Label: "Next page"}, PageDelta: 1})
	}
	if index > 0 {
		choices = append(choices, numberedChoice{Item: Item{Label: "Previous page"}, PageDelta: -1})
	}
	return choices
}

func actionPage(title string, items ...Item) *Page {
	return &Page{Title: title, Load: func(context.Context, *Session) (PageView, error) {
		return PageView{Actions: items}, nil
	}}
}

// Settings reread their status on entry and after each operation.
func statePage(title string, command, fields []string, items ...Item) *Page {
	return &Page{Title: title, Load: func(ctx context.Context, s *Session) (PageView, error) {
		return PageView{Context: pageStatus(ctx, s, title, command, fields...), Actions: items}, nil
	}}
}

func submenu(page *Page) Item {
	return Item{Label: page.Title, Next: func() *Page { return page }}
}
