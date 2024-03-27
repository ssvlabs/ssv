#!/bin/bash

prefix="./scripts/degradation-tester"
configFile="$prefix/config.yaml"
benchmarksResults="$prefix/benchmarks"

packagePaths=($(yq e '.Packages[].Path' $configFile))

for pkgPath in "${packagePaths[@]}"; do
    packageName=$(basename "$pkgPath")
    benchmarksPath="${benchmarksResults}/${packageName}_results_old.txt"
    
    echo "Updating benchmarks for ${packageName}"
    
    go test -bench=. -count=10 -benchmem "$pkgPath" | tee "$benchmarksPath"
    
    echo "✅ Benchmarks updated for ${packageName} package."
done
