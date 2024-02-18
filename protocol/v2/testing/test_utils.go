package testing

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"

	qbftstorage "github.com/bloxapp/ssv/protocol/v2/qbft/storage"
	"github.com/bloxapp/ssv/protocol/v2/types"
)

var (
	specModule   = "github.com/bloxapp/ssv-spec"
	specTestPath = "spectest/generate/tests.json"
)

// TODO: add missing tests

// GenerateBLSKeys generates randomly nodes
func GenerateBLSKeys(oids ...spectypes.OperatorID) (map[spectypes.OperatorID]*bls.SecretKey, []*spectypes.Operator) {
	_ = bls.Init(bls.BLS12_381)

	nodes := make([]*spectypes.Operator, 0)
	sks := make(map[spectypes.OperatorID]*bls.SecretKey)

	for i, oid := range oids {
		sk := &bls.SecretKey{}
		sk.SetByCSPRNG()

		nodes = append(nodes, &spectypes.Operator{
			OperatorID: spectypes.OperatorID(i),
			PubKey:     sk.GetPublicKey().Serialize(),
		})
		sks[oid] = sk
	}

	return sks, nodes
}

// MsgGenerator represents a message generator
type MsgGenerator func(height specqbft.Height) ([]spectypes.OperatorID, *specqbft.Message)

// CreateMultipleStoredInstances enables to create multiple stored instances (with decided messages).
func CreateMultipleStoredInstances(
	sks map[spectypes.OperatorID]*bls.SecretKey,
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
		sm, err := MultiSignMsg(sks, signers, msg)
		if err != nil {
			return nil, err
		}
		results = append(results, &qbftstorage.StoredInstance{
			State: &specqbft.State{
				ID:                   sm.Message.Identifier,
				Round:                sm.Message.Round,
				Height:               sm.Message.Height,
				LastPreparedRound:    sm.Message.Round,
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

func signMessage(msg *specqbft.Message, sk *bls.SecretKey) (*bls.Sign, error) {
	signatureDomain := spectypes.ComputeSignatureDomain(types.GetDefaultDomain(), spectypes.QBFTSignatureType)
	root, err := spectypes.ComputeSigningRoot(msg, signatureDomain)
	if err != nil {
		return nil, err
	}
	return sk.SignByte(root[:]), nil
}

// MultiSignMsg signs a msg with multiple signers
func MultiSignMsg(sks map[spectypes.OperatorID]*bls.SecretKey, signers []spectypes.OperatorID, msg *specqbft.Message) (*specqbft.SignedMessage, error) {
	_ = bls.Init(bls.BLS12_381)

	var operators = make([]spectypes.OperatorID, 0)
	var agg *bls.Sign
	for _, oid := range signers {
		signature, err := signMessage(msg, sks[oid])
		if err != nil {
			return nil, err
		}
		operators = append(operators, oid)
		if agg == nil {
			agg = signature
		} else {
			agg.Add(signature)
		}
	}

	return &specqbft.SignedMessage{
		Message:   *msg,
		Signature: agg.Serialize(),
		Signers:   operators,
	}, nil
}

// SignMsg handle MultiSignMsg error and return just specqbft.SignedMessage
func SignMsg(t *testing.T, sks map[spectypes.OperatorID]*bls.SecretKey, signers []spectypes.OperatorID, msg *specqbft.Message) *specqbft.SignedMessage {
	res, err := MultiSignMsg(sks, signers, msg)
	require.NoError(t, err)
	return res
}

// AggregateSign sign specqbft.Message and then aggregate
func AggregateSign(t *testing.T, sks map[spectypes.OperatorID]*bls.SecretKey, signers []spectypes.OperatorID, consensusMessage *specqbft.Message) *specqbft.SignedMessage {
	signedMsg := SignMsg(t, sks, signers, consensusMessage)
	// TODO: use SignMsg instead of AggregateSign
	// require.NoError(t, sigSignMsgnedMsg.Aggregate(signedMsg))
	return signedMsg
}

// AggregateInvalidSign sign specqbft.Message and then change the signer id to mock invalid sig
func AggregateInvalidSign(t *testing.T, sks map[spectypes.OperatorID]*bls.SecretKey, consensusMessage *specqbft.Message) *specqbft.SignedMessage {
	sigend := SignMsg(t, sks, []spectypes.OperatorID{1}, consensusMessage)
	sigend.Signers = []spectypes.OperatorID{2}
	return sigend
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
