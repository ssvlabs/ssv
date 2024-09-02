package hashmap

import (
	"encoding/hex"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type validatorStatus int

const (
	validatorStatusSubscribing validatorStatus = 1
	validatorStatusSubscribed  validatorStatus = 2
)

func TestRaceCondition(t *testing.T) {
	cmtIDsHex := []string{
		"9576fdcd4cfd9e563a5ce54e1a2e8a2950a94d8d0db49696f37889929ad813fd",
		"b9492d60036f93841da23f7ee49f987ace6cb7d07170b9e9aaa2be54f4fceaf7",
		"e2394daed1f3bfc1714dea43d0aaf663a10af88ed985aed5564e3d027d961b89",
	}
	var cmtIDs []string
	for _, cmtIDHex := range cmtIDsHex {
		cmtID, err := hex.DecodeString(cmtIDHex)
		require.NoError(t, err)
		cmtIDs = append(cmtIDs, string(cmtID))
	}

	var wwg sync.WaitGroup
	var errs []error
	var mu sync.Mutex
	for i := 0; i < 100; i++ {
		wwg.Add(1)
		go func() {
			defer wwg.Done()

			m := New[string, validatorStatus]()
			var wg sync.WaitGroup
			for _, cmtID := range cmtIDs {
				cmtID := cmtID
				n := 50 + rand.Intn(200)
				for j := 0; j < n; j++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						time.Sleep(time.Duration(rand.Intn(2000)) * time.Millisecond)
						_, found := m.GetOrInsert(cmtID, validatorStatusSubscribing)
						if !found {
							time.Sleep(time.Duration(rand.Intn(200)) * time.Millisecond)
							m.Set(cmtID, validatorStatusSubscribed)
						} else {
							time.Sleep(time.Duration(rand.Intn(200)) * time.Millisecond)
						}
					}()
				}
			}

			done := make(chan struct{})
			go func() {
				defer close(done)
				wg.Wait()
			}()

			ticker := time.NewTicker(1 * time.Millisecond)
			var keys []string
			defer ticker.Stop()
		Loop:
			for {
				keys = []string{}
				m.Range(func(key string, value validatorStatus) bool {
					keys = append(keys, key)
					return true
				})
				select {
				case <-done:
					keys = []string{}
					m.Range(func(key string, value validatorStatus) bool {
						keys = append(keys, key)
						return true
					})
					keysHex := []string{}
					for _, key := range keys {
						keysHex = append(keysHex, hex.EncodeToString([]byte(key)))
					}
					t.Logf("iteration %d: %d keys, %d expected", i, len(keys), len(cmtIDs))
					if len(keys) != len(cmtIDs) {
						t.Logf("expected %d keys, got %d (%v)", len(cmtIDs), len(keys), keysHex)
						mu.Lock()
						errs = append(errs, fmt.Errorf("expected %d keys, got %d (%v)", len(cmtIDs), len(keys), keysHex))
						mu.Unlock()
					}
					break Loop
				case <-ticker.C:
				}
			}
		}()
	}
	wwg.Wait()
	require.Empty(t, errs)
}

func TestRaceConditionGoMap(t *testing.T) {
	cmtIDsHex := []string{
		"9576fdcd4cfd9e563a5ce54e1a2e8a2950a94d8d0db49696f37889929ad813fd",
		"b9492d60036f93841da23f7ee49f987ace6cb7d07170b9e9aaa2be54f4fceaf7",
		"e2394daed1f3bfc1714dea43d0aaf663a10af88ed985aed5564e3d027d961b89",
	}
	var cmtIDs []string
	for _, cmtIDHex := range cmtIDsHex {
		cmtID, err := hex.DecodeString(cmtIDHex)
		require.NoError(t, err)
		cmtIDs = append(cmtIDs, string(cmtID))
	}

	var wwg sync.WaitGroup
	var errs []error
	var mu sync.Mutex
	for i := 0; i < 100; i++ {
		wwg.Add(1)
		go func() {
			defer wwg.Done()

			m := make(map[string]validatorStatus)
			var mtx sync.Mutex
			var wg sync.WaitGroup
			for _, cmtID := range cmtIDs {
				cmtID := cmtID
				n := 50 + rand.Intn(200)
				for j := 0; j < n; j++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						time.Sleep(time.Duration(rand.Intn(2000)) * time.Millisecond)
						mtx.Lock()
						_, found := m[cmtID]
						if !found {
							m[cmtID] = validatorStatusSubscribing
						}
						mtx.Unlock()
						if !found {
							time.Sleep(time.Duration(rand.Intn(200)) * time.Millisecond)
							mtx.Lock()
							m[cmtID] = validatorStatusSubscribed
							mtx.Unlock()
						} else {
							time.Sleep(time.Duration(rand.Intn(200)) * time.Millisecond)
						}
					}()
				}
			}

			done := make(chan struct{})
			go func() {
				defer close(done)
				wg.Wait()
			}()

			ticker := time.NewTicker(1 * time.Millisecond)
			var keys []string
			defer ticker.Stop()
		Loop:
			for {
				keys = []string{}
				mtx.Lock()
				for key := range m {
					keys = append(keys, key)
				}
				mtx.Unlock()
				select {
				case <-done:
					keys = []string{}
					mtx.Lock()
					for key := range m {
						keys = append(keys, key)
					}
					mtx.Unlock()
					keysHex := []string{}
					for _, key := range keys {
						keysHex = append(keysHex, hex.EncodeToString([]byte(key)))
					}
					t.Logf("iteration %d: %d keys, %d expected", i, len(keys), len(cmtIDs))
					if len(keys) != len(cmtIDs) {
						t.Logf("expected %d keys, got %d (%v)", len(cmtIDs), len(keys), keysHex)
						mu.Lock()
						errs = append(errs, fmt.Errorf("expected %d keys, got %d (%v)", len(cmtIDs), len(keys), keysHex))
						mu.Unlock()
					}
					break Loop
				case <-ticker.C:
				}
			}
		}()
	}
	wwg.Wait()
	require.Empty(t, errs)
}
