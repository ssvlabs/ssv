package gloas

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// Real Gloas data from the public Glamsterdam devnet-8 chain (mainnet preset), fetched as SSZ from
// beacon.glamsterdam-devnet-8.ethpandaops.io: the finalized block at slot 144352
// (GET /eth/v2/beacon/blocks/{root}) and its execution-payload envelope
// (GET /eth/v1/beacon/execution_payload_envelopes/{slot}). The expected block root is the chain's own
// header root for the slot; the proposer pubkey and genesis validators root come from the same node.
//
// EIP-7688 / EIP-7916 change only merkleization, so an SSZ round-trip cannot catch a wrong
// hash_tree_root (#3008); only a comparison against a consensus client's root can. These tests pin the
// node's Gloas block root — what the §4 proposer signs and the §6 envelope keys on — to the chain's, and
// re-run the proposer-signature check a beacon node performs on submit.
const (
	devnet8GoldenBlockFile    = "devnet8_gloas_block_144352.ssz"
	devnet8GoldenEnvelopeFile = "devnet8_gloas_envelope_144352.ssz"
	devnet8GoldenBlockSlot    = 144352
	devnet8GoldenBlockRoot    = "1f6c7bd0c7a6dc446057bacc566df52117dbfb3ee586148948271d6821cf22f1"
	devnet8GoldenProposer     = "a2d358758c872ab8c0ec9faac8594cc18a3f23f7a74301b7890876d70844aca7337972624a13496c5b811585d926286d"
	devnet8GenesisValsRoot    = "bb4a1a9e3f7f4e10edcd734e4acc3b5ffd4f830efe0af2748fa458cfee5d2658"
)

// devnet8GloasForkVersion is devnet-8's GLOAS_FORK_VERSION; slot 144352 (epoch 4511) is past GLOAS_FORK_EPOCH 1536.
var devnet8GloasForkVersion = phase0.Version{0x80, 0x73, 0x31, 0x83}

func loadGoldenBlock(t *testing.T) (raw []byte, signed *SignedBeaconBlock) {
	raw, err := os.ReadFile(filepath.Join("testdata", devnet8GoldenBlockFile))
	require.NoError(t, err)
	signed = &SignedBeaconBlock{}
	require.NoError(t, signed.UnmarshalSSZ(raw))
	require.EqualValues(t, devnet8GoldenBlockSlot, signed.Message.Slot)
	return raw, signed
}

func mustHex(t *testing.T, s string) []byte {
	b, err := hex.DecodeString(s)
	require.NoError(t, err)
	return b
}

func TestGloasBlockHashTreeRootMatchesChain(t *testing.T) {
	raw, signed := loadGoldenBlock(t)

	reencoded, err := signed.MarshalSSZ()
	require.NoError(t, err)
	require.Equal(t, raw, reencoded, "SSZ round-trip must be byte-identical")

	got, err := signed.Message.HashTreeRoot()
	require.NoError(t, err)
	require.Equal(t, mustHex(t, devnet8GoldenBlockRoot), got[:], "block hash_tree_root must match the chain's header root")

	// The proposer signs (and the §6 envelope keys on) the root of the block decoded from the QBFT value.
	blockSSZ, err := signed.Message.MarshalSSZ()
	require.NoError(t, err)
	decoded, err := DecodeBeaconBlock(blockSSZ)
	require.NoError(t, err)
	decodedRoot, err := decoded.HashTreeRoot()
	require.NoError(t, err)
	require.Equal(t, got, decodedRoot)
}

// The check a beacon node runs on a submitted block: the proposer's BLS signature must verify over
// compute_signing_root(block, DOMAIN_BEACON_PROPOSER at the slot's fork). It passes only if both the
// node's block root and its domain derivation agree with the chain's.
func TestGloasBlockProposerSignatureVerifies(t *testing.T) {
	require.NoError(t, bls.Init(bls.BLS12_381))
	require.NoError(t, bls.SetETHmode(bls.EthModeDraft07))

	_, signed := loadGoldenBlock(t)

	var genesisValidatorsRoot phase0.Root
	copy(genesisValidatorsRoot[:], mustHex(t, devnet8GenesisValsRoot))
	domain, err := spectypes.ComputeETHDomain(spectypes.DomainProposer, devnet8GloasForkVersion, genesisValidatorsRoot)
	require.NoError(t, err)
	signingRoot, err := spectypes.ComputeETHSigningRoot(signed.Message, domain)
	require.NoError(t, err)

	var pk bls.PublicKey
	require.NoError(t, pk.Deserialize(mustHex(t, devnet8GoldenProposer)))
	// Copy the signature out of the block: herumi's cgo bindings reject a pointer into a Go struct
	// that itself holds Go pointers.
	sigBytes := append([]byte(nil), signed.Signature[:]...)
	var sig bls.Sign
	require.NoError(t, sig.Deserialize(sigBytes))
	require.True(t, sig.VerifyByte(&pk, signingRoot[:]), "proposer signature must verify over the node's signing root")
}

// The §6 envelope for the same block: decodes with the aliased types, round-trips byte-identically,
// references the block by the root above, and blinds to the same hash tree root the full envelope has
// (the property the §6 duty's blinded signing relies on).
func TestGloasEnvelopeGoldenFixture(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", devnet8GoldenEnvelopeFile))
	require.NoError(t, err)

	signed := &SignedExecutionPayloadEnvelope{}
	require.NoError(t, signed.UnmarshalSSZ(raw))
	reencoded, err := signed.MarshalSSZ()
	require.NoError(t, err)
	require.Equal(t, raw, reencoded, "SSZ round-trip must be byte-identical")

	envelope := signed.Message
	require.Equal(t, mustHex(t, devnet8GoldenBlockRoot), envelope.BeaconBlockRoot[:])
	require.EqualValues(t, devnet8GoldenBlockSlot, envelope.Payload.SlotNumber)
	require.NotEmpty(t, envelope.Payload.Transactions)

	fullRoot, err := envelope.HashTreeRoot()
	require.NoError(t, err)
	blinded, err := Blinded(envelope)
	require.NoError(t, err)
	blindedRoot, err := blinded.HashTreeRoot()
	require.NoError(t, err)
	require.Equal(t, fullRoot, blindedRoot, "blinded envelope must hash to the full envelope's root")
}
