package provenance_test

import (
	"testing"

	buildercapella "github.com/attestantio/go-builder-client/api/capella"
	builderspec "github.com/attestantio/go-builder-client/spec"
	eth2api "github.com/attestantio/go-eth2-client/api"
	apiv1deneb "github.com/attestantio/go-eth2-client/api/v1/deneb"
	consensusspec "github.com/attestantio/go-eth2-client/spec"
	consensuscapella "github.com/attestantio/go-eth2-client/spec/capella"
	consensusdeneb "github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/holiman/uint256"

	"github.com/ssvlabs/ssv/mev/builderendpoint/provenance"
)

func TestFromBidExtractsBlockHash(t *testing.T) {
	t.Parallel()

	wantHash := phase0.Hash32{9}
	bid := &builderspec.VersionedSignedBuilderBid{
		Version: consensusspec.DataVersionCapella,
		Capella: &buildercapella.SignedBuilderBid{
			Message: &buildercapella.BuilderBid{
				Header: &consensuscapella.ExecutionPayloadHeader{BlockHash: wantHash},
				Value:  uint256.NewInt(1),
			},
		},
	}

	got, ok := provenance.FromBid(123, bid)
	if !ok {
		t.Fatalf("expected ok")
	}
	if got.Slot != 123 {
		t.Fatalf("unexpected slot: got %d want %d", got.Slot, 123)
	}
	if got.BlockHash != wantHash {
		t.Fatalf("unexpected block hash")
	}
}

func TestFromBlindedBlockExtractsSlotAndBlockHash(t *testing.T) {
	t.Parallel()

	wantSlot := phase0.Slot(7)
	wantHash := phase0.Hash32{3}

	block := &eth2api.VersionedSignedBlindedBeaconBlock{
		Version: consensusspec.DataVersionDeneb,
		Deneb: &apiv1deneb.SignedBlindedBeaconBlock{
			Message: &apiv1deneb.BlindedBeaconBlock{
				Slot: wantSlot,
				Body: &apiv1deneb.BlindedBeaconBlockBody{
					ExecutionPayloadHeader: &consensusdeneb.ExecutionPayloadHeader{
						BlockHash: wantHash,
					},
				},
			},
		},
	}

	got, ok := provenance.FromBlindedBlock(block)
	if !ok {
		t.Fatalf("expected ok")
	}
	if got.Slot != wantSlot {
		t.Fatalf("unexpected slot: got %d want %d", got.Slot, wantSlot)
	}
	if got.BlockHash != wantHash {
		t.Fatalf("unexpected block hash")
	}
}

func TestFromBlindedBlockMissingHeaderIsFalse(t *testing.T) {
	t.Parallel()

	block := &eth2api.VersionedSignedBlindedBeaconBlock{
		Version: consensusspec.DataVersionDeneb,
		Deneb: &apiv1deneb.SignedBlindedBeaconBlock{
			Message: &apiv1deneb.BlindedBeaconBlock{
				Slot: 1,
				Body: &apiv1deneb.BlindedBeaconBlockBody{},
			},
		},
	}

	_, ok := provenance.FromBlindedBlock(block)
	if ok {
		t.Fatalf("expected not ok")
	}
}
