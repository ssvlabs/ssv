package main

import (
	"bufio"
	"encoding/csv"
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

type Benchmark struct {
	TestName string
	OldValue float64
	OldCI    float64
	NewValue float64
	NewCI    float64
	VsBase   string
	P        string
}

type BenchStatOutput struct {
	OsData     string
	benchmarks []Benchmark
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

	totalErrors := checkFile(file, config)

	if totalErrors > 0 {
		os.Exit(1)
	}
}

func checkFile(file *os.File, config *Config) int {
	var currentSection string

	scanner := bufio.NewScanner(file)
	totalErrors := 0
	for scanner.Scan() {
		line := scanner.Text()

		switch {
		case strings.Contains(line, "sec/op"):
			currentSection = "sec/op"
			continue
		case strings.Contains(line, "B/op"):
			currentSection = "B/op"
			continue
		case strings.Contains(line, "allocs/op"):
			currentSection = "allocs/op"
			continue
		}

		totalErrors += checkLine(config, line, currentSection)
	}

	if err := scanner.Err(); err != nil {
		fmt.Printf("Error reading results file: %v\n", err)
	}

	return totalErrors
}

func checkLine(
	config *Config,
	line string,
	section string,
) int {
	if line == "" {
		return 0
	}
	csvRowReader := csv.NewReader(strings.NewReader(line))
	csvRowReader.Comment = '#'
	csvRowReader.Comma = ','

	row, err := csvRowReader.Read()
	if err != nil {
		fmt.Printf("failed parsing CSV line %s with erorr: %v\n", line, err)
		os.Exit(1)
	}

	if len(row) != 7 {
		// ignore all except the becnhmark result lines with exactly 7 columns
		return 0
	}

	// The "geomean" represents a statistical summary (geometric mean) of multiple test results,
	// not an individual test result, hence we should just skip it
	if row[0] == "geomean" {
		return 0
	}

	normalizedTestName := normalizeTestName(row[0])

	oldValue, err := strconv.ParseFloat(row[1], 64)
	if err != nil {
		fmt.Printf("⚠️ Error parsing float: %v\n", err)
		return 1
	}
	oldChangeStr := row[2]
	newValue, err := strconv.ParseFloat(row[3], 64)
	if err != nil {
		fmt.Printf("⚠️ Error parsing float: %v\n", err)
		return 1
	}
	newChangeStr := row[4]

	oldChange, err := strconv.ParseFloat(strings.TrimSuffix(oldChangeStr, "%"), 64)
	if err != nil {
		fmt.Printf("⚠️ Error parsing float: %v\n", err)
		return 1
	}
	newChange, err := strconv.ParseFloat(strings.TrimSuffix(newChangeStr, "%"), 64)
	if err != nil {
		fmt.Printf("⚠️ Error parsing float: %v\n", err)
		return 1
	}

	threshold := getThresholdForTestCase(config, normalizedTestName, section)

	if math.Abs(oldChange-newChange) > threshold {
		fmt.Printf("❌ Change in section %s for test %s exceeds threshold: %s\n", section, normalizedTestName, newChangeStr)
		return 1
	}

	b := &Benchmark{
		TestName: row[0],
		OldValue: oldValue,
		OldCI:    oldChange,
		NewValue: newValue,
		NewCI:    newChange,
		VsBase:   row[5],
		P:        row[6],
	}

	fmt.Printf("Debug: bench [TestName %s], [OldValue %f], [OldCI %f], [NewValue %f], [NewCI %f], [VsBase %s], [P %s]\n", b.TestName, b.OldValue, b.OldCI, b.NewValue, b.NewCI, b.VsBase, b.P)

	return 0
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

func checkLine2(
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
