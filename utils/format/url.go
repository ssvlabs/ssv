package format

import "net/url"

// RedactURL masks what may be a credential in a URL for logging, as go-eth2-client masks Client.Address():
// the password, the path and the query values. A URL that doesn't parse is replaced whole, since its parts
// can't be told apart.
func RedactURL(raw string) string {
	const masked = "xxxxx"
	u, err := url.Parse(raw)
	if err != nil {
		return "<malformed>"
	}
	if _, ok := u.User.Password(); ok {
		u.User = url.UserPassword(u.User.Username(), masked)
	}
	if u.Path != "" {
		u.Path, u.RawPath = masked, ""
	}
	if u.RawQuery != "" {
		query := u.Query()
		for key := range query {
			query[key] = []string{masked}
		}
		u.RawQuery = query.Encode()
	}
	return u.String()
}
