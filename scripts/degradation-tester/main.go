package main

import (
	"bufio"
	"fmt"
	"gopkg.in/yaml.v3"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
)

var testThresholds map[string]float64

type Config struct {
	Tests []BenchmarkingTestConfig `yaml:"Tests"`
}

type BenchmarkingTestConfig struct {
	PackagePath string     `yaml:"PackagePath"`
	TestCases   []TestCase `yaml:"TestCases"`
}

type TestCase struct {
	Name                   string  `yaml:"Name"`
	AllowedDeltaPercentage float64 `yaml:"AllowedDeltaPercentage"`
}

var config Config

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: go run script.go <config_filename> <results_filename>")
		os.Exit(1)
	}

	configFilename := os.Args[1]
	resultsFilename := os.Args[2]

	loadConfig(configFilename)

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

		if strings.Contains(line, "sec/op") {
			currentSection = "sec/op"
		} else if strings.Contains(line, "B/op") {
			currentSection = "B/op"
		} else if strings.Contains(line, "allocs/op") {
			currentSection = "allocs/op"
		}

		totalErrors += checkLine(line, currentSection)
	}

	if err := scanner.Err(); err != nil {
		fmt.Printf("Error reading results file: %v\n", err)
	}

	if totalErrors > 0 {
		os.Exit(1)
	}
}

func loadConfig(filename string) {
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

	testThresholds = make(map[string]float64)
	for _, pkg := range config.Tests {
		for _, testCase := range pkg.TestCases {
			testThresholds[testCase.Name] = testCase.AllowedDeltaPercentage
		}
	}

}

func checkLine(line, section string) int {
	reChange := regexp.MustCompile(`-?\d+\.\d+%`)
	reTestName := regexp.MustCompile(`^\S+`)
	matches := reChange.FindAllString(line, -1)
	testNameMatch := reTestName.FindString(line)

	if testNameMatch == "geomean" {
		return 0
	}

	normalizedTestName := normalizeTestName(testNameMatch)

	if len(matches) > 0 {
		changeStr := matches[len(matches)-1]
		change, err := strconv.ParseFloat(strings.TrimSuffix(changeStr, "%"), 64)
		if err != nil {
			fmt.Printf("Error parsing float: %v\n", err)
			os.Exit(1)
		}

		threshold := findThresholdForTest(normalizedTestName)
		if threshold == 0 {
			fmt.Printf("Test %s not found in config\n", normalizedTestName)
			os.Exit(1)
		}

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

func findThresholdForTest(testName string) float64 {
	if threshold, exists := testThresholds[testName]; exists {
		return threshold
	}
	return 0
}
