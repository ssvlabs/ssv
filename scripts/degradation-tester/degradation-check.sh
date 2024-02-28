#!/bin/bash

prefix="scripts/degradation-tester"
pathsFile="$prefix/paths.txt"
benchmarksResults="$prefix/benchmakrs"

while IFS= read -r line; do
    # removing .go from filename
    packageName=$(basename "$line" .go)
    outputFile="${benchmarksResults}/${packageName}_results_new.txt"
    oldBenchmarks="${benchmarksResults}/${packageName}_results_old.txt"

    echo "benchmarking package $packageName..."

    go test -bench=. -count=10 -benchmem "$line" | tee "$outputFile"

    benchstat $oldBenchmarks $outputFile &>"${benchmarksResults}/${packageName}_benchstat.txt"
done <"$pathsFile"