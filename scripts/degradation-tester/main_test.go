package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseBenchStatFile(t *testing.T) {
	benchstatCsvFileName := "benchstat.csv"
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

	// clear test artefacts
	defer func() {
		_ = os.Remove(benchstatCsvFileName)
	}()
}

const stubBenchStatCSVFileData = `
goos: darwin
goarch: amd64
pkg: github.com/bloxapp/ssv/message/validation
cpu: xxxx
,./scripts/degradation-tester/benchmarks/validation_results_old.txt,,./scripts/degradation-tester/benchmarks/validation_results_new.txt
,sec/op,CI,sec/op,CI,vs base,P
VerifyRSASignature-8,0.000266723,2%,0.0002658285,1%,~,p=0.123 n=10
geomean,0.0002667229999999998,,0.0002658284999999999,,-0.34%

,./scripts/degradation-tester/benchmarks/validation_results_old.txt,,./scripts/degradation-tester/benchmarks/validation_results_new.txt
,B/op,CI,B/op,CI,vs base,P
VerifyRSASignature-8,1904.5,22%,1892.5,22%,~,p=0.927 n=10
geomean,1904.4999999999998,,1892.4999999999995,,-0.63%

,./scripts/degradation-tester/benchmarks/validation_results_old.txt,,./scripts/degradation-tester/benchmarks/validation_results_new.txt
,allocs/op,CI,allocs/op,CI,vs base,P
VerifyRSASignature-8,9.5,16%,9.5,16%,~,p=0.878 n=10
geomean,9.500000000000002,,9.500000000000002,,+0.00%
`
