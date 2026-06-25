package gloas

import (
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/capella"
	"github.com/attestantio/go-eth2-client/spec/electra"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	bitfield "github.com/prysmaticlabs/go-bitfield"
)

// Regenerate with `go generate ./...`. -path is the package dir so sszgen resolves the sibling gloas
// types the body references; --exclude-objs leaves their (already-generated) SSZ in their own files,
// and --output collects only the body types here. Includes track go-eth2-client via `go list -m`.
//go:generate sh -c "go tool -modfile=../../../../tool.mod sszgen -path . --include $(go list -m -f '{{.Dir}}' github.com/attestantio/go-eth2-client)/spec/phase0,$(go list -m -f '{{.Dir}}' github.com/attestantio/go-eth2-client)/spec/altair,$(go list -m -f '{{.Dir}}' github.com/attestantio/go-eth2-client)/spec/capella,$(go list -m -f '{{.Dir}}' github.com/attestantio/go-eth2-client)/spec/electra,$(go list -m -f '{{.Dir}}' github.com/attestantio/go-eth2-client)/spec/bellatrix,$(go list -m -f '{{.Dir}}' github.com/attestantio/go-eth2-client)/spec/deneb --objs PayloadAttestation,BeaconBlockBody,BeaconBlock,SignedBeaconBlock --exclude-objs ExecutionPayloadBid,SignedExecutionPayloadBid,PayloadAttestationData --output ./beacon_block_encoding.go"

// PayloadAttestation is the aggregated PTC attestation the proposer includes in the block body for the
// previous slot's payload (consensus-specs gloas) — distinct from the single-member
// PayloadAttestationMessage SSV signs in §3. AggregationBits is a Bitvector[PTC_SIZE], PTC_SIZE = 512.
type PayloadAttestation struct {
	AggregationBits bitfield.Bitvector512 `ssz-size:"64"`
	Data            *PayloadAttestationData
	Signature       phase0.BLSSignature `ssz-size:"96"`
}

// BeaconBlockBody is the Gloas (ePBS) block body. Versus Electra it drops the inline execution payload
// and execution requests and adds SignedExecutionPayloadBid (the payload commitment), PayloadAttestations
// (the previous slot's PTC aggregate), and ParentExecutionRequests. Field order/tags match the pinned
// spec / go-eth2-client PR #280; everything else reuses the existing fork types.
type BeaconBlockBody struct {
	RANDAOReveal              phase0.BLSSignature `ssz-size:"96"`
	ETH1Data                  *phase0.ETH1Data
	Graffiti                  [32]byte                      `ssz-size:"32"`
	ProposerSlashings         []*phase0.ProposerSlashing    `ssz-max:"16"`
	AttesterSlashings         []*electra.AttesterSlashing   `ssz-max:"1"`
	Attestations              []*electra.Attestation        `ssz-max:"8"`
	Deposits                  []*phase0.Deposit             `ssz-max:"16"`
	VoluntaryExits            []*phase0.SignedVoluntaryExit `ssz-max:"16"`
	SyncAggregate             *altair.SyncAggregate
	BLSToExecutionChanges     []*capella.SignedBLSToExecutionChange `ssz-max:"16"`
	SignedExecutionPayloadBid *SignedExecutionPayloadBid
	PayloadAttestations       []*PayloadAttestation `ssz-max:"4"`
	ParentExecutionRequests   *electra.ExecutionRequests
}

// BeaconBlock is the Gloas (ePBS) beacon block.
type BeaconBlock struct {
	Slot          phase0.Slot
	ProposerIndex phase0.ValidatorIndex
	ParentRoot    phase0.Root `ssz-size:"32"`
	StateRoot     phase0.Root `ssz-size:"32"`
	Body          *BeaconBlockBody
}

// SignedBeaconBlock wraps a Gloas BeaconBlock with the proposer's signature.
type SignedBeaconBlock struct {
	Message   *BeaconBlock
	Signature phase0.BLSSignature `ssz-size:"96"`
}

// Encode/Decode are the convenience wrappers the proposer runner uses to marshal the block into the
// QBFT DataSSZ and to publish the signed block.
func (b *BeaconBlock) Encode() ([]byte, error)  { return b.MarshalSSZ() }
func (b *BeaconBlock) Decode(data []byte) error { return b.UnmarshalSSZ(data) }

func (b *SignedBeaconBlock) Encode() ([]byte, error)  { return b.MarshalSSZ() }
func (b *SignedBeaconBlock) Decode(data []byte) error { return b.UnmarshalSSZ(data) }
