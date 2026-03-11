package config

import "strings"

// NormalizeRelays expands relay list entries that contain separators ("," or ";"),
// trims whitespace, drops empty values, and de-duplicates while keeping order.
//
// This is primarily to make env vars like:
//
//	BUILDER_ENDPOINT_RELAYS="https://relay1;https://relay2"
//
// work as expected in addition to comma-separated lists.
func NormalizeRelays(relays []string) []string {
	if len(relays) == 0 {
		return nil
	}

	out := make([]string, 0, len(relays))
	seen := make(map[string]struct{}, len(relays))

	for _, entry := range relays {
		for _, part := range strings.FieldsFunc(entry, func(r rune) bool { return r == ',' || r == ';' }) {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if _, ok := seen[part]; ok {
				continue
			}
			seen[part] = struct{}{}
			out = append(out, part)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
