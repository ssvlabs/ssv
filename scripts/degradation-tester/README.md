# Degradation Tester

#### Continuous Benchmarking

Degradation Tester is a tool for detecting performance degradation in Go projects. It allows comparing current benchmark results against previous ones to identify potential performance issues.

#### Approving Performance Changes

If you've made changes that affect performance, whether improvements or degradations, it's essential to regenerate the benchmarks and replace the existing files in the `benchmarks` directory. This ensures that future comparisons accurately reflect the impact of your changes and maintains the integrity of the performance testing process.

 To regenerate and update the benchmarks, use the `update-benchmarks`:

```bash
make update-benchmarks
```

## Configuration

To use the tool, you need to set up a YAML configuration file. Here is an example configuration:

```yaml
DefaultOpDelta: 4.0
DefaultAllocDelta: 0
Packages:
  - Path: "./message/validation"
    Tests:
      - Name: "VerifyRSASignature"
        OpDelta: 3.0
  - Path: "./protocol/v2/types"
    Tests:
      - Name: "VerifyBLS"
        OpDelta: 6.0
      - Name: "VerifyPKCS1v15"
        OpDelta: 4.0
      - Name: "VerifyPKCS1v15FastHash"
        OpDelta: 6.0
      - Name: "VerifyPSS"
        OpDelta: 5.0
```

- `DefaultOpDelta` and `DefaultAllocDelta` specify the default performance change thresholds (in percentages) that are allowed for all tests unless specific values are provided.
- `Path` refers to the directory containing the tests.
- `Tests` is a list of tests with names and permissible performance change thresholds.

## Local Usage

### Installation

Install the benchmarks degradation checker tool:

`cd scripts/degradation-tester/ && go install .`

### Running Degradation Tests

To run degradation tests, execute:

```bash
make degradation-test
```

### Updating Benchmarks

To update benchmarks, execute:

```bash
make update-benchmarks
```

## Usage with GitHub Actions

`.github/workflows/degradation-test.yml` containing a workflow for automatically running degradation tests as part of the CI/CD process

### Scripts

The `./scripts/degradation-tester` directory contains the following scripts:

- `degradation-check.sh` - running degradation tests. Runs benchmarks, saves the new results, compares benchmarking results per all listed packages in `config.yaml`

- `update-benchmarks.sh` - A script for updating benchmarks

### Dependencies

> Note: Ensure yq is installed in your system for proper YAML config parsing.
