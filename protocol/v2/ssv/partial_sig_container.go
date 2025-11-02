package ssv

import (
    "encoding/hex"
    "maps"
    "sort"
    "strconv"
    "strings"
    "sync"

    "github.com/attestantio/go-eth2-client/spec/phase0"
    "github.com/pkg/errors"
    specssv "github.com/ssvlabs/ssv-spec/ssv"
    spectypes "github.com/ssvlabs/ssv-spec/types"

    "github.com/ssvlabs/ssv/protocol/v2/types"
    "github.com/ssvlabs/ssv/utils/threshold"
    "go.uber.org/zap"
)

type SigningRoot string

type PartialSigContainer struct {
	// signaturesMu protects access to the Signatures map to prevent concurrent read/write access
	// from multiple goroutines
	signaturesMu sync.RWMutex
	// Signature map: validator index -> signing root -> operator id (signer) -> signature (from the signer for the validator's signing root)
	Signatures map[phase0.ValidatorIndex]map[specssv.SigningRoot]map[spectypes.OperatorID]spectypes.Signature
	// Quorum is the number of min signatures needed for quorum
	Quorum uint64
}

func NewPartialSigContainer(quorum uint64) *PartialSigContainer {
	return &PartialSigContainer{
		Quorum:     quorum,
		Signatures: make(map[phase0.ValidatorIndex]map[specssv.SigningRoot]map[spectypes.OperatorID]spectypes.Signature),
	}
}

// PartialSigContainerStatus provides a snapshot summary of collected signatures.
// It reports, for each validator and signing root, how many partial signatures
// were seen and whether the quorum threshold was reached.
type PartialSigContainerStatus struct {
    // Quorum is the minimum number of signatures required per (validator, root).
    Quorum uint64 `json:"quorum"`
    // Validators maps validator index -> signing root -> aggregated info.
    Validators map[phase0.ValidatorIndex]map[specssv.SigningRoot]RootStatus `json:"validators"`
}

// RootStatus summarizes signature collection for a specific signing root.
type RootStatus struct {
    Count     uint64 `json:"count"`
    HasQuorum bool   `json:"has_quorum"`
}

// Status returns a read-only snapshot of the container's current state.
// No internal references are exposed: returned maps are freshly allocated.
func (ps *PartialSigContainer) Status() PartialSigContainerStatus {
    ps.signaturesMu.RLock()
    defer ps.signaturesMu.RUnlock()

    out := PartialSigContainerStatus{
        Quorum:     ps.Quorum,
        Validators: make(map[phase0.ValidatorIndex]map[specssv.SigningRoot]RootStatus, len(ps.Signatures)),
    }

    for vIdx, roots := range ps.Signatures {
        rootStatus := make(map[specssv.SigningRoot]RootStatus, len(roots))
        for r, signers := range roots {
            cnt := uint64(len(signers))
            rootStatus[r] = RootStatus{
                Count:     cnt,
                HasQuorum: cnt >= ps.Quorum,
            }
        }
        out.Validators[vIdx] = rootStatus
    }
    return out
}

func (ps *PartialSigContainer) AddSignature(sigMsg *spectypes.PartialSignatureMessage) {
	ps.signaturesMu.Lock()
	defer ps.signaturesMu.Unlock()

	if ps.Signatures[sigMsg.ValidatorIndex] == nil {
		ps.Signatures[sigMsg.ValidatorIndex] = make(map[specssv.SigningRoot]map[spectypes.OperatorID]spectypes.Signature)
	}
	if ps.Signatures[sigMsg.ValidatorIndex][signingRootHex(sigMsg.SigningRoot)] == nil {
		ps.Signatures[sigMsg.ValidatorIndex][signingRootHex(sigMsg.SigningRoot)] = make(map[spectypes.OperatorID]spectypes.Signature)
	}
	m := ps.Signatures[sigMsg.ValidatorIndex][signingRootHex(sigMsg.SigningRoot)]

	if m[sigMsg.Signer] == nil {
		m[sigMsg.Signer] = make([]byte, 96)
		copy(m[sigMsg.Signer], sigMsg.PartialSignature)
	}
}

// HasSignature returns true if container has signature for signer and signing root, else it returns false
func (ps *PartialSigContainer) HasSignature(validatorIndex phase0.ValidatorIndex, signer spectypes.OperatorID, signingRoot [32]byte) bool {
	_, err := ps.GetSignature(validatorIndex, signer, signingRoot)
	return err == nil
}

// GetSignature returns the signature for a given root and signer
func (ps *PartialSigContainer) GetSignature(validatorIndex phase0.ValidatorIndex, signer spectypes.OperatorID, signingRoot [32]byte) (spectypes.Signature, error) {
	ps.signaturesMu.RLock()
	defer ps.signaturesMu.RUnlock()

	if ps.Signatures[validatorIndex] == nil {
		return nil, errors.New("Dont have signature for the given validator index")
	}
	if ps.Signatures[validatorIndex][signingRootHex(signingRoot)] == nil {
		return nil, errors.New("Dont have signature for the given signing root")
	}
	if ps.Signatures[validatorIndex][signingRootHex(signingRoot)][signer] == nil {
		return nil, errors.New("Dont have signature on signing root for the given signer")
	}
	return ps.Signatures[validatorIndex][signingRootHex(signingRoot)][signer], nil
}

// GetSignatures Return signature map for given root
func (ps *PartialSigContainer) GetSignatures(validatorIndex phase0.ValidatorIndex, signingRoot [32]byte) map[spectypes.OperatorID]spectypes.Signature {
	ps.signaturesMu.RLock()
	defer ps.signaturesMu.RUnlock()

	signatures := ps.Signatures[validatorIndex][signingRootHex(signingRoot)]
	if signatures == nil {
		return nil
	}

	signaturesCopy := make(map[spectypes.OperatorID]spectypes.Signature, len(signatures))
	maps.Copy(signaturesCopy, signatures)

	// Return a copy to avoid external mutation and avoid data races
	return signaturesCopy
}

// Remove signer from signature map
func (ps *PartialSigContainer) Remove(validatorIndex phase0.ValidatorIndex, signer uint64, signingRoot [32]byte) {
	ps.signaturesMu.Lock()
	defer ps.signaturesMu.Unlock()

	if ps.Signatures[validatorIndex] == nil {
		return
	}
	if ps.Signatures[validatorIndex][signingRootHex(signingRoot)] == nil {
		return
	}
	if ps.Signatures[validatorIndex][signingRootHex(signingRoot)][signer] == nil {
		return
	}
	delete(ps.Signatures[validatorIndex][signingRootHex(signingRoot)], signer)
}

func (ps *PartialSigContainer) ReconstructSignature(root [32]byte, validatorPubKey []byte, validatorIndex phase0.ValidatorIndex) ([]byte, error) {
	ps.signaturesMu.RLock()
	defer ps.signaturesMu.RUnlock()

	// Reconstruct signatures
	if ps.Signatures[validatorIndex] == nil {
		return nil, errors.New("no signatures for the given validator index")
	}
	if ps.Signatures[validatorIndex][signingRootHex(root)] == nil {
		return nil, errors.New("no signatures for the given signing root")
	}

	operatorsSignatures := make(map[uint64][]byte)
	for operatorID, sig := range ps.Signatures[validatorIndex][signingRootHex(root)] {
		operatorsSignatures[operatorID] = sig
	}
	signature, err := threshold.ReconstructSignatures(operatorsSignatures)
	if err != nil {
		return nil, errors.Wrap(err, "failed to reconstruct signatures")
	}

	// Get validator pub key copy (This avoids cgo Go pointer to Go pointer issue)
	validatorPubKeyCopy := make([]byte, len(validatorPubKey))
	copy(validatorPubKeyCopy, validatorPubKey)

	if err := types.VerifyReconstructedSignature(signature, validatorPubKeyCopy, root); err != nil {
		return nil, errors.Wrap(err, "failed to verify reconstruct signature")
	}
	return signature.Serialize(), nil
}

func (ps *PartialSigContainer) HasQuorum(validatorIndex phase0.ValidatorIndex, root [32]byte) bool {
    ps.signaturesMu.RLock()
    defer ps.signaturesMu.RUnlock()

    return uint64(len(ps.Signatures[validatorIndex][signingRootHex(root)])) >= ps.Quorum
}

func signingRootHex(r [32]byte) specssv.SigningRoot {
    return specssv.SigningRoot(hex.EncodeToString(r[:]))
}

// String implements a detailed, log-friendly snapshot of the container.
// Output format (single line):
// PartialSigContainer{quorum=Q, validators=[{index=V, roots=[{root=R, count=C, has_quorum=T, signers=[id1,id2,...]} ...]} ...]}
func (ps *PartialSigContainer) String() string {
    ps.signaturesMu.RLock()
    defer ps.signaturesMu.RUnlock()

    var b strings.Builder
    b.Grow(256)
    b.WriteString("PartialSigContainer{quorum=")
    b.WriteString(strconv.FormatUint(ps.Quorum, 10))
    b.WriteString(", validators=[")

    // Collect and sort validator indices for stable output
    vIndices := make([]phase0.ValidatorIndex, 0, len(ps.Signatures))
    for v := range ps.Signatures {
        vIndices = append(vIndices, v)
    }
    sort.Slice(vIndices, func(i, j int) bool { return vIndices[i] < vIndices[j] })

    for vi, v := range vIndices {
        if vi > 0 {
            b.WriteByte(',')
            b.WriteByte(' ')
        }
        b.WriteString("{index=")
        b.WriteString(strconv.FormatUint(uint64(v), 10))
        b.WriteString(", roots=[")

        rootsMap := ps.Signatures[v]
        // Collect and sort roots for stable output
        roots := make([]string, 0, len(rootsMap))
        for r := range rootsMap {
            roots = append(roots, string(r))
        }
        sort.Strings(roots)

        for ri, r := range roots {
            if ri > 0 {
                b.WriteByte(',')
                b.WriteByte(' ')
            }
            signersMap := rootsMap[specssv.SigningRoot(r)]
            // Collect and sort signer IDs
            signers := make([]spectypes.OperatorID, 0, len(signersMap))
            for id := range signersMap {
                signers = append(signers, id)
            }
            sort.Slice(signers, func(i, j int) bool { return signers[i] < signers[j] })

            b.WriteString("{root=")
            b.WriteString(r)
            b.WriteString(", count=")
            b.WriteString(strconv.FormatUint(uint64(len(signers)), 10))
            b.WriteString(", has_quorum=")
            if uint64(len(signers)) >= ps.Quorum {
                b.WriteString("true")
            } else {
                b.WriteString("false")
            }
            b.WriteString(", signers=[")
            for si, id := range signers {
                if si > 0 {
                    b.WriteByte(',')
                }
                b.WriteString(strconv.FormatUint(uint64(id), 10))
            }
            b.WriteString("]}")
        }
        b.WriteString("]}")
    }

    b.WriteString("]}")
    return b.String()
}

// Fields returns a structured set of zap fields describing the current
// content of the container, including per-validator per-root signer IDs.
// Intended usage: logger.Debug("partial sigs", ps.Fields()...)
func (ps *PartialSigContainer) Fields() []zap.Field {
    ps.signaturesMu.RLock()
    defer ps.signaturesMu.RUnlock()

    // Build a deterministic, nested structure for logging
    vIndices := make([]phase0.ValidatorIndex, 0, len(ps.Signatures))
    for v := range ps.Signatures {
        vIndices = append(vIndices, v)
    }
    sort.Slice(vIndices, func(i, j int) bool { return vIndices[i] < vIndices[j] })

    validators := make([]map[string]any, 0, len(vIndices))
    var (
        totalRoots      int
        totalSignatures int
    )
    for _, v := range vIndices {
        rootsMap := ps.Signatures[v]
        // Sort roots for stable output
        roots := make([]string, 0, len(rootsMap))
        for r := range rootsMap {
            roots = append(roots, string(r))
        }
        sort.Strings(roots)

        rootEntries := make([]map[string]any, 0, len(roots))
        for _, r := range roots {
            signersMap := rootsMap[specssv.SigningRoot(r)]
            // Sort signers
            signers := make([]spectypes.OperatorID, 0, len(signersMap))
            for id := range signersMap {
                signers = append(signers, id)
            }
            sort.Slice(signers, func(i, j int) bool { return signers[i] < signers[j] })

            // Convert to []uint64 for JSON friendliness
            signerIDs := make([]uint64, len(signers))
            for i := range signers {
                signerIDs[i] = uint64(signers[i])
            }

            cnt := len(signers)
            totalRoots++
            totalSignatures += cnt

            rootEntries = append(rootEntries, map[string]any{
                "root":       r,
                "count":      cnt,
                "has_quorum": uint64(cnt) >= ps.Quorum,
                "signers":    signerIDs,
            })
        }

        validators = append(validators, map[string]any{
            "index": uint64(v),
            "roots": rootEntries,
        })
    }

    return []zap.Field{
        zap.Uint64("quorum", ps.Quorum),
        zap.Int("validators_count", len(validators)),
        zap.Int("roots_count", totalRoots),
        zap.Int("signatures_count", totalSignatures),
        zap.Any("validators", validators),
    }
}
