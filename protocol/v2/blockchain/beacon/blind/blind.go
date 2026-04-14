package blind

import (
	"fmt"

	"github.com/attestantio/go-eth2-client/api"
	apiv1capella "github.com/attestantio/go-eth2-client/api/v1/capella"
	apiv1deneb "github.com/attestantio/go-eth2-client/api/v1/deneb"
	apiv1electra "github.com/attestantio/go-eth2-client/api/v1/electra"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/capella"
	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	utilbellatrix "github.com/attestantio/go-eth2-client/util/bellatrix"
	utilcapella "github.com/attestantio/go-eth2-client/util/capella"
	ssz "github.com/ferranbt/fastssz"
)

// EnsureBlinded returns a VersionedProposal that is blinded along with the concrete blinded
// block object to sign and encode. If the input was already blinded, it is returned as-is.
func EnsureBlinded(p *api.VersionedProposal) (*api.VersionedProposal, ssz.Marshaler, error) {
	if p == nil {
		return nil, nil, fmt.Errorf("nil proposal")
	}

	if p.Blinded {
		switch p.Version {
		case spec.DataVersionCapella:
			if p.CapellaBlinded == nil {
				return nil, nil, fmt.Errorf("capella blinded block is nil")
			}
			return p, p.CapellaBlinded, nil
		case spec.DataVersionDeneb:
			if p.DenebBlinded == nil {
				return nil, nil, fmt.Errorf("deneb blinded block is nil")
			}
			return p, p.DenebBlinded, nil
		case spec.DataVersionElectra:
			if p.ElectraBlinded == nil {
				return nil, nil, fmt.Errorf("electra blinded block is nil")
			}
			return p, p.ElectraBlinded, nil
		case spec.DataVersionFulu:
			if p.FuluBlinded == nil {
				return nil, nil, fmt.Errorf("fulu blinded block is nil")
			}
			return p, p.FuluBlinded, nil
		default:
			return nil, nil, fmt.Errorf("unsupported version %d", p.Version)
		}
	}

	// Convert full → blinded per fork.
	switch p.Version {
	case spec.DataVersionCapella:
		if p.Capella == nil || p.Capella.Body == nil || p.Capella.Body.ExecutionPayload == nil {
			return nil, nil, fmt.Errorf("capella block or payload is nil")
		}

		payload := p.Capella.Body.ExecutionPayload
		txRoot, wRoot, err := computePayloadRoots(payload.Transactions, payload.Withdrawals)
		if err != nil {
			return nil, nil, err
		}

		eph := &capella.ExecutionPayloadHeader{
			ParentHash:       payload.ParentHash,
			FeeRecipient:     payload.FeeRecipient,
			StateRoot:        payload.StateRoot,
			ReceiptsRoot:     payload.ReceiptsRoot,
			LogsBloom:        payload.LogsBloom,
			PrevRandao:       payload.PrevRandao,
			BlockNumber:      payload.BlockNumber,
			GasLimit:         payload.GasLimit,
			GasUsed:          payload.GasUsed,
			Timestamp:        payload.Timestamp,
			ExtraData:        payload.ExtraData,
			BaseFeePerGas:    payload.BaseFeePerGas,
			BlockHash:        payload.BlockHash,
			TransactionsRoot: txRoot,
			WithdrawalsRoot:  wRoot,
		}

		bb := &apiv1capella.BlindedBeaconBlock{
			Slot:          p.Capella.Slot,
			ProposerIndex: p.Capella.ProposerIndex,
			ParentRoot:    p.Capella.ParentRoot,
			StateRoot:     p.Capella.StateRoot,
			Body: &apiv1capella.BlindedBeaconBlockBody{
				RANDAOReveal:           p.Capella.Body.RANDAOReveal,
				ETH1Data:               p.Capella.Body.ETH1Data,
				Graffiti:               p.Capella.Body.Graffiti,
				ProposerSlashings:      p.Capella.Body.ProposerSlashings,
				AttesterSlashings:      p.Capella.Body.AttesterSlashings,
				Attestations:           p.Capella.Body.Attestations,
				Deposits:               p.Capella.Body.Deposits,
				VoluntaryExits:         p.Capella.Body.VoluntaryExits,
				SyncAggregate:          p.Capella.Body.SyncAggregate,
				ExecutionPayloadHeader: eph,
				BLSToExecutionChanges:  p.Capella.Body.BLSToExecutionChanges,
			},
		}

		return &api.VersionedProposal{Version: p.Version, Blinded: true, CapellaBlinded: bb}, bb, nil

	case spec.DataVersionDeneb:
		if p.Deneb == nil || p.Deneb.Block == nil || p.Deneb.Block.Body == nil || p.Deneb.Block.Body.ExecutionPayload == nil {
			return nil, nil, fmt.Errorf("deneb block or payload is nil")
		}

		payload := p.Deneb.Block.Body.ExecutionPayload
		eph, err := buildDenebPayloadHeader(payload)
		if err != nil {
			return nil, nil, err
		}

		bb := &apiv1deneb.BlindedBeaconBlock{
			Slot:          p.Deneb.Block.Slot,
			ProposerIndex: p.Deneb.Block.ProposerIndex,
			ParentRoot:    p.Deneb.Block.ParentRoot,
			StateRoot:     p.Deneb.Block.StateRoot,
			Body: &apiv1deneb.BlindedBeaconBlockBody{
				RANDAOReveal:           p.Deneb.Block.Body.RANDAOReveal,
				ETH1Data:               p.Deneb.Block.Body.ETH1Data,
				Graffiti:               p.Deneb.Block.Body.Graffiti,
				ProposerSlashings:      p.Deneb.Block.Body.ProposerSlashings,
				AttesterSlashings:      p.Deneb.Block.Body.AttesterSlashings,
				Attestations:           p.Deneb.Block.Body.Attestations,
				Deposits:               p.Deneb.Block.Body.Deposits,
				VoluntaryExits:         p.Deneb.Block.Body.VoluntaryExits,
				SyncAggregate:          p.Deneb.Block.Body.SyncAggregate,
				ExecutionPayloadHeader: eph,
				BLSToExecutionChanges:  p.Deneb.Block.Body.BLSToExecutionChanges,
				BlobKZGCommitments:     p.Deneb.Block.Body.BlobKZGCommitments,
			},
		}

		return &api.VersionedProposal{Version: p.Version, Blinded: true, DenebBlinded: bb}, bb, nil

	case spec.DataVersionElectra:
		if p.Electra == nil || p.Electra.Block == nil || p.Electra.Block.Body == nil || p.Electra.Block.Body.ExecutionPayload == nil {
			return nil, nil, fmt.Errorf("electra block or payload is nil")
		}

		eph, err := buildDenebPayloadHeader(p.Electra.Block.Body.ExecutionPayload)
		if err != nil {
			return nil, nil, err
		}

		bb := &apiv1electra.BlindedBeaconBlock{
			Slot:          p.Electra.Block.Slot,
			ProposerIndex: p.Electra.Block.ProposerIndex,
			ParentRoot:    p.Electra.Block.ParentRoot,
			StateRoot:     p.Electra.Block.StateRoot,
			Body: &apiv1electra.BlindedBeaconBlockBody{
				RANDAOReveal:           p.Electra.Block.Body.RANDAOReveal,
				ETH1Data:               p.Electra.Block.Body.ETH1Data,
				Graffiti:               p.Electra.Block.Body.Graffiti,
				ProposerSlashings:      p.Electra.Block.Body.ProposerSlashings,
				AttesterSlashings:      p.Electra.Block.Body.AttesterSlashings,
				Attestations:           p.Electra.Block.Body.Attestations,
				Deposits:               p.Electra.Block.Body.Deposits,
				VoluntaryExits:         p.Electra.Block.Body.VoluntaryExits,
				SyncAggregate:          p.Electra.Block.Body.SyncAggregate,
				ExecutionPayloadHeader: eph,
				BLSToExecutionChanges:  p.Electra.Block.Body.BLSToExecutionChanges,
				BlobKZGCommitments:     p.Electra.Block.Body.BlobKZGCommitments,
				ExecutionRequests:      p.Electra.Block.Body.ExecutionRequests,
			},
		}

		return &api.VersionedProposal{Version: p.Version, Blinded: true, ElectraBlinded: bb}, bb, nil

	case spec.DataVersionFulu:
		// Fulu reuses Electra blinded block structures in this codebase.
		if p.Fulu == nil || p.Fulu.Block == nil || p.Fulu.Block.Body == nil || p.Fulu.Block.Body.ExecutionPayload == nil {
			return nil, nil, fmt.Errorf("fulu block or payload is nil")
		}

		eph, err := buildDenebPayloadHeader(p.Fulu.Block.Body.ExecutionPayload)
		if err != nil {
			return nil, nil, err
		}

		bb := &apiv1electra.BlindedBeaconBlock{
			Slot:          p.Fulu.Block.Slot,
			ProposerIndex: p.Fulu.Block.ProposerIndex,
			ParentRoot:    p.Fulu.Block.ParentRoot,
			StateRoot:     p.Fulu.Block.StateRoot,
			Body: &apiv1electra.BlindedBeaconBlockBody{
				RANDAOReveal:           p.Fulu.Block.Body.RANDAOReveal,
				ETH1Data:               p.Fulu.Block.Body.ETH1Data,
				Graffiti:               p.Fulu.Block.Body.Graffiti,
				ProposerSlashings:      p.Fulu.Block.Body.ProposerSlashings,
				AttesterSlashings:      p.Fulu.Block.Body.AttesterSlashings,
				Attestations:           p.Fulu.Block.Body.Attestations,
				Deposits:               p.Fulu.Block.Body.Deposits,
				VoluntaryExits:         p.Fulu.Block.Body.VoluntaryExits,
				SyncAggregate:          p.Fulu.Block.Body.SyncAggregate,
				ExecutionPayloadHeader: eph,
				BLSToExecutionChanges:  p.Fulu.Block.Body.BLSToExecutionChanges,
				BlobKZGCommitments:     p.Fulu.Block.Body.BlobKZGCommitments,
				ExecutionRequests:      p.Fulu.Block.Body.ExecutionRequests,
			},
		}

		return &api.VersionedProposal{Version: p.Version, Blinded: true, FuluBlinded: bb}, bb, nil
	default:
		return nil, nil, fmt.Errorf("unsupported version %d", p.Version)
	}
}

// computePayloadRoots returns the SSZ hash-tree-roots of the payload's transactions and
// withdrawals lists. The transaction and withdrawal types are identical across Capella,
// Deneb, Electra, and Fulu payloads, so the same helper serves every fork.
func computePayloadRoots(txs []bellatrix.Transaction, withdrawals []*capella.Withdrawal) (phase0.Root, phase0.Root, error) {
	txRoot, err := (&utilbellatrix.ExecutionPayloadTransactions{Transactions: txs}).HashTreeRoot()
	if err != nil {
		return phase0.Root{}, phase0.Root{}, fmt.Errorf("compute transactions root: %w", err)
	}
	wRoot, err := (&utilcapella.ExecutionPayloadWithdrawals{Withdrawals: withdrawals}).HashTreeRoot()
	if err != nil {
		return phase0.Root{}, phase0.Root{}, fmt.Errorf("compute withdrawals root: %w", err)
	}
	return txRoot, wRoot, nil
}

// buildDenebPayloadHeader converts a Deneb-shaped ExecutionPayload into its header form.
// Deneb, Electra, and Fulu all use deneb.ExecutionPayload / deneb.ExecutionPayloadHeader,
// so one builder serves all three forks.
func buildDenebPayloadHeader(payload *deneb.ExecutionPayload) (*deneb.ExecutionPayloadHeader, error) {
	txRoot, wRoot, err := computePayloadRoots(payload.Transactions, payload.Withdrawals)
	if err != nil {
		return nil, err
	}
	return &deneb.ExecutionPayloadHeader{
		ParentHash:       payload.ParentHash,
		FeeRecipient:     payload.FeeRecipient,
		StateRoot:        payload.StateRoot,
		ReceiptsRoot:     payload.ReceiptsRoot,
		LogsBloom:        payload.LogsBloom,
		PrevRandao:       payload.PrevRandao,
		BlockNumber:      payload.BlockNumber,
		GasLimit:         payload.GasLimit,
		GasUsed:          payload.GasUsed,
		Timestamp:        payload.Timestamp,
		ExtraData:        payload.ExtraData,
		BaseFeePerGas:    payload.BaseFeePerGas,
		BlockHash:        payload.BlockHash,
		TransactionsRoot: txRoot,
		WithdrawalsRoot:  wRoot,
		BlobGasUsed:      payload.BlobGasUsed,
		ExcessBlobGas:    payload.ExcessBlobGas,
	}, nil
}
