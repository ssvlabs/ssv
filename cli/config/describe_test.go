package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDescribe(t *testing.T) {
	type Inner struct {
		Port    int           `yaml:"Port" env:"PORT"`
		Timeout time.Duration `yaml:"Timeout" env:"TIMEOUT"`
		Max     uint64        `yaml:"Max" env:"MAX"`
		Ratio   float64       `yaml:"Ratio" env:"RATIO"`
		Peers   []string      `yaml:"Peers" env:"PEERS"`
	}
	type Sub struct {
		Key string `yaml:"Key" env:"KEY"`
	}
	type Outer struct {
		Name     string `yaml:"Name" env:"NAME" env-description:"the name"`
		Inner    Inner  `yaml:"inner"`
		Sub      Sub    `yaml:"sub" env-prefix:"SUB_"` // children inherit the env-prefix
		Token    string `yaml:"Token" env:"TOKEN" env-required:"true"`
		Opt      string `yaml:"Opt,omitempty" env:"OPT"` // yaml options must be stripped from the path
		Injected *int   // untagged runtime dependency, must be skipped
	}

	// Describe is defensive about non-struct and nil inputs.
	require.Nil(t, Describe(nil))
	require.Nil(t, Describe(42))
	require.Nil(t, Describe((*Outer)(nil)))

	docs := Describe(&Outer{Name: "abc", Inner: Inner{Port: 60, Timeout: 10 * time.Second, Max: 25, Ratio: 1.5, Peers: []string{"a", "b"}}})

	got := make(map[string]FieldDoc, len(docs))
	for _, d := range docs {
		got[d.YAMLPath] = d
	}

	require.Equal(t, "abc", got["Name"].Default)
	require.Equal(t, "NAME", got["Name"].EnvName)
	require.Equal(t, "the name", got["Name"].Description)

	// Nested yaml struct is recursed into with a dotted path; each scalar kind is formatted for display.
	require.Equal(t, "60", got["inner.Port"].Default)
	require.Equal(t, "10s", got["inner.Timeout"].Default) // Duration renders human-readably
	require.Equal(t, "25", got["inner.Max"].Default)
	require.Equal(t, "1.5", got["inner.Ratio"].Default)
	require.Equal(t, "a;b", got["inner.Peers"].Default) // slice joined with ";"

	// The container row exists but carries no env var of its own.
	require.Contains(t, got, "inner")
	require.Empty(t, got["inner"].EnvName)

	// Untagged fields (injected dependencies) are not described.
	require.NotContains(t, got, "Injected")

	// env-prefix on a parent is threaded into nested env var names (mirrors cleanenv).
	require.Equal(t, "SUB_KEY", got["sub.Key"].EnvName)
	// env-required is captured; non-required fields are not flagged.
	require.True(t, got["Token"].Required)
	require.False(t, got["Name"].Required)

	// yaml tag options (",omitempty") are stripped from the path.
	require.Contains(t, got, "Opt")
	require.NotContains(t, got, "Opt,omitempty")
}

// TestDescribeHelp_prefixAndRequired covers env-prefix threading and the required marker in the
// rendered help block.
func TestDescribeHelp_prefixAndRequired(t *testing.T) {
	type sub struct {
		Token string `yaml:"Token" env:"TOKEN" env-required:"true" env-description:"api token"`
	}
	type cfg struct {
		Sub sub `yaml:"sub" env-prefix:"SUB_"`
	}
	help := describeHelp(&cfg{})
	require.Contains(t, help, "SUB_TOKEN (required)")
}

type helpCfg struct {
	Level string `yaml:"Level" env:"LEVEL" env-description:"log level"`
	Addr  string `yaml:"Addr" env:"ADDR" env-description:"required address"`
}

func (c *helpCfg) ApplyDefaults() { c.Level = "info" }

func TestDescribeHelp(t *testing.T) {
	help := describeHelp(&helpCfg{})

	// Defaulter is applied to a fresh copy, so the seeded default is shown...
	require.Contains(t, help, `LEVEL (default "info")`)
	require.Contains(t, help, "log level")
	// ...while a field left at its zero value shows no default annotation.
	require.Contains(t, help, "ADDR\n")
	require.NotContains(t, help, `ADDR (default`)
}

// TestPrepare_appliesDefaults covers the Defaulter hook in Prepare: with no config file it falls
// back to the env path, and the in-code defaults must be seeded first so an unset field keeps its
// default.
func TestPrepare_appliesDefaults(t *testing.T) {
	c := &helpCfg{}
	require.NoError(t, Prepare(c, &Args{}))
	require.Equal(t, "info", c.Level)
}
