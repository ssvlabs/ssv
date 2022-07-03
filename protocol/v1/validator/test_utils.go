package validator

import (
	"encoding/hex"
	"fmt"
	"sync"
	"testing"

	api "github.com/attestantio/go-eth2-client/api/v1"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/proto"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	protocolp2p "github.com/bloxapp/ssv/protocol/v1/p2p"
	"github.com/bloxapp/ssv/protocol/v1/qbft/controller"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance"
	"github.com/bloxapp/ssv/protocol/v1/utils/threshold"
)

var (
	refAttestationDataByts = _byteArray("000000000000000000000000000000003a43a4bf26fb5947e809c1f24f7dc6857c8ac007e535d48e6e4eca2122fd776b0000000000000000000000000000000000000000000000000000000000000000000000000000000002000000000000003a43a4bf26fb5947e809c1f24f7dc6857c8ac007e535d48e6e4eca2122fd776b")

	// refSk = _byteArray("2c083f2c8fc923fa2bd32a70ab72b4b46247e8c1f347adc30b2f8036a355086c")
	refPk = _byteArray("a9cf360aa15fb1d1d30ee2b578dc5884823c19661886ae8b892775ccb3bd96b7d7345569a2aa0b14e4d015c54a6a0c54")

	// TODO: (lint) fix test
	//nolint
	refSplitShares = [][]byte{
		_byteArray("1a1b411e54ebb0973dc0f133c8b192cc4320fd464cbdcfe3be38b77f821f30bc"),
		_byteArray("6a93d37661cfe9cbaff9f051f2dd1d1995905932375e09357be1a50f7f4de323"),
		_byteArray("3596a78e633ad5071c0a77bb16b1a391b21ab47fb32ba1ba442a48e89ae11f9f"),
		_byteArray("62ff0c0cac676cd9e866377f4772d63f403b5734c02351701712a308d4d8e632"),
	}

	refSplitSharesPubKeys = [][]byte{
		_byteArray("84d90424a5511e3741ac3c99ee1dba39007a290410e805049d0ae40cde74191d785d7848f08b2dfb99b742ebfe846e3b"),
		_byteArray("b6ac738a09a6b7f3fb4f85bac26d8965f6329d431f484e8b43633f7b7e9afce0085bb592ea90df6176b2f2bd97dfd7f3"),
		_byteArray("a261c25548320f1aabfc2aac5da3737a0b8bbc992a5f4f937259d22d39fbf6ebf8ec561720de3a04f661c9772fcace96"),
		_byteArray("85dd2d89a3e320995507c46320f371dc85eb16f349d1c56d71b58663b5b6a5fd390fcf41cf9098471eb5437fd95be1ac"),
	}

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
	beacon          beacon.Beacon
	share           *beaconprotocol.Share
	signatureMu     sync.Mutex
	signatures      map[message.OperatorID][]byte
}

func (t *testIBFT) GetCurrentInstance() instance.Instancer {
	//TODO implement me
	panic("implement me")
}

func (t *testIBFT) Init() error {
	pk := &bls.PublicKey{}
	_ = pk.Deserialize(refPk)
	t.signatures = map[message.OperatorID][]byte{}
	return nil
}

func (t *testIBFT) StartInstance(opts instance.ControllerStartInstanceOptions) (*instance.Result, error) {
	commitData, err := (&message.CommitData{Data: opts.Value}).Encode()
	if err != nil {
		return nil, err
	}
	return &instance.Result{
		Decided: t.decided,
		Msg: &message.SignedMessage{
			Message: &message.ConsensusMessage{
				Data: commitData,
			},
			Signers: make([]message.OperatorID, t.signaturesCount),
		},
	}, nil
}

// GetIBFTCommittee returns a map of the iBFT committee where the key is the member's id.
func (t *testIBFT) GetIBFTCommittee() map[message.OperatorID]*beaconprotocol.Node {
	return map[message.OperatorID]*beaconprotocol.Node{
		1: {
			IbftID: 1,
			Pk:     refSplitSharesPubKeys[0],
		},
		2: {
			IbftID: 2,
			Pk:     refSplitSharesPubKeys[1],
		},
		3: {
			IbftID: 3,
			Pk:     refSplitSharesPubKeys[2],
		},
		4: {
			IbftID: 4,
			Pk:     refSplitSharesPubKeys[3],
		},
	}
}

func (t *testIBFT) GetIdentifier() []byte {
	return t.identifier
}

func (t *testIBFT) NextSeqNumber() (message.Height, error) {
	return 0, nil
}

func (t *testIBFT) OnFork(forkVersion forksprotocol.ForkVersion) error {
	return nil
}

func (t *testIBFT) PostConsensusDutyExecution(logger *zap.Logger, height message.Height, decidedValue []byte, signaturesCount int, duty *beaconprotocol.Duty) error {
	// get operator pk for sig
	pk, err := t.share.OperatorSharePubKey()
	if err != nil {
		return errors.Wrap(err, "could not find operator pk for signing duty")
	}

	retValueStruct := &beaconprotocol.DutyData{}
	if duty.Type != message.RoleTypeAttester {
		return errors.New("unsupported role, can't sign")
	}

	s := &spec.AttestationData{}
	if err := s.UnmarshalSSZ(decidedValue); err != nil {
		return errors.Wrap(err, "failed to marshal attestation")
	}

	signedAttestation, _, err := t.beacon.SignAttestation(s, duty, pk.Serialize())
	if err != nil {
		return errors.Wrap(err, "failed to sign attestation")
	}

	sg := &beaconprotocol.InputValueAttestation{Attestation: signedAttestation}
	retValueStruct.SignedData = sg
	retValueStruct.GetAttestation().Signature = signedAttestation.Signature
	retValueStruct.GetAttestation().AggregationBits = signedAttestation.AggregationBits

	t.signatureMu.Lock()
	signatures := t.signatures
	t.signatureMu.Unlock()

	seen := map[string]struct{}{}
	for _, sig := range signatures {
		seen[hex.EncodeToString(sig)] = struct{}{}
	}

	if l := len(seen); l < signaturesCount {
		return fmt.Errorf("not enough post consensus signatures, received %d", l)
	}

	signature, err := threshold.ReconstructSignatures(signatures)
	if err != nil {
		return errors.Wrap(err, "failed to reconstruct signatures")
	}

	blsSig := spec.BLSSignature{}
	copy(blsSig[:], signature.Serialize()[:])
	retValueStruct.GetAttestation().Signature = blsSig

	return t.beacon.SubmitAttestation(retValueStruct.GetAttestation())
}

func (t *testIBFT) ProcessMsg(msg *message.SSVMessage) error {
	signedMsg := &message.SignedMessage{}
	if err := signedMsg.Decode(msg.GetData()); err != nil {
		return errors.Wrap(err, "could not decode consensus signed message")
	}

	t.signatureMu.Lock()
	t.signatures[signedMsg.GetSigners()[0]] = signedMsg.Signature
	t.signatureMu.Unlock()
	return nil
}

func (t *testIBFT) ProcessSignatureMessage(msg *message.SignedPostConsensusMessage) error {
	t.signatureMu.Lock()
	t.signatures[msg.GetSigners()[0]] = msg.Message.DutySignature
	t.signatureMu.Unlock()
	return nil
}

type TestBeacon struct {
	refAttestationData       *spec.AttestationData
	LastSubmittedAttestation *spec.Attestation
	Signer                   beacon.KeyManager
}

func NewTestBeacon(t *testing.T) *TestBeacon {
	ret := &TestBeacon{}
	ret.refAttestationData = &spec.AttestationData{}
	err := ret.refAttestationData.UnmarshalSSZ(refAttestationDataByts) // ignore error
	require.NoError(t, err)

	ret.Signer = NewTestSigner()
	return ret
}

func (b *TestBeacon) StartReceivingBlocks() {
}

func (b *TestBeacon) GetDuties(epoch spec.Epoch, validatorIndices []spec.ValidatorIndex) ([]*beacon.Duty, error) {
	return nil, nil
}

func (b *TestBeacon) GetValidatorData(validatorPubKeys []spec.BLSPubKey) (map[spec.ValidatorIndex]*api.Validator, error) {
	return nil, nil
}

func (b *TestBeacon) GetAttestationData(slot spec.Slot, committeeIndex spec.CommitteeIndex) (*spec.AttestationData, error) {
	return b.refAttestationData, nil
}

func (b *TestBeacon) SignAttestation(data *spec.AttestationData, duty *beacon.Duty, pk []byte) (*spec.Attestation, []byte, error) {
	sig := spec.BLSSignature{}
	copy(sig[:], refAttestationSplitSigs[0])
	return &spec.Attestation{
		AggregationBits: nil,
		Data:            data,
		Signature:       sig,
	}, refSigRoot, nil
}

func (b *TestBeacon) SubmitAttestation(attestation *spec.Attestation) error {
	b.LastSubmittedAttestation = attestation
	return nil
}

func (b *TestBeacon) SubscribeToCommitteeSubnet(subscription []*api.BeaconCommitteeSubscription) error {
	panic("implement me")
}

func (b *TestBeacon) AddShare(shareKey *bls.SecretKey) error {
	return b.Signer.AddShare(shareKey)
}

func (b *TestBeacon) RemoveShare(pubKey string) error {
	return b.Signer.RemoveShare(pubKey)
}

func (b *TestBeacon) SignIBFTMessage(message *message.ConsensusMessage, pk []byte, forkVersion string) ([]byte, error) {
	return b.Signer.SignIBFTMessage(message, pk, forkVersion)
}

func (b *TestBeacon) GetDomain(data *spec.AttestationData) ([]byte, error) {
	panic("implement")
}

func (b *TestBeacon) ComputeSigningRoot(object interface{}, domain []byte) ([32]byte, error) {
	panic("implement")
}

func testingValidator(t *testing.T, decided bool, signaturesCount int, identifier []byte) *Validator {
	threshold.Init()

	ret := &Validator{}
	ret.beacon = NewTestBeacon(t)
	ret.logger = zap.L()

	// validatorStorage pk
	pk := &bls.PublicKey{}
	require.NoError(t, pk.Deserialize(refPk))

	share := &beaconprotocol.Share{
		NodeID:    1,
		PublicKey: pk,
		Committee: map[message.OperatorID]*beaconprotocol.Node{
			1: {
				IbftID: 1,
				Pk:     refSplitSharesPubKeys[0],
			},
			2: {
				IbftID: 2,
				Pk:     refSplitSharesPubKeys[1],
			},
			3: {
				IbftID: 3,
				Pk:     refSplitSharesPubKeys[2],
			},
			4: {
				IbftID: 4,
				Pk:     refSplitSharesPubKeys[3],
			},
		},
	}

	pi, err := protocolp2p.GenPeerID()
	require.NoError(t, err)

	p2pNet := protocolp2p.NewMockNetwork(zap.L(), pi, 10)

	ret.ibfts = make(controller.Controllers)
	ret.ibfts[message.RoleTypeAttester] = &testIBFT{
		decided:         decided,
		signaturesCount: signaturesCount,
		beacon:          ret.beacon,
		share:           share,
	}
	ret.ibfts[message.RoleTypeAttester].(*testIBFT).identifier = identifier
	require.NoError(t, ret.ibfts[message.RoleTypeAttester].Init())
	ret.signer = ret.beacon

	// nodes
	ret.network = beacon.NewNetwork(core.NetworkFromString("prater"))

	ret.p2pNetwork = p2pNet

	ret.Share = share

	return ret
}

// GenerateNodes generates randomly nodes
func GenerateNodes(cnt int) (map[message.OperatorID]*bls.SecretKey, map[message.OperatorID]*proto.Node) {
	_ = bls.Init(bls.BLS12_381)
	nodes := make(map[message.OperatorID]*proto.Node)
	sks := make(map[message.OperatorID]*bls.SecretKey)
	for i := 1; i <= cnt; i++ {
		sk := &bls.SecretKey{}
		sk.SetByCSPRNG()

		nodes[message.OperatorID(i)] = &proto.Node{
			IbftId: uint64(i),
			Pk:     sk.GetPublicKey().Serialize(),
		}
		sks[message.OperatorID(i)] = sk
	}
	return sks, nodes
}

type testSigner struct {
	lock sync.Locker
	keys map[string]*bls.SecretKey
}

// NewTestSigner creates a new signer for tests
func NewTestSigner() beacon.KeyManager {
	return &testSigner{&sync.Mutex{}, make(map[string]*bls.SecretKey)}
}

func (km *testSigner) AddShare(shareKey *bls.SecretKey) error {
	km.lock.Lock()
	defer km.lock.Unlock()

	if km.getKey(shareKey.GetPublicKey()) == nil {
		km.keys[shareKey.GetPublicKey().SerializeToHexStr()] = shareKey
	}
	return nil
}

func (km *testSigner) RemoveShare(pubKey string) error {
	//TODO implement me
	panic("implement me")
}

func (km *testSigner) getKey(key *bls.PublicKey) *bls.SecretKey {
	return km.keys[key.SerializeToHexStr()]
}

func (km *testSigner) SignIBFTMessage(message *message.ConsensusMessage, pk []byte, forkVersion string) ([]byte, error) {
	km.lock.Lock()
	defer km.lock.Unlock()

	if key := km.keys[hex.EncodeToString(pk)]; key != nil {
		sig, err := message.Sign(key, forkVersion) // TODO need to check fork v1?
		if err != nil {
			return nil, errors.Wrap(err, "could not sign ibft msg")
		}
		return sig.Serialize(), nil
	}
	return nil, errors.Errorf("could not find key for pk: %x", pk)
}

func (km *testSigner) SignAttestation(data *spec.AttestationData, duty *beacon.Duty, pk []byte) (*spec.Attestation, []byte, error) {
	return nil, nil, nil
}
