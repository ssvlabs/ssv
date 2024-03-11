package main

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

var opThresholds map[string]float64
var allocThresholds map[string]float64

type Config struct {
	DefaultOpDelta    float64                  `yaml:"DefaultOpDelta"`
	DefaultAllocDelta float64                  `yaml:"DefaultAllocDelta"`
	Packages          []BenchmarkingTestConfig `yaml:"Packages"`
}

type BenchmarkingTestConfig struct {
	Path  string     `yaml:"Path"`
	Tests []TestCase `yaml:"Tests"`
}

type TestCase struct {
	Name       string  `yaml:"Name"`
	OpDelta    float64 `yaml:"OpDelta"`
	AllocDelta float64 `yaml:"AllocDelta"`
}

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: degradation-tester <config_filename> <results_filename>")
		os.Exit(1)
	}

	configFilename := os.Args[1]
	resultsFilename := os.Args[2]

	config := loadConfig(configFilename)

	file, err := os.Open(resultsFilename)
	if err != nil {
		fmt.Printf("Error opening results file: %v\n", err)
		os.Exit(1)
	}
	defer file.Close()

	var currentSection string

	scanner := bufio.NewScanner(file)
	totalErrors := 0
	for scanner.Scan() {
		line := scanner.Text()

		switch {
		case strings.Contains(line, "sec/op"):
			currentSection = "sec/op"
		case strings.Contains(line, "B/op"):
			currentSection = "B/op"
		case strings.Contains(line, "allocs/op"):
			currentSection = "allocs/op"
		}

		totalErrors += checkLine(config, line, currentSection)
	}

	if err := scanner.Err(); err != nil {
		fmt.Printf("Error reading results file: %v\n", err)
	}

	if totalErrors > 0 {
		os.Exit(1)
	}
}

func loadConfig(filename string) *Config {
	config := &Config{}
	file, err := os.ReadFile(filename)
	if err != nil {
		fmt.Printf("Error reading config file: %v\n", err)
		os.Exit(1)
	}
	err = yaml.Unmarshal(file, &config)
	if err != nil {
		fmt.Printf("Error parsing config file: %v\n", err)
		os.Exit(1)
	}

	opThresholds = make(map[string]float64)
	allocThresholds = make(map[string]float64)

	for _, pkg := range config.Packages {
		for _, testCase := range pkg.Tests {
			if testCase.OpDelta > 0 {
				opThresholds[testCase.Name] = testCase.OpDelta
			}
			if testCase.AllocDelta > 0 {
				allocThresholds[testCase.Name] = testCase.AllocDelta
			}
		}
	}
	return config
}

func checkLine(
	config *Config,
	line string,
	section string,
) int {
	reChange := regexp.MustCompile(`-?\d+\.\d+%`)
	reTestName := regexp.MustCompile(`^\S+`)
	matches := reChange.FindAllString(line, -1)
	testNameMatch := reTestName.FindString(line)

	// The "geomean" represents a statistical summary (geometric mean) of multiple test results,
	// not an individual test result, hence we should just skip it
	if testNameMatch == "geomean" {
		return 0
	}

	normalizedTestName := normalizeTestName(testNameMatch)

	if len(matches) > 0 {
		changeStr := matches[len(matches)-1]
		change, err := strconv.ParseFloat(strings.TrimSuffix(changeStr, "%"), 64)
		if err != nil {
			fmt.Printf("⚠️ Error parsing float: %v\n", err)
			return 1
		}

		threshold := getThresholdForTestCase(config, normalizedTestName, section)

		if math.Abs(change) > threshold {
			fmt.Printf("❌ Change in section %s for test %s exceeds threshold: %s\n", section, normalizedTestName, changeStr)
			return 1
		}
	}
	return 0
}

func normalizeTestName(testName string) string {
	re := regexp.MustCompile(`-\d+$`)
	return re.ReplaceAllString(testName, "")
}

func getThresholdForTestCase(
	config *Config,
	testName string,
	section string,
) float64 {
	switch section {
	case "sec/op":
		if threshold, exists := opThresholds[testName]; exists {
			return threshold
		}
		return config.DefaultOpDelta
	case "allocs/op":
		if threshold, exists := allocThresholds[testName]; exists {
			return threshold
		}
		return config.DefaultAllocDelta
	default:
		return 0.0
	}
}
