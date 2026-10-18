package requestrewrite

import "context"

// Edit loads, validates and saves a new immutable document, rejecting concurrent
// edits. Secret-bearing rule data never appears in mutation results or errors.
func (s Store) Edit(ctx context.Context, edit func(*Document) error) (SaveResult, error) {
	old, e := s.Load()
	if e != nil {
		return SaveResult{}, e
	}
	doc := old.Document()
	if e = edit(&doc); e != nil {
		return SaveResult{}, e
	}
	candidate, e := Compile(doc)
	if e != nil {
		return SaveResult{}, e
	}
	return s.save(ctx, candidate, old.version)
}
func (s Store) AddDomain(ctx context.Context, domain Domain) (SaveResult, error) {
	return s.Edit(ctx, func(d *Document) error {
		for _, v := range d.Domains {
			if v.ID == domain.ID {
				return invalid()
			}
		}
		d.Domains = append(d.Domains, domain)
		return nil
	})
}
func (s Store) UpdateDomain(ctx context.Context, id string, domain Domain) (SaveResult, error) {
	return s.Edit(ctx, func(d *Document) error {
		if id != domain.ID {
			return invalid()
		}
		for i, v := range d.Domains {
			if v.ID == id {
				d.Domains[i] = domain
				return nil
			}
		}
		return invalid()
	})
}
func (s Store) RemoveDomain(ctx context.Context, id string) (SaveResult, error) {
	return s.Edit(ctx, func(d *Document) error {
		for i, v := range d.Domains {
			if v.ID == id {
				d.Domains = append(d.Domains[:i], d.Domains[i+1:]...)
				return nil
			}
		}
		return invalid()
	})
}
func (s Store) AddRule(ctx context.Context, rule Rule) (SaveResult, error) {
	return s.Edit(ctx, func(d *Document) error {
		for _, v := range d.Rules {
			if v.ID == rule.ID {
				return invalid()
			}
		}
		d.Rules = append(d.Rules, rule)
		return nil
	})
}
func (s Store) UpdateRule(ctx context.Context, id string, rule Rule) (SaveResult, error) {
	return s.Edit(ctx, func(d *Document) error {
		if id != rule.ID {
			return invalid()
		}
		for i, v := range d.Rules {
			if v.ID == id {
				d.Rules[i] = rule
				return nil
			}
		}
		return invalid()
	})
}
func (s Store) RemoveRule(ctx context.Context, id string) (SaveResult, error) {
	return s.Edit(ctx, func(d *Document) error {
		for i, v := range d.Rules {
			if v.ID == id {
				d.Rules = append(d.Rules[:i], d.Rules[i+1:]...)
				return nil
			}
		}
		return invalid()
	})
}
func (s Store) SetRuleEnabled(ctx context.Context, id string, enabled bool) (SaveResult, error) {
	return s.Edit(ctx, func(d *Document) error {
		for i := range d.Rules {
			if d.Rules[i].ID == id {
				d.Rules[i].Enabled = enabled
				return nil
			}
		}
		return invalid()
	})
}
func (s Store) ReorderRules(ctx context.Context, ids []string) (SaveResult, error) {
	return s.Edit(ctx, func(d *Document) error {
		if len(ids) != len(d.Rules) {
			return invalid()
		}
		byID := map[string]Rule{}
		for _, r := range d.Rules {
			byID[r.ID] = r
		}
		ordered := make([]Rule, 0, len(ids))
		for _, id := range ids {
			r, ok := byID[id]
			if !ok {
				return invalid()
			}
			ordered = append(ordered, r)
			delete(byID, id)
		}
		d.Rules = ordered
		return nil
	})
}

// RuleSummary deliberately omits all operation values and redirect consent.
type RuleSummary struct {
	ID         string   `json:"id"`
	Enabled    bool     `json:"enabled"`
	Match      Match    `json:"match"`
	Operations []string `json:"operations"`
	OnError    string   `json:"on_error"`
}

func (s *Snapshot) RuleSummaries() []RuleSummary {
	out := []RuleSummary{}
	for _, r := range s.doc.Rules {
		methods := append([]string{}, r.Match.Methods...)
		m := r.Match
		m.Methods = methods
		v := RuleSummary{ID: r.ID, Enabled: r.Enabled, Match: m, Operations: []string{}, OnError: r.OnError}
		for _, o := range r.Operations {
			v.Operations = append(v.Operations, o.Op)
		}
		out = append(out, v)
	}
	return out
}
