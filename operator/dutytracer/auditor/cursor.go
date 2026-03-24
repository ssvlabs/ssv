package auditor

import "strings"

// ValidateCursor validates the auditor findings cursor format.
// Cursor format: "<slot>/<reason>/<seq>".
func ValidateCursor(cur string) error {
	cur = strings.TrimSpace(cur)
	if cur == "" {
		return nil
	}
	_, err := parseCursor(cur)
	return err
}

