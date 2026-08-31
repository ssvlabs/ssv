package storage

import (
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv-spec/types/testingutils"
	typescomparable "github.com/ssvlabs/ssv-spec/types/testingutils/comparable"

	"github.com/ssvlabs/ssv/ssvsigner/keys/rsaencryption"
)

var (
	specModule = "github.com/ssvlabs/ssv-spec"
)

const specCacheDirEnv = "SSV_SPEC_CACHE_DIR"

// TODO: add missing tests

// GenerateOperatorSigner generates randomly nodes
func GenerateOperatorSigner(oids ...spectypes.OperatorID) ([]*rsa.PrivateKey, []*spectypes.Operator) {
	nodes := make([]*spectypes.Operator, 0, len(oids))
	sks := make([]*rsa.PrivateKey, 0, len(oids))

	for i := range oids {
		pubKey, privKey, err := rsaencryption.GenerateKeyPairPEM()
		if err != nil {
			panic(err)
		}
		opKey, err := rsaencryption.PEMToPrivateKey(privKey)
		if err != nil {
			panic(err)
		}

		nodes = append(nodes, &spectypes.Operator{
			OperatorID:        oids[i],
			SSVOperatorPubKey: pubKey,
		})

		sks = append(sks, opKey)
	}

	return sks, nodes
}

// MsgGenerator represents a message generator
type MsgGenerator func(height specqbft.Height) ([]spectypes.OperatorID, *specqbft.Message)

// CreateMultipleStoredInstances enables to create multiple stored instances (with decided messages).
func CreateMultipleStoredInstances(
	sks []*rsa.PrivateKey,
	start specqbft.Height,
	end specqbft.Height,
	generator MsgGenerator,
) ([]*StoredInstance, error) {
	results := make([]*StoredInstance, 0)
	for i := start; i <= end; i++ {
		signers, msg := generator(i)
		if msg == nil {
			break
		}
		sm := testingutils.MultiSignQBFTMsg(sks, signers, msg)

		var qbftMsg specqbft.Message
		if err := qbftMsg.Decode(sm.SSVMessage.Data); err != nil {
			return nil, err
		}

		results = append(results, &StoredInstance{
			State: &specqbft.State{
				ID:                   qbftMsg.Identifier,
				Round:                qbftMsg.Round,
				Height:               qbftMsg.Height,
				LastPreparedRound:    qbftMsg.Round,
				LastPreparedValue:    sm.FullData,
				Decided:              true,
				DecidedValue:         sm.FullData,
				ProposeContainer:     specqbft.NewMsgContainer(),
				PrepareContainer:     specqbft.NewMsgContainer(),
				CommitContainer:      specqbft.NewMsgContainer(),
				RoundChangeContainer: specqbft.NewMsgContainer(),
			},
			DecidedMessage: sm,
		})
	}
	return results, nil
}

// SignMsg handle MultiSignMsg error and return just specqbft.SignedMessage
func SignMsg(t *testing.T, sks []*rsa.PrivateKey, signers []spectypes.OperatorID, msg *specqbft.Message) *spectypes.SignedSSVMessage {
	return testingutils.MultiSignQBFTMsg(sks, signers, msg)
}

func GenerateSpecTestJSON(path string, module string) ([]byte, error) {
	// Step 1: Get the spec directory.
	p, err := GetSpecDir(path, module)
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

	// Fast path: use already-generated tests.json from the local artifact cache, provided the
	// state-comparison vectors the per-test overrides read later are also available (in-module for
	// pre-split spec versions, or previously regenerated into the artifact scaffold).
	// #nosec G304 -- test helper reads from a controlled cache path.
	jsonBytes, err := os.ReadFile(testJSONPath)
	if err == nil && len(jsonBytes) > 0 && (pathExists(filepath.Join(p, "state_comparison")) || pathExists(filepath.Join(scaffoldOut, "state_comparison"))) {
		return jsonBytes, nil
	}

	// Fast path for pre-split spec versions (e.g. the alan pin): build tests.json from the
	// pre-generated files the ssv-spec module ships in-module.
	jsonBytes, err = buildTestsJSONFromDir(filepath.Join(p, "tests"))
	if err == nil {
		_ = os.WriteFile(testJSONPath, jsonBytes, 0600)
		return jsonBytes, nil
	}

	// Split layout: newer ssv-spec versions ship no generated vectors in-module (they moved to the
	// sibling ssvlabs/spec-tests repo, which a module-cache checkout cannot host), so build and run
	// the module's generator to produce them locally.

	// Step 2: Build the Go package, outputting an executable to the artifact directory.
	binaryPath := filepath.Join(artifactDir, module)
	//nolint: gosec
	cmdBuild := exec.Command("go", "build", "-o", binaryPath, ".")
	cmdBuild.Dir = p
	buildOutput, err := cmdBuild.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("go build failed: %w; output: %s", err, buildOutput)
	}

	// Step 3: Execute the built binary. The split-layout generator resolves its output root as
	// <go.mod root of cwd>/../spec-tests/<module> (the sibling spec-tests checkout in a working
	// copy), so run it from a scaffold directory carrying a go.mod: it then writes tests.json,
	// tests/ and state_comparison/ under <artifactDir>/spec-tests/<module>.
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

	// Step 4: Read the generated tests.json file and cache it at the canonical path.
	// #nosec G304 -- test helper reads from a controlled cache path.
	jsonBytes, err = os.ReadFile(filepath.Join(scaffoldOut, "tests.json"))
	if err != nil {
		return nil, fmt.Errorf("failed to read tests.json: %w", err)
	}
	// #nosec G703 -- test helper writes to a controlled cache path.
	if err := os.WriteFile(testJSONPath, jsonBytes, 0600); err != nil {
		return nil, fmt.Errorf("failed to cache tests.json: %w", err)
	}

	// Keep spec-tests/<module>/state_comparison — the per-test overrides read it — and drop what
	// nothing reads again, to keep the artifact cache small.
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

// StateComparisonDir returns the directory holding the module's state_comparison vectors (the
// parent of the state_comparison folder), across both ssv-spec layouts:
//   - pre-split versions (e.g. the alan pin) ship the vectors in-module under
//     <module>/spectest/generate/state_comparison;
//   - split versions moved them to the sibling ssvlabs/spec-tests repo, which the module cache
//     cannot host, so they are regenerated into the local artifact cache by GenerateSpecTestJSON
//     (triggered here when missing).
func StateComparisonDir(module string) (string, error) {
	specDir, err := GetSpecDir("", module)
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

// ReadStateComparisonFile reads the state-comparison JSON for (testName, testType) of the given
// spec module ("qbft" or "ssv"), resolving the vectors dir via StateComparisonDir.
func ReadStateComparisonFile(module, testName, testType string) ([]byte, error) {
	root, err := StateComparisonDir(module)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(typescomparable.GetSCDir(root, testType), fmt.Sprintf("%s.json", testName))
	// #nosec G304 -- test helper reads from a controlled cache/module path.
	return os.ReadFile(filepath.Clean(path))
}

// UnmarshalStateComparison mirrors the spec's typescomparable.UnmarshalStateComparison on top of
// the layout-aware StateComparisonDir resolution (the spec's own helper hardcodes the split
// layout's sibling-checkout paths, which do not exist under the Go module cache).
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

func specArtifactsDir(module, specGeneratePath string) string {
	base := os.Getenv(specCacheDirEnv)
	if base == "" {
		base = filepath.Join(os.TempDir(), "ssv-spec-cache")
	}

	sum := sha256.Sum256([]byte(specGeneratePath))
	artifactKey := module + "-" + hex.EncodeToString(sum[:8])
	return filepath.Join(base, artifactKey)
}

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

		key := strings.TrimSuffix(entry.Name(), ".json")
		if split := strings.Index(key, "_"); split > 0 {
			key = "*" + key[:split] + key[split:]
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

// GetSpecDir returns the path to the ssv-spec module.
func GetSpecDir(path, module string) (string, error) {
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

	// check if there is a replace
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
			// A version-less replace target is a local directory, not a module in the cache
			// (go.mod semantics: a replacement path without a version must be a directory).
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
		// get from require
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

	// get module path
	p, err := GetModulePath(modPath, modVersion)
	if err != nil {
		return "", fmt.Errorf("could not get module path: %w", err)
	}

	if _, err := os.Stat(p); os.IsNotExist(err) {
		return "", fmt.Errorf("you don't have this module-%s/version-%s installed: %w", modPath, modVersion, err)
	}

	return filepath.Join(filepath.Clean(p), module), nil
}

func GetModulePath(name, version string) (string, error) {
	// first we need GOMODCACHE
	cache, ok := os.LookupEnv("GOMODCACHE")
	if !ok || cache == "" {
		if goPath := os.Getenv("GOPATH"); goPath != "" {
			cache = path.Join(goPath, "pkg", "mod")
		} else {
			out, err := exec.Command("go", "env", "GOMODCACHE").Output()
			if err != nil {
				return "", fmt.Errorf("could not resolve GOMODCACHE: %w", err)
			}
			cache = strings.TrimSpace(string(out))
		}
	}
	if cache == "" {
		return "", errors.New("could not resolve module cache path")
	}

	// then we need to escape path
	escapedPath, err := module.EscapePath(name)
	if err != nil {
		return "", err
	}

	// version also
	escapedVersion, err := module.EscapeVersion(version)
	if err != nil {
		return "", err
	}

	return path.Join(cache, escapedPath+"@"+escapedVersion), nil
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
	// The alan_spec build resolves the ssv-spec version from go.spec.alan.mod instead of
	// go.mod, so the spec-test vectors come from the alan (pre-Boole) spec release.
	modFileName := specGoModFilename()

	// #nosec G304 -- modFileName is selected by build tags from fixed constants.
	buf, err := os.ReadFile(filepath.Join(filepath.Clean(root), modFileName))
	if err != nil {
		return nil, fmt.Errorf("could not read %s", modFileName)
	}

	return modfile.Parse(modFileName, buf, nil)
}
