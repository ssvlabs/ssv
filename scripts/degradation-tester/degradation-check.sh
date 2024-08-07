#!/bin/bash


prefix="scripts/degradation-tester"
configFile="$prefix/config.yaml"
benchmarksResults="$prefix/benchmarks"

# Requires yq and benchstat to be installed
packagePaths=($(yq e '.Packages[].Path' $configFile))

for pkgPath in "${packagePaths[@]}"; do
  # Get the package name from the path. Replacing the / with _ to get a unique name for the benchmark test
  packageBenchName=$(echo "$pkgPath" | sed 's/\.\///g; s/\//_/g')
  outputFile="${benchmarksResults}/${packageBenchName}_benchmarks_new.txt"
  oldBenchmarks="${benchmarksResults}/${packageBenchName}_benchmarks_old.txt"
  benchStatFile="${benchmarksResults}/${packageBenchName}_benchstat.txt"


  # count should be at least 10. Ideally 20
  go test -bench=. -count=16 -benchmem "$pkgPath" | tee "$outputFile"

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
