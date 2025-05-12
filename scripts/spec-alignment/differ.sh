#!/bin/bash

# Get the path to the ssv-spec folder.
SPEC_PATH=$(go mod download -json github.com/ssvlabs/ssv-spec | jq -r .Dir)

# Run Differ.
differ \
    --config ./differ.config.yaml \
    --output ./output.diff \
    ../../ \
    $SPEC_PATH