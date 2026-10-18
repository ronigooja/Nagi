package requestrewrite

import (
	"strconv"
	"strings"
)

func pointerParts(pointer string) ([]string, error) {
	if pointer == "" {
		return nil, nil
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, invalid()
	}
	parts := strings.Split(pointer[1:], "/")
	for i, p := range parts {
		var b strings.Builder
		for j := 0; j < len(p); j++ {
			if p[j] != '~' {
				b.WriteByte(p[j])
				continue
			}
			j++
			if j >= len(p) {
				return nil, invalid()
			}
			switch p[j] {
			case '0':
				b.WriteByte('~')
			case '1':
				b.WriteByte('/')
			default:
				return nil, invalid()
			}
		}
		parts[i] = b.String()
	}
	return parts, nil
}
func arrayIndex(s string, n int, appendOK bool) (int, error) {
	if s == "-" && appendOK {
		return n, nil
	}
	if s == "" || len(s) > 1 && s[0] == '0' {
		return 0, invalid()
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, invalid()
		}
	}
	i, e := strconv.Atoi(s)
	if e != nil || i < 0 || i > n || i == n && !appendOK {
		return 0, invalid()
	}
	return i, nil
}
func editJSON(root any, parts []string, value any, remove bool) (any, error) {
	if len(parts) == 0 {
		if remove {
			return nil, invalid()
		}
		return value, nil
	}
	key := parts[0]
	last := len(parts) == 1
	switch node := root.(type) {
	case map[string]any:
		old, exists := node[key]
		if !exists && (!last || remove) {
			return nil, invalid()
		}
		if last {
			if remove {
				delete(node, key)
			} else {
				node[key] = value
			}
			return node, nil
		}
		next, e := editJSON(old, parts[1:], value, remove)
		if e != nil {
			return nil, e
		}
		node[key] = next
		return node, nil
	case []any:
		i, e := arrayIndex(key, len(node), last && !remove)
		if e != nil {
			return nil, e
		}
		if last {
			if remove {
				return append(node[:i], node[i+1:]...), nil
			}
			if i == len(node) {
				return append(node, value), nil
			}
			node[i] = value
			return node, nil
		}
		next, e := editJSON(node[i], parts[1:], value, remove)
		if e != nil {
			return nil, e
		}
		node[i] = next
		return node, nil
	}
	return nil, invalid()
}
