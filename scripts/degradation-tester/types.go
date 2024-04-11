package main

import "encoding/json"

type Config struct {
	DefaultSecOpDelta   float64                  `yaml:"DefaultSecOpDelta"`
	DefaultBOpDelta     float64                  `yaml:"DefaultBOpDelta"`
	DefaultAllocOpDelta float64                  `yaml:"DefaultAllocOpDelta"`
	Packages            []BenchmarkingTestConfig `yaml:"Packages"`
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

const (
	SectionTypeSecOp   = "sec/op"
	SectionTypeBop     = "B/op"
	SectionTypeAllocOp = "allocs/op"
)

type TestResult struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
	//Value *big.Float `json:"value"`
	CI string `json:"ci"`
}

type BenchmarkResult struct {
	Pkg string                            `json:"pkg"`
	Res map[string]map[string]*TestResult `json:"res"`
}

func (r *BenchmarkResult) MustMarshalJsonIndent() []byte {
	jsonBytes, err := json.MarshalIndent(r, "", "	")
	if err != nil {
		panic(err)
	}
	return jsonBytes
}

func NewBenchmarkResult(pkg string) *BenchmarkResult {
	r := &BenchmarkResult{
		Pkg: pkg,
		Res: make(map[string]map[string]*TestResult),
	}
	r.Res = make(map[string]map[string]*TestResult)
	r.Res[SectionTypeSecOp] = make(map[string]*TestResult)
	r.Res[SectionTypeBop] = make(map[string]*TestResult)
	r.Res[SectionTypeAllocOp] = make(map[string]*TestResult)
	return r
}

type DegradationIssue struct {
	TestName  string
	Threshold float64
	Diff      float64
}

type DegradationCheckResult struct {
	PkgName     string
	TotalIssues uint32
	Issues      map[string][]*DegradationIssue
}

func NewDegradationCheckResult(pkgName string) *DegradationCheckResult {
	dc := &DegradationCheckResult{
		PkgName: pkgName,
		Issues:  make(map[string][]*DegradationIssue),
	}
	dc.Issues = make(map[string][]*DegradationIssue)
	return dc
}
