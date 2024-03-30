package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckLineWithValidData(t *testing.T) {
	config := &Config{
		DefaultOpDelta:    10,
		DefaultAllocDelta: 10,
	}
	line := "VerifyRSASignature-8,0.000266723,2%,0.0002658285,1%,~,p=0.123 n=10"
	section := "sec/op"

	errors := checkLine(config, line, section)

	require.Equal(t, 0, errors)
}

func TestCheckLineWithInvalidData(t *testing.T) {
	config := &Config{
		DefaultOpDelta:    10,
		DefaultAllocDelta: 10,
	}
	line := "VerifyRSASignature-8,0.000266723,2%,0.0002658285,1%,~,p=0.123 n=10"
	section := "invalid_section"

	errors := checkLine(config, line, section)

	require.Equal(t, 1, errors)
}

func TestLoadConfigWithValidFile(t *testing.T) {
	configFilename := "valid_config.yaml"
	file, err := os.Create(configFilename)
	defer func() {
		_ = os.Remove(configFilename)
	}()
	require.NoError(t, err)

	_, err = file.WriteString(stubConfig)
	require.NoError(t, err)

	config, err := loadConfig(configFilename)

	require.NoError(t, err)
	require.NotNil(t, config)
	require.Equal(t, 4.2, config.DefaultOpDelta)
	require.Equal(t, 2.3, config.DefaultAllocDelta)
}

func TestLoadConfigWithInvalidFile(t *testing.T) {
	configFilename := "invalid_config.yaml"
	_, err := loadConfig(configFilename)
	require.Error(t, err)
}

func TestNormalizeTestName(t *testing.T) {
	testName := "VerifyRSASignature-8"
	expected := "VerifyRSASignature"

	result := normalizeTestName(testName)

	require.Equal(t, expected, result)
}

func TestGetThresholdForTestCase(t *testing.T) {
	config := &Config{
		DefaultOpDelta:    10,
		DefaultAllocDelta: 10,
	}
	t.Run("TestGetThresholdForTestCaseWithDefaultSection", func(t *testing.T) {
		testName := "VerifyRSASignature"
		section := "sec/op"

		require.Equal(t, 10.0, getThresholdForTestCase(config, testName, section))
	})
	t.Run("TestGetThresholdForTestCaseWithDefinedValue", func(t *testing.T) {
		opThresholds = make(map[string]float64)
		allocThresholds = make(map[string]float64)

		testName := "VerifyRSASignature"

		opThresholds[testName] = 42.0
		allocThresholds[testName] = 23.0

		require.Equal(t, opThresholds[testName], getThresholdForTestCase(config, testName, "sec/op"))
		require.Equal(t, allocThresholds[testName], getThresholdForTestCase(config, testName, "allocs/op"))
	})
}

func TestParseBenchStatFile(t *testing.T) {
	benchstatCsvFileName := "benchstat.csv"
	defer func() {
		_ = os.Remove(benchstatCsvFileName)
	}()
	f, err := os.Create(benchstatCsvFileName)
	require.NoError(t, err)

	_, err = f.WriteString(stubBenchStatCSVFileData)
	require.NoError(t, err)

	cfg := &Config{
		DefaultAllocDelta: 10,
		DefaultOpDelta:    10,
	}
	f, err = os.Open(benchstatCsvFileName)
	require.NoError(t, err)
	totalErrors := checkFile(f, cfg)
	require.Equal(t, 0, totalErrors)
}

const stubBenchStatCSVFileData = `
goos: darwin
goarch: amd64
pkg: github.com/bloxapp/ssv/message/validation
cpu: xxxx
,./scripts/degradation-tester/benchmarks/message_validation_benchmarks_old.txt,,./scripts/degradation-tester/benchmarks/validation_results_new.txt
,sec/op,CI,sec/op,CI,vs base,P
VerifyRSASignature-8,0.000266723,2%,0.0002658285,1%,~,p=0.123 n=10
geomean,0.0002667229999999998,,0.0002658284999999999,,-0.34%

,./scripts/degradation-tester/benchmarks/message_validation_benchmarks_old.txt,,./scripts/degradation-tester/benchmarks/validation_results_new.txt
,B/op,CI,B/op,CI,vs base,P
VerifyRSASignature-8,1904.5,22%,1892.5,22%,~,p=0.927 n=10
geomean,1904.4999999999998,,1892.4999999999995,,-0.63%

,./scripts/degradation-tester/benchmarks/message_validation_benchmarks_old.txt,,./scripts/degradation-tester/benchmarks/validation_results_new.txt
,allocs/op,CI,allocs/op,CI,vs base,P
VerifyRSASignature-8,9.5,16%,9.5,16%,~,p=0.878 n=10
geomean,9.500000000000002,,9.500000000000002,,+0.00%
`

const stubConfig = `
DefaultOpDelta: 4.2
DefaultAllocDelta: 2.3
Packages:
  - Path: "./some/package/path"
    Tests:
      - Name: "VerifySomeSignature"
        OpDelta: 10.0
        AllocDelta: 4.5
`
