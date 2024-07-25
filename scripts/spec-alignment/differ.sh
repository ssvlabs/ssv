#!/bin/bash

# Get the path to the ssv-spec folder.
SPEC_PATH=$(go mod download -json github.com/ssvlabs/ssv-spec | jq -r .Dir)

# Get the path to the ssv-spec folder.
GENESIS_SPEC_PATH=$(go mod download -json github.com/ssvlabs/ssv-spec-pre-cc | jq -r .Dir)

# Run Differ.
differ \
    --config ./differ.config.yaml \
    --output ./output.diff \
    ../../ \
    $SPEC_PATH

# Run Differ vs. genesis spec.
differ \
    --config ./genesis-differ.config.yaml \
    --output ./output.diff \
    ../../ \
    $GENESIS_SPEC_PATH