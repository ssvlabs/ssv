package validation

import (
	"context"
	"time"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/protocol/v2/types"
	bls "github.com/herumi/bls-eth-go-binary/bls"
	"go.uber.org/zap"

	"github.com/pkg/errors"
)

const (
	// accumulationTimeout is the maximum time to wait for more signatures to arrive before verifying the batch.
	accumulationTimeout = 50 * time.Millisecond

	// maxBatchSize is the maximum number of signatures to verify in a single batch.
	maxBatchSize = 50
)

type signatureVerifier struct {
	logger *zap.Logger
	jobs   chan *verificationJob
}

func newSignatureVerifier(
	ctx context.Context,
	logger *zap.Logger,
	concurrency int,
) *signatureVerifier {
	v := &signatureVerifier{
		logger: logger,
		jobs:   make(chan *verificationJob, maxBatchSize*2),
	}

	for i := 0; i < concurrency; i++ {
		go v.verifier(ctx)
	}

	return v
}

func (v *signatureVerifier) verifier(ctx context.Context) {
	batch := make([]*verificationJob, 0, maxBatchSize)
	ticker := time.NewTicker(accumulationTimeout)

	// Release resources on exit.
	defer func() {
		ticker.Stop()

		// Halt any pending jobs.
		for _, job := range batch {
			job.result <- verificationResult{valid: false, err: errors.New("signature verifier stopped")}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case job := <-v.jobs:
			batch = append(batch, job)
			if len(batch) >= maxBatchSize {
				v.verifyBatch(batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				v.verifyBatch(batch)
				batch = batch[:0]
			}
		}
	}
}

func (v *signatureVerifier) verifyBatch(batch []*verificationJob) {
	sigs := make([]bls.Sign, len(batch))
	pubKeys := make([]bls.PublicKey, len(batch))
	var concatenatedMsg []byte
	for i, batch := range batch {
		sigs[i] = *batch.sig
		pubKeys[i] = *batch.pubKey
		concatenatedMsg = append(concatenatedMsg, batch.data...)
	}

	verified := bls.MultiVerify(sigs, pubKeys, concatenatedMsg)
	for _, job := range batch {
		job.result <- verificationResult{valid: verified, err: nil}
	}
}

// TODO: maybe rewrite use VerifyMultiPubKey
func (v *signatureVerifier) Verify(
	ctx context.Context,
	msg *specqbft.SignedMessage,
	domain spectypes.DomainType,
	typ spectypes.SignatureType,
	committee []*spectypes.Operator,
) error {
	// Get the public keys of the signers.
	signers := msg.GetSigners()
	pks := make([]bls.PublicKey, 0, len(signers))
	for _, id := range signers {
		found := false
		for _, n := range committee {
			if id == n.GetID() {
				pk, err := types.DeserializeBLSPublicKey(n.GetPublicKey())
				if err != nil {
					return errors.Wrap(err, "failed to deserialize public key")
				}
				pks = append(pks, pk)
				found = true
			}
		}
		if !found {
			return errors.New("unknown signer")
		}
	}

	// decode sig
	sign := &bls.Sign{}
	if err := sign.Deserialize(msg.Signature); err != nil {
		return errors.Wrap(err, "failed to deserialize signature")
	}

	root, err := spectypes.ComputeSigningRoot(
		msg,
		spectypes.ComputeSignatureDomain(domain, typ),
	)
	if err != nil {
		return errors.Wrap(err, "could not compute signing root")
	}

	pk := pks[0]
	if len(pks) != 1 {
		// Aggregate public keys
		// TODO: maybe there is a better way to do this, but since committee is small, it should be fine
		// TODO: maybe we should use the same aggregation as in prysm
		// TODO: research PublicKey.Recover()
		for i := 1; i < len(pks); i++ {
			pk.Add(&pks[i])
		}
	}

	// Queue the verification job, wait for the result and return it.
	out := make(chan verificationResult)
	defer close(out)

	job := &verificationJob{root, sign, &pk, out}
	v.jobs <- job
	result := <-out
	if result.err != nil {
		return result.err
	}
	if !result.valid {
		// Batch verification failed, try to verify this signature alone.
		if !job.verify() {
			return errors.New("signature invalid")
		}
	}

	return nil
}

type verificationJob struct {
	data   []byte
	sig    *bls.Sign
	pubKey *bls.PublicKey

	//TODO: use result channel to communicate results to qbft layer
	result chan verificationResult
}

func (s *verificationJob) verify() bool {
	return s.sig.FastAggregateVerify([]bls.PublicKey{*s.pubKey}, s.data)
}

type verificationResult struct {
	valid bool
	err   error
}
