package controller

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	qbftstorage "github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/traces"
	"github.com/ssvlabs/ssv/protocol/v2/qbft"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/instance"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

// NewDecidedHandler handles newly saved decided messages.
// it will be called in a new goroutine to avoid concurrency issues
type NewDecidedHandler func(msg qbftstorage.Participation)

// Controller is a QBFT coordinator responsible for starting and following the entire life cycle of multiple QBFT Instances
type Controller struct {
	Identifier []byte

	// IdentifierFn, when non-nil, resolves the QBFT message identifier at a given height.
	// It is used on both send (StartNewInstance) and receive (ProcessMsg) paths to produce
	// the fork-correct SSV domain for the height/slot. When nil, falls back to Identifier.
	// JSON-tagged as "-" because funcs are not JSON-serialisable; Controller is only marshaled
	// in spec-test fixtures and is never persisted and rehydrated in production.
	IdentifierFn func(height specqbft.Height) []byte `json:"-"`

	// LatestInstanceHeight is the height of the latest Instance Controller spun up. That latest instance
	// is the "relevant" one currently driving consensus.
	// JSON-tagged as "Height" to preserve compatibility with existing spec-test fixtures.
	LatestInstanceHeight specqbft.Height `json:"Height"`
	// RecentInstances keeps track of N latest instances Controller has worked with, so late messages can still be
	// matched to their respective instance.
	// JSON-tagged as "StoredInstances" to preserve compatibility with existing spec-test fixtures.
	RecentInstances Instances `json:"StoredInstances"`

	CommitteeMember   *spectypes.CommitteeMember
	OperatorSigner    ssvtypes.OperatorSigner `json:"-"`
	NewDecidedHandler NewDecidedHandler       `json:"-"`
	config            qbft.IConfig
	fullNode          bool
}

// identifierAtHeight returns the QBFT message identifier for the given height.
// When IdentifierFn is set it delegates to it (producing the fork-correct domain);
// otherwise it falls back to the static Identifier field.
func (c *Controller) identifierAtHeight(height specqbft.Height) []byte {
	if c.IdentifierFn != nil {
		return c.IdentifierFn(height)
	}
	return c.Identifier
}

func NewController(
	identifier []byte,
	committeeMember *spectypes.CommitteeMember,
	config qbft.IConfig,
	signer ssvtypes.OperatorSigner,
	fullNode bool,
) *Controller {
	return &Controller{
		Identifier:           identifier,
		LatestInstanceHeight: specqbft.FirstHeight,
		CommitteeMember:      committeeMember,
		RecentInstances:      make(Instances, 0, InstancesDefaultCapacity),
		config:               config,
		OperatorSigner:       signer,
		fullNode:             fullNode,
	}
}

// StartNewInstance will attempt to start a new QBFT instance.
func (c *Controller) StartNewInstance(
	ctx context.Context,
	logger *zap.Logger,
	height specqbft.Height,
	value []byte,
	valueChecker ssv.ValueChecker,
	roundTimerF ssv.QBFTRoundTimerF,
) (*instance.Instance, error) {
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "qbft.controller.start"),
		trace.WithAttributes(observability.BeaconSlotAttribute(phase0.Slot(height))))
	defer span.End()

	if err := valueChecker.CheckValue(value); err != nil {
		return nil, traces.Errorf(span, "value invalid: %w", err)
	}

	if height < c.LatestInstanceHeight {
		return nil, spectypes.WrapError(spectypes.StartInstanceErrorCode, traces.Errorf(
			span,
			"attempting to start an instance with a past height %d, current instance height %d",
			height,
			c.LatestInstanceHeight,
		),
		)
	}

	if c.RecentInstances.FindInstance(height) != nil {
		return nil, spectypes.WrapError(spectypes.InstanceAlreadyRunningErrorCode, traces.Errorf(
			span,
			"instance with height %d already running",
			height,
		))
	}

	// Create & start a new instance, and also terminate all the older instances after that as "no longer relevant".
	// NOTE: the c.markRecentInstancesIrrelevant() call comes first because `addNewInstance` might evict an instance
	//       from c.RecentInstances, meaning c.markRecentInstancesIrrelevant() will miss it if called after.
	c.markRecentInstancesIrrelevant()
	newInstance := instance.NewInstance(
		ctx,
		logger,
		c.GetConfig(),
		c.CommitteeMember,
		c.identifierAtHeight(height),
		height,
		c.OperatorSigner,
		roundTimerF,
	)
	newInstance.Start(ctx, value, valueChecker)
	c.RecentInstances.addNewInstance(newInstance)
	c.LatestInstanceHeight = height

	span.SetStatus(codes.Ok, "")

	return newInstance, nil
}

// markRecentInstancesIrrelevant marks all recent instances as irrelevant.
func (c *Controller) markRecentInstancesIrrelevant() {
	for _, i := range c.RecentInstances {
		i.MarkIrrelevant()
	}
}

// ProcessMsg processes a new msg, returns decided message or error
func (c *Controller) ProcessMsg(
	ctx context.Context,
	logger *zap.Logger,
	signedMessage *spectypes.SignedSSVMessage,
	roundTimerF ssv.QBFTRoundTimerF,
) (*spectypes.SignedSSVMessage, error) {
	msg, err := specqbft.NewProcessingMessage(signedMessage)
	if err != nil {
		return nil, fmt.Errorf("could not create ProcessingMessage from signed message: %w", err)
	}

	if !bytes.Equal(c.identifierAtHeight(msg.QBFTMessage.Height), msg.QBFTMessage.Identifier) {
		return nil, spectypes.NewError(spectypes.MessageIdentifierInvalidErrorCode, "message doesn't belong to Identifier")
	}

	if c.isDecidedMsg(msg) {
		return c.UponDecided(logger, msg, roundTimerF)
	}

	if c.isFutureMessage(msg) {
		return nil, NewRetryableError(spectypes.WrapError(spectypes.FutureMessageErrorCode, ErrFutureConsensusMsg))
	}

	return c.UponExistingInstanceMsg(ctx, logger, msg)
}

func (c *Controller) UponExistingInstanceMsg(ctx context.Context, logger *zap.Logger, msg *specqbft.ProcessingMessage) (*spectypes.SignedSSVMessage, error) {
	inst := c.RecentInstances.FindInstance(msg.QBFTMessage.Height)
	if inst == nil {
		return nil, NewRetryableError(ErrInstanceNotFound)
	}

	prevDecided, _ := inst.IsDecided()
	if prevDecided {
		return nil, spectypes.NewError(spectypes.SkipConsensusMessageAsInstanceIsDecidedErrorCode, "not processing consensus message since instance is already decided")
	}

	decided, _, decidedMsg, err := inst.ProcessMsg(ctx, logger, msg)
	if instance.IsRetryable(err) {
		return nil, NewRetryableError(err)
	}
	if err != nil {
		return nil, fmt.Errorf("could not process msg: %w", err)
	}
	if !decided {
		return nil, nil
	}

	if err := c.broadcastDecided(decidedMsg, msg.QBFTMessage.Height); err != nil {
		// no need to fail processing instance deciding if failed to save/ broadcast
		logger.Debug("❌ failed to broadcast decided message", zap.Error(err))
	}

	return decidedMsg, nil
}

// GetIdentifier returns QBFT Identifier, used to identify messages
func (c *Controller) GetIdentifier() []byte {
	return c.Identifier
}

// isFutureMessage tells whether the provided message height is from a future instance.
// It takes into consideration a special case where FirstHeight instance didn't start yet
// but c.CurrentInstanceHeight == FirstHeight.
func (c *Controller) isFutureMessage(msg *specqbft.ProcessingMessage) bool {
	if c.LatestInstanceHeight == specqbft.FirstHeight && c.RecentInstances.FindInstance(c.LatestInstanceHeight) == nil {
		return true
	}
	if msg.QBFTMessage.Height > c.LatestInstanceHeight {
		return true
	}
	return false
}

// GetRoot returns the state's deterministic root
func (c *Controller) GetRoot() ([32]byte, error) {
	marshaledRoot, err := json.Marshal(c)
	if err != nil {
		return [32]byte{}, fmt.Errorf("could not encode controller: %w", err)
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret, nil
}

// Encode implementation
func (c *Controller) Encode() ([]byte, error) {
	return json.Marshal(c)
}

// Decode implementation
func (c *Controller) Decode(data []byte) error {
	err := json.Unmarshal(data, &c)
	if err != nil {
		return fmt.Errorf("could not decode controller: %w", err)
	}
	return nil
}

func (c *Controller) broadcastDecided(aggregatedCommit *spectypes.SignedSSVMessage, height specqbft.Height) error {
	net := c.GetConfig().GetNetwork()
	if err := net.BroadcastAtSlot(aggregatedCommit, phase0.Slot(height)); err != nil {
		// We do not return error here, just Log broadcasting error.
		return fmt.Errorf("could not broadcast decided: %w", err)
	}
	return nil
}

func (c *Controller) GetConfig() qbft.IConfig {
	return c.config
}
