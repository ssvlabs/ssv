#!/bin/bash

prefix="./scripts/degradation-tester"
configFile="$prefix/config.yaml"
benchmarksResults="$prefix/benchmarks"

packagePaths=($(yq e '.Packages[].Path' $configFile))

for pkgPath in "${packagePaths[@]}"; do
    packageBenchName=$(echo "$pkgPath" | sed 's/\.\///g; s/\//_/g')
    benchmarksPath="${benchmarksResults}/${packageBenchName}_benchmarks_old.txt"

    go test -bench=. -count=10 -benchmem "$pkgPath" | tee "$benchmarksPath"

    echo "✅ Benchmarks updated for ${packageName} package."
done
