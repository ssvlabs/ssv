## SSV Remote Signer

### Overview

This document outlines the design and rationale for implementing a lightweight remote signing service, ssv-signer, inspired by the Web3Signer APIs and design principles. The goal is to enhance security and performance for Ethereum-based distributed validator technology (DVT).

### Rationale

Remote signers were developed to protect cryptographic keys by isolating the hardware holding the keys from the node software. In this model:

`The node requests signatures from a remote signer API, which securely holds the keys. The node uses the provided signatures without direct access to the keys, preventing key leaks from node software vulnerabilities. By separating concerns, a minimal program that exclusively handles keys and signing becomes significantly harder to compromise compared to a complex node application.`

### Web3Signer

Web3Signer is an Ethereum-standard implementation for managing BLS keys in a remote signer. It features a built-in global slashing protection database, making it a robust solution for secure key management. Leveraging this database ensures added protection against unintentional or malicious double-signing events.

### Design

Introducing `ssv-signer`

ssv-signer is a lightweight remote signing service inspired by Web3Signer. It will:

    Be preconfigured with a Web3Signer endpoint and an operator private key keystore.

    Provide 3 essential API endpoints:

#### API Endpoints

- `POST /v1/validators` - Receives an encrypted validator share and a corresponding validator public key. Verifies the validity of the provided keys. Decrypts the share and stores it in the configured Web3Signer instance. Adds the decrypted shares directly into the Web3Signer instance, leveraging its built-in capabilities, including the slashing protection database.
    - Calls https://consensys.github.io/web3signer/web3signer-eth2.html#tag/Keymanager/operation/KEYMANAGER_IMPORT and uses the same response/request format.
    - Requires creating a keystore for the SSV share (search `keystorev4` package)
    - Note: keystore private key is the share private key, so the corresponding public key should be the share public key
    - Slashing data may not be necessary
    - Note: if `ssv-signer` can't decrypt the share, return an error, in ssv-node like today, don't prevent saving it.

- `DELETE /v1/validators` - remove a share from the ssv-signer and web3signer
    - Calls https://consensys.github.io/web3signer/web3signer-eth2.html#tag/Keymanager/operation/KEYMANAGER_DELETE and uses the same response/request format

- `POST /v1/validators/sign/:identifier` - Mimics the Web3Signer signing endpoint. Accepts a share public key and payload to sign (following the Web3Signer API specifications). Communicates with the Web3Signer instance to generate and return the signature, effectively acting as a proxy.
    - Calls https://consensys.github.io/web3signer/web3signer-eth2.html#tag/Signing/operation/ETH2_SIGN and uses the same response/request format
    - Note: public key is share public key and not validator public key

- `GET /v1/operator/identity` - returns RSA public key, used by the node on startup to determine its own public key and therefore operator ID

- `POST /v1/operator/sign` - signs a payload using the operator rsa key


#### Packages

Check this out as an example for project layout and so on: https://github.com/ssvlabs/slashing-protector

- Try to use https://github.com/alecthomas/kong for ssv-signer CLI
- `server` - run server that provides the API ( consider`fasthttp`)
- `client` - library to use the ssv-signer HTTP API
- `web3signer` - interacts with web3signer (prefer to use existing library [check Prysm or Vouch by @attestantio])
- `crypto` - openssl - maybe use `ssv`s package that can do both OpenSSL and Go RSA, OperatorKeys, keystore handling code

## Node Integration

On the SSV node, there are currently two options for managing an operator's private key:

- Raw Key
- Keystore

With the introduction of ssv-signer, a third option will be added with `SSVSignerEndpoint` configuration.


#### Startup (with remote signing enabled)
- Check ConfigLock to make sure remote signing is enabled in existing database, if any.
- Fail if remote signer is offline.
- Get ssv-signer operator public key and persist it to database. Crash if it already exists and is different (changing operators with same DB is not allowed, like today).
- Check that all the keys its supposed to have are available. (ssv-signer checks in web3signer)

#### Startup (with remote signing disabled)
- Check ConfigLock to make sure remote signing is disabled in existing database, if any.


#### Syncing Events
- in `handleValidatorAdded`: we should call the `POST /v1/validators` route here for the added share
    - if it fails on share decryption, which only the ssv-signer can know: return malformedError
    - if it fails for any other reason: retry X times or crash

#### Code
- MUST keep using SSV's slashing protection within the remote signing module. Potentially, can copy usage of slashinprotection from SSV's local signer to the remote signer implementation
- Try to keep using the existing interfaces, where possible. For example `OperatorSigner` could remain as-is and just receive a new implementation for the remote signing case.

## Remote Endpoint (ssv-signer)

- Imports client package from `ssv-signer` repo.
- Replaces EKM (Encrypted Keystore Manager) to save shares keys. ( only when using remote signer )

#### Setup and Configuration

An essential part of implementing ssv-signer is providing clear instructions on how to set up the service alongside Web3Signer. The setup will include:

- Configuring ssv-signer with the Web3Signer endpoint and operator private key keystore.

An example walkthrough demonstrating how to:
- Deploy and configure Web3Signer.
- Set up ssv-signer to interact with Web3Signer.
- Use the combined setup to manage validator shares and perform signing operations.

This documentation will ensure ease of adoption and smooth integration for end users.

## Gotcha's

### DB Sync

There's an issue with syncing shares to the signer.
If the node doesn't sync from scratch, the signer would not have all the shares. Plus, it will have share private keys in the database, which defeats the purpose of remote signing.

Solution:
Use the existing config lock to freeze the signing mode to the db.


## Summary

`ssv-signer` provides a secure, performant, and lightweight solution for remote signing inspired by Web3Signer. By isolating key management, leveraging Web3Signer’s embedded slashing protection database, and implementing robust APIs, it enhances the overall security posture and operational efficiency of distributed validator nodes in Ethereum ecosystems.


### Future Considerations
- Maybe we can explore some form of share syncing mechanism on node startup, such as listing the signer's available keys and adding the missing ones, before SyncHistory begins.
- Consider signing all beacon objects at once in one request in `CommitteeRunner.ProcessConsensus` 