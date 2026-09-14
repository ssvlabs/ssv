"""Reference values for TestGetAggregateAndProofGloasFixedVector, from the consensus-specs pyspec.

Run against a consensus-specs checkout at the SIP #94 pin (v1.7.0-alpha.14), Python >= 3.12:

    pip install ".[test]" && python -m pysetup.generate_specs --fork gloas
    PYTHONPATH=tests/core/pyspec python gloas_aggregate_and_proof_fixture.py

The fixture must stay byte-identical to the Go test's; the printed ssz_sha256 checks that.
"""

import hashlib

from eth_consensus_specs.gloas import mainnet as spec

aggregation_bits = spec.AggregationBits(*[i in (0, 5, 255, 256, 511, 699) for i in range(700)])
committee_bits = spec.CommitteeBits(*[i in (1, 7, 63) for i in range(64)])
attestation = spec.Attestation(
    aggregation_bits=aggregation_bits,
    data=spec.AttestationData(
        slot=1234567,
        index=1,
        beacon_block_root=spec.Root(b"\x01" * 32),
        source=spec.Checkpoint(epoch=38579, root=spec.Root(b"\x02" * 32)),
        target=spec.Checkpoint(epoch=38580, root=spec.Root(b"\x03" * 32)),
    ),
    signature=spec.BLSSignature(bytes(range(96))),
    committee_bits=committee_bits,
)
aggregate_and_proof = spec.AggregateAndProof(
    aggregator_index=4242,
    aggregate=attestation,
    selection_proof=spec.BLSSignature(bytes(0xBB ^ i for i in range(96))),
)

# devnet-8's GLOAS_FORK_VERSION and genesis validators root, as in the golden block test.
domain = spec.compute_domain(
    spec.DOMAIN_AGGREGATE_AND_PROOF,
    spec.Version(bytes.fromhex("80733183")),
    spec.Root(bytes.fromhex("bb4a1a9e3f7f4e10edcd734e4acc3b5ffd4f830efe0af2748fa458cfee5d2658")),
)

print("ssz_sha256", hashlib.sha256(aggregate_and_proof.encode_bytes()).hexdigest())
print("attestation_root", spec.hash_tree_root(attestation).hex())
print("root", spec.hash_tree_root(aggregate_and_proof).hex())
print("domain", domain.hex())
print("signing_root", spec.compute_signing_root(aggregate_and_proof, domain).hex())
