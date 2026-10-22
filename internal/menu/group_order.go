package menu

import (
	"context"
	"fmt"
	"strconv"

	"github.com/ronigooja/Nagi/internal/i18n"
)

func groupOrderPage(l i18n.Context) *Page {
	var names []string
	profile := ""
	initialized := false
	return &Page{ID: "group.order.draft", Title: groupText(l, "order", "Adjust display order"), Load: func(ctx context.Context, s *Session) (PageView, error) {
		if !initialized {
			var data struct {
				Profile string            `json:"profile"`
				Groups  []menuGroupConfig `json:"group_configs"`
			}
			if err := localizeddecodeMenuResult(l, s.Fetch(ctx, "proxy", "group", "list"), &data); err != nil {
				return PageView{}, err
			}
			profile = data.Profile
			for _, g := range data.Groups {
				names = append(names, g.Name)
			}
			initialized = true
		}
		v := PageView{Empty: groupText(l, "empty", "No configured proxy groups.")}
		for _, name := range names {
			name := name
			v.Items = append(v.Items, Item{Name: name, Label: name, Next: func() *Page {
				return &Page{ID: "group.order.move", Title: name, Load: func(ctx context.Context, s *Session) (PageView, error) {
					index := -1
					for i, n := range names {
						if n == name {
							index = i
							break
						}
					}
					view := PageView{}
					if index < 0 {
						return view, nil
					}
					start := index / pageSize * pageSize
					end := min(start+pageSize, len(names))
					move := func(target int) func(context.Context, *Session) (Navigation, error) {
						return func(context.Context, *Session) (Navigation, error) {
							moveGroupOrder(names, index, target)
							page := target / pageSize
							return Navigation{Back: true, BackPage: &page}, nil
						}
					}
					if start > 0 {
						view.Actions = append(view.Actions, Item{ID: "group.order.previous_page", Label: groupText(l, "previous_page", "Move to previous page"), Navigate: move(index - pageSize)})
					}
					if end < len(names) {
						view.Actions = append(view.Actions, Item{ID: "group.order.next_page", Label: groupText(l, "next_page", "Move to next page"), Navigate: move(min(index+pageSize, len(names)-1))})
					}
					view.Actions = append(view.Actions, Item{ID: "group.order.position", Label: groupText(l, "position", "Move to position"), Navigate: func(ctx context.Context, s *Session) (Navigation, error) {
						count := end - start
						for {
							value, err := s.Prompt(fmt.Sprintf("%s (1-%d)", groupText(l, "target_position", "Target position"), count))
							if err != nil {
								return Navigation{}, err
							}
							target, parseErr := strconv.Atoi(value)
							if parseErr != nil || target < 1 || target > count || strconv.Itoa(target) != value {
								s.writeFeedback(feedbackWarning, groupText(l, "invalid_position", "Enter a whole number within the displayed range."))
								continue
							}
							return move(start+target-1)(ctx, s)
						}
					}})
					return view, nil
				}}
			}})
		}
		if len(names) > 0 {
			v.Actions = append(v.Actions, Item{ID: "group.order.save", Label: groupText(l, "save_order", "Save display order"), Navigate: func(ctx context.Context, s *Session) (Navigation, error) {
				var current struct {
					Profile string `json:"profile"`
				}
				if err := localizeddecodeMenuResult(l, s.Fetch(ctx, "proxy", "group", "list"), &current); err != nil {
					return Navigation{}, err
				}
				if current.Profile != profile {
					return Navigation{}, fmt.Errorf("%s", groupText(l, "profile_changed", "The selected profile changed. Reopen display ordering and try again."))
				}
				args := append([]string{"proxy", "group", "order"}, names...)
				result := s.Run(ctx, args...)
				return Navigation{Back: result.Code == 0 && result.Error == nil}, nil
			}})
		}
		v.Context = groupText(l, "order_hint", "Select a group to move it. Changes take effect after Save display order.")
		// Names stay in the draft's order; pagination never changes the list.
		return v, nil
	}}
}

func moveGroupOrder(names []string, from, to int) {
	if from == to {
		return
	}
	name := names[from]
	if to < from {
		copy(names[to+1:from+1], names[to:from])
	} else {
		copy(names[from:to], names[from+1:to+1])
	}
	names[to] = name
}
