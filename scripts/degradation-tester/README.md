# Degradation Tester

### Continuous Benchmarking

Degradation Tester is a tool for detecting performance degradation. It allows to compare current benchmark results against previous ones to identify potential performance issues.
It's important to use the same environment and CPU for testing. We use the Github Actions workflow for the benchmarks, which currently uses the `AMD EPYC 7763 64-Core Processor`.

### Approving Performance Changes

If you've made changes that affect performance, whether they are improvements or degradations, it's essential to regenerate the benchmarks and replace the existing files in the `benchmarks` directory. This ensures that future comparisons accurately reflect the impact of your changes and maintains the integrity of the performance testing process.

To regenerate and update the benchmarks, use the `update-benchmarks` command in the Makefile. To update the benchmarks in the repo using Github Actions, you should use the manually triggered workflow for your Pull Request named **Manual Benchmarks Update**.

To run the workflow:
- Open the PR
- Go to Actions
- Choose `Manual Benchmarks Update` from the list of available actions
- Click `Run workflow` and choose your branch from the dropdown menu

The workflow is located in `.github/workflows/manual-benchmarks-update-.yml`.

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

### Dependecies

- `yq`
- `benchstat`

### Installation

Install the benchmarks degradation checker tool:

`cd scripts/degradation-tester/ && go install .`

### Running Degradation Tests

To run the degradation tests, execute:

```bash
make degradation-test
```

### Updating Benchmarks

To replace the benchmarks with your local ones, execute:

```bash
make update-benchmarks
```

## Usage with GitHub Actions

`.github/workflows/degradation-test.yml` containing a workflow for automatically running degradation tests as part of the CI/CD process

`.github/workflows/manual-benchmarks-update-.yml`. containing a workflow for updating the benchmarks with manual triggering

## Scripts

The `./scripts/degradation-tester` directory contains the following scripts:

- `degradation-check.sh` - running degradation tests. Runs benchmarks, saves the new results, compares benchmarking results per all listed packages in `config.yaml`

- `update-benchmarks.sh` - A script for updating benchmarks
