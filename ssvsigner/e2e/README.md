# SSV-Signer E2E Tests

End-to-end testing suite for SSV-Signer slashing protection functionality.

## Overview

Tests the complete signing flow with triple-layer slashing protection:
**SSV Node** → **LocalKeyManager** + **RemoteKeyManager** → **SSV-Signer** → **Web3Signer** → **PostgreSQL**

## Test Environment

- **Web3Signer 25.4.1** with PostgreSQL slashing protection database
- **Mainnet beacon configuration** for realistic testing
- **Docker containers** managed via testcontainers-go
- **Shared test suite** for easy test reuse across signing domains

## Test Structure

```
e2e/
├── suite.go                    # Shared E2ETestSuite
├── common/                     # Utilities (keys, beacon objects, mocks)
├── signing/                    # Test implementations
│   ├── attestation_test.go     # Attestation slashing tests
│   └── proposer_test.go        # Block proposal slashing tests
├── testenv/                    # Container & environment management
└── testdata/migrations/        # Web3Signer DB schema (V1-V12)
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

```bash
# Run all signing tests
go test ./signing/

# Run specific domain tests
go test ./signing/ -run TestAttestationSlashing
go test ./signing/ -run TestBlockSlashing

# Run with longer timeout for slow systems
go test ./signing/ -timeout=300s
```

## Key Features

- **Triple Protection**: Tests LocalKeyManager, RemoteKeyManager, and Web3Signer slashing protection
- **Real Network Config**: Uses mainnet beacon parameters and fork schedule
- **Container Isolation**: Fresh environment per test with persistent volumes
- **Comprehensive Coverage**: Slashing protection, concurrency, and restart scenarios
- **Easy Extension**: Shared test suite enables adding new signing domains

## Requirements

- Docker (for testcontainers)
- Go 1.21+
- ~2GB available memory for containers