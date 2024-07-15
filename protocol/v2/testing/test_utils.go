package testing

import (
	"archive/tar"
	"compress/gzip"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
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

	qbftstorage "github.com/ssvlabs/ssv/protocol/v2/qbft/storage"
	"github.com/ssvlabs/ssv/utils/rsaencryption"
)

var (
	specModule   = "github.com/ssvlabs/ssv-spec"
	specTestPath = "spectest/generate/tests.json"
)

// TODO: add missing tests

// GenerateOperatorSigner generates randomly nodes
func GenerateOperatorSigner(oids ...spectypes.OperatorID) ([]*rsa.PrivateKey, []*spectypes.Operator) {
	nodes := make([]*spectypes.Operator, 0, len(oids))
	sks := make([]*rsa.PrivateKey, 0, len(oids))

	for i := range oids {
		pubKey, privKey, err := rsaencryption.GenerateKeys()
		if err != nil {
			panic(err)
		}
		opKey, err := rsaencryption.PemToPrivateKey(privKey)
		if err != nil {
			panic(err)
		}

		nodes = append(nodes, &spectypes.Operator{
			OperatorID:        spectypes.OperatorID(i),
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
) ([]*qbftstorage.StoredInstance, error) {
	results := make([]*qbftstorage.StoredInstance, 0)
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

		results = append(results, &qbftstorage.StoredInstance{
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

func GetSSVMappingSpecTestJSON(path string, module string) ([]byte, error) {
	p, err := GetSpecDir(path, module)
	if err != nil {
		return nil, errors.Wrap(err, "could not get spec test dir")
	}
	gzPath := filepath.Join(p, "spectest", "generate", "tests.json.gz")
	untypedTests := map[string]interface{}{}

	file, err := os.Open(gzPath)
	if err != nil {
		return nil, errors.Wrap(err, "failed to open gzip file")
	}
	defer file.Close()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create gzip reader")
	}
	defer gzipReader.Close()

	decompressedData, err := io.ReadAll(gzipReader)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read decompressed data")
	}

	if err := json.Unmarshal(decompressedData, &untypedTests); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal JSON")
	}
	return decompressedData, nil
}

func GetSpecTestJSON(path string, module string) ([]byte, error) {
	p, err := GetSpecDir(path, module)
	if err != nil {
		return nil, fmt.Errorf("could not get spec test dir: %w", err)
	}
	return os.ReadFile(filepath.Join(filepath.Clean(p), filepath.Clean(specTestPath)))
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
	if !ok {
		cache = path.Join(os.Getenv("GOPATH"), "pkg", "mod")
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

func ExtractTarGz(gzipStream io.Reader) {
	uncompressedStream, err := gzip.NewReader(gzipStream)
	if err != nil {
		log.Fatal("ExtractTarGz: NewReader failed")
	}

	tarReader := tar.NewReader(uncompressedStream)

	for true {
		header, err := tarReader.Next()

		if err == io.EOF {
			break
		}

		if err != nil {
			log.Fatalf("ExtractTarGz: Next() failed: %s", err.Error())
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.Mkdir(header.Name, 0755); err != nil {
				log.Fatalf("ExtractTarGz: Mkdir() failed: %s", err.Error())
			}
		case tar.TypeReg:
			outFile, err := os.Create(header.Name)
			if err != nil {
				log.Fatalf("ExtractTarGz: Create() failed: %s", err.Error())
			}
			if _, err := io.Copy(outFile, tarReader); err != nil {
				log.Fatalf("ExtractTarGz: Copy() failed: %s", err.Error())
			}
			outFile.Close()

		default:
			log.Fatalf(
				"ExtractTarGz: uknown type: %s in %s",
				header.Typeflag,
				header.Name)
		}

	}
}

func unpackTestsJson(path string) error {
	r, err := os.Open(fmt.Sprintf("%s.gz", path))
	if err != nil {
		errors.Wrap(err, "could not open file")
	}
	ExtractTarGz(r)

	return nil
}
