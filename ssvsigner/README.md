# SSV Remote Signer

## Overview

SSV Remote Signer is a service that separates the key management functions from the SSV node.

The service consists of two main components:

1. **Web3Signer** - A third-party service that provides secure key management and slashing protection
2. **SSV-Signer** - A lightweight service that connects SSV nodes to Web3Signer; it can also be used as a library for
   local signing

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
    - Apply all migrations from V1 to V12 in `web3signer`'s `migrations/postgresql` folder maintaining their order and
      replacing `V1_initial.sql` with the migration name
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

- Run `web3signer` with the arguments in the example. You might need to change HTTP port, Ethereum network, PostgreSQL
  address
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

For TLS-enabled Web3Signer:

```bash
web3signer --http-listen-port=9000 \
  eth2 \
  --network=mainnet \
  --slashing-protection-db-url="jdbc:postgresql://${POSTGRES_HOST}/web3signer" \
  --slashing-protection-db-username=postgres \
  --slashing-protection-db-password=password \
  --key-manager-api-enabled=true \
  --tls-keystore-file=/path/to/keystore.p12 \
  --tls-keystore-password-file=/path/to/password.txt \
  --tls-known-clients-file=/path/to/knownClients.txt
```

This configures Web3Signer to accept secure connections from SSV-Signer. When TLS is enabled on Web3Signer, make sure
to:

1. Use `https://` in the `WEB3SIGNER_ENDPOINT` value for SSV-Signer
2. Configure client TLS options for SSV-Signer to connect securely (
   see [Client TLS Configuration](#client-tls-configuration-ssv-signer-connecting-to-web3signer) section for detailed
   examples)

#### Important Web3Signer Options:

| Option                              | Description                                           |
|-------------------------------------|-------------------------------------------------------|
| `--http-listen-port`                | The port Web3Signer listens on (default: 9000)        |
| `--network`                         | The Ethereum network (mainnet, goerli, holesky, etc.) |
| `--slashing-protection-db-url`      | JDBC connection string to PostgreSQL                  |
| `--slashing-protection-db-username` | Database username                                     |
| `--slashing-protection-db-password` | Database password                                     |
| `--key-manager-api-enabled`         | Enables the Key Manager API (required for SSV-Signer) |
| `--tls-keystore-file`               | PKCS12 keystore file for server TLS                   |
| `--tls-keystore-password-file`      | File containing the keystore password                 |
| `--tls-known-clients-file`          | File with trusted client certificate fingerprints     |

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

### 5. Configure TLS for SSV-Signer

SSV-Signer supports TLS to secure connections in two ways:

1. **Server TLS** - Secures incoming connections from SSV nodes to SSV-Signer
2. **Client TLS** - Secures outgoing connections from SSV-Signer to Web3Signer

The TLS implementation is designed to match Web3Signer's TLS approach exactly, ensuring compatibility and simplifying
configuration for users who are already familiar with Web3Signer.

#### Using PKCS12 Keystores

SSV-Signer uses PKCS12 keystores for certificates, matching Web3Signer's approach. A PKCS12 keystore (.p12 file)
contains both the certificate and its private key.

To generate a PKCS12 keystore:

```bash
# Generate private key and certificate
openssl req -newkey rsa:2048 -nodes -keyout key.pem -x509 -days 365 -out cert.pem

# Create PKCS12 keystore from the key and certificate
openssl pkcs12 -export -out keystore.p12 -inkey key.pem -in cert.pem -name "tls-key"
```

You'll be prompted to enter a password for the keystore. Save this password as you'll need it for configuration.

#### Known Clients Authentication and Server Certificate

TLS authentication in SSV-Signer can be configured in two ways:

1. **Known Clients File** - For server TLS, uses certificate fingerprints for authenticating clients
2. **Server Certificate File** - For client TLS, uses a trusted PEM certificate for server verification

**Known Clients File Format:**

The known clients file contains fingerprints and has the following format:

```
# Format: <common_name> <sha256-fingerprint>
client1 DF:65:B8:02:08:5E:91:82:0F:91:F5:1C:96:56:92:C4:1A:F6:C6:27:FD:6C:FC:31:F2:BB:90:17:22:59:5B:50
client2 AB:CD:EF:12:34:56:78:90:AB:CD:EF:12:34:56:78:90:AB:CD:EF:12:34:56:78:90:AB:CD:EF:12:34:56:78:90
```

**Server Certificate File Format:**

The server certificate file contains a complete X.509 certificate in PEM format:

```
-----BEGIN CERTIFICATE-----
MIIDXTCCAkWgAwIBAgIJAJC1HiIAZAiIMA0GCSqGSIb3DQEBCwUAMEUxCzAJBgNV
BAYTAkFVMRMwEQYDVQQIDApTb21lLVN0YXRlMSEwHwYDVQQKDBhJbnRlcm5ldCBX
...
aWRnaXRzIFB0eSBMdGQwHhcNMTUxMjE1MDQzMDUyWhcNMTYxMjE0MDQzMDUyWjBF
...more base64 encoded data...
-----END CERTIFICATE-----
```

To get a certificate's fingerprint:

```bash
# For PEM certificates
openssl x509 -in cert.pem -fingerprint -sha256 -noout

# For PKCS12 keystores
keytool -list -v -keystore keystore.p12 -storetype PKCS12
```

#### Server TLS Configuration (SSV-Signer accepting connections)

To configure SSV-Signer to use TLS for incoming connections:

```bash
PRIVATE_KEY=OPERATOR_PRIVATE_KEY \
LISTEN_ADDR=0.0.0.0:8443 \
WEB3SIGNER_ENDPOINT=http://localhost:9000 \
KEYSTORE_FILE=/path/to/server.p12 \
KEYSTORE_PASSWORD_FILE=/path/to/server_password.txt \
KNOWN_CLIENTS_FILE=/path/to/known_clients.txt \
./ssv-signer
```

This is equivalent to Web3Signer's:

```bash
--tls-keystore-file=/path/to/keystore.p12 \
--tls-keystore-password-file=/path/to/password.txt \
--tls-known-clients-file=/path/to/knownClients.txt
```

#### Client TLS Configuration (SSV-Signer connecting to Web3Signer)

To configure SSV-Signer to use TLS when connecting to Web3Signer:

```bash
PRIVATE_KEY=OPERATOR_PRIVATE_KEY \
LISTEN_ADDR=0.0.0.0:8080 \
WEB3SIGNER_ENDPOINT=https://localhost:9000 \
WEB3SIGNER_KEYSTORE_FILE=/path/to/client.p12 \
WEB3SIGNER_KEYSTORE_PASSWORD_FILE=/path/to/client_password.txt \
WEB3SIGNER_SERVER_CERT_FILE=/path/to/server.pem \
./ssv-signer
```

When using `WEB3SIGNER_SERVER_CERT_FILE`, SSV-Signer will verify the Web3Signer's server certificate against the
provided trusted certificate.

This configuration corresponds to Web3Signer's TLS authentication model, where clients can verify server identity using
certificate fingerprints.

#### Full Mutual TLS Example

A complete setup with both server and client TLS would look like:

```bash
PRIVATE_KEY=OPERATOR_PRIVATE_KEY \
LISTEN_ADDR=0.0.0.0:8443 \
WEB3SIGNER_ENDPOINT=https://localhost:9000 \
# Server TLS (accepting connections from SSV node)
KEYSTORE_FILE=/path/to/server.p12 \
KEYSTORE_PASSWORD_FILE=/path/to/server_password.txt \
KNOWN_CLIENTS_FILE=/path/to/known_clients.txt \
# Client TLS (connecting to Web3Signer)
WEB3SIGNER_KEYSTORE_FILE=/path/to/client.p12 \
WEB3SIGNER_KEYSTORE_PASSWORD_FILE=/path/to/client_password.txt \
WEB3SIGNER_SERVER_CERT_FILE=/path/to/server.pem \
./ssv-signer
```

#### Command Line Options Reference

| SSV-Signer Option                   | Description                                           |
|-------------------------------------|-------------------------------------------------------|
| `KEYSTORE_FILE`                     | Server PKCS12 keystore file                           |
| `KEYSTORE_PASSWORD_FILE`            | Path to file containing password for server keystore  |
| `KNOWN_CLIENTS_FILE`                | Known clients fingerprints file                       |
| `WEB3SIGNER_KEYSTORE_FILE`          | Client PKCS12 keystore file for Web3Signer connection |
| `WEB3SIGNER_KEYSTORE_PASSWORD_FILE` | Path to file containing password for client keystore  |
| `WEB3SIGNER_SERVER_CERT_FILE`       | Server certificate file (PEM format) for Web3Signer   |

#### Security Recommendations

1. **TLS 1.3 Required**: SSV-Signer enforces TLS 1.3 as the minimum version for all TLS connections, providing better
   security and performance than older versions
2. **Certificate Fingerprint Verification**: The implementation uses certificate fingerprints for authentication,
   preventing MITM attacks without requiring a trusted CA hierarchy
3. **Rotate certificates regularly**: Update your certificates and fingerprints periodically (recommended every 6-12
   months). Certificate rotation helps limit the impact of potential private key compromises, ensures that outdated
   cryptographic methods aren't used long-term, and maintains alignment with evolving security standards. For more
   information on certificate lifecycle management,
   see [NIST Guidelines for TLS Implementations](https://nvlpubs.nist.gov/nistpubs/SpecialPublications/NIST.SP.800-52r2.pdf).
4. **Use strong credentials**: Generate keystores with strong passwords and use secure password files with appropriate
   permissions

#### TLS Configuration Options and Validation Rules

SSV-Signer supports various TLS configuration combinations for both server and client connections. Understanding these
options helps ensure secure and proper setup.

**Server TLS Validation Rules** (SSV-Signer accepting connections from SSV node):

| Configuration | KEYSTORE_FILE | KEYSTORE_PASSWORD_FILE | KNOWN_CLIENTS_FILE | Validity  | Description                                                    |
|---------------|---------------|------------------------|--------------------|-----------|----------------------------------------------------------------|
| No TLS        | ❌             | ❌                      | ❌                  | ✅ Valid   | No TLS encryption for incoming connections                     |
| Basic TLS     | ✅             | ✅                      | ❌                  | ✅ Valid   | Server presents certificate but doesn't verify clients         |
| Mutual TLS    | ✅             | ✅                      | ✅                  | ✅ Valid   | Server verifies client certificates against known fingerprints |
| Invalid       | ✅             | ❌                      | ❌                  | ❌ Invalid | Missing keystore password file                                 |
| Invalid       | ❌             | ❌                      | ✅                  | ❌ Invalid | Client verification without server certificate                 |

**Client TLS Validation Rules** (SSV-Signer connecting to Web3Signer):

| Configuration      | WEB3SIGNER_KEYSTORE_FILE | WEB3SIGNER_KEYSTORE_PASSWORD_FILE | WEB3SIGNER_SERVER_CERT_FILE | Validity  | Description                                                |
|--------------------|--------------------------|-----------------------------------|-----------------------------|-----------|------------------------------------------------------------|
| No TLS             | ❌                        | ❌                                 | ❌                           | ✅ Valid   | No TLS for outgoing connections (use HTTP endpoint)        |
| Certificate Only   | ❌                        | ❌                                 | ✅                           | ✅ Valid   | Verify server using trusted certificate                    |
| Client Certificate | ✅                        | ✅                                 | ❌                           | ✅ Valid   | Present client certificate for mutual TLS                  |
| Full Mutual TLS    | ✅                        | ✅                                 | ✅                           | ✅ Valid   | Present client certificate and verify server (most secure) |
| Invalid            | ✅                        | ❌                                 | ❌                           | ❌ Invalid | Missing keystore password file                             |

When implementing TLS, consider:

- For production environments, use Full Mutual TLS configuration for maximum security
- The server always requires both keystore and password file if TLS is enabled
- Client side can use fingerprint verification without presenting a certificate
- TLS 1.3 is enforced as the minimum protocol version for all TLS connections

## API Endpoints

SSV-Signer exposes the following API endpoints:

| Endpoint                           | Method | Description                                             |
|------------------------------------|--------|---------------------------------------------------------|
| `/v1/validators`                   | GET    | List all validators (shares) registered with the signer |
| `/v1/validators`                   | POST   | Add validator shares to the signer                      |
| `/v1/validators`                   | DELETE | Remove validator shares from the signer                 |
| `/v1/validators/sign/{identifier}` | POST   | Sign a payload with a specific validator share          |
| `/v1/operator/identity`            | GET    | Get the operator's public key                           |
| `/v1/operator/sign`                | POST   | Sign data with the operator's key                       |

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
**Solution**: The SSV node syncs validator shares to the signer during the first start. If restarting from a new clean
database, cleaning keys in Web3Signer is required to ensure all shares are properly registered.

## Security Considerations

1. **Network Security**: Ensure communication between all components occurs over secure networks.
2. **Key Protection**: Store operator keys securely restricting access to them.
3. **Access Control**: Limit access to the SSV-Signer and Web3Signer endpoints to only the necessary services.

## Performance Considerations

1. **Resource Requirements**: Web3Signer may require significant resources for large validator sets. If Web3Signer has a
   large number of keys, it requires very long time to initialize.
2. **Latency**: Keep the SSV-Signer and Web3Signer services close to the SSV node to minimize signing latency.
3. **Database Performance**: For operators with many validators, ensure the PostgreSQL database is properly sized and
   optimized.

## Migration Guide

When migrating from local signing to remote signing:

1. Set up Web3Signer and SSV-Signer as described above
2. Configure the SSV node to use remote signing and a new database
3. Restart the SSV node
4. Verify validators are correctly registered with the signer

## Limitations

1. The SSV node adds shares to the remote signer and removes them while syncing events. So, when the SSV node is started
   with a fresh DB, the keys still remain in the remote signing provider (e.g. Web3Signer) and must be removed manually.
   Also, if a remote signing provider instance (e.g. Web3Signer) is changed without copying the keys, the SSV node must
   be restarted with a fresh DB to sync the events.
2. Changing operators with an existing database is not supported.
3. Web3Signer is a third-party component with its own limitations and dependencies.

## Additional Resources

- [Web3Signer Documentation](https://docs.web3signer.consensys.io/)
- [SSV Remote Signer Design Documentation](./DESIGN.md)
