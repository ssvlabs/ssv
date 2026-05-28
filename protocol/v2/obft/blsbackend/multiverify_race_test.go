//go:build race

package blsbackend

// raceEnabled is set under -race so skipIfRace can short-circuit tests that
// would otherwise trip herumi/bls.MultiVerify's checkptr false-positive. See
// multiverify_batch_test.go's skipIfRace doc-comment and
// docs/OBFT-F4-IMPLEMENTATION-PLAN.md §race-detector.
const raceEnabled = true
