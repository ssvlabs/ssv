package logs

import (
	"fmt"
	"strings"
)

type RAW []string

type ParsedLine map[string]any

type Parsed []ParsedLine

type Parser func(log string) (map[string]any, error)

func (pl ParsedLine) Get(key string) (any, error) {
	if f, ok := pl[key]; ok {
		return f, nil
	}
	return nil, fmt.Errorf("field doesn't exist %v", key)
}

func (p Parsed) Fields(key string, failOnError bool) (RAW, error) {
	var res RAW
	for _, v := range p {
		got, err := v.Get(key)
		if err == nil {
			res = append(res, fmt.Sprint(got))
		}

		if failOnError {
			return nil, err
		}

	}
	return res, nil
}

func GrepLine(line string, matches RAW) bool {
	matched := true
	for _, m := range matches {
		if !strings.Contains(line, m) {
			matched = false
		}
	}
	return matched
}

func (r RAW) Grep(matches []string) RAW {
	raw := make([]string, 0)
	for _, log := range r {
		if GrepLine(log, matches) {
			raw = append(raw, log)
		}
	}

	return raw
}

func (r RAW) ParseAll(p Parser) Parsed {
	parsed := Parsed{}

	for _, log := range r {
		removeall := strings.Split(log, "{")
		removeall2 := strings.Split(removeall[1], "}")
		comb := strings.Join([]string{"{", removeall2[0], "}"}, "")
		singleparsed, err := p(comb)
		if err != nil {
			// todo: log parse err
			fmt.Println("Error parsing log ", err, log)
			continue
		}
		parsed = append(parsed, singleparsed)
	}

	return parsed
}

func (p Parsed) GrepCondition(find map[string]func(any) bool) Parsed {
	parsed := Parsed{} // make([]map[string]any, 0)
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
