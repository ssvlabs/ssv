package networkconfig

import (
	"crypto/sha256"
	"encoding/binary"
)

// IsAggregatorSelected returns true if the given slot signature selects the validator as an
// aggregator for a committee of committeeLength members, given targetAggregatorsPerCommittee.
//
// This is the shared implementation of the beacon-chain aggregator-selection check, used by both
// beacon/goclient and protocol/v2/ssv/runner. It must stay bit-identical across pre/post-Boole
// call sites.
//
// Spec pseudocode definition:
//
//	def is_aggregator(state: BeaconState, slot: Slot, index: CommitteeIndex, slot_signature: BLSSignature) -> bool:
//	 committee = get_beacon_committee(state, slot, index)
//	 modulo = max(1, len(committee) // TARGET_AGGREGATORS_PER_COMMITTEE)
//	 return bytes_to_uint64(hash(slot_signature)[0:8]) % modulo == 0
//
// A zero targetAggregatorsPerCommittee (never the case in practice — the spec constant is 16)
// is clamped to modulo 1 rather than panicking on division.
func IsAggregatorSelected(targetAggregatorsPerCommittee, committeeLength uint64, slotSig []byte) bool {
	// Modulo must be at least 1. The guard on targetAggregatorsPerCommittee keeps the helper
	// total: a misconfigured zero target degrades to modulo 1 ("selected", so aggregation
	// duties are still attempted) instead of a division panic.
	modulo := uint64(1)
	if targetAggregatorsPerCommittee > 0 {
		modulo = max(1, committeeLength/targetAggregatorsPerCommittee)
	}

	h := sha256.Sum256(slotSig)
	return binary.LittleEndian.Uint64(h[:8])%modulo == 0
}
