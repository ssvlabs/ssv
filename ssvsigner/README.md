# SSV Remote Signer

## Overview

SSV Remote Signer is a service that separates the key management functions from the SSV node.

The service consists of two main components:
1. **Web3Signer** - A third-party service that provides secure key management and slashing protection
2. **SSV-Signer** - A lightweight service that connects SSV nodes to Web3Signer; it can also be used as a library for local signing

For more detailed technical overview, check [design documentation](./DESIGN.md).

## Setup Guide

### Prerequisites

- PostgreSQL database for Web3Signer's slashing protection
- The operator's private key (raw format or keystore file)
- Network access between SSV node, SSV-Signer, and Web3Signer services

### 1. Set Up the Slashing Protection Database

Consensys tutorial: https://docs.web3signer.consensys.io/how-to/configure-slashing-protection

#### Short summary

- Run `postgresql` (either Docker or install it) and create DB named `web3signer`

```bash
# Run PostgreSQL with Docker
docker run -e POSTGRES_PASSWORD=password -e POSTGRES_USER=postgres -e POSTGRES_DB=web3signer -p 5432:5432 postgres
```

- Apply `web3signer` migrations to the DB
  - Download and unpack `web3signer` from https://github.com/Consensys/web3signer/releases
  - Apply all migrations from V1 to V12 in `web3signer`'s `migrations/postgresql` folder maintaining their order and replacing `V1_initial.sql` with the migration name
  - It may be done using `psql` or `flyway`

PSQL example:

```bash
psql --echo-all --host=localhost --port=5432 --dbname=web3signer --username=postgres -f ./web3signer/migrations/postgresql/V1_initial.sql
# Continue with all migrations in sequence
```

Flyway example (adjust variables):

```bash
flyway migrate -url="jdbc:postgresql://localhost/web3signer" -locations="filesystem:/web3signer/migrations/postgresql"
```

### 2. Run Web3Signer

Consensys tutorial: https://docs.web3signer.consensys.io/get-started/start-web3signer

#### Short summary

- Run `web3signer` with the arguments in the example. You might need to change HTTP port, Ethereum network, PostgreSQL address
  - Either download and unpack `web3signer` from https://github.com/Consensys/web3signer/releases
  - Or use `web3signer` Docker image

Docker example:

```bash
docker run -p 9000:9000 consensys/web3signer:latest \
  --http-listen-port=9000 \
  eth2 \
  --network=mainnet \
  --slashing-protection-db-url="jdbc:postgresql://${POSTGRES_HOST}/web3signer" \
  --slashing-protection-db-username=postgres \
  --slashing-protection-db-password=password \
  --key-manager-api-enabled=true
```

Release file example:

```bash
web3signer --http-listen-port=9000 \
  eth2 \
  --network=mainnet \
  --slashing-protection-db-url="jdbc:postgresql://${POSTGRES_HOST}/web3signer" \
  --slashing-protection-db-username=postgres \
  --slashing-protection-db-password=password \
  --key-manager-api-enabled=true
```

#### Important Web3Signer Options:

| Option | Description |
|--------|-------------|
| `--http-listen-port` | The port Web3Signer listens on (default: 9000) |
| `--network` | The Ethereum network (mainnet, goerli, holesky, etc.) |
| `--slashing-protection-db-url` | JDBC connection string to PostgreSQL |
| `--slashing-protection-db-username` | Database username |
| `--slashing-protection-db-password` | Database password |
| `--key-manager-api-enabled` | Enables the Key Manager API (required for SSV-Signer) |

### 3. Run SSV-Signer

- Run `./cmd/ssv-signer` passing the following arguments:
  - `LISTEN_ADDR` - address to listen on (`:8080` by default)
  - `WEB3SIGNER_ENDPOINT` - `web3signer`'s address from the previous step 
  - `PRIVATE_KEY_FILE` and `PASSWORD_FILE` - operator private key or path to operator keystore and password files

Using environment variables with a keystore file:

```bash
PRIVATE_KEY_FILE=/path/to/keystore.json \
PASSWORD_FILE=/path/to/password.txt \
LISTEN_ADDR=0.0.0.0:8080 \
WEB3SIGNER_ENDPOINT=http://localhost:9000 \
./ssv-signer
```

#### SSV-Signer Configuration Options:

| Option              | Environment Variable  | Required | Default | Description                                  |
|---------------------|-----------------------|----------|---------|----------------------------------------------|
| Listen Address      | `LISTEN_ADDR`         | Yes      | `:8080` | Address and port for the signer to listen on |
| Web3Signer Endpoint | `WEB3SIGNER_ENDPOINT` | Yes      | -       | URL of the Web3Signer service                |
| Private Key File    | `PRIVATE_KEY_FILE`    | Yes      | -       | Path to operator's keystore file             |
| Password File       | `PASSWORD_FILE`       | Yes      | -       | Path to file containing keystore password    |

### 4. Configure SSV Node to Use Remote Signer

Update your SSV node configuration to use the remote signer:

```bash
# Example SSV node configuration for remote signing
SSV_SIGNER_ENDPOINT=http://ssv-signer-address:8080 ./ssv-node
```

## API Endpoints

SSV-Signer exposes the following API endpoints:

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/v1/validators` | GET | List all validators (shares) registered with the signer |
| `/v1/validators` | POST | Add validator shares to the signer |
| `/v1/validators` | DELETE | Remove validator shares from the signer |
| `/v1/validators/sign/{identifier}` | POST | Sign a payload with a specific validator share |
| `/v1/operator/identity` | GET | Get the operator's public key |
| `/v1/operator/sign` | POST | Sign data with the operator's key |

## Common Issues and Troubleshooting

### Connection Issues

**Problem**: SSV node cannot connect to SSV-Signer  
**Solution**: Verify network connectivity and ensure the `SSV_SIGNER_ENDPOINT` points to the correct address and port.

**Problem**: SSV-Signer cannot connect to Web3Signer  
**Solution**: Check that Web3Signer is running and the `WEB3SIGNER_ENDPOINT` is correctly configured.

### Database Issues

**Problem**: Web3Signer fails to start due to database errors  
**Solution**: Verify PostgreSQL is running and migrations have been applied correctly.

### Key Management Issues

**Problem**: Already existing keys after restart
**Solution**: The SSV node syncs validator shares to the signer during the first start. If restarting from a new clean database, cleaning keys in Web3Signer is required to ensure all shares are properly registered.

## Security Considerations

1. **Network Security**: Ensure communication between all components occurs over secure networks.
2. **Key Protection**: Store operator keys securely restricting access to them.
3. **Access Control**: Limit access to the SSV-Signer and Web3Signer endpoints to only the necessary services.

## Performance Considerations

1. **Resource Requirements**: Web3Signer may require significant resources for large validator sets. If Web3Signer has a large number of keys, it requires very long time to initialize. 
2. **Latency**: Keep the SSV-Signer and Web3Signer services close to the SSV node to minimize signing latency.
3. **Database Performance**: For operators with many validators, ensure the PostgreSQL database is properly sized and optimized.

## Migration Guide

When migrating from local signing to remote signing:

1. Set up Web3Signer and SSV-Signer as described above
2. Configure the SSV node to use remote signing and a new database
3. Restart the SSV node
4. Verify validators are correctly registered with the signer

## Limitations

1. The SSV node adds shares to the remote signer and removes them while syncing events. So, when the SSV node is started with a fresh DB, the keys still remain in the remote signing provider (e.g. Web3Signer) and must be removed manually. Also, if a remote signing provider instance (e.g. Web3Signer) is changed without copying the keys, the SSV node must be restarted with a fresh DB to sync the events.
2. Changing operators with an existing database is not supported.
3. Web3Signer is a third-party component with its own limitations and dependencies.

## Additional Resources

- [Web3Signer Documentation](https://docs.web3signer.consensys.io/)
- [SSV Remote Signer Design Documentation](./DESIGN.md)