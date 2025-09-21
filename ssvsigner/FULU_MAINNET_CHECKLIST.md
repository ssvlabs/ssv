# Fulu Mainnet Preparation Checklist

This document tracks all required updates for the Fulu fork mainnet release (expected November 2025).

## Prerequisites
- [ ] Fulu fork epoch officially scheduled for mainnet
- [ ] Web3Signer release available with Fulu mainnet configuration

## Required Updates

### 1. Dependency Updates
Update the following dependencies to versions that include Fulu mainnet support:

#### In `ssvsigner/go.mod`:
- [ ] `github.com/attestantio/go-eth2-client` - Update to official release tag with Fulu mainnet support
- [ ] `github.com/ssvlabs/eth2-key-manager` - Update to official release tag with Fulu mainnet support
- [ ] `github.com/ssvlabs/ssv-spec` - Update to official release tag with Fulu mainnet support

#### In parent `go.mod`:
- [ ] `github.com/ssvlabs/go-eth2-client` - Merge upstream official Fulu mainnet release and update fork
  - Current: v0.6.31-0.20250921085701-7014c8fd0091 (includes Fulu testnet support and SSV-specific changes)
- [ ] `github.com/ssvlabs/eth2-key-manager` - Update to official release tag with Fulu mainnet support
- [ ] `github.com/ssvlabs/ssv-spec` - Update to official release tag with Fulu mainnet support

### 2. Configuration Updates

#### Web3Signer Version:
Update to the latest version that includes Fulu mainnet epoch (check for version > 25.9.0):
- [ ] `ssvsigner/e2e/testenv/containers.go` line 144
- [ ] `ssvsigner/e2e/README.md` - Update documentation references
- [ ] Remove `--Xnetwork-fulu-fork-epoch` flag (`ssvsigner/e2e/testenv/containers.go` line 168)

#### Beacon Configuration (`ssvsigner/e2e/common/beacon_config.go`):
- [ ] Update Fulu epoch from placeholder `420000` to official mainnet epoch

#### Mock Beacon Responses (`beacon/goclient/mocks/mock-beacon-responses.json`):
- [ ] Add `FULU_FORK_VERSION` and `FULU_FORK_EPOCH` entries (currently missing)

#### Test Files with Fulu Epochs:
- [ ] Verify `beacon/goclient/attest_test.go` has correct Fulu epoch
- [ ] Verify `beacon/goclient/genesis_test.go` has correct Fulu epoch

### 3. Testing
- [ ] Run all unit tests
- [ ] Run e2e tests with updated configuration
- [ ] Verify signing works correctly with Fulu fork rules

### 4. Documentation
- [ ] Update README if needed
- [ ] Remove this checklist file after completion

## How to Find All TODOs
Run the following command to find all Fulu-related TODOs in the codebase:
```bash
grep -r "TODO(fulu)" .
```

## References
- [Ethereum Consensus Specs - Fulu](https://github.com/ethereum/consensus-specs/blob/dev/specs/fulu/)
- [Web3Signer Releases](https://github.com/Consensys/web3signer/releases)
- [SSV Spec Repository](https://github.com/ssvlabs/ssv-spec)

## Notes
- Fulu fork version: `0x06000000`
- Previous version (Electra): `0x05000000`
- All TODOs are marked with `TODO(fulu)` for easy searching