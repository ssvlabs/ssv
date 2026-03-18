package goclient

import (
	"context"
	"encoding/hex"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/beacon/goclient/mocks"
	"github.com/ssvlabs/ssv/networkconfig"
)

type countingMultiClient struct {
	MultiClient
	domainCalls atomic.Int32
	onDomain    func(context.Context)
}

func (c *countingMultiClient) Domain(ctx context.Context, domainType phase0.DomainType, epoch phase0.Epoch) (phase0.Domain, error) {
	c.domainCalls.Add(1)
	if c.onDomain != nil {
		c.onDomain(ctx)
	}
	return c.MultiClient.Domain(ctx, domainType, epoch)
}

func Test_computeVoluntaryExitDomain(t *testing.T) {
	ctx := t.Context()

	t.Run("success", func(t *testing.T) {
		mockServer := mocks.NewServer(nil)
		defer mockServer.Close()

		client, err := New(
			ctx,
			zap.NewNop(),
			Options{
				BeaconConfig:   networkconfig.TestNetwork.Beacon,
				BeaconNodeAddr: mockServer.URL,
				CommonTimeout:  400 * time.Millisecond,
				LongTimeout:    500 * time.Millisecond,
			},
		)
		require.NoError(t, err)

		domain, err := client.computeVoluntaryExitDomain()
		require.NoError(t, err)
		require.NotNil(t, domain)

		currentForkVersion, err := hex.DecodeString("03000000")
		require.NoError(t, err)
		require.Len(t, currentForkVersion, 4)

		genesisValidatorsRoot, err := hex.DecodeString("4b363db94e286120d76eb905340fdd4e54bfe9f06bf33ff6cf5ad27f511bfe95")
		require.NoError(t, err)
		require.Len(t, genesisValidatorsRoot, 32)

		forkData := &phase0.ForkData{
			CurrentVersion:        [4]byte(currentForkVersion),
			GenesisValidatorsRoot: [32]byte(genesisValidatorsRoot),
		}

		root, err := forkData.HashTreeRoot()
		require.NoError(t, err)

		require.EqualValues(t, append(spectypes.DomainVoluntaryExit[:], root[:]...), domain)
	})
}

func TestDomainDataCachesByEpochAndDomain(t *testing.T) {
	ctx := t.Context()

	mockServer := mocks.NewServer(nil)
	defer mockServer.Close()

	client, err := New(
		ctx,
		zap.NewNop(),
		Options{
			BeaconConfig:   networkconfig.TestNetwork.Beacon,
			BeaconNodeAddr: mockServer.URL,
			CommonTimeout:  400 * time.Millisecond,
			LongTimeout:    500 * time.Millisecond,
		},
	)
	require.NoError(t, err)

	countingClient := &countingMultiClient{MultiClient: client.multiClient}
	client.multiClient = countingClient

	first, err := client.DomainData(ctx, 123, spectypes.DomainAttester)
	require.NoError(t, err)

	second, err := client.DomainData(ctx, 123, spectypes.DomainAttester)
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.EqualValues(t, 1, countingClient.domainCalls.Load())

	third, err := client.DomainData(ctx, 123, spectypes.DomainSyncCommittee)
	require.NoError(t, err)
	require.NotEqual(t, phase0.Domain{}, third)
	require.EqualValues(t, 2, countingClient.domainCalls.Load())

	fourth, err := client.DomainData(ctx, 124, spectypes.DomainAttester)
	require.NoError(t, err)
	require.NotEqual(t, phase0.Domain{}, fourth)
	require.EqualValues(t, 3, countingClient.domainCalls.Load())
}

func TestDomainDataCoalescesConcurrentRequests(t *testing.T) {
	ctx := t.Context()

	mockServer := mocks.NewServer(nil)
	defer mockServer.Close()

	client, err := New(
		ctx,
		zap.NewNop(),
		Options{
			BeaconConfig:   networkconfig.TestNetwork.Beacon,
			BeaconNodeAddr: mockServer.URL,
			CommonTimeout:  time.Second,
			LongTimeout:    time.Second,
		},
	)
	require.NoError(t, err)

	countingClient := &countingMultiClient{
		MultiClient: client.multiClient,
		onDomain: func(ctx context.Context) {
			select {
			case <-ctx.Done():
			case <-time.After(25 * time.Millisecond):
			}
		},
	}
	client.multiClient = countingClient

	const goroutines = 16
	var (
		wg      sync.WaitGroup
		results [goroutines]phase0.Domain
		errs    [goroutines]error
	)
	start := make(chan struct{})

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i], errs[i] = client.DomainData(ctx, 321, spectypes.DomainAttester)
		}(i)
	}

	close(start)
	wg.Wait()

	for i := 0; i < goroutines; i++ {
		require.NoError(t, errs[i])
		require.Equal(t, results[0], results[i])
	}
	require.EqualValues(t, 1, countingClient.domainCalls.Load())
}

func TestDomainDataCoalescedFetchSurvivesWinnerCancellation(t *testing.T) {
	ctx := t.Context()

	mockServer := mocks.NewServer(nil)
	defer mockServer.Close()

	client, err := New(
		ctx,
		zap.NewNop(),
		Options{
			BeaconConfig:   networkconfig.TestNetwork.Beacon,
			BeaconNodeAddr: mockServer.URL,
			CommonTimeout:  time.Second,
			LongTimeout:    time.Second,
		},
	)
	require.NoError(t, err)

	started := make(chan struct{})
	release := make(chan struct{})

	countingClient := &countingMultiClient{
		MultiClient: client.multiClient,
		onDomain: func(ctx context.Context) {
			select {
			case started <- struct{}{}:
			default:
			}

			select {
			case <-ctx.Done():
			case <-release:
			}
		},
	}
	client.multiClient = countingClient

	firstCtx, cancelFirst := context.WithCancel(ctx)
	firstResultCh := make(chan struct {
		domain phase0.Domain
		err    error
	}, 1)
	go func() {
		domain, err := client.DomainData(firstCtx, 555, spectypes.DomainAttester)
		firstResultCh <- struct {
			domain phase0.Domain
			err    error
		}{domain: domain, err: err}
	}()

	<-started
	cancelFirst()

	secondResultCh := make(chan struct {
		domain phase0.Domain
		err    error
	}, 1)
	go func() {
		domain, err := client.DomainData(ctx, 555, spectypes.DomainAttester)
		secondResultCh <- struct {
			domain phase0.Domain
			err    error
		}{domain: domain, err: err}
	}()

	close(release)

	firstResult := <-firstResultCh
	secondResult := <-secondResultCh

	require.NoError(t, firstResult.err)
	require.NoError(t, secondResult.err)
	require.Equal(t, firstResult.domain, secondResult.domain)
	require.EqualValues(t, 1, countingClient.domainCalls.Load())
}
