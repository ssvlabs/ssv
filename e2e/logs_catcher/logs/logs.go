package logs

type RAW []string

type Parsed []map[string]any

type Parser func(log string) (map[string]any, error)

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
	for _, log := range p {
		for f, check := range find {
			dat, ok := log[f]
			if !ok {
				continue
			}
			if check(dat) {
				parsed = append(parsed, log)
			}
		}
	}

	return parsed
}
