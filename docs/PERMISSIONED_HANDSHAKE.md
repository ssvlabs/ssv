# Permissioned connectivity

## Abstract and rationale

For SSV Mainnet we will do a gradual release where at first only operators registered in the contract will be able to connect other operators. This feature makes peers identify their operator Public Key signing it and more info used to prevent replay attacks during the handshake. The receiving peer validates the signature and makes sure that this Operator Public Key exists in the contract.

Eventually when we gain more confidence in mainnet we will deprecate this feature and allow all players to connect each other.

Relevant files -
	- `network/peers/connections/filters.go`
	- `network/peers/connections/handshaker.go`
	- `network/peers/peers_index.go`
	- `network/records/signed_node_info.go`


## Design

### Additional Handshake Protocol

- Send a signed message signed with an Operator Public Key.
- Verify that the Operator Public Key is in the list of active operators (from contract)
- Verify the signature

### Prevent re-cycling of the message

An attacker could take the message and send it to another peer, identifying as a different operator.
We solve this the following way:
- Attach sender and receiver PeerID in the signed message and check their validity
- PeerID is a libp2p entity that consists of a network address and a public key. Every peer holds its own private key to sign and encrypt the top level messages we pass between peers. By attaching them to the message we can ensure that the handshake data was created between the two included peers. see libp2p [docs](https://docs.libp2p.io/concepts/fundamentals/peers/) for more info.
- Attach Timestamp in the signed message
- prevent re-using the same message for the same two peers in the future


### Message Structure

```go=
Message {
	OperatorKey	PublicKey
	Signature  	[]byte // HandshakeData signed by OperatorKey's private key
	HandshakeData {
    	Sender 	PeerID
    	Receiver   PeerID
    	Timestamp  Time
	}
}
```


### Signature scheme

For this purpose we have picked up the already existing Operator Public/Private key pairs that are used in our contract. Every operator has a **RSA Public/Private Key pair** that is saved in our database once we discover them from the contract.
The Private Key of the operator is used to sign the `HandshakeData` object, again using the RSA scheme. This way we can ensure by verifying with the PublicKey that the original Operator that is in our database/contract has signed the message.

#### RSA Note

RSA is mostly known as an encryption/decryption scheme but can be also used for signing. see [this](https://www.cs.cornell.edu/courses/cs5430/2015sp/notes/rsa_sign_vs_dec.php) doc from Cornell.

We're using SignPKCS1v15/VerifyPKCS1v15 which are already implemented in Go's `rsa` library for signing.

#### Signing and verifying code example

```go=

var operatorPrivateKey rsa.PrivateKey // assume we have a key

var message // some message struct that is hashable

// Sign -
signature, _ := rsa.SignPKCS1v15(nil, operatorPrivateKey, crypto.SHA256, message.Hash())

// attach sign to message

var signedMessage = {
	Signature: signature
	Pubkey: operatorPrivateKey.PublicKey,
	Data: message
}


// Verify -

if err := rsa.VerifyPKCS1v15(signedMessage.Pubkey, crypto.SHA256, signedMessage.Data.Hash(), signedMessage.Signature); err != nil {
    return err
}


```


#### note about encryption
Our handshake messages are encrypted on the P2P level, this is done by a different key that is called the `network key` that is used to sign and encrypt the messages without any relation to the consensus layer.
So the whole message is signed and encrypted using this key, the inner `HandshakeData` structure is signed by our operator key.


### Exporters and different roles support
In order to support different roles of nodes in the network and make it not mandatory for them to be listed in the contract, a **Whitelist** of operator public keys are going to be used, every node even if it doesn't participate as an active operator will hold an operator key pair. Exporters and other node roles we want to allow in the network will be configured in a whitelist and allowed to be connected.


### Rollout plan

#### Testnet-Mainnet rollout

The permissioned feature will be turned on in the node between specified ETH slots. By default it will be off, it will be set on when the Slot is after `PermissionedActivateSlot`, it will be turned off again when the slot is after `PermissionedDeactivateSlot`. Both are p2p config parameters.

