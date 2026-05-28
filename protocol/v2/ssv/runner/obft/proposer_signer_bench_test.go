package obft

import (
	"fmt"
	"testing"

	apiv1deneb "github.com/attestantio/go-eth2-client/api/v1/deneb"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/holiman/uint256"
	"github.com/prysmaticlabs/go-bitfield"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/obft/blsbackend"
)

// Benchmark B2 from docs/OBFT-PERFORMANCE-AUDIT-PLAN.md: quantify the cost
// of signingRootFor — the SSZ-unmarshal + tree-root + domain compute path
// that fires on every BLS sign / verify / aggregate-verify of an OBFT
// V-side partial. Grounds F2's per-slot-cost estimate.

// makeBenchV builds a [version | SSZ blinded block] candidate. `attCount`
// controls how many phase0.Attestations fill the body — the dominant size
// driver in real Beacon blocks. 0 = minimal block (~1 KB), 64 = mid-range
// (~10 KB), 128 = MAX_ATTESTATIONS pre-Electra (~20 KB realistic upper
// bound for production blinded blocks per Q-Open-3).
func makeBenchV(b *testing.B, attCount int) []byte {
	b.Helper()

	atts := make([]*phase0.Attestation, attCount)
	for i := range atts {
		atts[i] = &phase0.Attestation{
			// Aggregation bits: 128 committee members → 16 bytes + 1 length-bit
			// byte. Matches mainnet committee size for typical attestations.
			AggregationBits: bitfield.NewBitlist(128),
			Data: &phase0.AttestationData{
				Slot:            phase0.Slot(uint64(12345 + i)),
				Index:           phase0.CommitteeIndex(uint64(i % 64)),
				BeaconBlockRoot: phase0.Root{byte(i)},
				Source:          &phase0.Checkpoint{Epoch: 100, Root: phase0.Root{byte(i)}},
				Target:          &phase0.Checkpoint{Epoch: 101, Root: phase0.Root{byte(i)}},
			},
			Signature: phase0.BLSSignature{},
		}
	}

	bb := &apiv1deneb.BlindedBeaconBlock{
		Slot:          12345,
		ProposerIndex: 42,
		ParentRoot:    phase0.Root{8},
		StateRoot:     phase0.Root{9},
		Body: &apiv1deneb.BlindedBeaconBlockBody{
			RANDAOReveal: phase0.BLSSignature{},
			ETH1Data: &phase0.ETH1Data{
				DepositRoot:  phase0.Root{},
				DepositCount: 0,
				BlockHash:    make([]byte, 32),
			},
			Graffiti:     [32]byte{7},
			Attestations: atts,
			SyncAggregate: &altair.SyncAggregate{
				SyncCommitteeBits:      bitfield.Bitvector512(make([]byte, 64)),
				SyncCommitteeSignature: phase0.BLSSignature{},
			},
			ExecutionPayloadHeader: &deneb.ExecutionPayloadHeader{
				ParentHash:       [32]byte{1},
				FeeRecipient:     [20]byte{2},
				StateRoot:        [32]byte{3},
				ReceiptsRoot:     [32]byte{4},
				LogsBloom:        [256]byte{},
				PrevRandao:       [32]byte{5},
				BlockNumber:      10,
				GasLimit:         11,
				GasUsed:          12,
				Timestamp:        13,
				ExtraData:        []byte{0xaa, 0xbb},
				BaseFeePerGas:    uint256.NewInt(0),
				BlockHash:        [32]byte{6},
				TransactionsRoot: [32]byte{14},
				WithdrawalsRoot:  [32]byte{15},
			},
		},
	}

	ssz, err := bb.MarshalSSZ()
	if err != nil {
		b.Fatalf("MarshalSSZ: %v", err)
	}
	return EncodeCandidate(spec.DataVersionDeneb, ssz)
}

func BenchmarkProposerSigner_signingRootFor(b *testing.B) {
	beacon := networkconfig.TestNetwork.Beacon
	innerSigner := blsbackend.New(nil) // verify-only inner — signingRootFor doesn't sign
	signer, err := NewProposerSigner(innerSigner, beacon)
	if err != nil {
		b.Fatalf("NewProposerSigner: %v", err)
	}
	ps, ok := signer.(*proposerSigner)
	if !ok {
		b.Fatalf("expected *proposerSigner concrete type")
	}

	for _, attCount := range []int{0, 32, 64, 128} {
		b.Run(fmt.Sprintf("atts=%d", attCount), func(b *testing.B) {
			v := makeBenchV(b, attCount)
			b.Logf("V size: %d bytes", len(v))
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, err := ps.signingRootFor(v)
				if err != nil {
					b.Fatalf("signingRootFor: %v", err)
				}
			}
		})
	}
}
