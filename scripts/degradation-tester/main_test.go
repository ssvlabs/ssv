package main

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckLineWithValidData(t *testing.T) {
	line := "VerifyRSASignature-8,0.000266723,2%"
	section := "sec/op"

	res := NewBenchmarkResult("pkg")
	require.NoError(t, handleResultRow(line, section, res))
	require.Equal(t, 1, len(res.Res[section]))
	require.Equal(t, 0.000266723, res.Res[section]["VerifyRSASignature"].Value)
}

func TestCheckLineWithInvalidData(t *testing.T) {
	line := "VerifyRSASignature-8,0.000266723,2%,0.0002658285,1%,~,p=0.123 n=10"
	section := "invalid_section"

	res := NewBenchmarkResult("pkg")
	require.Error(t, handleResultRow(line, section, res))
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
	require.Equal(t, 4.2, config.DefaultSecOpDelta)
	require.Equal(t, 1.0, config.DefaultBOpDelta)
	require.Equal(t, 2.3, config.DefaultAllocOpDelta)
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

	_, err = f.WriteString(benchstatResultOpenSSLUpgrade)
	require.NoError(t, err)

	cfg := &Config{
		DefaultSecOpDelta:   10,
		DefaultBOpDelta:     10,
		DefaultAllocOpDelta: 10,
	}
	f, err = os.Open(benchstatCsvFileName)
	require.NoError(t, err)
	checkResult := checkFile(f, cfg)
	require.Equal(t, uint32(3), checkResult.TotalIssues)
	for _, err := range checkResult.Issues[SectionTypeSecOp] {
		fmt.Println(err)
	}
	for _, err := range checkResult.Issues[SectionTypeBop] {
		fmt.Println(err)
	}
	for _, err := range checkResult.Issues[SectionTypeAllocOp] {
		fmt.Println(err)
	}
}

var benchFast = `
goos: darwin
goarch: arm64
pkg: github.com/bloxapp/ssv/scripts/degradation-tester
BenchmarkFastFunc-12    	       1	3001104583 ns/op	      96 B/op	       3 allocs/op
BenchmarkFastFunc-12    	       1	3000802917 ns/op	      80 B/op	       1 allocs/op
BenchmarkFastFunc-12    	       1	3001080000 ns/op	      96 B/op	       2 allocs/op
BenchmarkFastFunc-12    	       1	3000279167 ns/op	      80 B/op	       1 allocs/op
BenchmarkFastFunc-12    	       1	3001074167 ns/op	      80 B/op	       1 allocs/op
BenchmarkFastFunc-12    	       1	3000398125 ns/op	      80 B/op	       1 allocs/op
BenchmarkFastFunc-12    	       1	3001061375 ns/op	      80 B/op	       1 allocs/op
BenchmarkFastFunc-12    	       1	3001055667 ns/op	      80 B/op	       1 allocs/op
BenchmarkFastFunc-12    	       1	3001052167 ns/op	      80 B/op	       1 allocs/op
BenchmarkFastFunc-12    	       1	3000777625 ns/op	      96 B/op	       2 allocs/op
BenchmarkFastFunc-12    	       1	3000908750 ns/op	      80 B/op	       1 allocs/op
BenchmarkFastFunc-12    	       1	3001065125 ns/op	      80 B/op	       1 allocs/op
BenchmarkFastFunc-12    	       1	3001063541 ns/op	      80 B/op	       1 allocs/op
BenchmarkFastFunc-12    	       1	3000293458 ns/op	      80 B/op	       1 allocs/op
BenchmarkFastFunc-12    	       1	3001074041 ns/op	      80 B/op	       1 allocs/op
BenchmarkFastFunc-12    	       1	3001058000 ns/op	      80 B/op	       1 allocs/op
BenchmarkFastFunc-12    	       1	3000674750 ns/op	      80 B/op	       1 allocs/op
BenchmarkFastFunc-12    	       1	3000571333 ns/op	      80 B/op	       1 allocs/op
BenchmarkFastFunc-12    	       1	3000453709 ns/op	      80 B/op	       1 allocs/op
BenchmarkFastFunc-12    	       1	3001055292 ns/op	      80 B/op	       1 allocs/op
PASS
ok  	github.com/bloxapp/ssv/scripts/degradation-tester	60.693s
`

var benchSlow = `
goos: darwin
goarch: arm64
pkg: github.com/bloxapp/ssv/scripts/degradation-tester
BenchmarkFastFunc-12    	       1	4001147542 ns/op	    5512 B/op	       7 allocs/op
BenchmarkFastFunc-12    	       1	3001054875 ns/op	      96 B/op	       2 allocs/op
BenchmarkFastFunc-12    	       1	4001054000 ns/op	      88 B/op	       2 allocs/op
BenchmarkFastFunc-12    	       1	3001111166 ns/op	      80 B/op	       1 allocs/op
BenchmarkFastFunc-12    	       1	4001046500 ns/op	      80 B/op	       1 allocs/op
BenchmarkFastFunc-12    	       1	5000376375 ns/op	      80 B/op	       1 allocs/op
BenchmarkFastFunc-12    	       1	5001059584 ns/op	      80 B/op	       1 allocs/op
BenchmarkFastFunc-12    	       1	5001063167 ns/op	      80 B/op	       1 allocs/op
BenchmarkFastFunc-12    	       1	3001043833 ns/op	      80 B/op	       1 allocs/op
BenchmarkFastFunc-12    	       1	3000403917 ns/op	      80 B/op	       1 allocs/op
BenchmarkFastFunc-12    	       1	3001043542 ns/op	      96 B/op	       2 allocs/op
BenchmarkFastFunc-12    	       1	3001066250 ns/op	      80 B/op	       1 allocs/op
BenchmarkFastFunc-12    	       1	4001067709 ns/op	      80 B/op	       1 allocs/op
BenchmarkFastFunc-12    	       1	5000513708 ns/op	      80 B/op	       1 allocs/op
BenchmarkFastFunc-12    	       1	3001065958 ns/op	      80 B/op	       1 allocs/op
BenchmarkFastFunc-12    	       1	5000536417 ns/op	      80 B/op	       1 allocs/op
BenchmarkFastFunc-12    	       1	4000695958 ns/op	      80 B/op	       1 allocs/op
BenchmarkFastFunc-12    	       1	3000862750 ns/op	      80 B/op	       1 allocs/op
BenchmarkFastFunc-12    	       1	3000408166 ns/op	      80 B/op	       1 allocs/op
BenchmarkFastFunc-12    	       1	3000444458 ns/op	      80 B/op	       1 allocs/op
PASS
ok  	github.com/bloxapp/ssv/scripts/degradation-tester	75.247s
`

var benchstatResult = `
goos: linux
goarch: amd64
pkg: github.com/bloxapp/ssv/message/validation
cpu: AMD EPYC 7763 64-Core Processor
,scripts/degradation-tester/benchmarks/message_validation_benchmarks_old.txt
,sec/op,CI
VerifyRSASignature-4,0.00025610300000000004,1%
geomean,0.0002561029999999998

,scripts/degradation-tester/benchmarks/message_validation_benchmarks_old.txt
,B/op,CI
VerifyRSASignature-4,1872,22%
geomean,1871.9999999999993

,scripts/degradation-tester/benchmarks/message_validation_benchmarks_old.txt
,allocs/op,CI
VerifyRSASignature-4,9.5,16%
geomean,9.500000000000002

cpu: AMD EPYC 7763 64-Core Processor                
,scripts/degradation-tester/benchmarks/message_validation_benchmarks_new.txt
,sec/op,CI
VerifyRSASignature-4,2.5498e-05,2%
geomean,2.5498e-05

,scripts/degradation-tester/benchmarks/message_validation_benchmarks_new.txt
,B/op,CI
VerifyRSASignature-4,187,28%
geomean,187.00000000000003

,scripts/degradation-tester/benchmarks/message_validation_benchmarks_new.txt
,allocs/op,CI
VerifyRSASignature-4,4,0%
geomean,4

`
var benchstatResult1 = `
B31: summaries must be >0 to compute geomean
B45: summaries must be >0 to compute geomean
B74: summaries must be >0 to compute geomean
B88: summaries must be >0 to compute geomean
goos: linux
goarch: amd64
pkg: github.com/bloxapp/ssv/protocol/v2/types
cpu: AMD EPYC 7763 64-Core Processor
,protocol_v2_types_benchmarks_old.txt
,sec/op,CI
VerifyPKCS1v15OpenSSL-4,2.3903500000000002e-05,1%
SignPKCS1v15OpenSSL-4,0.0006678130000000001,1%
VerifyBLS-4,0.001211613,1%
VerifyPKCS1v15-4,0.00025212400000000005,0%
VerifyPKCS1v15FastHash-4,0.00025184100000000004,0%
VerifyPSS-4,0.000255005,1%
SignBLS-4,0.0005117625,1%
SignPKCS1v15-4,0.0019599385,0%
SignPKCS1v15FastHash-4,0.0019667615,0%
SignPSS-4,0.001962333,0%
geomean,0.0005109289272306781

,protocol_v2_types_benchmarks_old.txt
,B/op,CI
VerifyPKCS1v15OpenSSL-4,40,0%
SignPKCS1v15OpenSSL-4,304,0%
VerifyBLS-4,0,0%
VerifyPKCS1v15-4,912,0%
VerifyPKCS1v15FastHash-4,912,0%
VerifyPSS-4,1120,0%
SignBLS-4,288,0%
SignPKCS1v15-4,896,0%
SignPKCS1v15FastHash-4,896,0%
SignPSS-4,1296,0%
geomean

,protocol_v2_types_benchmarks_old.txt
,allocs/op,CI
VerifyPKCS1v15OpenSSL-4,3,0%
SignPKCS1v15OpenSSL-4,5,0%
VerifyBLS-4,0,0%
VerifyPKCS1v15-4,6,0%
VerifyPKCS1v15FastHash-4,6,0%
VerifyPSS-4,11,0%
SignBLS-4,1,0%
SignPKCS1v15-4,5,0%
SignPKCS1v15FastHash-4,5,0%
SignPSS-4,10,0%
geomean

cpu: AMD EPYC 7763 64-Core Processor
,protocol_v2_types_benchmarks_new.txt
,sec/op,CI
VerifyPKCS1v15OpenSSL-4,2.3847s000000000004e-05,1%
SignPKCS1v15OpenSSL-4,0.0006664995,1%
VerifyBLS-4,0.001211802,0%
VerifyPKCS1v15-4,0.00025212050000000005,0%
VerifyPKCS1v15FastHash-4,0.00025198449999999997,0%
VerifyPSS-4,0.000254662,0%
SignBLS-4,0.0005147915000000001,1%
SignPKCS1v15-4,0.0019562055,0%
SignPKCS1v15FastHash-4,0.0019551660000000004,0%
SignPSS-4,0.0019617575000000003,0%
geomean,0.0005105621525118493

,protocol_v2_types_benchmarks_new.txt
,B/op,CI
VerifyPKCS1v15OpenSSL-4,40,0%
SignPKCS1v15OpenSSL-4,304,0%
VerifyBLS-4,0,0%
VerifyPKCS1v15-4,912,0%
VerifyPKCS1v15FastHash-4,912,0%
VerifyPSS-4,1120,0%
SignBLS-4,288,0%
SignPKCS1v15-4,896,0%
SignPKCS1v15FastHash-4,896,0%
SignPSS-4,1296,0%
geomean

,protocol_v2_types_benchmarks_new.txt
,allocs/op,CI
VerifyPKCS1v15OpenSSL-4,3,0%
SignPKCS1v15OpenSSL-4,5,0%
VerifyBLS-4,0,0%
VerifyPKCS1v15-4,6,0%
VerifyPKCS1v15FastHash-4,6,0%
VerifyPSS-4,11,0%
SignBLS-4,1,0%
SignPKCS1v15-4,5,0%
SignPKCS1v15FastHash-4,5,0%
SignPSS-4,10,0%
geomean
`

const stubConfig = `
DefaultSecOpDelta: 4.2
DefaultBOpDelta: 1.0
DefaultAllocOpDelta: 2.3
Packages:
  - Path: "./some/package/path"
    Tests:
      - Name: "VerifySomeSignature"
        OpDelta: 10.0
        AllocDelta: 4.5
`

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
