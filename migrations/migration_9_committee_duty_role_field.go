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

			roleByte, err := estore.CommitteeRunnerRoleToPrefix(spectypes.RoleCommittee)
			if err != nil {
				return fmt.Errorf("map committee runner role to prefix: %w", err)
			}

			newKey := make([]byte, 0, newKeyLen)
			newKey = append(newKey, obj.Key[:slotKeyLen]...)
			newKey = append(newKey, roleByte)
			newKey = append(newKey, obj.Key[slotKeyLen:]...)
			if err := opt.Db.Set(prefix, newKey, value); err != nil {
				return fmt.Errorf("set committee duty with role: %w", err)
			}
			if err := opt.Db.Delete(prefix, obj.Key); err != nil {
				return fmt.Errorf("delete legacy committee duty: %w", err)
			}

			migrated++
			return nil
		}); err != nil {
			return fmt.Errorf("migrate committee duty role field: %w", err)
		}

		return nil
	},
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
