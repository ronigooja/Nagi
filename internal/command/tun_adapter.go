package command

import "net"

// tunAdapterReport checks a configured device name against OS interfaces. An
// interface being up does not prove that OS routes or DNS capture use it.
func tunAdapterReport(tun map[string]any, interfaces func() ([]net.Interface, error)) map[string]any {
	result := map[string]any{"adapter_status": "inactive"}
	if tun["enable"] != true {
		return result
	}
	result["adapter_status"] = "unknown"
	name, _ := tun["device"].(string)
	if name == "" {
		return result
	}
	result["adapter_name"] = name
	available, err := interfaces()
	if err != nil {
		return result
	}
	result["adapter_status"] = "missing"
	for _, iface := range available {
		if iface.Name == name {
			result["adapter_status"] = "down"
			if iface.Flags&net.FlagUp != 0 {
				result["adapter_status"] = "up"
			}
			break
		}
	}
	return result
}
