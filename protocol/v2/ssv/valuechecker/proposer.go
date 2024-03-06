package valuechecker

import (
	"fmt"

	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv-spec/types"
	ssz "github.com/ferranbt/fastssz"
)

func (vc *ValueChecker) getBlockData(cd *types.ConsensusData) (spec.Slot, ssz.HashRoot, error) {
	blindedBlockData, blindedHashRoot, err := cd.GetBlindedBlockData()
	if err != nil {
		blockData, hashRoot, err := cd.GetBlockData()
		if err != nil {
			return 0, nil, fmt.Errorf("no block data")
		}

		slot, err := blockData.Slot()
		if err != nil {
			return 0, nil, fmt.Errorf("get slot from block data: %w", err)
		}

		return slot, hashRoot, nil
	}

	slot, err := blindedBlockData.Slot()
	if err != nil {
		return 0, nil, fmt.Errorf("get slot from blinded block data: %w", err)
	}

	return slot, blindedHashRoot, nil
}

func (vc *ValueChecker) checkSlashableProposal(slot spec.Slot, hashRoot ssz.HashRoot) error {
	blockHashRoot, err := hashRoot.HashTreeRoot()
	if err != nil {
		return fmt.Errorf("get blinded block data hash: %w", err)
	}

	if blockHashRoot == vc.currentBlockHashRoot {
		return nil
	}

	if err := vc.signer.IsBeaconBlockSlashable(vc.sharePublicKey, slot); err != nil {
		return err
	}

	vc.currentBlockHashRoot = blockHashRoot
	return nil
}

func (vc *ValueChecker) ProposerValueCheckF(data []byte) error {
	cd := &types.ConsensusData{}
	if err := cd.Decode(data); err != nil {
		return fmt.Errorf("decode consensus data: %w", err)
	}
	if err := cd.Validate(); err != nil {
		return fmt.Errorf("invalid value: %w", err)
	}

	if err := vc.checkDuty(&cd.Duty, types.BNRoleProposer); err != nil {
		return fmt.Errorf("duty invalid: %w", err)
	}

	slot, hashRoot, err := vc.getBlockData(cd)
	if err != nil {
		return err
	}

	return vc.checkSlashableProposal(slot, hashRoot)
}
