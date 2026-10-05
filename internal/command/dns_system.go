package command

import (
	"context"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// systemDNSObservation records OS state, not proof that application DNS uses mihomo.
type systemDNSObservation struct {
	Platform        string            `json:"platform"`
	RouteInterfaces map[string]string `json:"route_interfaces"`
	Resolvers       []string          `json:"resolvers"`
	RouteSource     string            `json:"route_source"`
	ResolverSource  string            `json:"resolver_source"`
	Issues          []string          `json:"issues"`
}

func systemDNSCheck(ctx context.Context) systemDNSObservation {
	result := systemDNSObservation{Platform: runtime.GOOS, RouteInterfaces: map[string]string{}, Resolvers: []string{}, Issues: []string{}}
	switch runtime.GOOS {
	case "linux":
		result.RouteSource = "/proc/net/route and /proc/net/ipv6_route"
		result.ResolverSource = "/etc/resolv.conf"
		if data, err := os.ReadFile("/proc/net/route"); err == nil {
			if name := linuxIPv4Default(string(data)); name != "" {
				result.RouteInterfaces["ipv4"] = name
			} else {
				result.Issues = append(result.Issues, "IPv4 default route not found")
			}
		} else {
			result.Issues = append(result.Issues, "IPv4 route table unavailable")
		}
		if data, err := os.ReadFile("/proc/net/ipv6_route"); err == nil {
			if name := linuxIPv6Default(string(data)); name != "" {
				result.RouteInterfaces["ipv6"] = name
			} else {
				result.Issues = append(result.Issues, "IPv6 default route not found")
			}
		} else {
			result.Issues = append(result.Issues, "IPv6 route table unavailable")
		}
		if data, err := os.ReadFile("/etc/resolv.conf"); err == nil {
			result.Resolvers = resolverAddresses(string(data))
			if len(result.Resolvers) == 0 {
				result.Issues = append(result.Issues, "no nameserver in resolv.conf")
			}
		} else {
			result.Issues = append(result.Issues, "resolver configuration unavailable")
		}
	case "darwin":
		result.RouteSource = "route -n get -inet/-inet6 default"
		result.ResolverSource = "scutil --dns"
		for _, family := range []string{"ipv4", "ipv6"} {
			flag := "-inet"
			if family == "ipv6" {
				flag = "-inet6"
			}
			if data, ok := dnsReadCommand(ctx, "route", "-n", "get", flag, "default"); ok {
				if name := macRouteInterface(data); name != "" {
					result.RouteInterfaces[family] = name
				} else {
					result.Issues = append(result.Issues, family+" default route interface not found")
				}
			} else {
				result.Issues = append(result.Issues, family+" route query unavailable")
			}
		}
		if data, ok := dnsReadCommand(ctx, "scutil", "--dns"); ok {
			result.Resolvers = macResolverAddresses(data)
			if len(result.Resolvers) == 0 {
				result.Issues = append(result.Issues, "system resolver addresses not found")
			}
		} else {
			result.Issues = append(result.Issues, "system resolver query unavailable")
		}
	default:
		result.Issues = append(result.Issues, "system DNS observation unsupported on this platform")
	}
	return result
}

func dnsReadCommand(ctx context.Context, name string, args ...string) (string, bool) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	data, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil || len(data) > 1<<20 {
		return "", false
	}
	return string(data), true
}

func linuxIPv4Default(data string) string {
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 8 && fields[1] == "00000000" && fields[7] == "00000000" && fields[0] != "Iface" {
			return fields[0]
		}
	}
	return ""
}

func linuxIPv6Default(data string) string {
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 10 && fields[0] == strings.Repeat("0", 32) && fields[1] == "00" {
			return fields[len(fields)-1]
		}
	}
	return ""
}

func resolverAddresses(data string) []string {
	result, seen := []string{}, map[string]bool{}
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "nameserver" {
			continue
		}
		if ip := net.ParseIP(fields[1]); ip != nil && !seen[ip.String()] {
			result = append(result, ip.String())
			seen[ip.String()] = true
		}
	}
	return result
}

func macRouteInterface(data string) string {
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) == 2 && fields[0] == "interface:" {
			return fields[1]
		}
	}
	return ""
}

func macResolverAddresses(data string) []string {
	lines := []string{}
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) >= 3 && strings.HasPrefix(fields[0], "nameserver[") && fields[1] == ":" {
			lines = append(lines, "nameserver "+fields[2])
		}
	}
	return resolverAddresses(strings.Join(lines, "\n"))
}
