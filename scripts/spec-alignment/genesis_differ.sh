#!/bin/bash

# Get the path to the ssv-spec folder.
SPEC_PATH=$(go mod download -json github.com/ssvlabs/ssv-spec-pre-cc | jq -r .Dir)

# Run Differ.
differ \
    --config ./genesis_differ.config.yaml \
    --output ./genesis_output.diff \
    ../../ \
    $SPEC_PATH