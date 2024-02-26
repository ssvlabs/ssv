# Changelog

All notable changes to SSV Network contracts will be documented in this file.

This project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### [v1.1.0] 2024-01-08
- [c80dc3b](https://github.com/bloxapp/ssv-network/commit/c80dc3b) - [Feature] Bulk exit of validators.
- [6431a00](https://github.com/bloxapp/ssv-network/commit/6431a00) - [Feature] Bulk removal of validators.
- [9c609cd](https://github.com/bloxapp/ssv-network/commit/9c609cd) - [Feature] Bulk registration of validators.
- [c9a90b1](https://github.com/bloxapp/ssv-network/commit/c9a90b1) - [Update] Added triggers for compilation in a few tasks (upgrading / updating).
- [4471f05](https://github.com/bloxapp/ssv-network/commit/4471f05) - [Update] Deployment tests and update task and documentation.
- [7564dfe](https://github.com/bloxapp/ssv-network/commit/7564dfe) - [Feature] Integration ssv-keys in ssv-network for generating keyshares.
- [8647401](https://github.com/bloxapp/ssv-network/commit/8647401) - [Update]: Configuration for publishing npm package.

## [Released]

### [v1.0.2] 2023-11-08
- [8f5df42](https://github.com/bloxapp/ssv-network/commit/8f5df42633d2b92c6bb70253a41e6afa80b9f111) - Change ValidatorExited signature: owner indexed.

### [v1.0.1] 2023-11-08
#### Added
- [0ab954e](https://github.com/bloxapp/ssv-network/commit/0ab954ec24fc0b32b51c278958c3d51480940f1a) - Permissionless audited version.


### [v1.0.0-rc3-permissionless-validators] 2023-11-08
Takes v1.0.0-rc3 as the base tag.
- [8878241](https://github.com/bloxapp/ssv-network/commit/88782410ad3223c75f205484811a010231c64152) Enable permissionless validator registration.


### [v1.0.0] - 2023-10-30

#### Fixed
- [22d2859](https://github.com/bloxapp/ssv-network/pull/262/commits/22d2859d8fe6267b09c7a1c9c645df19bdaa03ff) Fix bug in network earnings withdrawals.
- [d25d188](https://github.com/bloxapp/ssv-network/pull/265/commits/d25d18886459e631fb4453df7a47db19982ec80e) Fix Types.shrink() bug.

#### Added
- [bf0c51d](https://github.com/bloxapp/ssv-network/pull/263/commits/bf0c51d4df191018052d11425c9fcc252de61431) A validator can voluntarily exit.


### [v1.0.0.rc4] - 2023-08-31

- Audit fixes/recommendations
- Validate a cluster with 0 validators can not be liquidated
- Deployment process now uses hardhat tasks
- The DAO can set a maximum operator fee (SSV)
- Remove the setRegisterAuth function (register operator/validator without restrictions)
- SSVNetworkViews contract does not throw an error as a way of return.
