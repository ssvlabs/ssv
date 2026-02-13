package storage

import (
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv-spec/types/testingutils"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"

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

	// Fast path: use already-generated tests.json from the local artifact cache.
	// #nosec G304 -- test helper reads from a controlled cache path.
	jsonBytes, err := os.ReadFile(testJSONPath)
	if err == nil && len(jsonBytes) > 0 {
		return jsonBytes, nil
	}

	// Fast path for first CI run: build tests.json from pre-generated files in ssv-spec module.
	jsonBytes, err = buildTestsJSONFromDir(filepath.Join(p, "tests"))
	if err == nil {
		_ = os.WriteFile(testJSONPath, jsonBytes, 0600)
		return jsonBytes, nil
	}

	// Step 2: Build the Go package, outputting an executable to the artifact directory.
	binaryPath := filepath.Join(artifactDir, module)
	//nolint: gosec
	cmdBuild := exec.Command("go", "build", "-o", binaryPath, ".")
	cmdBuild.Dir = p
	buildOutput, err := cmdBuild.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("go build failed: %w; output: %s", err, buildOutput)
	}

	// Step 3: Execute the built binary.
	// It is assumed that running the binary generates tests.json in artifactDir.
	//nolint: gosec
	cmdRun := exec.Command(binaryPath)
	cmdRun.Dir = artifactDir
	runOutput, err := cmdRun.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to run binary: %w; output: %s", err, runOutput)
	}

	// Step 4: Read the generated tests.json file.
	//nolint: gosec
	jsonBytes, err = os.ReadFile(testJSONPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read tests.json: %w", err)
	}

	// Keep only tests.json to keep artifact cache size small.
	_ = os.Remove(binaryPath)
	_ = os.RemoveAll(filepath.Join(artifactDir, "tests"))
	_ = os.RemoveAll(filepath.Join(artifactDir, "state_comparison"))

	return jsonBytes, nil
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
	goModFile, err := getGoModFile(path)
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
			return "", errors.Errorf("could not find %s module", specModule)
		}
		modPath = req.Mod.Path
		modVersion = req.Mod.Version
	}

	// get module path
	p, err := GetModulePath(modPath, modVersion)
	if err != nil {
		return "", errors.Wrap(err, "could not get module path")
	}

	if _, err := os.Stat(p); os.IsNotExist(err) {
		return "", errors.Wrapf(err, "you don't have this module-%s/version-%s installed", modPath, modVersion)
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

func getGoModFile(path string) (*modfile.File, error) {
	// find project root path
	for {
		if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
			break
		}
		path = filepath.Dir(path)
		if path == "/" {
			return nil, errors.New("could not find go.mod file")
		}
	}

	// read go.mod
	buf, err := os.ReadFile(filepath.Join(filepath.Clean(path), "go.mod"))
	if err != nil {
		return nil, errors.New("could not read go.mod")
	}

	// parse go.mod
	return modfile.Parse("go.mod", buf, nil)
}
