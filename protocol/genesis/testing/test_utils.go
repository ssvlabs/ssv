package testing

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	"github.com/stretchr/testify/require"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"

	qbftstorage "github.com/ssvlabs/ssv/protocol/genesis/qbft/storage"
	"github.com/ssvlabs/ssv/protocol/genesis/types"
)

var (
	specModule   = "github.com/ssvlabs/ssv-spec-pre-cc"
	specTestPath = "spectest/generate/tests.json"
)

// TODO: add missing tests

// GenerateBLSKeys generates randomly nodes
func GenerateBLSKeys(oids ...genesisspectypes.OperatorID) (map[genesisspectypes.OperatorID]*bls.SecretKey, []*genesisspectypes.Operator) {
	_ = bls.Init(bls.BLS12_381)

	nodes := make([]*genesisspectypes.Operator, 0)
	sks := make(map[genesisspectypes.OperatorID]*bls.SecretKey)

	for i, oid := range oids {
		sk := &bls.SecretKey{}
		sk.SetByCSPRNG()

		nodes = append(nodes, &genesisspectypes.Operator{
			OperatorID: genesisspectypes.OperatorID(i),
			PubKey:     sk.GetPublicKey().Serialize(),
		})
		sks[oid] = sk
	}

	return sks, nodes
}

// MsgGenerator represents a message generator
type MsgGenerator func(height genesisspecqbft.Height) ([]genesisspectypes.OperatorID, *genesisspecqbft.Message)

// CreateMultipleStoredInstances enables to create multiple stored instances (with decided messages).
func CreateMultipleStoredInstances(
	sks map[genesisspectypes.OperatorID]*bls.SecretKey,
	start genesisspecqbft.Height,
	end genesisspecqbft.Height,
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
			State: &genesisspecqbft.State{
				ID:                   sm.Message.Identifier,
				Round:                sm.Message.Round,
				Height:               sm.Message.Height,
				LastPreparedRound:    sm.Message.Round,
				LastPreparedValue:    sm.FullData,
				Decided:              true,
				DecidedValue:         sm.FullData,
				ProposeContainer:     genesisspecqbft.NewMsgContainer(),
				PrepareContainer:     genesisspecqbft.NewMsgContainer(),
				CommitContainer:      genesisspecqbft.NewMsgContainer(),
				RoundChangeContainer: genesisspecqbft.NewMsgContainer(),
			},
			DecidedMessage: sm,
		})
	}
	return results, nil
}

func signMessage(msg *genesisspecqbft.Message, sk *bls.SecretKey) (*bls.Sign, error) {
	signatureDomain := genesisspectypes.ComputeSignatureDomain(types.GetDefaultDomain(), genesisspectypes.QBFTSignatureType)
	root, err := genesisspectypes.ComputeSigningRoot(msg, signatureDomain)
	if err != nil {
		return nil, err
	}
	return sk.SignByte(root[:]), nil
}

// MultiSignMsg signs a msg with multiple signers
func MultiSignMsg(sks map[genesisspectypes.OperatorID]*bls.SecretKey, signers []genesisspectypes.OperatorID, msg *genesisspecqbft.Message) (*genesisspecqbft.SignedMessage, error) {
	_ = bls.Init(bls.BLS12_381)

	var operators = make([]genesisspectypes.OperatorID, 0)
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

	return &genesisspecqbft.SignedMessage{
		Message:   *msg,
		Signature: agg.Serialize(),
		Signers:   operators,
	}, nil
}

// SignMsg handle MultiSignMsg error and return just genesisspecqbft.SignedMessage
func SignMsg(t *testing.T, sks map[genesisspectypes.OperatorID]*bls.SecretKey, signers []genesisspectypes.OperatorID, msg *genesisspecqbft.Message) *genesisspecqbft.SignedMessage {
	res, err := MultiSignMsg(sks, signers, msg)
	require.NoError(t, err)
	return res
}

// AggregateSign sign genesisspecqbft.Message and then aggregate
func AggregateSign(t *testing.T, sks map[genesisspectypes.OperatorID]*bls.SecretKey, signers []genesisspectypes.OperatorID, consensusMessage *genesisspecqbft.Message) *genesisspecqbft.SignedMessage {
	signedMsg := SignMsg(t, sks, signers, consensusMessage)
	// TODO: use SignMsg instead of AggregateSign
	// require.NoError(t, sigSignMsgnedMsg.Aggregate(signedMsg))
	return signedMsg
}

// AggregateInvalidSign sign genesisspecqbft.Message and then change the signer id to mock invalid sig
func AggregateInvalidSign(t *testing.T, sks map[genesisspectypes.OperatorID]*bls.SecretKey, consensusMessage *genesisspecqbft.Message) *genesisspecqbft.SignedMessage {
	sigend := SignMsg(t, sks, []genesisspectypes.OperatorID{1}, consensusMessage)
	sigend.Signers = []genesisspectypes.OperatorID{2}
	return sigend
}

func GetSpecTestJSON(path string, module string) ([]byte, error) {
	p, err := GetSpecDir(path, module)
	if err != nil {
		return nil, fmt.Errorf("could not get spec test dir: %w", err)
	}
	return os.ReadFile(filepath.Join(filepath.Clean(p), filepath.Clean(specTestPath)))
}

// GetSpecDir returns the path to the ssv-spec-pre-cc module.
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
