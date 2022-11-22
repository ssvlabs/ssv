package types

import (
	"crypto/sha256"
	"encoding/json"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	qbftstorage "github.com/bloxapp/ssv/protocol/v2/qbft/storage"
	"github.com/pkg/errors"
)

type signing interface {
	// GetSigner returns a Signer instance
	GetSigner() spectypes.SSVSigner
	// GetSignatureDomainType returns the Domain type used for signatures
	GetSignatureDomainType() spectypes.DomainType
}

type IConfig interface {
	signing
	// GetValueCheckF returns value check function
	GetValueCheckF() specqbft.ProposedValueCheckF
	// GetProposerF returns func used to calculate proposer
	GetProposerF() specqbft.ProposerF
	// GetNetwork returns a p2p Network instance
	GetNetwork() specqbft.Network
	// GetStorage returns a storage instance
	GetStorage() qbftstorage.QBFTStore
	// GetTimer returns round timer
	GetTimer() specqbft.Timer
}

type Config struct {
	Signer      spectypes.SSVSigner
	SigningPK   []byte
	Domain      spectypes.DomainType
	ValueCheckF specqbft.ProposedValueCheckF
	ProposerF   specqbft.ProposerF
	Storage     qbftstorage.QBFTStore
	Network     specqbft.Network
	Timer       specqbft.Timer
}

// GetSigner returns a Signer instance
func (c *Config) GetSigner() spectypes.SSVSigner {
	return c.Signer
}

// GetSigningPubKey returns the public key used to sign all QBFT messages
func (c *Config) GetSigningPubKey() []byte {
	return c.SigningPK
}

// GetSignatureDomainType returns the Domain type used for signatures
func (c *Config) GetSignatureDomainType() spectypes.DomainType {
	return c.Domain
}

// GetValueCheckF returns value check instance
func (c *Config) GetValueCheckF() specqbft.ProposedValueCheckF {
	return c.ValueCheckF
}

// GetProposerF returns func used to calculate proposer
func (c *Config) GetProposerF() specqbft.ProposerF {
	return c.ProposerF
}

// GetNetwork returns a p2p Network instance
func (c *Config) GetNetwork() specqbft.Network {
	return c.Network
}

// GetStorage returns a storage instance
func (c *Config) GetStorage() qbftstorage.QBFTStore {
	return c.Storage
}

// GetTimer returns round timer
func (c *Config) GetTimer() specqbft.Timer {
	return c.Timer
}

type State struct {
	Share                           *spectypes.Share
	ID                              []byte // instance Identifier
	Round                           specqbft.Round
	Height                          specqbft.Height
	LastPreparedRound               specqbft.Round
	LastPreparedValue               []byte
	ProposalAcceptedForCurrentRound *specqbft.SignedMessage
	Decided                         bool
	DecidedValue                    []byte

	ProposeContainer     *specqbft.MsgContainer
	PrepareContainer     *specqbft.MsgContainer
	CommitContainer      *specqbft.MsgContainer
	RoundChangeContainer *specqbft.MsgContainer
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

// type ProposedValueCheckF func(data []byte) error
// type ProposerF func(state *qbft.State, round qbft.Round) types.OperatorID
