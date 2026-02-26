package relayurl

import (
	"net/url"
	"strings"
)

// StripUserInfo removes any userinfo (everything before `@`) from a relay URL.
//
// Many relay URLs include credentials in the authority component, e.g.
// `https://0x<pubkey>@relay.example.org`. Those must never be logged or used as
// metric label values.
func StripUserInfo(raw string) string {
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

// Host returns the host[:port] portion of a relay URL with any userinfo stripped.
// It is intended for low-cardinality metric labels.
func Host(raw string) string {
	s := StripUserInfo(raw)

	// url.Parse treats inputs without scheme as paths; add a dummy scheme for parsing.
	parsed, err := url.Parse(s)
	if err == nil && parsed != nil && parsed.Host != "" {
		return parsed.Host
	}

	parsed, err = url.Parse("scheme://" + s)
	if err == nil && parsed != nil && parsed.Host != "" {
		return parsed.Host
	}

	// Last-ditch fallback: strip any path component.
	if cut, _, ok := strings.Cut(s, "/"); ok {
		return cut
	}
	return s
}
