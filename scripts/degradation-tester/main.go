package main

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

func main() {

	if len(os.Args) < 3 {
		fmt.Println("Usage: degradation-tester <config_filename> <results_filename>")
		os.Exit(1)
	}

	configFilename := os.Args[1]
	resultsFilename := os.Args[2]

	cfg, err := loadConfig(configFilename)
	if err != nil {
		fmt.Printf("Error loading conifg: %v", err)
		os.Exit(1)
	}

	file, err := os.Open(resultsFilename)
	if err != nil {
		fmt.Printf("Error opening results file: %v", err)
		os.Exit(1)
	}
	defer func() {
		_ = file.Close()
	}()

	scenario := DetermineScenarioType(file)
	var checkResult *DegradationCheckResult
	switch scenario {
	case CompactScenarioType:
		checkResult = checkFileCompact(file, cfg)
		break
	case FullScenarioType:
		checkResult = checkFileFull(file, cfg)
		break
	case UnknownScenarioType:
		panic("Unknown format of becnhstat file")
	}

	if checkResult.TotalIssues > 0 {
		for section, issues := range checkResult.Issues {
			for _, issue := range issues {
				fmt.Printf("❌ Test %s / %s in section %s exceeds thereshold of %.1f%%, with diff %.8f%%!\n", checkResult.PkgName, issue.TestName, section, issue.Threshold, issue.Diff)
			}
		}

		os.Exit(1)
	}
}

func DetermineScenarioType(file *os.File) int {
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()

		if strings.Contains(line, "old.txt") {
			if strings.Contains(line, "new.txt") {
				return CompactScenarioType
			}
			return FullScenarioType
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Printf("Error reading results file: %v\n", err)
	}

	return UnknownScenarioType
}

func checkFileCompact(file *os.File, cfg *Config) *DegradationCheckResult {
	var currentSection string
	var currentPkgPath string

	oldRes := NewBenchmarkResult(currentPkgPath)
	newRes := NewBenchmarkResult(currentPkgPath)

	scanner := bufio.NewScanner(file)

	ignoreBErrorsRegexp := regexp.MustCompile(`B\d+:`)

	for scanner.Scan() {
		line := scanner.Text()

		switch {
		case line == "",
			ignoreBErrorsRegexp.MatchString(line),
			strings.HasPrefix(line, "pkg:"),
			strings.HasPrefix(line, "cpu:"),
			strings.HasPrefix(line, "goarch:"),
			strings.HasPrefix(line, "goos:"),
			strings.Contains(line, "old.txt") && strings.Contains(line, "new.txt"),
			strings.Contains(line, "geomean"):
			continue
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

		err := handleDoubleResultRow(line, currentSection, oldRes, newRes)
		if err != nil {
			panic(err)
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Printf("Error reading results file: %v\n", err)
	}

	return CheckDegradation(cfg, oldRes, newRes)
}

func checkFileFull(file *os.File, cfg *Config) *DegradationCheckResult {
	var currentSection string
	var currentPkgPath string

	var oldRes, newRes, curRes *BenchmarkResult

	scanner := bufio.NewScanner(file)

	ignoreBErrorsRegexp := regexp.MustCompile(`B\d+:`)
	for scanner.Scan() {
		line := scanner.Text()

		switch {
		case line == "",
			ignoreBErrorsRegexp.MatchString(line),
			strings.HasPrefix(line, "pkg:"),
			strings.HasPrefix(line, "cpu:"),
			strings.HasPrefix(line, "goarch:"),
			strings.HasPrefix(line, "goos:"),
			strings.Contains(line, "geomean"):
			continue
		case strings.Contains(line, "old.txt"):
			if oldRes != nil {
				continue
			}
			row := MustParseCsvLine(line)
			currentPkgPath = row[1]
			oldRes = NewBenchmarkResult(currentPkgPath)
			curRes = oldRes
			continue
		case strings.Contains(line, "new.txt"):
			if newRes != nil {
				continue
			}
			row := MustParseCsvLine(line)
			currentPkgPath = row[1]
			newRes = NewBenchmarkResult(currentPkgPath)
			curRes = newRes
			continue
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

		err := handleSingleResultRow(line, currentSection, curRes)
		if err != nil {
			panic(err)
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Printf("Error reading results file: %v\n", err)
	}

	return CheckDegradation(cfg, oldRes, newRes)
}

func handleSingleResultRow(
	line string,
	section string,
	results *BenchmarkResult,
) error {
	row := MustParseCsvLine(line)
	return results.MustFillTestResult(section, row...)
}

func handleDoubleResultRow(
	line string,
	section string,
	oldRes *BenchmarkResult,
	newRes *BenchmarkResult,
) error {
	row := MustParseCsvLine(line)
	if err := oldRes.MustFillTestResult(section, row...); err != nil {
		return err
	}

	newResRow := make([]string, 3)
	newResRow[0] = row[0]
	newResRow[1] = row[3]
	newResRow[2] = row[4]
	if err := newRes.MustFillTestResult(section, newResRow...); err != nil {
		return err
	}
	return nil
}

func loadConfig(filename string) (*Config, error) {
	config := &Config{}
	file, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("Error reading config file: %v\n", err)
	}
	err = yaml.Unmarshal(file, &config)
	if err != nil {
		return nil, fmt.Errorf("Error parsing config file: %v\n", err)
	}

	config.opThresholds = make(map[string]float64)
	config.allocThresholds = make(map[string]float64)

	for _, pkg := range config.Packages {
		for _, testCase := range pkg.Tests {
			if testCase.OpDelta > 0 {
				config.opThresholds[testCase.Name] = testCase.OpDelta
			}
			if testCase.AllocDelta > 0 {
				config.allocThresholds[testCase.Name] = testCase.AllocDelta
			}
		}
	}
	return config, nil
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
		if threshold, exists := config.opThresholds[testName]; exists {
			return threshold
		}
		return config.DefaultSecOpDelta
	case "B/op":
		if threshold, exists := config.opThresholds[testName]; exists {
			return threshold
		}
		return config.DefaultBOpDelta
	case "allocs/op":
		if threshold, exists := config.allocThresholds[testName]; exists {
			return threshold
		}
		return config.DefaultAllocOpDelta
	default:
		return 0.0
	}
}

func CheckDegradation(
	cfg *Config,
	oldRes *BenchmarkResult,
	newRes *BenchmarkResult,
) *DegradationCheckResult {
	checkRes := NewDegradationCheckResult(newRes.Pkg)
	for section, oldTests := range oldRes.Res {
		newTests, exists := newRes.Res[section]
		if !exists {
			panic(fmt.Sprintf("❌ Section %s not found in new benchmarks of pkg %s. Please manualy update the benchmarks!\n", section, newRes.Pkg))
		}

		for testName, oldTest := range oldTests {
			newTest, exists := newTests[testName]
			if !exists {
				panic(fmt.Sprintf("❌ Test %s not found in new benchmarks of pkg %s. Please manualy update the benchmarks!\n", testName, newRes.Pkg))
			}

			threshold := getThresholdForTestCase(cfg, testName, section)

			a := newTest.Value
			b := oldTest.Value

			// Floats comparison. Swap values if `a` is greater than `b`.
			if a-b > 1e-6 {
				a, b = b, a
			}

			// Calculate the absolute change in percentage
			absChange := (b - a) / a * 100.0

			if absChange > threshold {
				checkRes.TotalIssues++
				checkRes.Issues[section] = append(checkRes.Issues[section], &DegradationIssue{
					TestName:  testName,
					Diff:      absChange,
					Threshold: threshold,
				})
			}
		}
	}
	return checkRes
}

func MustParseCsvLine(line string) []string {
	csvRowReader := csv.NewReader(strings.NewReader(line))
	csvRowReader.Comment = '#'
	csvRowReader.Comma = ','

	row, err := csvRowReader.Read()
	if err != nil {
		panic(fmt.Sprintf("failed parsing CSV line %s with erorr: %v\n", line, err))
	}

	return row
}
