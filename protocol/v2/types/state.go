package types

import (
	"crypto/sha256"
	"encoding/json"
	"github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/protocol/v2/network"
	"github.com/pkg/errors"
)

type signing interface {
	// GetSigner returns a Signer instance
	GetSigner() types.SSVSigner
	// GetSignatureDomainType returns the Domain type used for signatures
	GetSignatureDomainType() types.DomainType
}

type IConfig interface {
	signing
	// GetValueCheckF returns value check function
	GetValueCheckF() ProposedValueCheckF
	// GetProposerF returns func used to calculate proposer
	GetProposerF() ProposerF
	// GetNetwork returns a p2p Network instance
	GetNetwork() network.Network
	// GetStorage returns a storage instance
	GetStorage() qbft.Storage
	// GetTimer returns round timer
	GetTimer() qbft.Timer
}

type Config struct {
	Signer      types.SSVSigner
	SigningPK   []byte
	Domain      types.DomainType
	ValueCheckF ProposedValueCheckF
	ProposerF   ProposerF
	Storage     qbft.Storage
	Network     network.Network
	Timer       qbft.Timer
}

// GetSigner returns a Signer instance
func (c *Config) GetSigner() types.SSVSigner {
	return c.Signer
}

// GetSigningPubKey returns the public key used to sign all QBFT messages
func (c *Config) GetSigningPubKey() []byte {
	return c.SigningPK
}

// GetSignatureDomainType returns the Domain type used for signatures
func (c *Config) GetSignatureDomainType() types.DomainType {
	return c.Domain
}

// GetValueCheckF returns value check instance
func (c *Config) GetValueCheckF() ProposedValueCheckF {
	return c.ValueCheckF
}

// GetProposerF returns func used to calculate proposer
func (c *Config) GetProposerF() ProposerF {
	return c.ProposerF
}

// GetNetwork returns a p2p Network instance
func (c *Config) GetNetwork() network.Network {
	return c.Network
}

// GetStorage returns a storage instance
func (c *Config) GetStorage() qbft.Storage {
	return c.Storage
}

// GetTimer returns round timer
func (c *Config) GetTimer() qbft.Timer {
	return c.Timer
}

type State struct {
	Share                           *types.Share
	ID                              []byte // instance Identifier
	Round                           qbft.Round
	Height                          qbft.Height
	LastPreparedRound               qbft.Round
	LastPreparedValue               []byte
	ProposalAcceptedForCurrentRound *qbft.SignedMessage
	Decided                         bool
	DecidedValue                    []byte

	ProposeContainer     *qbft.MsgContainer
	PrepareContainer     *qbft.MsgContainer
	CommitContainer      *qbft.MsgContainer
	RoundChangeContainer *qbft.MsgContainer
}

// GetRoot returns the state's deterministic root
func (s *State) GetRoot() ([]byte, error) {
	marshaledRoot, err := s.Encode()
	if err != nil {
		return nil, errors.Wrap(err, "could not encode state")
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret[:], nil
}

// Encode returns a msg encoded bytes or error
func (s *State) Encode() ([]byte, error) {
	return json.Marshal(s)
}

// Decode returns error if decoding failed
func (s *State) Decode(data []byte) error {
	return json.Unmarshal(data, &s)
}

type ProposedValueCheckF func(data []byte) error
type ProposerF func(state *qbft.State, round qbft.Round) types.OperatorID
