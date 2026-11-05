// Package cookieutil parses browser cookie headers without depending on a
// particular site adapter.
package cookieutil

import "strings"

// Parse splits a Cookie header into a name/value map.
func Parse(s string) map[string]string {
	out := make(map[string]string)
	for _, part := range strings.Split(s, ";") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) == 2 && kv[0] != "" {
			out[kv[0]] = kv[1]
		}
	}
	return out
}
