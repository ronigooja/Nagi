package menu

import "context"

// Page supplies a fresh view when opened, refreshed, or after an action.
type Page struct {
	Title string
	Help  []string
	Load  func(context.Context, *Session) (PageView, error)
}

type PageView struct {
	Context string
	Items   []Item // Resources: filtered and paginated numeric choices.
	Actions []Item // Fixed actions: always visible letter choices.
	Empty   string
}

type Navigation struct {
	Next *Page
	Back bool
}

// Item opens a page, dispatches a command, or performs a custom flow.
type Item struct {
	Label    string
	Name     string // Name used for filtering; defaults to Label.
	Key      string
	Args     []string
	Impact   string // Effect shown beside the action; never asks for confirmation.
	Next     func() *Page
	Action   func(context.Context, *Session) error
	Navigate func(context.Context, *Session) (Navigation, error)
	Help     []string
}

const pageSize = 10

// actionKeys excludes the reserved navigation keys b/r/h/q/n/p.
const actionKeys = "acdefgijklmostuvwxyz"

func fixedActions(view PageView) []Item {
	actions := append([]Item(nil), view.Actions...)
	used := map[string]bool{}
	for _, item := range actions {
		if item.Key != "" {
			used[item.Key] = true
		}
	}
	for i := range actions {
		if actions[i].Key != "" {
			continue
		}
		for _, key := range actionKeys {
			k := string(key)
			if !used[k] {
				actions[i].Key = k
				used[k] = true
				break
			}
		}
	}
	return actions
}
