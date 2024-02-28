#!/bin/bash

prefix="scripts/degradation-tester"
pathsFile="$prefix/paths.txt"
benchmarksResults="$prefix/benchmakrs"

while IFS= read -r line; do
    # removing .go from filename
    filename=$(basename "$line" .go)
    outputFile="${benchmarksResults}/${filename}_results_new.txt"
    echo "benchmarking $filename..."

    go test -bench=. "$line" &> "$outputFile"
done < "$pathsFile"
