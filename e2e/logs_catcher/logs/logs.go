package logs

import "strings"

type RAW []string

type Parsed []map[string]any

type Parser func(log string) (map[string]any, error)

func GrepLine(line string, matches []string) bool {
	matched := true
	for _, m := range matches {
		if !strings.Contains(line, m) {
			matched = false
		}
	}
	return matched
}

func (r RAW) Grep(matches []string) []string {
	raw := make([]string, 0)
	for _, log := range r {
		if GrepLine(log, matches) {
			raw = append(raw, log)
		}
	}

	return raw
}

func (r RAW) ParseAll(p Parser) Parsed {
	parsed := make([]map[string]any, 0)

	for _, log := range r {
		singleparsed, err := p(log)
		if err != nil {
			// todo: log parse err
			continue
		}
		parsed = append(parsed, singleparsed)
	}

	return parsed
}

func (p Parsed) GrepCondition(find map[string]func(any) bool) Parsed {
	parsed := make([]map[string]any, 0)
LogLoop:
	for _, log := range p {
		checkedGood := true
		for f, check := range find {
			dat, ok := log[f]
			if !ok {
				continue LogLoop
			}
			if !check(dat) {
				checkedGood = false
			}
		}
		if checkedGood {
			parsed = append(parsed, log)
		}
	}

	return parsed
}
