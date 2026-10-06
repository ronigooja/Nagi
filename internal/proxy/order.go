package proxy

import "gopkg.in/yaml.v3"

// GroupOrderFromYAML reads the configured group sequence without node details.
func GroupOrderFromYAML(data []byte) ([]string, error) {
	var config struct {
		Groups []struct {
			Name string `yaml:"name"`
		} `yaml:"proxy-groups"`
	}
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	order := make([]string, 0, len(config.Groups))
	for _, group := range config.Groups {
		if group.Name != "" {
			order = append(order, group.Name)
		}
	}
	return order, nil
}
