package node

import (
	"context"
	"encoding/hex"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/ibft"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network/local"
	"github.com/herumi/bls-eth-go-binary/bls"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"go.uber.org/zap"
	"time"
)

var (
	refAttestationDataByts = _byteArray("1a203a43a4bf26fb5947e809c1f24f7dc6857c8ac007e535d48e6e4eca2122fd776b2222122000000000000000000000000000000000000000000000000000000000000000002a24080212203a43a4bf26fb5947e809c1f24f7dc6857c8ac007e535d48e6e4eca2122fd776b")
	refSk                  = _byteArray("2c083f2c8fc923fa2bd32a70ab72b4b46247e8c1f347adc30b2f8036a355086c")
	refPk                  = _byteArray("a9cf360aa15fb1d1d30ee2b578dc5884823c19661886ae8b892775ccb3bd96b7d7345569a2aa0b14e4d015c54a6a0c54")

	refSplitShares = [][]byte{ // sk split to 4: 2c083f2c8fc923fa2bd32a70ab72b4b46247e8c1f347adc30b2f8036a355086c
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
}

func (t *testIBFT) StartInstance(opts ibft.StartOptions) (bool, int) {
	return t.decided, t.signaturesCount
}

// GetIBFTCommittee returns a map of the iBFT committee where the key is the member's id.
func (i *testIBFT) GetIBFTCommittee() map[uint64]*proto.Node {
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

/**
testBeacon
*/
type testBeacon struct {
	refAttestationData       *ethpb.AttestationData
	LastSubmittedAttestation *ethpb.Attestation
}

func newTestBeacon() *testBeacon {
	ret := &testBeacon{}
	// parse ref att. data
	ret.refAttestationData = &ethpb.AttestationData{}
	ret.refAttestationData.Unmarshal(refAttestationDataByts) // ignore error

	return ret
}

func (t *testBeacon) StreamDuties(ctx context.Context, pubKey []byte) (<-chan *ethpb.DutiesResponse_Duty, error) {
	return nil, nil
}

func (t *testBeacon) GetAttestationData(ctx context.Context, slot, committeeIndex uint64) (*ethpb.AttestationData, error) {
	return t.refAttestationData, nil
}

func (t *testBeacon) SignAttestation(ctx context.Context, data *ethpb.AttestationData, validatorIndex uint64, committee []uint64) (*ethpb.Attestation, []byte, error) {
	return &ethpb.Attestation{
		AggregationBits: nil,
		Data:            data,
		Signature:       refAttestationSplitSigs[0],
	}, refSigRoot, nil
}

func (t *testBeacon) SubmitAttestation(ctx context.Context, attestation *ethpb.Attestation, validatorIndex uint64) error {
	t.LastSubmittedAttestation = attestation
	return nil
}

func (t *testBeacon) GetAggregationData(ctx context.Context, slot, committeeIndex uint64) (*ethpb.AggregateAttestationAndProof, error) {
	return nil, nil
}

func (t *testBeacon) SignAggregation(ctx context.Context, data *ethpb.AggregateAttestationAndProof) (*ethpb.SignedAggregateAttestationAndProof, error) {
	return nil, nil
}

func (t *testBeacon) SubmitAggregation(ctx context.Context, data *ethpb.SignedAggregateAttestationAndProof) error {
	return nil
}

func (t *testBeacon) GetProposalData(ctx context.Context, slot uint64) (*ethpb.BeaconBlock, error) {
	return nil, nil
}

func (t *testBeacon) SignProposal(ctx context.Context, block *ethpb.BeaconBlock) (*ethpb.SignedBeaconBlock, error) {
	return nil, nil
}

func (t *testBeacon) SubmitProposal(ctx context.Context, block *ethpb.SignedBeaconBlock) error {
	return nil
}

func (t *testBeacon) RolesAt(ctx context.Context, slot uint64, duty *ethpb.DutiesResponse_Duty) ([]beacon.Role, error) {
	return nil, nil
}

func testingSSVNode(decided bool, signaturesCount int) *ssvNode {
	ret := &ssvNode{}
	ret.beacon = newTestBeacon()
	ret.logger = zap.L()
	ret.iBFT = &testIBFT{decided: decided, signaturesCount: signaturesCount}

	// nodes
	ret.network = local.NewLocalNetwork()
	ret.nodeID = 1

	// validator pk
	pk := &bls.PublicKey{}
	pk.Deserialize(refPk)
	ret.validatorPubKey = pk

	// timeout
	ret.signatureCollectionTimeout = time.Second
	return ret
}

/**
utils
*/
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
