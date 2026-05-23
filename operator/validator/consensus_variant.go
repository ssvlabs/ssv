package validator

// UseTwoabOBFT selects the proposer-duty consensus variant operator-wide:
//
//   - false (default) — bare OBFT (protocol/v2/obft/base), the production path.
//   - true            — 2abOBFT (protocol/v2/obft/twoab), the split-Phase-2
//     variant (Phase-2a Value/NoValue coordination + dynamic Phase-2b commit).
//
// Like IBEUseOptionB (see ibe_option.go) this is a compile-time toggle: it
// applies to every proposer duty this operator runs. Cluster-wide consistency
// — every operator in a cluster must agree on the value, since the two variants
// use distinct wire formats (ProtocolTag) and SSVMessage types
// (SSVOBFTMsgType vs SSV2abOBFTMsgType) — is the operational requirement,
// enforced today by uniform binary deployment. A future runtime/yaml flag can
// replace this const without changing the controller wiring (the proposer
// runner already selects its driver from whichever controller it is given).
const UseTwoabOBFT = false
