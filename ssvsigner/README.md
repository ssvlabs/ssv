# SSV Remote Signer

## Overview

For SSV Remote signer overview, check [design documentation](./DESIGN.md).

## Setup ssv-signer

To set up ssv-signer, we need to set up web3signer with a slashing protection DB.

### Setup slashing protection DB

Consensys tutorial: https://docs.web3signer.consensys.io/how-to/configure-slashing-protection

#### Short summary

- Run `postgresql` (either Docker or install it) and create DB named `web3signer`

Docker example:

```bash
  docker run -e POSTGRES_PASSWORD=password -e POSTGRES_USER=postgres -e POSTGRES_DB=web3signer -p 5432:5432 postgres
```

- Apply `web3signer` migrations to the DB
    - Download and unpack `web3signer` from https://github.com/Consensys/web3signer/releases
    - Apply all migrations from V1 to V12 in `web3signer`'s `migrations/postgresql` folder maintaining their order and
      replacing `${MIGRATION_NAME}.sql` with the migration name
    - It may be done using `psql` or `flyway`

PSQL example:

```bash
  psql --echo-all --host=localhost --port=5432 --dbname=web3signer --username=postgres -f ./web3signer/migrations/postgresql/V1_initial.sql
```

Flyway example (adjust variables):

```bash
  flyway migrate -url="jdbc:postgresql://localhost/web3signer" -locations="filesystem:/web3signer/migrations/postgresql"
```

### Run `web3signer`

Consensys tutorial: https://docs.web3signer.consensys.io/get-started/start-web3signer

#### Short summary

- Run `web3signer` with the arguments in the example. You might need to change HTTP port, Ethereum network, PostgreSQL
  address
    - Either download and unpack `web3signer` from https://github.com/Consensys/web3signer/releases
    - Or use `web3signer` Docker image

Docker example:

```bash
  docker run -p 9000:9000 consensys/web3signer:develop --http-listen-port=9000 eth2 --network=holesky --slashing-protection-db-url="jdbc:postgresql://${POSTGRES_HOST}/web3signer" --slashing-protection-db-username=postgres --slashing-protection-db-password=password --key-manager-api-enabled=true
```

Release file example:

```bash
  web3signer --http-listen-port=9000 eth2 --network=holesky --slashing-protection-db-url="jdbc:postgresql://${POSTGRES_HOST}/web3signer" --slashing-protection-db-username=postgres --slashing-protection-db-password=password --key-manager-api-enabled=true
```

### Run `ssv-signer`

- Run `./cmd/ssv-signer` passing the following arguments:
    - `LISTEN_ADDR` - address to listen on (`:8080` by default)
    - `WEB3SIGNER_ENDPOINT` - `web3signer`'s address from the previous step
    - `PRIVATE_KEY` or (`PRIVATE_KEY_FILE` and `PASSWORD_FILE`) - operator private key or path to operator keystore and
      password files

Example:

```bash
  PRIVATE_KEY=OPERATOR_PRIVATE_KEY LISTEN_ADDR=0.0.0.0:8080 WEB3SIGNER_ENDPOINT=http://localhost:9000 ./ssv-signer
```

### Configure TLS for SSV-Signer

SSV-Signer supports TLS to secure connections in two ways:

1. **Server TLS** - Secures incoming connections to SSV-Signer from SSV nodes
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

#### Known Clients/Servers Authentication

TLS authentication in SSV-Signer is based on certificate fingerprints, matching Web3Signer's approach. This method (
sometimes called "certificate pinning") verifies the identity of clients/servers by comparing their certificate
fingerprints with expected values.

The known clients/servers files have the following formats:

**Known Clients File Format:**

```
# Format: <common_name> <sha256-fingerprint>
client1 DF:65:B8:02:08:5E:91:82:0F:91:F5:1C:96:56:92:C4:1A:F6:C6:27:FD:6C:FC:31:F2:BB:90:17:22:59:5B:50
client2 AB:CD:EF:12:34:56:78:90:AB:CD:EF:12:34:56:78:90:AB:CD:EF:12:34:56:78:90:AB:CD:EF:12:34:56:78:90
```

**Known Servers File Format:**

```
# Format: <hostname>:<port> <sha256-fingerprint>
localhost:9000 6C:B2:3E:F9:88:43:5E:62:69:9F:A9:9D:41:14:03:BA:83:24:AC:04:CE:BD:92:49:1B:8D:B2:A4:86:39:4C:BB
web3signer.example.com:9000 6C:B2:3E:F9:88:43:5E:62:69:9F:A9:9D:41:14:03:BA:83:24:AC:04:CE:BD:92:49:1B:8D:B2:A4:86:39:4C:BB
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
SERVER_KEYSTORE_FILE=/path/to/server.p12 \
SERVER_KEYSTORE_PASSWORD_FILE=/path/to/server_password.txt \
SERVER_KNOWN_CLIENTS_FILE=/path/to/known_clients.txt \
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
CLIENT_KEYSTORE_FILE=/path/to/client.p12 \
CLIENT_KEYSTORE_PASSWORD_FILE=/path/to/client_password.txt \
CLIENT_KNOWN_SERVERS_FILE=/path/to/known_servers.txt \
./ssv-signer
```

This corresponds to Web3Signer's downstream TLS configuration.

#### Full Mutual TLS Example

A complete setup with both server and client TLS would look like:

```bash
PRIVATE_KEY=OPERATOR_PRIVATE_KEY \
LISTEN_ADDR=0.0.0.0:8443 \
WEB3SIGNER_ENDPOINT=https://localhost:9000 \
# Server TLS (accepting connections)
SERVER_KEYSTORE_FILE=/path/to/server.p12 \
SERVER_KEYSTORE_PASSWORD_FILE=/path/to/server_password.txt \
SERVER_KNOWN_CLIENTS_FILE=/path/to/known_clients.txt \
# Client TLS (connecting to Web3Signer)
CLIENT_KEYSTORE_FILE=/path/to/client.p12 \
CLIENT_KEYSTORE_PASSWORD_FILE=/path/to/client_password.txt \
CLIENT_KNOWN_SERVERS_FILE=/path/to/known_servers.txt \
./ssv-signer
```

#### Command Line Options Reference

| SSV-Signer Option               | Description                                          | Web3Signer Equivalent                          |
|---------------------------------|------------------------------------------------------|------------------------------------------------|
| `SERVER_KEYSTORE_FILE`          | Server PKCS12 keystore file                          | `--tls-keystore-file`                          |
| `SERVER_KEYSTORE_PASSWORD_FILE` | Path to file containing password for server keystore | `--tls-keystore-password-file`                 |
| `SERVER_KNOWN_CLIENTS_FILE`     | Known clients fingerprints file                      | `--tls-known-clients-file`                     |
| `CLIENT_KEYSTORE_FILE`          | Client PKCS12 keystore file                          | `--downstream-http-tls-keystore-file`          |
| `CLIENT_KEYSTORE_PASSWORD_FILE` | Path to file containing password for client keystore | `--downstream-http-tls-keystore-password-file` |
| `CLIENT_KNOWN_SERVERS_FILE`     | Known servers fingerprints file                      | `--downstream-http-tls-known-servers-file`     |

#### Security Recommendations

1. **Use TLS 1.3**: SSV-Signer defaults to TLS 1.3 for maximum security
2. **Rotate certificates regularly**: Update your certificates and fingerprints periodically
