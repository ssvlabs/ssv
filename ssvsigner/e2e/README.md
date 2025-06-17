# SSV-Signer E2E Tests

End-to-end testing suite for SSV-Signer slashing protection functionality.

## Overview

This E2E suite tests all three slashing protection databases:

1. **LocalKeyManager's BadgerDB** - Used when SSV Node signs directly
2. **RemoteKeyManager's BadgerDB** - Used when SSV-Signer handles signing
3. **Web3Signer's PostgreSQL** - Final protection layer for remote signing

The tests validate that each database correctly prevents slashable operations in both signing paths:
- **Local path**: SSV Node with LocalKeyManager → Direct signing
- **Remote path**: SSV Node → SSV-Signer with RemoteKeyManager → Web3Signer

## Test Environment

- **Web3Signer 25.4.1** with PostgreSQL slashing protection database
- **Docker containers** managed via testcontainers-go

> **Note**: Database migrations in `testdata/migrations/` are copied from Web3Signer. When upgrading Web3Signer versions, check for new migrations at: https://github.com/ConsenSys/web3signer/tree/main/slashing-protection/src/main/resources/migrations/postgresql

## Test Structure

```
e2e/
├── suite.go                    # Shared E2ETestSuite
├── common/                     # Utilities (keys, beacon objects, mocks)
├── signing/                    # Test implementations
│   ├── attestation_test.go     # Attestation slashing tests
│   └── proposer_test.go        # Block proposal slashing tests
├── testenv/                    # Container & environment management
└── testdata/migrations/        # Web3Signer DB migrations
```

## Available Tests

### Attestation Slashing (`TestAttestationSlashing`)
- Double vote protection
- Surrounding/surrounded vote protection
- Valid progression testing
- Concurrent signing safety
- Container restart persistence

### Block Proposal Slashing (`TestBlockSlashing`)  
- Double proposal protection
- Lower slot proposal protection
- Valid progression testing
- Concurrent signing safety
- Container restart persistence

## Running Tests

### Requirements

- Docker (for testcontainers)
- Go 1.21+
- ~2GB available memory for containers

### Quick Start

The easiest way to run tests is using the provided Makefile:

```bash
# From ssvsigner directory
make test-e2e     # Builds Docker image and runs all tests
```

### Manual Testing

If you prefer to run tests manually:

```bash
# Build Docker image (from repository root)
docker build -f ssvsigner/Dockerfile -t ssv-signer:latest .

# Run tests (from ssvsigner/e2e directory)
go test ./signing/                                    # All tests
go test ./signing/ -run TestAttestationSlashing      # Attestation tests only
go test ./signing/ -run TestBlockSlashing            # Proposer tests only
```

## Key Features

- **Triple Protection**: Tests LocalKeyManager, RemoteKeyManager, and Web3Signer slashing protection
- **Container Isolation**: Fresh environment per test with persistent volumes to test slashing protection database persistence across restarts
- **Comprehensive Coverage**: Slashing protection, concurrency, and restart scenarios
- **Easy Extension**: Shared test suite enables adding new signing domains

