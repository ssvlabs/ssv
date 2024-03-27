#!/bin/bash


prefix="./scripts/degradation-tester"
configFile="$prefix/config.yaml"
benchmarksResults="$prefix/benchmarks"

packagePaths=($(yq e '.Packages[].Path' $configFile))

for pkgPath in "${packagePaths[@]}"; do
  packageName=$(basename "$pkgPath")
  outputFile="${benchmarksResults}/${packageName}_results_new.txt"
  oldBenchmarks="${benchmarksResults}/${packageName}_results_old.txt"
  benchStatFile="${benchmarksResults}/${packageName}_benchstat.txt"

  go test -bench=. -count=10 -benchmem "$pkgPath" | tee "$outputFile"

  benchstat -format csv "$oldBenchmarks" "$outputFile" | tee "${benchStatFile}"

  degradation-tester "${configFile}" "${benchStatFile}"
  if [ $? -ne 0 ]; then
    echo "❌ Degradation tests have failed for ${packageName} package."
    rm "${benchStatFile}" "${outputFile}"
    exit 1
  fi

  echo "✅ Degradation tests have passed for ${packageName} package."
  rm "${benchStatFile}" "${outputFile}"
done
