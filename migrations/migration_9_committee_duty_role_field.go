package migrations

import (
	"context"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	estore "github.com/ssvlabs/ssv/exporter/store"
	traces "github.com/ssvlabs/ssv/exporter/traces"
	"github.com/ssvlabs/ssv/storage/basedb"
)

// migrationBatchSize caps how many rewrites are held in memory before being flushed in a
// single Update transaction. A mainnet exporter can have tens of millions of legacy "cd"
// records, so batching bounds the accumulated values (on badger, GetAll still materializes
// every key under the prefix up front, so only the values are bounded here).
const migrationBatchSize = 5000

// migrationBatchBytes caps the accumulated payload per flush. badger's transaction limit is
// 15% of MemTableSize (~9.6MB under the badger.DefaultOptions this repo opens with), and a
// single CommitteeDutyTrace can reach ~4MB via ProposalData, so flushing at 2MB keeps the
// worst case (threshold plus one oversized record) well under the cap. Pebble batches are
// memory-bound; this costs nothing there.
const migrationBatchBytes = 2 << 20

// This migration updates legacy committee duty keys and values to include the runner role field.
// It processes only legacy keys (slot+committeeID) and skips already role-aware keys.
var migration_9_migrate_committee_duty_role_field = Migration{
	Name: "migration_9_migrate_committee_duty_role_field",
	Run: func(ctx context.Context, logger *zap.Logger, opt Options, key []byte, completed CompletedFunc) (err error) {
		var migrated int

		defer func() {
			if err != nil {
				return
			}
			if err = completed(opt.Db); err != nil {
				err = fmt.Errorf("complete migration: %w", err)
				return
			}
			logger.Info(
				"migration completed",
				zap.Int("migrated", migrated),
			)
		}()

		const (
			committeeDutyTraceKey = "cd"
			slotKeyLen            = 4
			roleKeyLen            = 1
			committeeIDLen        = 32
			oldKeyLen             = slotKeyLen + committeeIDLen
			newKeyLen             = slotKeyLen + roleKeyLen + committeeIDLen
		)

		prefix := []byte(committeeDutyTraceKey)

		roleByte, err := estore.CommitteeRunnerRoleToPrefix(spectypes.RoleCommittee)
		if err != nil {
			return fmt.Errorf("map committee runner role to prefix: %w", err)
		}

		pending := make([]migration_9_pendingRewrite, 0, migrationBatchSize)
		pendingBytes := 0

		flush := func() error {
			if len(pending) == 0 {
				return nil
			}
			if err := opt.Db.Update(func(txn basedb.Txn) error {
				for _, rewrite := range pending {
					if err := txn.Set(prefix, rewrite.newKey, rewrite.value); err != nil {
						return fmt.Errorf("set committee duty with role: %w", err)
					}
					if err := txn.Delete(prefix, rewrite.oldKey); err != nil {
						return fmt.Errorf("delete legacy committee duty: %w", err)
					}
				}
				return nil
			}); err != nil {
				return fmt.Errorf("flush committee duty role field batch: %w", err)
			}
			migrated += len(pending)
			logger.Info("migration in progress", zap.Int("migrated", migrated))
			pending = pending[:0]
			pendingBytes = 0
			return nil
		}

		if err = opt.Db.GetAll(prefix, func(i int, obj basedb.Obj) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			if len(obj.Key) != oldKeyLen {
				return nil
			}

			legacyTrace := new(migration_9_CommitteeDutyTraceV1)
			if err := legacyTrace.UnmarshalSSZ(obj.Value); err != nil {
				return fmt.Errorf("unmarshal legacy committee duty: %w", err)
			}

			trace := &traces.CommitteeDutyTrace{
				ConsensusTrace: traces.ConsensusTrace{
					Rounds:   convertRoundsV1(legacyTrace.Rounds),
					Decideds: convertDecidedsV1(legacyTrace.Decideds),
				},
				Slot:         phase0.Slot(legacyTrace.Slot),
				Role:         spectypes.RoleCommittee,
				CommitteeID:  spectypes.CommitteeID(legacyTrace.CommitteeID),
				OperatorIDs:  convertOperatorIDsV1(legacyTrace.OperatorIDs),
				ProposalData: legacyTrace.ProposalData,
				SyncCommittee: convertSignerDataV1(
					legacyTrace.SyncCommittee,
				),
				Attester: convertSignerDataV1(legacyTrace.Attester),
			}

			value, err := trace.MarshalSSZ()
			if err != nil {
				return fmt.Errorf("marshal committee duty with role: %w", err)
			}

			newKey := make([]byte, 0, newKeyLen)
			newKey = append(newKey, obj.Key[:slotKeyLen]...)
			newKey = append(newKey, roleByte)
			newKey = append(newKey, obj.Key[slotKeyLen:]...)

			pending = append(pending, migration_9_pendingRewrite{
				newKey: newKey,
				value:  value,
				oldKey: obj.Key,
			})
			pendingBytes += len(value) + len(newKey) + len(obj.Key)

			if len(pending) >= migrationBatchSize || pendingBytes >= migrationBatchBytes {
				return flush()
			}

			return nil
		}); err != nil {
			return fmt.Errorf("migrate committee duty role field: %w", err)
		}

		if err = flush(); err != nil {
			return fmt.Errorf("migrate committee duty role field: %w", err)
		}

		return nil
	},
}

// migration_9_pendingRewrite holds a single legacy-to-role-aware key rewrite
// accumulated in memory before being committed as part of a batch.
type migration_9_pendingRewrite struct {
	newKey []byte
	value  []byte
	oldKey []byte
}

func convertSignerDataV1(in []*migration_9_SignerData) []*traces.SignerData {
	out := make([]*traces.SignerData, 0, len(in))
	for _, item := range in {
		if item == nil {
			continue
		}
		validatorIdx := make([]phase0.ValidatorIndex, len(item.ValidatorIdx))
		for i, idx := range item.ValidatorIdx {
			validatorIdx[i] = phase0.ValidatorIndex(idx)
		}
		out = append(out, &traces.SignerData{
			Signer:       item.Signer,
			ValidatorIdx: validatorIdx,
			ReceivedTime: item.ReceivedTime,
		})
	}
	return out
}

func convertOperatorIDsV1(in []uint64) []spectypes.OperatorID {
	out := make([]spectypes.OperatorID, len(in))
	copy(out, in)
	return out
}

func convertDecidedsV1(in []*migration_9_DecidedTrace) []*traces.DecidedTrace {
	out := make([]*traces.DecidedTrace, 0, len(in))
	for _, item := range in {
		if item == nil {
			continue
		}
		signers := convertOperatorIDsV1(item.Signers)
		out = append(out, &traces.DecidedTrace{
			Round:        item.Round,
			BeaconRoot:   phase0.Root(item.BeaconRoot),
			Signers:      signers,
			ReceivedTime: item.ReceivedTime,
		})
	}
	return out
}

func convertQBFTTraceV1(in *migration_9_QBFTTrace) *traces.QBFTTrace {
	if in == nil {
		return nil
	}
	return &traces.QBFTTrace{
		Round:        in.Round,
		BeaconRoot:   phase0.Root(in.BeaconRoot),
		Signer:       in.Signer,
		ReceivedTime: in.ReceivedTime,
	}
}

func convertQBFTTracesV1(in []*migration_9_QBFTTrace) []*traces.QBFTTrace {
	out := make([]*traces.QBFTTrace, 0, len(in))
	for _, item := range in {
		if item == nil {
			continue
		}
		out = append(out, convertQBFTTraceV1(item))
	}
	return out
}

func convertRoundChangeV1(in *migration_9_RoundChangeTrace) *traces.RoundChangeTrace {
	if in == nil {
		return nil
	}
	return &traces.RoundChangeTrace{
		QBFTTrace:     *convertQBFTTraceV1(&in.migration_9_QBFTTrace),
		PreparedRound: in.PreparedRound,
		PrepareMessages: convertQBFTTracesV1(
			in.PrepareMessages,
		),
	}
}

func convertRoundChangesV1(in []*migration_9_RoundChangeTrace) []*traces.RoundChangeTrace {
	out := make([]*traces.RoundChangeTrace, 0, len(in))
	for _, item := range in {
		if item == nil {
			continue
		}
		out = append(out, convertRoundChangeV1(item))
	}
	return out
}

func convertProposalTraceV1(in *migration_9_ProposalTrace) *traces.ProposalTrace {
	if in == nil {
		return nil
	}
	return &traces.ProposalTrace{
		QBFTTrace: *convertQBFTTraceV1(
			&in.migration_9_QBFTTrace,
		),
		RoundChanges:    convertRoundChangesV1(in.RoundChanges),
		PrepareMessages: convertQBFTTracesV1(in.PrepareMessages),
	}
}

func convertRoundTraceV1(in *migration_9_RoundTrace) *traces.RoundTrace {
	if in == nil {
		return nil
	}
	return &traces.RoundTrace{
		Proposer:      in.Proposer,
		ProposalTrace: convertProposalTraceV1(in.ProposalTrace),
		Prepares:      convertQBFTTracesV1(in.Prepares),
		Commits:       convertQBFTTracesV1(in.Commits),
		RoundChanges:  convertRoundChangesV1(in.RoundChanges),
	}
}

func convertRoundsV1(in []*migration_9_RoundTrace) []*traces.RoundTrace {
	out := make([]*traces.RoundTrace, 0, len(in))
	for _, item := range in {
		if item == nil {
			continue
		}
		out = append(out, convertRoundTraceV1(item))
	}
	return out
}
