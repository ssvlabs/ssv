# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

SSV (Secret Shared Validators) is a secure and scalable staking infrastructure for distributed Ethereum validators. It uses MPC threshold schemes with Byzantine fault-tolerant consensus to enable decentralized validator operations.

## Common Development Commands

### Building the Project
```bash
# Build the main binary
make build

# Build with Docker
make docker-image
```

### Running Tests
```bash
# Run all tests (unit + integration + spec)
make full-test

# Run unit tests only (fast, for development)
make unit-test

# Run integration tests
make integration-test

# Run spec tests (protocol compliance)
make spec-test

# Run a single test
go test -v -run TestName ./path/to/package

# Run tests with coverage
COVERAGE=true make unit-test
```

### Code Quality
```bash
# Run linters
make lint

# Format code (uses goimports with local prefix)
make format

# Generate mocks and other code
make generate
```

### Running Local Development Environment
```bash
# Run 4-node local cluster with Docker Compose
make docker-all

# Run local cluster for debugging
make docker-debug

# Start a single node
./bin/ssvnode start-node --config ./config/config.yaml
```

## High-Level Architecture

### Core Components and Data Flow

1. **Operator Node** (`operator/`): Central orchestrator managing validator lifecycle
    - Receives duties from beacon chain → schedules execution → coordinates with other operators
    - Manages validator registration/deregistration based on contract events

2. **SSV Protocol** (`protocol/v2/`): Implements distributed validator protocol
    - **QBFT Consensus** (`qbft/`): Byzantine fault-tolerant consensus for operator agreement
    - **Runners** (`ssv/runner/`): Execute specific validator duties (attestation, proposal, sync committee)
    - Flow: Pre-consensus signatures → QBFT consensus → Post-consensus aggregation → Beacon submission

3. **P2P Network** (`network/`): Libp2p-based communication layer
    - Discovery via discv5, message broadcast via GossipSub
    - Subnets isolate validator communication by public key
    - Routes messages to appropriate validators and duty queues

4. **Beacon Chain Integration** (`beacon/`): Interfaces with Ethereum consensus layer
    - Fetches validator duties and chain state
    - Submits aggregated attestations, proposals, and sync committee messages
    - Supports multiple beacon nodes for redundancy

5. **Contract Sync** (`eth/`): Monitors SSV smart contract on Ethereum
    - Syncs validator shares, operator events, and configuration changes
    - Event-driven updates trigger validator lifecycle changes

6. **Storage** (`registry/storage/`): Persistent state management
    - BadgerDB for validator shares and metadata
    - In-memory caches for performance
    - SSZ encoding for efficient storage

### Key Design Patterns

- **Message Queue Pattern**: Each duty type has dedicated queues for concurrent processing
- **Event-Driven Architecture**: Responds to beacon slots, contract events, and P2P messages
- **Repository Pattern**: Clean storage interfaces abstract database operations
- **Strategy Pattern**: Different runners implement common interface for various duties

### Important Interfaces

- **Runner Interface**: Abstracts duty execution (`StartNewDuty`, `ProcessConsensus`, etc.)
- **P2PNetwork**: Network operations and message routing
- **Storage Interfaces**: `Shares`, `ValidatorStore` for data persistence
- **Beacon/Execution Clients**: Abstract blockchain interactions

## Development Guidelines

### Code Style (from .cursorrules)
- **Error handling**: Lowercase, concise, prefer `fmt.Errorf` over `errors.Wrap`
- **Logging**: Use zap, lowercase messages, prefer DEBUG level unless operator-actionable
- **Testing**: Use `github.com/stretchr/testify` for assertions
- **Function arguments**: Order as `(ctx, logger, deps..., args...)`

### Common Patterns
- Use `fields.FieldName` for standard log fields (Validator, Slot, etc.)
- Prefer method logger > struct logger > global logger
- Comprehensive error handling with proper context
- Extensive use of interfaces for testability

### Testing Approach
- Unit tests for individual components
- Integration tests for cross-component flows
- Spec tests validate protocol compliance
- Use mocks via `go.uber.org/mock`

## Critical Paths

1. **Validator Duty Flow**:
   Slot ticker → Duty fetcher → Scheduler → Runner → Consensus → Submission

2. **Message Processing**:
   P2P receive → Validation → Router → Queue → Runner → State update

3. **Contract Event Flow**:
   Event sync → Handler → Validator controller → Share update

Understanding these flows is essential for debugging and feature development.
