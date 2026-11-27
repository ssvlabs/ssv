package testing

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	typescomparable "github.com/ssvlabs/ssv-spec/types/testingutils/comparable"
)

const (
	specModule      = "github.com/ssvlabs/ssv-spec"
	specCacheDirEnv = "SSV_SPEC_CACHE_DIR"
)

// GenerateSpecTestJSON returns tests.json, every spec test of the given ssv-spec module ("qbft" or "ssv") keyed by
// its type and name, at the version specModuleDir resolves from path. The result is cached under specArtifactsDir,
// assembled from the tests the module ships or, for versions that ship none, produced by the module's generator.
func GenerateSpecTestJSON(path string, module string) ([]byte, error) {
	p, err := specModuleDir(path, module)
	if err != nil {
		return nil, fmt.Errorf("could not get spec test dir: %w", err)
	}

	p = filepath.Join(p, "spectest", "generate")

	artifactDir := specArtifactsDir(module, p)
	if err := os.MkdirAll(artifactDir, 0750); err != nil {
		return nil, fmt.Errorf("failed to create spec artifacts directory: %w", err)
	}
	testJSONPath := filepath.Join(artifactDir, "tests.json")
	scaffoldOut := filepath.Join(artifactDir, "spec-tests", module)

	// Use the cached tests.json if the state-comparison vectors are there too: in-module for pre-split
	// spec versions, else generated along with it.
	// #nosec G304 -- test helper reads from a controlled cache path.
	jsonBytes, err := os.ReadFile(testJSONPath)
	if err == nil && len(jsonBytes) > 0 && (pathExists(filepath.Join(p, "state_comparison")) || pathExists(filepath.Join(scaffoldOut, "state_comparison"))) {
		return jsonBytes, nil
	}

	// Pre-split spec versions (e.g. the alan pin) ship their tests in-module.
	jsonBytes, err = buildTestsJSONFromDir(filepath.Join(p, "tests"))
	if err == nil {
		_ = os.WriteFile(testJSONPath, jsonBytes, 0600)
		return jsonBytes, nil
	}

	// Split versions moved their tests to the ssvlabs/spec-tests repo, which the module cache doesn't
	// hold, so build and run the module's generator.
	binaryPath := filepath.Join(artifactDir, module)
	//nolint: gosec
	cmdBuild := exec.Command("go", "build", "-o", binaryPath, ".")
	cmdBuild.Dir = p
	buildOutput, err := cmdBuild.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("go build failed: %w; output: %s", err, buildOutput)
	}

	// The generator writes tests.json, tests/ and state_comparison/ to <go.mod root of its working
	// directory>/../spec-tests/<module>, a sibling spec-tests checkout in a working copy. Run from a
	// scaffold directory with a go.mod, it writes them to scaffoldOut.
	scaffoldRoot := filepath.Join(artifactDir, "specrun")
	if err := os.MkdirAll(scaffoldRoot, 0750); err != nil {
		return nil, fmt.Errorf("failed to create generator scaffold directory: %w", err)
	}
	if err := os.WriteFile(filepath.Join(scaffoldRoot, "go.mod"), []byte("module spectest-scaffold\n"), 0600); err != nil {
		return nil, fmt.Errorf("failed to write generator scaffold go.mod: %w", err)
	}
	//nolint: gosec
	cmdRun := exec.Command(binaryPath)
	cmdRun.Dir = scaffoldRoot
	runOutput, err := cmdRun.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to run binary: %w; output: %s", err, runOutput)
	}

	// #nosec G304 -- test helper reads from a controlled cache path.
	jsonBytes, err = os.ReadFile(filepath.Join(scaffoldOut, "tests.json"))
	if err != nil {
		return nil, fmt.Errorf("failed to read tests.json: %w", err)
	}
	// #nosec G703 -- test helper writes to a controlled cache path.
	if err := os.WriteFile(testJSONPath, jsonBytes, 0600); err != nil {
		return nil, fmt.Errorf("failed to cache tests.json: %w", err)
	}

	// Keep state_comparison/, which stateComparisonDir serves, and drop the rest to keep the cache small.
	_ = os.Remove(binaryPath)
	_ = os.RemoveAll(scaffoldRoot)
	_ = os.RemoveAll(filepath.Join(scaffoldOut, "tests"))
	_ = os.Remove(filepath.Join(scaffoldOut, "tests.json"))

	return jsonBytes, nil
}

// pathExists reports whether path exists.
func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// stateComparisonDir returns the directory containing the module's state_comparison/ vectors. Pre-split spec
// versions (e.g. the alan pin) ship them in-module, under <module>/spectest/generate. For split versions,
// GenerateSpecTestJSON generates them into the artifact cache, and this runs it when they're missing.
func stateComparisonDir(module string) (string, error) {
	specDir, err := specModuleDir("", module)
	if err != nil {
		return "", fmt.Errorf("could not get spec dir: %w", err)
	}
	generateDir := filepath.Join(specDir, "spectest", "generate")
	if pathExists(filepath.Join(generateDir, "state_comparison")) {
		return generateDir, nil
	}
	scaffoldOut := filepath.Join(specArtifactsDir(module, generateDir), "spec-tests", module)
	if pathExists(filepath.Join(scaffoldOut, "state_comparison")) {
		return scaffoldOut, nil
	}
	if _, err := GenerateSpecTestJSON("", module); err != nil {
		return "", fmt.Errorf("state-comparison vectors unavailable and generation failed: %w", err)
	}
	if !pathExists(filepath.Join(scaffoldOut, "state_comparison")) {
		return "", fmt.Errorf("state-comparison vectors missing after generation under %s", scaffoldOut)
	}
	return scaffoldOut, nil
}

// ReadStateComparisonFile reads the state-comparison JSON of the given spec module's ("qbft" or "ssv") test.
func ReadStateComparisonFile(module, testName, testType string) ([]byte, error) {
	root, err := stateComparisonDir(module)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(typescomparable.GetSCDir(root, testType), fmt.Sprintf("%s.json", testName))
	// #nosec G304 -- test helper reads from a controlled cache/module path.
	return os.ReadFile(filepath.Clean(path))
}

// UnmarshalStateComparison is the spec's typescomparable.UnmarshalStateComparison on top of
// stateComparisonDir: the spec's own helper reads a sibling spec-tests checkout, which the module
// cache doesn't have.
func UnmarshalStateComparison[T spectypes.Root](module, testName, testType string, targetState T) (T, error) {
	var nilT T
	byteValue, err := ReadStateComparisonFile(module, testName, testType)
	if err != nil {
		return nilT, err
	}
	if err := json.Unmarshal(byteValue, targetState); err != nil {
		return nilT, err
	}
	return targetState, nil
}

// specArtifactsDir returns the cache directory for the vectors of a spec generator path, one per module and
// spec version, under $SSV_SPEC_CACHE_DIR or else <tmp>/ssv-spec-cache.
func specArtifactsDir(module, specGeneratePath string) string {
	base := os.Getenv(specCacheDirEnv)
	if base == "" {
		base = filepath.Join(os.TempDir(), "ssv-spec-cache")
	}

	sum := sha256.Sum256([]byte(specGeneratePath))
	artifactKey := module + "-" + hex.EncodeToString(sum[:8])
	return filepath.Join(base, artifactKey)
}

// buildTestsJSONFromDir assembles tests.json from the per-test files a pre-split spec version ships.
func buildTestsJSONFromDir(testsDir string) ([]byte, error) {
	entries, err := os.ReadDir(testsDir)
	if err != nil {
		return nil, fmt.Errorf("read tests directory: %w", err)
	}

	tests := make(map[string]json.RawMessage, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		// Test *tests.ControllerSpecTest_<name> ships as tests.ControllerSpecTest_<name>.json.
		key := strings.TrimSuffix(entry.Name(), ".json")
		if strings.Index(key, "_") > 0 {
			key = "*" + key
		}
		testPath := filepath.Join(testsDir, entry.Name())
		// #nosec G304 -- testsDir is controlled by the test harness.
		content, readErr := os.ReadFile(testPath)
		if readErr != nil {
			return nil, fmt.Errorf("read test file %s: %w", entry.Name(), readErr)
		}
		if len(content) == 0 {
			continue
		}
		tests[key] = json.RawMessage(content)
	}

	if len(tests) == 0 {
		return nil, errors.New("no pre-generated tests found")
	}

	output, err := json.Marshal(tests)
	if err != nil {
		return nil, fmt.Errorf("marshal tests map: %w", err)
	}

	return output, nil
}

// specModuleDir returns the directory of the given ssv-spec module ("qbft" or "ssv") at the version pinned by the
// module file (see specGoModFilename) nearest to path, or to the working directory if path is empty. It follows a
// replace directive.
func specModuleDir(path, module string) (string, error) {
	if path == "" {
		var err error
		path, err = os.Getwd()
		if err != nil {
			return "", errors.New("could not get current directory")
		}
	}
	root, err := findGoModDir(path)
	if err != nil {
		return "", err
	}
	goModFile, err := parseGoModFile(root)
	if err != nil {
		return "", errors.New("could not get go.mod file")
	}

	var modPath, modVersion string
	var replace *modfile.Replace
	for _, r := range goModFile.Replace {
		if strings.EqualFold(specModule, r.Old.Path) {
			replace = r
			break
		}
	}

	if replace != nil {
		modPath = replace.New.Path
		modVersion = replace.New.Version
		if modVersion == "" {
			// A replacement without a version is a local directory, not a module in the cache.
			dir := modPath
			if !filepath.IsAbs(dir) {
				dir = filepath.Join(root, dir)
			}
			if _, err := os.Stat(dir); err != nil {
				return "", fmt.Errorf("local replace directory for %s not found: %w", specModule, err)
			}
			return filepath.Join(filepath.Clean(dir), module), nil
		}
	} else {
		var req *modfile.Require
		for _, r := range goModFile.Require {
			if strings.EqualFold(specModule, r.Mod.Path) {
				req = r
				break
			}
		}
		if req == nil {
			return "", fmt.Errorf("could not find %s module", specModule)
		}
		modPath = req.Mod.Path
		modVersion = req.Mod.Version
	}

	p, err := moduleCachePath(modPath, modVersion)
	if err != nil {
		return "", fmt.Errorf("could not get module path: %w", err)
	}

	if _, err := os.Stat(p); os.IsNotExist(err) {
		return "", fmt.Errorf("you don't have this module-%s/version-%s installed: %w", modPath, modVersion, err)
	}

	return filepath.Join(filepath.Clean(p), module), nil
}

// moduleCachePath returns where the module cache holds the given module version.
func moduleCachePath(name, version string) (string, error) {
	cache, err := moduleCacheDir()
	if err != nil {
		return "", err
	}

	escapedPath, err := module.EscapePath(name)
	if err != nil {
		return "", err
	}

	escapedVersion, err := module.EscapeVersion(version)
	if err != nil {
		return "", err
	}

	return filepath.Join(cache, escapedPath+"@"+escapedVersion), nil
}

// moduleCache holds the module cache directory once moduleCacheDir has resolved it.
var moduleCache struct {
	sync.Mutex
	dir string
}

// moduleCacheDir resolves the module cache directory (from the environment, see moduleCacheDirFromEnv, else
// through `go env GOMODCACHE`) once per process. Every spec test reads its vectors through here, and forking
// `go env` from each of them at once has hung -race runs on macOS, where a child stalled before exec keeps the
// others' pipes open. A failed lookup isn't kept, so the next caller retries it.
func moduleCacheDir() (string, error) {
	moduleCache.Lock()
	defer moduleCache.Unlock()
	if moduleCache.dir != "" {
		return moduleCache.dir, nil
	}

	dir, ok := moduleCacheDirFromEnv(os.Getenv("GOMODCACHE"), os.Getenv("GOPATH"))
	if !ok {
		out, err := exec.Command("go", "env", "GOMODCACHE").Output()
		if err != nil {
			return "", fmt.Errorf("could not resolve GOMODCACHE: %w", err)
		}
		dir = strings.TrimSpace(string(out))
	}
	if dir == "" {
		return "", errors.New("could not resolve module cache path")
	}
	moduleCache.dir = dir
	return dir, nil
}

// moduleCacheDirFromEnv applies the go command's default for the module cache: GOMODCACHE, else pkg/mod
// under the first GOPATH entry. It reports false when neither applies, leaving the answer to `go env`.
func moduleCacheDirFromEnv(goModCache, goPath string) (string, bool) {
	if goModCache != "" {
		return goModCache, true
	}
	if list := filepath.SplitList(goPath); len(list) > 0 && list[0] != "" {
		return filepath.Join(list[0], "pkg", "mod"), true
	}
	return "", false
}

// findGoModDir walks up from path to the directory containing the module file.
func findGoModDir(path string) (string, error) {
	modFileName := specGoModFilename()
	for {
		if _, err := os.Stat(filepath.Join(path, modFileName)); err == nil {
			return path, nil
		}
		path = filepath.Dir(path)
		if path == "/" {
			return "", fmt.Errorf("could not find %s file", modFileName)
		}
	}
}

// parseGoModFile reads and parses the module file in root (as located by findGoModDir).
func parseGoModFile(root string) (*modfile.File, error) {
	modFileName := specGoModFilename()
	// #nosec G304 -- modFileName is selected by build tags from fixed constants.
	buf, err := os.ReadFile(filepath.Join(filepath.Clean(root), modFileName))
	if err != nil {
		return nil, fmt.Errorf("could not read %s", modFileName)
	}

	return modfile.Parse(modFileName, buf, nil)
}
