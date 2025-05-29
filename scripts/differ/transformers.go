package main

import (
	"regexp"
	"strings"
)

// Transformer applies a transformation to a given code string.
type Transformer func(string) string

type Transformers []Transformer

func (t Transformers) Transform(code string) string {
	for _, transformer := range t {
		code = transformer(code)
	}
	return code
}

func NopTransformer() Transformer {
	return func(code string) string { return code }
}

func NoComments() Transformer {
	expr := regexp.MustCompile(`(?m)^[ \t]*//.*\n`)
	return func(code string) string { return expr.ReplaceAllString(code, "") }
}

func NoPackageNames(packages []string) Transformer {
	if len(packages) == 0 {
		return NopTransformer()
	}
	expr := regexp.MustCompile(`(?m)([ \t()\[\]\{\}*&]|^)(?:` + strings.Join(packages, "|") + `)\.([a-zA-Z_][a-zA-Z0-9_]*)([ \t()\[\]\{\}*&,:]|$)`)
	return func(code string) string { return expr.ReplaceAllString(code, "$1$2$3") }
}

func NoEmptyLines() Transformer {
	expr := regexp.MustCompile(`(?m)^\s*$[\r\n]*|[\r\n]+\s+\z`)
	return func(code string) string { return expr.ReplaceAllString(code, "") }
}
