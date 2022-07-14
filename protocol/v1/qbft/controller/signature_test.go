package controller

import (
	"encoding/hex"
	"testing"
	"time"

	api "github.com/attestantio/go-eth2-client/api/v1"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
)

func TestVerifyPartialSignature(t *testing.T) {
	require.NoError(t, bls.Init(bls.BLS12_381))

	tests := []struct {
		name          string
		skByts        []byte
		root          []byte
		useWrongRoot  bool
		useEmptyRoot  bool
		ibftID        spectypes.OperatorID
		expectedError string
	}{
		{
			"valid/ id 1",
			refSplitShares[0],
			[]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9},
			false,
			false,
			1,
			"",
		},
		{
			"valid/ id 2",
			refSplitShares[1],
			[]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 1},
			false,
			false,
			2,
			"",
		},
		{
			"valid/ id 3",
			refSplitShares[2],
			[]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 2},
			false,
			false,
			3,
			"",
		},
		{
			"wrong ibft id",
			refSplitShares[2],
			[]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 2},
			false,
			false,
			2,
			"could not verify signature from iBFT member 2",
		},
		{
			"wrong root",
			refSplitShares[2],
			[]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 2},
			true,
			false,
			3,
			"could not verify signature from iBFT member 3",
		},
		{
			"empty root",
			refSplitShares[2],
			[]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 2},
			false,
			true,
			3,
			"could not verify signature from iBFT member 3",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			//identifier := _byteArray("6139636633363061613135666231643164333065653262353738646335383834383233633139363631383836616538623839323737356363623362643936623764373334353536396132616130623134653464303135633534613661306335345f4154544553544552")

			sk := &bls.SecretKey{}
			require.NoError(t, sk.Deserialize(test.skByts))

			sig := sk.SignByte(test.root)

			usedRoot := test.root
			if test.useWrongRoot {
				usedRoot = []byte{0, 0, 0, 0, 0, 0, 0}
			}
			if test.useEmptyRoot {
				usedRoot = []byte{}
			}

			// validatorStorage pk
			pk := &bls.PublicKey{}
			err := pk.Deserialize(refPk)
			require.NoError(t, err)

			b := newTestBeacon(t)

			share := &beacon.Share{
				NodeID:    1,
				PublicKey: pk,
				Committee: map[spectypes.OperatorID]*beacon.Node{
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
			role := spectypes.BNRoleAttester
			identifier := message.NewIdentifier(share.PublicKey.Serialize(), role)
			opts := Options{
				Role:           role,
				Identifier:     identifier,
				Logger:         zap.L(),
				InstanceConfig: qbft.DefaultConsensusParams(),
				ValidatorShare: share,
				Beacon:         b,
				Signer:         b,
				SigTimeout:     time.Second * 2,
				Version:        forksprotocol.GenesisForkVersion,
			}

			ibftc := New(opts)

			err = ibftc.(*Controller).verifyPartialSignature(sig.Serialize(), usedRoot, test.ibftID, ibftc.GetIBFTCommittee()) // TODO need to fetch the committee from storage
			if len(test.expectedError) > 0 {
				require.EqualError(t, err, test.expectedError)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

var (
	refAttestationDataByts = _byteArray("000000000000000000000000000000003a43a4bf26fb5947e809c1f24f7dc6857c8ac007e535d48e6e4eca2122fd776b0000000000000000000000000000000000000000000000000000000000000000000000000000000002000000000000003a43a4bf26fb5947e809c1f24f7dc6857c8ac007e535d48e6e4eca2122fd776b")

	// refSk = _byteArray("2c083f2c8fc923fa2bd32a70ab72b4b46247e8c1f347adc30b2f8036a355086c")
	refPk = _byteArray("a9cf360aa15fb1d1d30ee2b578dc5884823c19661886ae8b892775ccb3bd96b7d7345569a2aa0b14e4d015c54a6a0c54")

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

	// TODO: (lint) fix test
	//nolint
	refAttestationSig = _byteArray("b4fa352d2d6dbdf884266af7ea0914451929b343527ea6c1737ac93b3dde8b7c98e6ce61d68b7a2e7b7af8f8d0fd429d0bdd5f930b83e6842bf4342d3d1d3d10fc0d15bab7649bb8aa8287ca104a1f79d396ce0217bb5cd3e6503a3bce4c9776")
	refSigRoot        = _byteArray("ae1f95e7f59eb99862ba7b3666a71a01facf4524e5922c6cb8f3b964a5041962")
)

func _byteArray(input string) []byte {
	res, _ := hex.DecodeString(input)
	return res
}

/**
testBeacon
*/
// TODO: replace with gomock
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

func (b *testBeacon) GetDuties(epoch spec.Epoch, validatorIndices []spec.ValidatorIndex) ([]*spectypes.Duty, error) {
	return nil, nil
}

func (b *testBeacon) GetValidatorData(validatorPubKeys []spec.BLSPubKey) (map[spec.ValidatorIndex]*api.Validator, error) {
	return nil, nil
}

func (b *testBeacon) GetAttestationData(slot spec.Slot, committeeIndex spec.CommitteeIndex) (*spec.AttestationData, error) {
	return b.refAttestationData, nil
}

func (b *testBeacon) SignAttestation(data *spec.AttestationData, duty *spectypes.Duty, pk []byte) (*spec.Attestation, []byte, error) {
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

func (b *testBeacon) RemoveShare(pubKey string) error {
	panic("implement me")
}

func (b *testBeacon) SignIBFTMessage(data message.Root, pk []byte, sigType message.SignatureType) ([]byte, error) {
	panic("implement me")
}

func (b *testBeacon) GetDomain(data *spec.AttestationData) ([]byte, error) {
	panic("implement")
}

func (b *testBeacon) ComputeSigningRoot(object interface{}, domain []byte) ([32]byte, error) {
	panic("implement")
}
