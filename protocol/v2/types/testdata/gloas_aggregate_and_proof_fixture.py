"""Reference values for TestGetAggregateAndProofGloasFixedVector, from the consensus-specs pyspec.

Run against a consensus-specs checkout at the commit SIP #94 pins,
a5a1bc630401eedbe2f3d87934c99012578c113b (past the v1.7.0-alpha.14 tag), with Python >= 3.12:

    pip install ".[test]" && python -m pysetup.generate_specs --all-forks
    PYTHONPATH=tests/core/pyspec python gloas_aggregate_and_proof_fixture.py

Output at that commit, pinned by the Go test:

    ssz_sha256 a3010361b4058e8668275075d425c0bee5cb704851eaf94b0dceb411802929bb
    attestation_root c67fcdd0fc5173cea66fa19b4b2fc26c6c5de463c9d7358fdad7952341931372
    root 25d7a728d9874ba5baf1087c0cefcbf97fa9daa39d8e62fbd7a699042f068c06
    domain 06000000d620b8f54e1c0237c64157679dd01e643a0911ba1344e568ee73e279
    signing_root 3934b9a7a7c92a7329790a8fe2ed98d15a9884a6425bc58b406c9450478429a0

The fixture must stay byte-identical to the Go test's; ssz_sha256 checks that.
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
