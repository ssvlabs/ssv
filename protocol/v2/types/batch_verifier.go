package types

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/jamiealquiza/tachymeter"
	"go.uber.org/zap"
	"golang.org/x/exp/maps"
)

var Verifier = NewBatchVerifier(runtime.NumCPU(), 10, time.Millisecond*5)

func init() {
	go Verifier.Start()
}

const messageSize = 32

// SignatureRequest represents a BLS signature request.
type SignatureRequest struct {
	mu sync.Mutex

	Signature *bls.Sign
	PubKeys   []bls.PublicKey
	Message   [messageSize]byte
	Results   []chan bool // Results are sent on these channels.

	// timings keeps the start time of the request, multiple if it's aggregated.
	timings []time.Time
}

func (r *SignatureRequest) Finish(result bool) {
	r.mu.Lock()
	results := r.Results
	r.mu.Unlock()

	for _, res := range results {
		res <- result
	}
}

type requestsMap map[[messageSize]byte]*SignatureRequest

type requests struct {
	mu sync.RWMutex
	rm requestsMap
}

func (r *requests) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.rm)
}

func (r *requests) Values() []*SignatureRequest {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return maps.Values(r.rm)
}

func (r *requests) Get(msg [messageSize]byte) (*SignatureRequest, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	v, ok := r.rm[msg]
	return v, ok
}

func (r *requests) Set(msg [messageSize]byte, sr *SignatureRequest) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.rm[msg] = sr
}

func (r *requests) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.rm = make(requestsMap)
}

type batch struct {
	start    time.Time
	requests []*SignatureRequest
}

// BatchVerifier efficiently schedules and batches BLS signature verifications.
// It accumulates requests into batches, which are verified
// in aggregate (to reduce CPU cost) and concurrently
// (to maximize CPU utilization).
type BatchVerifier struct {
	concurrency int
	batchSize   int
	timeout     time.Duration

	timer   *time.Timer // Controls the timeout of the current batch.
	started time.Time   // Records when the current batch started.
	ticker  *time.Ticker
	pending requests
	mu      sync.Mutex

	batches chan batch // Channel to receive batches of requests.

	busyWorkers atomic.Int32 // Count of currently busy workers.

	// debug struct to calculate the average batch size.
	debug struct {
		lens                    [8 * 1024]byte         // Lengths of the batches.
		batches                 int                    // Number of batches processed.
		reqs                    int                    // Number of requests processed.
		dups                    int                    // Number of duplicate message requests.
		fails                   int                    // Number of failed batches.
		failReqs                int                    // Number of requests in failed batches.
		requestTotalDurations   *tachymeter.Tachymeter // Lifetime duration of individual requests.
		requestPendingDurations *tachymeter.Tachymeter // Pending duration of individual requests.
		batchDurations          *tachymeter.Tachymeter // Duration of batches.
		batchPendingDurations   *tachymeter.Tachymeter // Pending duration of batches.
		sync.Mutex                                     // Mutex to guard access to the debug fields.
	}
}

// NewBatchVerifier returns a BatchVerifier.
//
// `concurrency`: number of worker goroutines.
//
// `batchSize`: target batch size.
//
// `timeout`: max batch wait time, adjusts based on load (see `adaptiveTimeout`).
func NewBatchVerifier(concurrency, batchSize int, timeout time.Duration) *BatchVerifier {
	nopTimer := time.NewTimer(0)
	v := &BatchVerifier{
		concurrency: concurrency,
		batchSize:   batchSize,
		timeout:     timeout,
		timer:       nopTimer,
		ticker:      time.NewTicker(timeout),
		pending:     requests{rm: make(requestsMap)},
		batches:     make(chan batch, concurrency*2*128),
	}
	n := concurrency * 1024
	v.debug.requestTotalDurations = tachymeter.New(&tachymeter.Config{Size: batchSize * n})
	v.debug.requestPendingDurations = tachymeter.New(&tachymeter.Config{Size: batchSize * n})
	v.debug.batchDurations = tachymeter.New(&tachymeter.Config{Size: n})
	v.debug.batchPendingDurations = tachymeter.New(&tachymeter.Config{Size: n})
	return v
}

// AggregateVerify adds a request to the current batch or verifies it immediately if a similar one exists.
// It returns the result of the signature verification.
func (b *BatchVerifier) AggregateVerify(signature *bls.Sign, pks []bls.PublicKey, message [messageSize]byte) bool {
	start := time.Now()
	defer func() {
		b.debug.Lock()
		b.debug.requestTotalDurations.AddTime(time.Since(start))
		b.debug.Unlock()
	}()
	result := make(chan bool)

	// If an identical message is already pending, aggregate it's request
	// with the current one and return the result.
	b.mu.Lock()
	if dup, exists := b.pending.Get(message); exists {
		b.mu.Unlock()

		b.debug.Lock()
		b.debug.reqs++
		b.debug.dups++
		b.debug.Unlock()

		dup.mu.Lock()
		dup.PubKeys = append(dup.PubKeys, pks...)
		sig := *dup.Signature
		sig.Add(signature)
		dup.Signature = &sig
		dup.Results = append(dup.Results, result)
		dup.timings = append(dup.timings, start)
		dup.mu.Unlock()

		valid := <-result
		if !valid {
			// If the aggregated verification failed, fall back to individual verification.
			return b.verifySingle(&SignatureRequest{
				Signature: signature,
				PubKeys:   pks,
				Message:   message,
			})
		}
		return valid
	} else {
		b.debug.Lock()
		b.debug.reqs++
		b.debug.Unlock()
	}

	sr := &SignatureRequest{
		Signature: signature,
		PubKeys:   pks,
		Message:   message,
		Results:   []chan bool{result},
		timings:   []time.Time{start},
	}
	b.pending.Set(message, sr)
	if b.pending.Len() == b.batchSize {
		// Batch size reached: stop the timer and dispatch the batch.
		b.timer.Stop()
		previouslyStarted := b.started
		b.started = time.Time{}
		pending := b.pending.Values()
		b.pending.Reset()
		b.mu.Unlock()

		b.debug.Lock()
		b.debug.batchPendingDurations.AddTime(time.Since(previouslyStarted))
		b.debug.Unlock()

		b.batches <- batch{
			start:    previouslyStarted,
			requests: pending,
		}
	} else {
		// Batch has grown: adjust the timer.
		b.timer.Stop()
		t := b.adaptiveTimeout(b.pending.Len())
		b.timer.Reset(t)
		b.started = time.Now()
		b.mu.Unlock()
	}

	valid := <-result
	if !valid {
		// If the batch failed, fall back to individual verification.
		return b.verifySingle(sr)
	}
	return valid
}

// adaptiveTimeout calculates the timeout based on the proportion of pending requests.
func (b *BatchVerifier) adaptiveTimeout(pending int) time.Duration {
	if b.started.IsZero() {
		b.started = time.Now()
	}
	workload := int(b.busyWorkers.Load()) + len(b.batches) + int(float64(pending)/float64(b.batchSize))
	if workload > b.concurrency {
		workload = b.concurrency
	}
	// busyness := float64(workload) / float64(b.concurrency) / 2
	busyness := float64(workload+1) / float64(b.concurrency) * 2
	if busyness > 1 {
		busyness = 1
	}

	timeLeft := b.timeout - time.Since(b.started)
	if timeLeft <= 0 {
		return 0
	}
	// log.Printf("workload: %d, busyness: %f, timeLeft: %s, adjusted: %s", workload, busyness, timeLeft, time.Duration(busyness*float64(timeLeft)))
	return time.Duration(busyness * float64(timeLeft))
}

// Start launches the worker goroutines.
func (b *BatchVerifier) Start() {
	go func() {
		for {
			time.Sleep(12 * time.Second)
			stats := b.Stats()
			formatGauge := func(g Gauge[time.Duration]) string {
				ms := func(d time.Duration) string {
					return fmt.Sprintf("%.1fms", d.Seconds()*1000)
				}
				return fmt.Sprintf("min: %s,\nmax: %s,\navg: %s,\np99: %s,\np95: %s,\np50: %s", ms(g.Min), ms(g.Max), ms(g.Avg), ms(g.P99), ms(g.P95), ms(g.P50))
			}
			zap.L().Debug("BatchVerifier stats",
				zap.Int("total_requests", stats.TotalRequests),
				zap.Int("duplicate_requests", stats.DuplicateRequests),
				zap.Int("total_batches", stats.TotalBatches),
				zap.Float64("average_batch_size", stats.AverageBatchSize),
				zap.Int("pending_requests", stats.PendingRequests),
				zap.Int("pending_batches", stats.PendingBatches),
				zap.Int("busy_workers", stats.BusyWorkers),
				zap.Any("recent_batch_sizes", stats.RecentBatchSizes),
				zap.Int("failed_batches", stats.FailedBatches),
				zap.Int("failed_requests", stats.FailedRequests),
				zap.String("request_total_durations", formatGauge(stats.RequestTotalDurations)),
				zap.String("request_pending_durations", formatGauge(stats.RequestPendingDurations)),
				zap.String("batch_durations", formatGauge(stats.BatchDurations)),
				zap.String("batch_pending_durations", formatGauge(stats.BatchPendingDurations)),
			)
		}
	}()

	for i := 0; i < b.concurrency; i++ {
		go b.worker()
	}
}

// worker is a goroutine that processes batches of requests.
func (b *BatchVerifier) worker() {
	for {
		select {
		case newBatch := <-b.batches:
			b.verify(newBatch)
			// case <-b.timer.C:
		case <-b.ticker.C:
			// Dispatch the pending requests when the timer expires.
			b.mu.Lock()
			previouslyStarted := b.started
			pending := b.pending.Values()
			b.pending.Reset()
			b.mu.Unlock()

			if len(pending) > 0 {
				b.debug.Lock()
				b.debug.batchPendingDurations.AddTime(time.Since(previouslyStarted))
				b.debug.Unlock()

				b.verify(batch{
					start:    previouslyStarted,
					requests: pending,
				})
			}
		}
	}
}

type Gauge[T any] struct {
	Min T
	Max T
	Avg T
	P99 T
	P95 T
	P50 T
}

func gaugeFromTachymeter(t *tachymeter.Tachymeter) Gauge[time.Duration] {
	c := t.Calc()
	return Gauge[time.Duration]{
		Min: c.Time.Min,
		Max: c.Time.Max,
		Avg: c.Time.Avg,
		P99: c.Time.P99,
		P95: c.Time.P95,
		P50: c.Time.P50,
	}
}

type Stats struct {
	TotalRequests           int
	DuplicateRequests       int
	TotalBatches            int
	AverageBatchSize        float64
	PendingRequests         int
	PendingBatches          int
	BusyWorkers             int
	FailedBatches           int
	FailedRequests          int
	RequestTotalDurations   Gauge[time.Duration]
	RequestPendingDurations Gauge[time.Duration]
	BatchDurations          Gauge[time.Duration]
	BatchPendingDurations   Gauge[time.Duration]
	RecentBatchSizes        [32]byte
}

func (b *BatchVerifier) Stats() (stats Stats) {
	b.debug.Lock()
	defer b.debug.Unlock()

	stats.TotalRequests = b.debug.reqs
	stats.DuplicateRequests = b.debug.dups
	stats.TotalBatches = b.debug.batches
	stats.FailedBatches = b.debug.fails
	stats.FailedRequests = b.debug.failReqs
	stats.RequestTotalDurations = gaugeFromTachymeter(b.debug.requestTotalDurations)
	stats.RequestPendingDurations = gaugeFromTachymeter(b.debug.requestPendingDurations)
	stats.BatchDurations = gaugeFromTachymeter(b.debug.batchDurations)
	stats.BatchPendingDurations = gaugeFromTachymeter(b.debug.batchPendingDurations)

	// Calculate the average batch size.
	lens := b.debug.lens[:]
	if b.debug.batches < len(b.debug.lens) {
		lens = lens[:b.debug.batches]
	}
	var sum float64
	for _, l := range lens {
		sum += float64(l)
	}
	stats.AverageBatchSize = sum / float64(len(lens))

	stats.PendingRequests = b.pending.Len()
	stats.PendingBatches = len(b.batches)
	stats.BusyWorkers = int(b.busyWorkers.Load())

	startIndex := len(lens) - len(stats.RecentBatchSizes)
	if startIndex < 0 {
		startIndex = 0
	}
	copy(stats.RecentBatchSizes[:], lens[startIndex:])

	return
}

// verify verifies a batch of requests and sends the results back via the Result channels.
func (b *BatchVerifier) verify(batch batch) {
	b.busyWorkers.Add(1)
	defer b.busyWorkers.Add(-1)

	b.debug.Lock()
	for _, req := range batch.requests {
		req.mu.Lock()
		timings := req.timings
		req.mu.Unlock()
		for _, t := range timings {
			b.debug.requestPendingDurations.AddTime(time.Since(t))
		}
	}
	b.debug.Unlock()

	defer func() {
		b.debug.Lock()
		b.debug.batchDurations.AddTime(time.Since(batch.start))
		b.debug.Unlock()
	}()

	if len(batch.requests) == 1 {
		batch.requests[0].Finish(b.verifySingle(batch.requests[0]))
		return
	}

	// Prepare the signature, public keys, and messages for batch verification.
	sig := *batch.requests[0].Signature
	pks := make([]bls.PublicKey, len(batch.requests))
	msgs := make([]byte, len(batch.requests)*messageSize)
	for i, req := range batch.requests {
		req.mu.Lock()
		if i > 0 {
			sig.Add(req.Signature)
		}
		pk := req.PubKeys[0]
		for j := 1; j < len(req.PubKeys); j++ {
			pk.Add(&req.PubKeys[j])
		}
		pks[i] = pk
		copy(msgs[messageSize*i:], req.Message[:])
		req.mu.Unlock()
	}

	// Batch verify the signatures.
	valid := sig.AggregateVerifyNoCheck(pks, msgs)
	for _, req := range batch.requests {
		req.Finish(valid)
	}

	// Update the debug fields under lock.
	b.debug.Lock()
	b.debug.batches++
	b.debug.lens[b.debug.batches%len(b.debug.lens)] = byte(len(batch.requests))
	if !valid {
		b.debug.fails++
		b.debug.failReqs += len(batch.requests)
	}
	b.debug.Unlock()
}

// verifySingle verifies a single request and sends the result back via the Result channel.
func (b *BatchVerifier) verifySingle(req *SignatureRequest) bool {
	req.mu.Lock()
	message := req.Message
	signature := req.Signature
	pubKeys := req.PubKeys
	req.mu.Unlock()

	return signature.FastAggregateVerify(pubKeys, message[:])
}
