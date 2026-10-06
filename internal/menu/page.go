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
	Items   []Item
	Empty   string
}

// Item either opens a child page, runs a command, or performs a custom flow.
type Item struct {
	Label   string
	Args    []string
	Confirm string
	Next    func() *Page
	Action  func(context.Context, *Session) error
	Help    []string
}

const pageSize = 10
