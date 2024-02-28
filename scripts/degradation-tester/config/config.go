package config

type Config struct {
	Tests []BenchmarkingTestConfig `yaml:"Tests"`
}

type BenchmarkingTestConfig struct {
	TestPath               string `yaml:"TestPath"`
	ComparisonBenchPath    string `yaml:"ComparisonBenchPath"`
	AllowedDeltaPercentage uint32 `yaml:"AllowedDeltaPercentage"`
}
