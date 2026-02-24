package provenance

import (
	"github.com/attestantio/go-builder-client/spec"
	"github.com/attestantio/go-eth2-client/api"
	consensusspec "github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// Key identifies an execution payload by (slot, block hash).
//
// This is used to route unblinding to the relay that provided the winning bid.
type Key struct {
	Slot      phase0.Slot
	BlockHash phase0.Hash32
}

func (k Key) IsZero() bool {
	return k.Slot == 0 && k.BlockHash == (phase0.Hash32{})
}

// FromBid extracts a provenance key from a builder bid.
//
// Note: the bid itself does not carry a slot, so it must be supplied by the caller.
func FromBid(slot phase0.Slot, bid *spec.VersionedSignedBuilderBid) (Key, bool) {
	if bid == nil || bid.IsEmpty() {
		return Key{}, false
	}

	var headerBlockHash phase0.Hash32

	switch bid.Version {
	case consensusspec.DataVersionBellatrix:
		if bid.Bellatrix == nil || bid.Bellatrix.Message == nil || bid.Bellatrix.Message.Header == nil {
			return Key{}, false
		}
		headerBlockHash = bid.Bellatrix.Message.Header.BlockHash
	case consensusspec.DataVersionCapella:
		if bid.Capella == nil || bid.Capella.Message == nil || bid.Capella.Message.Header == nil {
			return Key{}, false
		}
		headerBlockHash = bid.Capella.Message.Header.BlockHash
	case consensusspec.DataVersionDeneb:
		if bid.Deneb == nil || bid.Deneb.Message == nil || bid.Deneb.Message.Header == nil {
			return Key{}, false
		}
		headerBlockHash = bid.Deneb.Message.Header.BlockHash
	case consensusspec.DataVersionElectra:
		if bid.Electra == nil || bid.Electra.Message == nil || bid.Electra.Message.Header == nil {
			return Key{}, false
		}
		headerBlockHash = bid.Electra.Message.Header.BlockHash
	case consensusspec.DataVersionFulu:
		if bid.Fulu == nil || bid.Fulu.Message == nil || bid.Fulu.Message.Header == nil {
			return Key{}, false
		}
		headerBlockHash = bid.Fulu.Message.Header.BlockHash
	default:
		return Key{}, false
	}

	if headerBlockHash == (phase0.Hash32{}) {
		return Key{}, false
	}

	return Key{Slot: slot, BlockHash: headerBlockHash}, true
}

// FromBlindedBlock extracts a provenance key from a blinded beacon block.
func FromBlindedBlock(block *api.VersionedSignedBlindedBeaconBlock) (Key, bool) {
	if block == nil {
		return Key{}, false
	}

	var (
		slot      phase0.Slot
		blockHash phase0.Hash32
	)

	switch block.Version {
	case consensusspec.DataVersionBellatrix:
		if block.Bellatrix == nil || block.Bellatrix.Message == nil || block.Bellatrix.Message.Body == nil || block.Bellatrix.Message.Body.ExecutionPayloadHeader == nil {
			return Key{}, false
		}
		slot = block.Bellatrix.Message.Slot
		blockHash = block.Bellatrix.Message.Body.ExecutionPayloadHeader.BlockHash
	case consensusspec.DataVersionCapella:
		if block.Capella == nil || block.Capella.Message == nil || block.Capella.Message.Body == nil || block.Capella.Message.Body.ExecutionPayloadHeader == nil {
			return Key{}, false
		}
		slot = block.Capella.Message.Slot
		blockHash = block.Capella.Message.Body.ExecutionPayloadHeader.BlockHash
	case consensusspec.DataVersionDeneb:
		if block.Deneb == nil || block.Deneb.Message == nil || block.Deneb.Message.Body == nil || block.Deneb.Message.Body.ExecutionPayloadHeader == nil {
			return Key{}, false
		}
		slot = block.Deneb.Message.Slot
		blockHash = block.Deneb.Message.Body.ExecutionPayloadHeader.BlockHash
	case consensusspec.DataVersionElectra:
		if block.Electra == nil || block.Electra.Message == nil || block.Electra.Message.Body == nil || block.Electra.Message.Body.ExecutionPayloadHeader == nil {
			return Key{}, false
		}
		slot = block.Electra.Message.Slot
		blockHash = block.Electra.Message.Body.ExecutionPayloadHeader.BlockHash
	case consensusspec.DataVersionFulu:
		if block.Fulu == nil || block.Fulu.Message == nil || block.Fulu.Message.Body == nil || block.Fulu.Message.Body.ExecutionPayloadHeader == nil {
			return Key{}, false
		}
		slot = block.Fulu.Message.Slot
		blockHash = block.Fulu.Message.Body.ExecutionPayloadHeader.BlockHash
	default:
		return Key{}, false
	}

	if blockHash == (phase0.Hash32{}) {
		return Key{}, false
	}

	return Key{Slot: slot, BlockHash: blockHash}, true
}
