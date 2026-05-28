//go:build !race

package blsbackend

// raceEnabled is false in normal (non-race) builds. See its race-enabled twin
// multiverify_race_test.go for the F4 -race-skip mechanics.
const raceEnabled = false
