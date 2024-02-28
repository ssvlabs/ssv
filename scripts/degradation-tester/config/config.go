package config

type Config struct {
	Tests []BenchmarkingTestConfig `yaml:"Tests"`
}

type BenchmarkingTestConfig struct {
	PackagePath string     `yaml:"PackagePath"`
	Tests       []TestCase `yaml:"TestCases"`
}

type TestCase struct {
	Name                   string  `yaml:"Name"`
	AllowedDeltaPercentage float32 `yaml:"AllowedDeltaPercentage"`
}
