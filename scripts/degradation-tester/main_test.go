package main

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckLineWithSingleRowValidData(t *testing.T) {
	line := "VerifyRSASignature-8,0.000266723,2%"
	section := "sec/op"

	res := NewBenchmarkResult("pkg")
	require.NoError(t, handleSingleResultRow(line, section, res))
	require.Equal(t, 1, len(res.Res[section]))
	require.Equal(t, 0.000266723, res.Res[section]["VerifyRSASignature"].Value)
}

func TestCheckLineWithDoubleRowData(t *testing.T) {
	line := "VerifyRSASignature-4,2.5280500000000003e-05,1%,2.54185e-05,0%,+0.55%,p=0.008 n=10+16"
	section := "sec/op"

	oldRes := NewBenchmarkResult("pkg")
	newRes := NewBenchmarkResult("pkg")
	require.NoError(t, handleDoubleResultRow(line, section, oldRes, newRes))
}

func TestNormalizeTestName(t *testing.T) {
	testName := "VerifyRSASignature-8"
	expected := "VerifyRSASignature"

	result := normalizeTestName(testName)

	require.Equal(t, expected, result)
}

func TestGetThresholdForTestCase(t *testing.T) {
	config := &Config{
		DefaultSecOpDelta:   10.0,
		DefaultBOpDelta:     10.0,
		DefaultAllocOpDelta: 10.0,
	}
	t.Run("TestGetThresholdForTestCaseWithDefaultSection", func(t *testing.T) {
		testName := "VerifyRSASignature"
		section := "sec/op"

		require.Equal(t, 10.0, getThresholdForTestCase(config, testName, section))
	})
	t.Run("TestGetThresholdForTestCaseWithDefinedValue", func(t *testing.T) {
		config := &Config{}
		config.opThresholds = make(map[string]float64)
		config.allocThresholds = make(map[string]float64)

		testName := "VerifyRSASignature"

		config.opThresholds[testName] = 42.0
		config.allocThresholds[testName] = 23.0

		require.Equal(t, config.opThresholds[testName], getThresholdForTestCase(config, testName, "sec/op"))
		require.Equal(t, config.allocThresholds[testName], getThresholdForTestCase(config, testName, "allocs/op"))
	})
}

func TestCheckBenchstatFull(t *testing.T) {
	benchstatCsvFileName := "benchstat.csv"
	defer func() {
		_ = os.Remove(benchstatCsvFileName)
	}()
	f, err := os.Create(benchstatCsvFileName)
	require.NoError(t, err)

	_, err = f.WriteString(benchstatResultOpenSSLUpgrade)
	require.NoError(t, err)

	cfg := &Config{
		DefaultSecOpDelta:   10,
		DefaultBOpDelta:     10,
		DefaultAllocOpDelta: 10,
	}

	f, err = os.Open(benchstatCsvFileName)
	require.NoError(t, err)

	checkResult := checkFileFull(f, cfg)

	require.Equal(t, uint32(3), checkResult.TotalIssues)
	require.Equal(t, 1, len(checkResult.Issues[SectionTypeSecOp]))
	require.Equal(t, "VerifyRSASignature", checkResult.Issues[SectionTypeSecOp][0].TestName)
	require.Equal(t, 10.0, checkResult.Issues[SectionTypeSecOp][0].Threshold)
	require.Equal(t, "913.04562805", fmt.Sprintf("%.8f", checkResult.Issues[SectionTypeSecOp][0].Diff))

	require.Equal(t, 1, len(checkResult.Issues[SectionTypeBop]))
	require.Equal(t, "VerifyRSASignature", checkResult.Issues[SectionTypeBop][0].TestName)
	require.Equal(t, 10.0, checkResult.Issues[SectionTypeBop][0].Threshold)
	require.Equal(t, "909.16442049", fmt.Sprintf("%.8f", checkResult.Issues[SectionTypeBop][0].Diff))

	require.Equal(t, 1, len(checkResult.Issues[SectionTypeAllocOp]))
	require.Equal(t, "VerifyRSASignature", checkResult.Issues[SectionTypeAllocOp][0].TestName)
	require.Equal(t, 10.0, checkResult.Issues[SectionTypeAllocOp][0].Threshold)
	require.Equal(t, "137.50000000", fmt.Sprintf("%.8f", checkResult.Issues[SectionTypeAllocOp][0].Diff))
}

func TestCheckBenchstatCompact(t *testing.T) {
	benchstatCsvFileName := "benchstat.csv"
	defer func() {
		_ = os.Remove(benchstatCsvFileName)
	}()
	f, err := os.Create(benchstatCsvFileName)
	require.NoError(t, err)

	_, err = f.WriteString(benchstatCompact)
	require.NoError(t, err)

	cfg := &Config{
		DefaultSecOpDelta:   10,
		DefaultBOpDelta:     10,
		DefaultAllocOpDelta: 10,
	}
	f, err = os.Open(benchstatCsvFileName)
	require.NoError(t, err)
	checkResult := checkFileCompact(f, cfg)

	require.Equal(t, 1, len(checkResult.Issues[SectionTypeBop]))
	require.Equal(t, "VerifyRSASignature", checkResult.Issues[SectionTypeBop][0].TestName)
	require.Equal(t, 10.0, checkResult.Issues[SectionTypeBop][0].Threshold)
	require.Equal(t, "35.30997305", fmt.Sprintf("%.8f", checkResult.Issues[SectionTypeBop][0].Diff))
}

var benchstatResultOpenSSLUpgrade = `
goos: linux
goarch: amd64
pkg: github.com/bloxapp/ssv/message/validation
cpu: AMD EPYC 7763 64-Core Processor
,old.txt
,sec/op,CI
VerifyRSASignature-4,0.00025610300000000004,1%
geomean,0.0002561029999999998

,old.txt
,B/op,CI
VerifyRSASignature-4,1872,22%
geomean,1871.9999999999993

,old.txt
,allocs/op,CI
VerifyRSASignature-4,9.5,16%
geomean,9.500000000000002

cpu: AMD EPYC 7763 64-Core Processor
,new.txt
,sec/op,CI
VerifyRSASignature-4,2.5280500000000003e-05,1%
geomean,2.528050000000001e-05

,new.txt
,B/op,CI
VerifyRSASignature-4,185.5,27%
geomean,185.49999999999994

,new.txt
,allocs/op,CI
VerifyRSASignature-4,4,0%
geomean,4
`
var benchstatCompact = `
goos: linux
goarch: amd64
pkg: github.com/bloxapp/ssv/message/validation
cpu: AMD EPYC 7763 64-Core Processor                
,scripts/degradation-tester/benchmarks/message_validation_benchmarks_old.txt,,scripts/degradation-tester/benchmarks/message_validation_benchmarks_new.txt
,sec/op,CI,sec/op,CI,vs base,P
VerifyRSASignature-4,2.5280500000000003e-05,1%,2.54185e-05,0%,+0.55%,p=0.008 n=10+16
geomean,2.528050000000001e-05,,2.5418499999999984e-05,,+0.55%

,scripts/degradation-tester/benchmarks/message_validation_benchmarks_old.txt,,scripts/degradation-tester/benchmarks/message_validation_benchmarks_new.txt
,B/op,CI,B/op,CI,vs base,P
VerifyRSASignature-4,185.5,27%,251,26%,+35.31%,p=0.020 n=10+16
geomean,185.49999999999994,,250.9999999999999,,+35.31%

,scripts/degradation-tester/benchmarks/message_validation_benchmarks_old.txt,,scripts/degradation-tester/benchmarks/message_validation_benchmarks_new.txt
,allocs/op,CI,allocs/op,CI,vs base,P
VerifyRSASignature-4,4,0%,4,0%,~,p=0.243 n=10+16
geomean,4,,4,,+0.00%
`
