package slashinginterceptor

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"sort"

	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/capella"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"
)

type ProposerSlashingTest struct {
	Name      string
	Slashable bool
	Apply     func(*spec.VersionedBeaconBlock) error
}

var ProposerSlashingTests = []ProposerSlashingTest{
	{
		Name:      "HigherSlot_DifferentRoot",
		Slashable: false,
		Apply: func(block *spec.VersionedBeaconBlock) error {
			switch block.Version {
			case spec.DataVersionCapella:
				block.Capella.Slot++
			default:
				return fmt.Errorf("unsupported version: %s", block.Version)
			}
			_, err := rand.Read(block.Capella.ParentRoot[:])
			return err
		},
	},
	{
		Name:      "SameSlot_DifferentRoot",
		Slashable: true,
		Apply: func(block *spec.VersionedBeaconBlock) error {
			switch block.Version {
			case spec.DataVersionCapella:
				_, err := rand.Read(block.Capella.ParentRoot[:])
				return err
			default:
				return fmt.Errorf("unsupported version: %s", block.Version)
			}
		},
	},
	{
		Name:      "LowerSlot_SameRoot",
		Slashable: true,
		Apply: func(block *spec.VersionedBeaconBlock) error {
			switch block.Version {
			case spec.DataVersionCapella:
				block.Capella.Slot--
			default:
				return fmt.Errorf("unsupported version: %s", block.Version)
			}
			return nil
		},
	},
}

// PROPOSER

func (s *SlashingInterceptor) InterceptProposerDuties(
	ctx context.Context,
	epoch phase0.Epoch,
	indices []phase0.ValidatorIndex,
	duties []*v1.ProposerDuty,
) ([]*v1.ProposerDuty, error) {
	return []*v1.ProposerDuty{}, nil // Currently disabled

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.blockedEpoch(epoch) {
		return []*v1.ProposerDuty{}, nil
	}

	if !s.fakeProposerDuties {
		return duties, nil
	}

	logger, gateway := s.requestContext(ctx)

	// If no indices are provided, it means that the client wants duties for all validators.
	if len(indices) == 0 {
		for _, state := range s.validators {
			indices = append(indices, state.validator.Index)
		}
	}

	// Sort indices for deterministic response.
	sort.Slice(indices, func(i, j int) bool {
		return indices[i] < indices[j]
	})

	// Fake a proposer duty for each validator.
	duties = make([]*v1.ProposerDuty, 0, len(indices))
	for i, index := range indices {
		state, ok := s.validators[index]
		if !ok {
			return nil, fmt.Errorf("validator not found: %d", index)
		}
		duty := &v1.ProposerDuty{
			ValidatorIndex: index,
			PubKey:         state.validator.Validator.PublicKey,
			Slot:           s.network.FirstSlotAtEpoch(epoch) + phase0.Slot(i),
		}
		logger := logger.With(
			zap.Any("epoch", epoch),
			zap.Any("slot", duty.Slot),
			zap.Any("validator", duty.ValidatorIndex),
		)
		if _, ok := state.firstProposerDuty[gateway]; !ok {
			if epoch != s.startEpoch {
				return nil, fmt.Errorf("misbehavior: first proposer duty wasn't requested during the start epoch")
			}
			state.firstProposerDuty[gateway] = duty
			logger.Info("validator got first proposer duty")
		} else if _, ok := state.secondProposerDuty[gateway]; !ok {
			if epoch != s.endEpoch {
				return nil, fmt.Errorf("misbehavior: second proposer duty wasn't requested during the end epoch")
			}
			state.secondProposerDuty[gateway] = duty
			logger.Info("validator got second proposer duty")
		} else {
			return nil, fmt.Errorf("second proposer duties already requested")
		}
		duties = append(duties, duty)
	}

	return duties, nil
}

func (s *SlashingInterceptor) InterceptBlockProposal(
	ctx context.Context,
	slot phase0.Slot,
	randaoReveal phase0.BLSSignature,
	graffiti [32]byte,
	block *spec.VersionedBeaconBlock,
) (*spec.VersionedBeaconBlock, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	epoch := s.network.EstimatedEpochAtSlot(slot)
	if s.blockedEpoch(epoch) {
		return nil, fmt.Errorf("block proposal requested for blocked epoch %d", epoch)
	}

	logger, gateway := s.requestContext(ctx)
	logger = logger.With(
		zap.Any("epoch", epoch),
		zap.Any("slot", slot),
	)

	for _, state := range s.validators {
		logger := logger.With(zap.Any("validator", state.validator.Index))

		if duty, ok := state.firstProposerDuty[gateway]; ok && duty.Slot == slot {
			// Fake a block at the slot of the duty.
			var err error
			block, err := fakeBeaconBlock(slot, randaoReveal, graffiti)
			if err != nil {
				return nil, fmt.Errorf("failed to create base block: %w", err)
			}
			state.firstBlock[gateway] = block
			logger.Info("produced first block proposal")
			return block, nil
		}

		if duty, ok := state.secondProposerDuty[gateway]; ok && duty.Slot == slot {
			if _, ok := state.firstBlock[gateway]; !ok {
				return nil, fmt.Errorf("unexpected state: requested second block before first block")
			}

			// Copy the first block to avoid mutating it.
			var secondBlock *spec.VersionedBeaconBlock
			b, err := json.Marshal(state.firstBlock[gateway])
			if err != nil {
				return nil, fmt.Errorf("failed to marshal first block: %w", err)
			}
			if err := json.Unmarshal(b, &secondBlock); err != nil {
				return nil, fmt.Errorf("failed to unmarshal first block: %w", err)
			}

			// Apply the test on the first block.
			if err := state.proposerTest.Apply(secondBlock); err != nil {
				return nil, fmt.Errorf("failed to apply proposer slashing test: %w", err)
			}

			state.secondBlock[gateway] = secondBlock
			logger.Info("produced second block proposal")

			return secondBlock, nil
		}
	}
	return nil, fmt.Errorf("block proposal requested for unknown duty")
}

func (s *SlashingInterceptor) InterceptSubmitBlockProposal(ctx context.Context, block *spec.VersionedSignedBeaconBlock) (*spec.VersionedSignedBeaconBlock, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	slot := block.Capella.Message.Slot
	epoch := s.network.EstimatedEpochAtSlot(slot)
	if s.blockedEpoch(epoch) {
		return nil, fmt.Errorf("block proposal submitted for blocked epoch %d", epoch)
	}

	logger, gateway := s.requestContext(ctx)
	logger = logger.With(zap.Any("epoch", epoch), zap.Any("slot", slot))

	for _, state := range s.validators {
		logger := logger.With(zap.Any("validator", state.validator.Index))

		if duty, ok := state.firstProposerDuty[gateway]; ok && duty.Slot == slot {
			if _, ok := state.firstSubmittedBlock[gateway]; ok {
				return nil, fmt.Errorf("first block already submitted")
			}
			state.firstSubmittedBlock[gateway] = block
			logger.Info("submitted first block proposal")
			return nil, nil
		}
		if duty, ok := state.secondProposerDuty[gateway]; ok && duty.Slot == slot {
			if _, ok := state.secondSubmittedBlock[gateway]; ok {
				return nil, fmt.Errorf("second block already submitted")
			}
			state.secondSubmittedBlock[gateway] = block
			if state.proposerTest.Slashable {
				return nil, fmt.Errorf("misbehavior: slashable block was submitted during the end epoch")
			}
			logger.Info("submitted second block proposal")
			return nil, nil
		}
	}

	return nil, fmt.Errorf("block proposal submitted for unknown duty at slot %d", slot)
}

func fakeBeaconBlock(slot phase0.Slot, randaoReveal phase0.BLSSignature, graffiti [32]byte) (*spec.VersionedBeaconBlock, error) {
	var block *capella.BeaconBlock
	blockJSON := []byte(`{"slot":"1861337","proposer_index":"1610","parent_root":"0x353f35e506b07b6a20dd5d24c92b6dfda1b9e221848963d7a40cb7fe4e23c35a","state_root":"0x8e9cfedb909c4c9bbc52ca4174b279d31ffe40a667077c7b31a66bbd1a66d0be","body":{"randao_reveal":"0xb019a2ef4b7763c7bd5a9b3dbe00ba38cdfb6ee0fcecfe4a7301687973717f7c193072a3ebd70de3aed153f960b11fc40037b8164542e1364a31489ff7a0a1119ef46bc2ea099886446cba3acc4721a2534c292f6eeb650719f32477707aab3e","eth1_data":{"deposit_root":"0x9df92d765b5aa041fd4bbe8d5878eb89290efa78e444c1a603eecfae2ea05fa4","deposit_count":"403","block_hash":"0x1ee29c95ea816db342e04fbd1f00cb14940a45d025d2d14bf0d33187d8f4d5ea"},"graffiti":"0x626c6f787374616b696e672e636f6d0000000000000000000000000000000000","proposer_slashings":[],"attester_slashings":[],"attestations":[{"aggregation_bits":"0xffffffffffffff37","data":{"slot":"1861336","index":"0","beacon_block_root":"0x353f35e506b07b6a20dd5d24c92b6dfda1b9e221848963d7a40cb7fe4e23c35a","source":{"epoch":"58165","root":"0x5c0a59daecb2f509e0d5a038da44c55ef5827b0b979e6e397cc7f5846f9f3f4c"},"target":{"epoch":"58166","root":"0x835469d3b4348c2f5c2964afaac731acc0117eb0f5cec64bf166ab26847f77a8"}},"signature":"0x991b97e124a8dc672d135c167f030ceff9ab500dfd9a8bb79e199a303bfa54cebc205e396a16466e51d54cb7e0d981eb0eb03295bdb39b69d9559421aeea22838d22732d66ea79d3bebb8a8a335679491b60ad6d815318cbfd92a1a867c70255"}],"deposits":[],"voluntary_exits":[],"sync_aggregate":{"sync_committee_bits":"0xfffffbfffffffffffffffcffffdfffffffffffffffefffffffffffff7ffdffffbfffffffffffefffffebfffffffffffffffffffdff7fffffffffffffffffffff","sync_committee_signature":"0x91713624034f3c1e37baa3de1d082883c6e44cc830f4ecf0ca58cc06153aba97ca4539edfdb0f49a0d18b354f585422e114feb07b689db0a47ee72231d8e6fb2970663f518ca0a3fda0e701cd4662405c4f2fa9fdac0533c790f92404240432e"},"execution_payload":{"parent_hash":"0x4b37c3a4f001b91cfcac61b7f6bdfd2218525e4f421a4ab52fa513b00e76ab6f","fee_recipient":"0x0f35b0753e261375c9a6cb44316b4bdc7e765509","state_root":"0xc9d0b31cc38141d01998220dcc0e7830994e6a1d11fa3a26d7d113df82c1a53a","receipts_root":"0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421","logs_bloom":"0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000","prev_randao":"0x251fdfd6ebef77085f24016900debbd334e67cf51ee48d959f01ef12698a49a5","block_number":"3031542","gas_limit":"30000000","gas_used":"0","timestamp":"1678069644","extra_data":"0xd883010b02846765746888676f312e32302e31856c696e7578","base_fee_per_gas":"7","block_hash":"0x4cb1ca5a2947972cb018416fe4489b9daf674d5be97fd2ebb6de1a1608226733","transactions":[],"withdrawals":[{"index":"645759","validator_index":"425","address":"0x25c4a76e7d118705e7ea2e9b7d8c59930d8acd3b","amount":"2886"},{"index":"645760","validator_index":"428","address":"0x25c4a76e7d118705e7ea2e9b7d8c59930d8acd3b","amount":"2886"},{"index":"645761","validator_index":"429","address":"0x25c4a76e7d118705e7ea2e9b7d8c59930d8acd3b","amount":"2886"},{"index":"645762","validator_index":"431","address":"0x25c4a76e7d118705e7ea2e9b7d8c59930d8acd3b","amount":"2886"},{"index":"645763","validator_index":"434","address":"0x25c4a76e7d118705e7ea2e9b7d8c59930d8acd3b","amount":"2886"},{"index":"645764","validator_index":"437","address":"0x25c4a76e7d118705e7ea2e9b7d8c59930d8acd3b","amount":"2886"},{"index":"645765","validator_index":"440","address":"0x25c4a76e7d118705e7ea2e9b7d8c59930d8acd3b","amount":"1924"},{"index":"645766","validator_index":"441","address":"0x25c4a76e7d118705e7ea2e9b7d8c59930d8acd3b","amount":"1924"},{"index":"645767","validator_index":"448","address":"0x25c4a76e7d118705e7ea2e9b7d8c59930d8acd3b","amount":"1924"},{"index":"645768","validator_index":"450","address":"0x25c4a76e7d118705e7ea2e9b7d8c59930d8acd3b","amount":"1924"},{"index":"645769","validator_index":"451","address":"0x25c4a76e7d118705e7ea2e9b7d8c59930d8acd3b","amount":"1924"},{"index":"645770","validator_index":"456","address":"0x25c4a76e7d118705e7ea2e9b7d8c59930d8acd3b","amount":"1924"},{"index":"645771","validator_index":"458","address":"0x25c4a76e7d118705e7ea2e9b7d8c59930d8acd3b","amount":"1924"},{"index":"645772","validator_index":"465","address":"0x25c4a76e7d118705e7ea2e9b7d8c59930d8acd3b","amount":"1924"},{"index":"645773","validator_index":"467","address":"0x25c4a76e7d118705e7ea2e9b7d8c59930d8acd3b","amount":"1924"},{"index":"645774","validator_index":"468","address":"0x25c4a76e7d118705e7ea2e9b7d8c59930d8acd3b","amount":"1924"}]},"bls_to_execution_changes":[]}}`)
	if err := json.Unmarshal(blockJSON, &block); err != nil {
		return nil, err
	}
	block.Slot = slot
	block.Body.RANDAOReveal = randaoReveal
	block.Body.Graffiti = graffiti
	return &spec.VersionedBeaconBlock{
		Version: spec.DataVersionCapella,
		Capella: block,
	}, nil
}
