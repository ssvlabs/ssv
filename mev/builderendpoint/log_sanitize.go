package builderendpoint

import (
	"net/url"
	"strings"
)

// sanitizeRelayURLForLog removes relay credentials from a relay URL string.
//
// Many relay URLs include credentials in the authority component, e.g.
// `https://0x<pubkey>@relay.example.org`. We must avoid logging those.
//
// This function removes any userinfo (everything before `@`) and returns a URL
// string safe to include in logs.
func sanitizeRelayURLForLog(raw string) string {
	parsed, err := url.Parse(raw)
	if err == nil && parsed != nil && parsed.User != nil {
		parsed.User = nil
		return parsed.String()
	}

	// Fallback: if parsing fails, still attempt to remove userinfo.
	if at := strings.LastIndex(raw, "@"); at != -1 {
		// Keep scheme prefix if present; drop everything between scheme and '@'.
		if scheme := strings.Index(raw, "://"); scheme != -1 && scheme < at {
			return raw[:scheme+3] + raw[at+1:]
		}
		return raw[at+1:]
	}

	return raw
}
