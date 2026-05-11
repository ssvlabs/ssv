package twoab

// Instance is the per-slot 2abOBFT consensus state machine.
//
// **Phase B status**: skeleton only — fields and constructor will be
// filled in over Phases E (Phase 1), F (Phase 2a verdict broadcast),
// G (Phase 2b convergence + emission), H (Phase 3 reconstruction), and
// I (evidence). Calling any method on a Phase-B-built Instance will
// return ErrNotImplemented.
//
// The public API will mirror base.Instance where the protocols share
// semantic structure (BuildPhase1Bundle, ObservePhase1Bundle, etc.) and
// add 2ab-specific methods for Phase 2a (BuildVerdict, ObserveVerdict)
// and Phase 2b (BuildOwnOnion2b, ObserveOnion2b). See the impl plan at
// docs/2abOBFT-IMPL-PLAN.md for the per-phase API breakdown.
type Instance struct {
	cfg           *Config
	ownOperatorID OperatorID

	// TODO(Phase E-I): protocol state machine fields.
}

// NewInstance constructs a 2abOBFT Instance. Phase-B stub — returns
// ErrNotImplemented on every method until Phase E onward fills in the
// protocol logic.
//
// The constructor signature is provisional and may grow additional
// parameters (signer, ibe, pubKeyShares, evidenceObserver, etc.) as
// later phases land — mirroring base.NewInstance.
func NewInstance(cfg *Config, ownOperatorID OperatorID) (*Instance, error) {
	if cfg == nil {
		return nil, ErrNilConfig
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Instance{
		cfg:           cfg,
		ownOperatorID: ownOperatorID,
	}, nil
}

// Config returns the instance's config (read-only).
func (i *Instance) Config() *Config { return i.cfg }

// OwnOperatorID returns the local operator's ID.
func (i *Instance) OwnOperatorID() OperatorID { return i.ownOperatorID }
