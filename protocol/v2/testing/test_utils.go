package testing

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	spectestingutils "github.com/bloxapp/ssv-spec/types/testingutils"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/storage"
	p2pv1 "github.com/bloxapp/ssv/network/p2p"
	"github.com/bloxapp/ssv/operator/validator"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	qbftstorage "github.com/bloxapp/ssv/protocol/v2/qbft/storage"
	validatorprotocol "github.com/bloxapp/ssv/protocol/v2/ssv/validator"
	"github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/bloxapp/ssv/utils/rsaencryption"
	"github.com/bloxapp/ssv/utils/threshold"
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

// CreateShare creates a new beacon.Share
func CreateShare(operators [][]byte) (*types.SSVShare, map[uint64]*bls.SecretKey, error) {
	threshold.Init()

	sk := &bls.SecretKey{}
	sk.SetByCSPRNG()

	m, err := threshold.Create(sk.Serialize(), 3, 4)
	if err != nil {
		return nil, nil, err
	}

	committee := make([]*spectypes.Operator, 0, len(operators))
	for i := 0; i < len(operators); i++ {
		oid := spectypes.OperatorID(i + 1)
		committee = append(committee, &spectypes.Operator{
			OperatorID: oid,
			PubKey:     m[uint64(oid)].GetPublicKey().Serialize(),
		})
	}

	return &types.SSVShare{
		Share: spectypes.Share{
			OperatorID:      1,
			ValidatorPubKey: sk.GetPublicKey().Serialize(),
			Committee:       committee,
		},
		Metadata: types.Metadata{
			BeaconMetadata: &beaconprotocol.ValidatorMetadata{},
			OwnerAddress:   "0x0",
			Operators:      operators,
			Liquidated:     false,
		},
	}, m, nil
}

// CreateShareAndValidators creates a share and the corresponding validators objects
func CreateShareAndValidators(
	ctx context.Context,
	logger *zap.Logger,
	net *p2pv1.LocalNet,
	kms []spectypes.KeyManager,
	stores []*storage.QBFTStores,
) (
	*types.SSVShare,
	map[uint64]*bls.SecretKey,
	[]*validatorprotocol.Validator,
	error,
) {
	validators := make([]*validatorprotocol.Validator, 0)
	operators := make([][]byte, 0)
	for _, k := range net.NodeKeys {
		pub, err := rsaencryption.ExtractPublicKey(k.OperatorKey)
		if err != nil {
			return nil, nil, nil, err
		}
		operators = append(operators, []byte(pub))
	}

	// create share
	share, sks, err := CreateShare(operators)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("could not create share: %w", err)
	}
	// add to key-managers and subscribe to topic
	for i, km := range kms {
		err = km.AddShare(sks[uint64(i+1)])
		if err != nil {
			return nil, nil, nil, err
		}

		options := validatorprotocol.Options{
			Storage: stores[i],
			Network: net.Nodes[i],
			SSVShare: &types.SSVShare{
				Share: spectypes.Share{
					OperatorID:      spectypes.OperatorID(i + 1),
					ValidatorPubKey: share.ValidatorPubKey,
					SharePubKey:     share.SharePubKey,
					Committee:       share.Committee,
					Quorum:          share.Quorum,
					PartialQuorum:   share.PartialQuorum,
					DomainType:      share.DomainType,
					Graffiti:        share.Graffiti,
				},
				Metadata: types.Metadata{
					BeaconMetadata: &beaconprotocol.ValidatorMetadata{
						Balance: share.BeaconMetadata.Balance,
						Status:  share.BeaconMetadata.Status,
						Index:   spec.ValidatorIndex(i),
					},
					OwnerAddress: share.OwnerAddress,
					Operators:    share.Operators,
					Liquidated:   share.Liquidated,
				},
			},
			Beacon: spectestingutils.NewTestingBeaconNode(),
			Signer: km,
		}

		l := logger.With(zap.String("w", fmt.Sprintf("node-%d", i)))
		val := validatorprotocol.NewValidator(ctx, options)
		val.DutyRunners = validator.SetupRunners(ctx, l, options)
		validators = append(validators, val)
	}
	return share, sks, validators, nil
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
				LastPreparedValue:    sm.Message.Data,
				Decided:              true,
				DecidedValue:         sm.Message.Data,
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
	return sk.SignByte(root), nil
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
		Message:   msg,
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

// NewInMemDb returns basedb.IDb with in-memory type
func NewInMemDb() basedb.IDb {
	db, _ := kv.New(basedb.Options{
		Type:   "badger-memory",
		Path:   "",
		Logger: zap.L(),
	})
	return db
}

// CommitDataToBytes encode commit data and handle error if exist
func CommitDataToBytes(t *testing.T, input *specqbft.CommitData) []byte {
	ret, err := json.Marshal(input)
	require.NoError(t, err)
	return ret
}
