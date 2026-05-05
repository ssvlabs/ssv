# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

SSV Signer is a lightweight remote signing service that acts as a security intermediary between SSV nodes and Web3Signer. It provides secure key management by isolating validator private keys from the main SSV node, implementing a three-tier architecture for enhanced security in distributed validator technology (DVT).

**Key Design Principles:**
- Separation of concerns: SSV node (protocol logic) → SSV Signer (key decryption) → Web3Signer (signing & slashing protection)
- Keys never exist in unencrypted form on SSV nodes
- Minimal attack surface with focused functionality
- Compatible with existing Web3Signer deployments

## Architecture Overview

### Three-Tier Security Model
1. **SSV Node**: Protocol logic, no access to private keys
2. **SSV Signer**: Decrypts validator shares, routes signing requests
3. **Web3Signer**: Performs BLS signing with slashing protection database

### Core Components

1. **Server** (`server.go`): FastHTTP-based API server
    - Handles validator management and signing requests
    - Configurable TLS with mutual authentication support
    - Proxies and transforms requests to Web3Signer

2. **Client** (`client.go`): HTTP client library for SSV nodes
    - Used by SSV nodes to communicate with SSV Signer
    - Supports TLS configuration
    - Implements retry logic and error handling

3. **EKM** (`ekm/`): Ethereum Key Manager abstraction
    - `RemoteKeyManager`: Delegates to SSV Signer (production)
    - `LocalKeyManager`: Local signing for development/testing
    - Unified interface for key operations

4. **Web3Signer Integration** (`web3signer/`):
    - Client for Consensys Web3Signer APIs
    - Handles keystore import/delete operations
    - Manages signing requests with proper formatting

5. **TLS** (`tls/`): Security configuration
    - Supports PKCS12 keystores (matching Web3Signer)
    - Certificate fingerprint verification
    - Configurable client and server TLS

## Common Development Commands

### Building
```bash
# Build the main signer binary
cd ssvsigner
go build -o ./bin/ssv-signer ./cmd/ssv-signer

# Build the key purge utility
go build -o ./bin/purge-keys ./cmd/purge-keys

# Build with Docker
docker build -f Dockerfile -t ssv-signer .
```

### Testing
```bash
# Run all tests
go test -v ./...

# Run specific package tests
go test -v ./ekm/...
go test -v ./web3signer/...

# Run with coverage
go test -v -cover ./...

# Run integration tests (requires running Web3Signer)
go test -v -tags=integration ./...
```

### Running Locally

#### Basic Setup (No TLS)
```bash
# Start SSV Signer
PRIVATE_KEY_FILE=/path/to/keystore.json \
PASSWORD_FILE=/path/to/password.txt \
LISTEN_ADDR=0.0.0.0:8080 \
WEB3SIGNER_ENDPOINT=http://localhost:9000 \
./bin/ssv-signer
```

#### With Server TLS (SSV Signer accepting secure connections)
```bash
PRIVATE_KEY_FILE=/path/to/keystore.json \
PASSWORD_FILE=/path/to/password.txt \
LISTEN_ADDR=0.0.0.0:8443 \
WEB3SIGNER_ENDPOINT=http://localhost:9000 \
KEYSTORE_FILE=/path/to/server.p12 \
KEYSTORE_PASSWORD_FILE=/path/to/server_password.txt \
KNOWN_CLIENTS_FILE=/path/to/known_clients.txt \
./bin/ssv-signer
```

#### Purging Keys from Web3Signer
```bash
# Remove all keys from Web3Signer (useful for testing)
WEB3SIGNER_ENDPOINT=http://localhost:9000 \
BATCH_SIZE=20 \
./bin/purge-keys
```

## API Endpoints

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/v1/validators` | GET | List validator public keys |
| `/v1/validators` | POST | Add encrypted validator shares |
| `/v1/validators` | DELETE | Remove validator shares |
| `/v1/validators/sign/{pubkey}` | POST | Sign beacon object with validator share |
| `/v1/operator/identity` | GET | Get operator RSA public key |
| `/v1/operator/sign` | POST | Sign data with operator RSA key |

## Key Flows

### Adding a Validator Share
1. SSV node receives encrypted share from Ethereum event
2. Calls `POST /v1/validators` with encrypted share data
3. SSV Signer:
    - Decrypts share using operator RSA private key
    - Validates share public key matches
    - Creates EIP-2335 keystore with random password
    - Imports keystore to Web3Signer
4. Web3Signer stores keystore and initializes slashing protection

### Signing Operation
1. SSV node needs signature for beacon object
2. Calls `POST /v1/validators/sign/{pubkey}` with signing request
3. SSV Signer transforms request to Web3Signer format
4. Web3Signer performs slashing checks and signs
5. Signature returned through the chain

## Important Design Decisions

### Security Considerations
- Operator private key never leaves SSV Signer
- Validator shares only decrypted transiently for keystore creation
- TLS 1.3 minimum for all secure connections
- Certificate fingerprint verification prevents MITM attacks

### Integration with SSV Node
- SSV node uses `RemoteKeyManager` when configured with `SSVSignerEndpoint`
- On startup, node validates signer availability and operator key consistency
- Configuration is locked to prevent switching between local/remote signing

### Error Handling
- Decryption errors return specific status for malformed shares
- Connection failures trigger retries with exponential backoff
- Web3Signer errors are propagated with original status codes

### Performance Optimizations
- Per-validator locking prevents concurrent signing conflicts
- Batch operations for adding/removing multiple validators
- Connection pooling in HTTP client

## Testing Approach

### Unit Tests
- Mock Web3Signer client for server tests
- Mock SSV Signer server for client tests
- Comprehensive TLS configuration validation

### Integration Tests
- Require running Web3Signer instance
- Test full flow from encrypted share to signature
- Validate slashing protection behavior

### Common Test Patterns
```go
// Mock Web3Signer for testing
mockSigner := &MockRemoteSigner{
    ListKeysFunc: func(ctx context.Context) ([]phase0.BLSPubKey, error) {
        return []phase0.BLSPubKey{testPubKey}, nil
    },
}

// Test server with mock
srv := NewServer(logger, operatorKey, mockSigner)
```

## Debugging Tips

1. **Connection Issues**: Check `WEB3SIGNER_ENDPOINT` format (http/https)
2. **TLS Problems**: Verify certificate fingerprints match between client/server
3. **Decryption Failures**: Ensure operator key matches encrypted shares
4. **Signing Errors**: Check Web3Signer logs for slashing protection violations

## Module Management

This component has its own `go.mod` as it's designed to be moved to a separate repository in the future. For local development with the main SSV repository:

```bash
# Add to go.work file in repository root
use (
    .
    ./ssvsigner
)

# When running make commands from root
GOWORK=off make tools
GOWORK=off make lint
```

## Critical Implementation Details

### RSA Encryption
- Uses 2048-bit RSA keys with PKCS#1 v1.5 padding
- OpenSSL support via go-crypto-openssl for compatibility
- Fallback to pure Go implementation when OpenSSL unavailable

### Keystore Generation
- EIP-2335 compliant keystores for Web3Signer
- Random 16-character passwords generated per keystore
- Passwords not stored (Web3Signer imports and forgets)

### Slashing Protection
- Primary protection in Web3Signer's PostgreSQL database
- Secondary minimal tracking in SSV node
- `BumpSlashingProtection` ensures safe starting points