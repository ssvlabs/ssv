package controller

import (
	"encoding/base64"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"time"

	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/utils/threshold"
	"github.com/herumi/bls-eth-go-binary/bls"

	"github.com/pkg/errors"
	"go.uber.org/atomic"
	"go.uber.org/zap"
)

type TimerState int

const (
	StateSleep = iota
	StateRunning
	StateTimeout
)

func (s TimerState) toString() string {
	switch s {
	case StateSleep:
		return "sleep"
	case StateRunning:
		return "running"
	case StateTimeout:
		return "timeout"
	}
	return ""
}

type SignatureState struct {
	timer      *time.Timer
	state      atomic.Int32
	signatures map[message.OperatorID][]byte

	height                     message.Height
	SignatureCollectionTimeout time.Duration
	sigCount                   int
	root                       []byte
	valueStruct                *beaconprotocol.DutyData
	duty                       *beaconprotocol.Duty
}

func (s *SignatureState) start(logger *zap.Logger, height message.Height, signaturesCount int, root []byte, valueStruct *beaconprotocol.DutyData, duty *beaconprotocol.Duty) {
	// set var's
	s.height = height
	s.sigCount = signaturesCount
	s.root = root
	s.valueStruct = valueStruct
	s.duty = duty

	// start timer
	s.timer = time.NewTimer(s.SignatureCollectionTimeout)
	s.state.Store(StateRunning)
	// init map
	s.signatures = make(map[message.OperatorID][]byte, s.sigCount)

	go func() {
		<-s.timer.C
		s.state.Store(StateTimeout)
		logger.Error("could not process post consensus signature", zap.Error(errors.Errorf("timed out waiting for post consensus signatures, received %d", len(s.signatures))))
	}()
}

// stopTimer stops timer from firing and drain the channel. also set state to sleep
func (s *SignatureState) stopTimer() {
	if !s.timer.Stop() {
		<-s.timer.C
	}
	s.state.Store(StateSleep)
}

func (s *SignatureState) clear() {
	s.sigCount = 0
	s.root = nil
	s.valueStruct = nil
	s.duty = nil
	s.state.Store(StateSleep)
	// don't reset height until new height set
}

func (s *SignatureState) getState() TimerState {
	return TimerState(s.state.Load())
}

func (c *Controller) verifyPartialSignature(signature []byte, root []byte, ibftID message.OperatorID, committiee map[message.OperatorID]*beaconprotocol.Node) error {
	if val, found := committiee[ibftID]; found {
		pk := &bls.PublicKey{}
		if err := pk.Deserialize(val.Pk); err != nil {
			return errors.Wrap(err, "could not deserialized pk")
		}
		sig := &bls.Sign{}
		if err := sig.Deserialize(signature); err != nil {
			return errors.Wrap(err, "could not deserialized signature")
		}

		// protect nil root
		root = ensureRoot(root)
		// verify
		if !sig.VerifyByte(pk, root) {
			return errors.Errorf("could not verify signature from iBFT member %d", ibftID)
		}
		return nil
	}
	return errors.Errorf("could not find iBFT member %d", ibftID)
}

// signDuty signs the duty after iBFT came to consensus
func (c *Controller) signDuty(decidedValue []byte, duty *beaconprotocol.Duty) ([]byte, []byte, *beaconprotocol.DutyData, error) {
	// get operator pk for sig
	pk, err := c.ValidatorShare.OperatorSharePubKey()
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "could not find operator pk for signing duty")
	}

	// sign input value
	var sig []byte
	var root []byte
	retValueStruct := &beaconprotocol.DutyData{}
	switch duty.Type {
	case message.RoleTypeAttester:
		s := &spec.AttestationData{}
		if err := s.UnmarshalSSZ(decidedValue); err != nil {
			return nil, nil, nil, errors.Wrap(err, "failed to marshal attestation")
		}
		signedAttestation, r, err := c.signer.SignAttestation(s, duty, pk.Serialize())
		if err != nil {
			return nil, nil, nil, errors.Wrap(err, "failed to sign attestation")
		}

		sg := &beaconprotocol.InputValueAttestation{Attestation: signedAttestation}
		retValueStruct.SignedData = sg
		retValueStruct.GetAttestation().Signature = signedAttestation.Signature
		retValueStruct.GetAttestation().AggregationBits = signedAttestation.AggregationBits
		sig = signedAttestation.Signature[:]
		root = ensureRoot(r)
	default:
		return nil, nil, nil, errors.New("unsupported role, can't sign")
	}
	return sig, root, retValueStruct, err
}

// reconstructAndBroadcastSignature reconstructs the received signatures from other
// nodes and broadcasts the reconstructed signature to the beacon-chain
func (c *Controller) reconstructAndBroadcastSignature(signatures map[message.OperatorID][]byte, root []byte, inputValue *beaconprotocol.DutyData, duty *beaconprotocol.Duty) error {
	// Reconstruct signatures
	signature, err := threshold.ReconstructSignatures(signatures)
	if err != nil {
		return errors.Wrap(err, "failed to reconstruct signatures")
	}
	// verify reconstructed sig
	if res := signature.VerifyByte(c.ValidatorShare.PublicKey, root); !res {
		return errors.New("could not reconstruct a valid signature")
	}

	c.logger.Info("signatures successfully reconstructed", zap.String("signature", base64.StdEncoding.EncodeToString(signature.Serialize())), zap.Int("signature count", len(signatures)))

	// Submit validation to beacon node
	switch duty.Type {
	case message.RoleTypeAttester:
		c.logger.Debug("submitting attestation")
		blsSig := spec.BLSSignature{}
		copy(blsSig[:], signature.Serialize()[:])
		inputValue.GetAttestation().Signature = blsSig
		if err := c.beacon.SubmitAttestation(inputValue.GetAttestation()); err != nil {
			return errors.Wrap(err, "failed to broadcast attestation")
		}
	default:
		return errors.New("role is undefined, can't reconstruct signature")
	}
	return nil
}

// ensureRoot ensures that root will have sufficient allocated memory
// otherwise we get panic from bls:
// github.com/herumi/bls-eth-go-binary/bls.(*Sign).VerifyByte:738
func ensureRoot(root []byte) []byte {
	n := len(root)
	if n == 0 {
		n = 1
	}
	tmp := make([]byte, n)
	copy(tmp[:], root[:])
	return tmp[:]
}
