package validator

import (
	"encoding/hex"
	api "github.com/attestantio/go-eth2-client/api/v1"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/beacon/valcheck"
	"github.com/bloxapp/ssv/fixtures"
	"github.com/bloxapp/ssv/ibft"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network/local"
	"github.com/bloxapp/ssv/network/msgqueue"
	beacon2 "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/utils/threshold"
	"github.com/bloxapp/ssv/validator/storage"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
	"time"
)

var (
	refAttestationDataByts = _byteArray("000000000000000000000000000000003a43a4bf26fb5947e809c1f24f7dc6857c8ac007e535d48e6e4eca2122fd776b0000000000000000000000000000000000000000000000000000000000000000000000000000000002000000000000003a43a4bf26fb5947e809c1f24f7dc6857c8ac007e535d48e6e4eca2122fd776b")

	//refSk                  = _byteArray("2c083f2c8fc923fa2bd32a70ab72b4b46247e8c1f347adc30b2f8036a355086c")
	refPk = fixtures.RefPk

	refSplitShares        = fixtures.RefSplitShares
	refSplitSharesPubKeys = fixtures.RefSplitSharesPubKeys

	refAttestationSplitSigs = [][]byte{
		_byteArray("90d44ba2e926c07a71086d3edd04d433746a80335c828f415c0dcb505a1357a454e94338a2139b201d031e4aa6294f3110caa5f2f9ecdd3727fcc9b3ea733e1819993ba06d175cfc55525515d46ef035d1c8bf5c9dab7536b51d936708aeaa22"),
		_byteArray("8edac629489ceda10b88d4241615cbf5fc8727daba4978276af62fd93069b5d4a8264f3881e0151d364ecef292fd8930114f59c98b1794b546399e48882573024d6237092807a21a45afd2baa1e43c81690997cb0b38f6bc10a74b7e18ed1ff5"),
		_byteArray("b28d49731ba2c7dd227ffcea5755e3126ae1101f7c014fb837777ba61c07c7bf1e0a8560f4867691badb0e9bb87ed026199ceecfa7618b0f05acf7c7bbfed66a524b5bb3417e3e25b68dfc2c55f8f3d9f9b12c3967d7742059453324f8b3e46f"),
		_byteArray("890a3eb48f9189be5a53452c156a0725a67c7cc2178fd5505d13349b8e05963ed6fdcd9239dafb0cdecf8c306e400358000f014ba5db49ab8a2355eaafba38e79fb65f15ec7e80d2b259e19a96cc4383ae974a74ec7d69ce17e404965338fcdf"),
	}

	refAttestationSig = _byteArray("b4fa352d2d6dbdf884266af7ea0914451929b343527ea6c1737ac93b3dde8b7c98e6ce61d68b7a2e7b7af8f8d0fd429d0bdd5f930b83e6842bf4342d3d1d3d10fc0d15bab7649bb8aa8287ca104a1f79d396ce0217bb5cd3e6503a3bce4c9776")
	refSigRoot        = _byteArray("ae1f95e7f59eb99862ba7b3666a71a01facf4524e5922c6cb8f3b964a5041962")
)

func _byteArray(input string) []byte {
	res, _ := hex.DecodeString(input)
	return res
}

/**
testIBFT
*/
type testIBFT struct {
	decided         bool
	signaturesCount int
	identifier      []byte
}

func (t *testIBFT) Init() error {
	pk := &bls.PublicKey{}
	_ = pk.Deserialize(refPk)
	return nil
}

func (t *testIBFT) StartInstance(opts ibft.ControllerStartInstanceOptions) (*ibft.InstanceResult, error) {
	return &ibft.InstanceResult{
		Decided: t.decided,
		Msg: &proto.SignedMessage{
			Message: &proto.Message{
				Value: opts.Value,
			},
			SignerIds: make([]uint64, t.signaturesCount),
		},
	}, nil
}

// GetIBFTCommittee returns a map of the iBFT committee where the key is the member's id.
func (t *testIBFT) GetIBFTCommittee() map[uint64]*proto.Node {
	return map[uint64]*proto.Node{
		1: {
			IbftId: 1,
			Pk:     refSplitSharesPubKeys[0],
		},
		2: {
			IbftId: 2,
			Pk:     refSplitSharesPubKeys[1],
		},
		3: {
			IbftId: 3,
			Pk:     refSplitSharesPubKeys[2],
		},
		4: {
			IbftId: 4,
			Pk:     refSplitSharesPubKeys[3],
		},
	}
}

func (t *testIBFT) GetIdentifier() []byte {
	return t.identifier
}

func (t *testIBFT) NextSeqNumber() (uint64, error) {
	return 0, nil
}

/**
testBeacon
*/
type testBeacon struct {
	refAttestationData       *spec.AttestationData
	LastSubmittedAttestation *spec.Attestation
}

func newTestBeacon(t *testing.T) *testBeacon {
	ret := &testBeacon{}
	ret.refAttestationData = &spec.AttestationData{}
	err := ret.refAttestationData.UnmarshalSSZ(refAttestationDataByts) // ignore error
	require.NoError(t, err)
	return ret
}

func (b *testBeacon) ExtendIndexMap(index spec.ValidatorIndex, pubKey spec.BLSPubKey) {
}

func (b *testBeacon) StartReceivingBlocks() {

}

func (b *testBeacon) GetDuties(epoch spec.Epoch, validatorIndices []spec.ValidatorIndex) ([]*beacon2.Duty, error) {
	return nil, nil
}

func (b *testBeacon) GetValidatorData(validatorPubKeys []spec.BLSPubKey) (map[spec.ValidatorIndex]*api.Validator, error) {
	return nil, nil
}

func (b *testBeacon) GetAttestationData(slot spec.Slot, committeeIndex spec.CommitteeIndex) (*spec.AttestationData, error) {
	return b.refAttestationData, nil
}

func (b *testBeacon) SignAttestation(data *spec.AttestationData, duty *beacon2.Duty, pk []byte) (*spec.Attestation, []byte, error) {
	sig := spec.BLSSignature{}
	copy(sig[:], refAttestationSplitSigs[0])
	return &spec.Attestation{
		AggregationBits: nil,
		Data:            data,
		Signature:       sig,
	}, refSigRoot, nil
}

func (b *testBeacon) SubmitAttestation(attestation *spec.Attestation) error {
	b.LastSubmittedAttestation = attestation
	return nil
}

func (b *testBeacon) SubscribeToCommitteeSubnet(subscription []*api.BeaconCommitteeSubscription) error {
	panic("implement me")
}

func (b *testBeacon) AddShare(shareKey *bls.SecretKey) error {
	panic("implement me")
}

func (b *testBeacon) SignIBFTMessage(message *proto.Message, pk []byte) ([]byte, error) {
	panic("implement me")
}

func (b *testBeacon) GetDomain(data *spec.AttestationData) ([]byte, error) {
	panic("implement")
}
func (b *testBeacon) ComputeSigningRoot(object interface{}, domain []byte) ([32]byte, error) {
	panic("implement")
}

func testingValidator(t *testing.T, decided bool, signaturesCount int, identifier []byte) *Validator {
	threshold.Init()

	ret := &Validator{}
	ret.beacon = newTestBeacon(t)
	ret.logger = zap.L()
	ret.ibfts = make(map[beacon2.RoleType]ibft.Controller)
	ret.ibfts[beacon2.RoleTypeAttester] = &testIBFT{decided: decided, signaturesCount: signaturesCount}
	ret.ibfts[beacon2.RoleTypeAttester].(*testIBFT).identifier = identifier
	require.NoError(t, ret.ibfts[beacon2.RoleTypeAttester].Init())
	ret.valueCheck = valcheck.New()
	ret.signer = ret.beacon

	// nodes
	ret.network = local.NewLocalNetwork()
	ret.msgQueue = msgqueue.New()

	// validatorStorage pk
	pk := &bls.PublicKey{}
	err := pk.Deserialize(refPk)

	ret.Share = &storage.Share{
		NodeID:    1,
		PublicKey: pk,
		Committee: map[uint64]*proto.Node{
			1: {
				IbftId: 1,
				Pk:     refSplitSharesPubKeys[0],
			},
			2: {
				IbftId: 2,
				Pk:     refSplitSharesPubKeys[1],
			},
			3: {
				IbftId: 3,
				Pk:     refSplitSharesPubKeys[2],
			},
			4: {
				IbftId: 4,
				Pk:     refSplitSharesPubKeys[3],
			},
		},
	}

	require.NoError(t, err)

	// timeout
	ret.signatureCollectionTimeout = time.Second * 2

	go ret.listenToSignatureMessages()
	return ret
}

// GenerateNodes generates randomly nodes
func GenerateNodes(cnt int) (map[uint64]*bls.SecretKey, map[uint64]*proto.Node) {
	_ = bls.Init(bls.BLS12_381)
	nodes := make(map[uint64]*proto.Node)
	sks := make(map[uint64]*bls.SecretKey)
	for i := 1; i <= cnt; i++ {
		sk := &bls.SecretKey{}
		sk.SetByCSPRNG()

		nodes[uint64(i)] = &proto.Node{
			IbftId: uint64(i),
			Pk:     sk.GetPublicKey().Serialize(),
		}
		sks[uint64(i)] = sk
	}
	return sks, nodes
}
