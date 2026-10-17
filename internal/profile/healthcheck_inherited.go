package profile

import "gopkg.in/yaml.v3"

// InheritedHealthchecks previews reset values using the same policy/override
// composition as activation, entirely in memory. No file or engine is changed.
func (s *Store) InheritedHealthchecks(name, kind, targetName string) ([]Healthcheck, error) {
	_, source, overlay, err := s.healthMaps(name)
	if err != nil {
		return nil, err
	}
	if kind != "subscription" {
		if local, e := healthTarget(overlay, kind, targetName); e == nil {
			fields := healthFields(local, kind)
			delete(fields, "interval")
			delete(fields, "url")
			if kind == "group" {
				original, _ := healthTarget(source, kind, targetName)
				for _, field := range []string{"interval", "url"} {
					if value, ok := original[field]; ok {
						fields[field] = value
					}
				}
			}
		}
		if kind == "group" {
			for _, key := range []string{groupHealthcheckIntervalsKey, groupHealthcheckURLsKey} {
				if values, ok := overlay[key].(map[string]any); ok {
					delete(values, targetName)
				}
			}
		}
	}
	// Keep the original map intact for legacy group snapshot comparisons.
	data, err := yaml.Marshal(source)
	if err != nil {
		return nil, err
	}
	var base map[string]any
	if err = yaml.Unmarshal(data, &base); err != nil {
		return nil, err
	}
	mergeMap(base, overlay)
	if kind == "subscription" {
		// Preview removal of the subscription's own policies, retaining all
		// explicit Provider/group overrides exactly as reset does.
		err = applyHealthcheckPolicy(base, source, overlay, nil, nil)
	} else {
		err = s.applySubscriptionHealthcheck(name, base, source, overlay)
	}
	if err != nil {
		return nil, err
	}
	if err = applyGroupHealthcheckIntervals(base, overlay[groupHealthcheckIntervalsKey]); err != nil {
		return nil, err
	}
	if err = applyGroupHealthcheckURLs(base, overlay[groupHealthcheckURLsKey]); err != nil {
		return nil, err
	}
	if kind != "subscription" {
		value, err := s.healthcheckFromMaps(name, kind, targetName, base, source, overlay)
		return []Healthcheck{value}, err
	}
	var result []Healthcheck
	providers, _ := base["proxy-providers"].(map[string]any)
	for target, raw := range providers {
		provider, _ := raw.(map[string]any)
		if healthFields(provider, "provider") == nil {
			continue
		}
		value, err := s.healthcheckFromMaps(name, "provider", target, base, source, overlay)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	_, groups, err := namedList(base["proxy-groups"])
	if err != nil {
		return nil, err
	}
	for target, group := range groups {
		if !subscriptionGroupEligible(group) {
			continue
		}
		value, err := s.healthcheckFromMaps(name, "group", target, base, source, overlay)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, nil
}
