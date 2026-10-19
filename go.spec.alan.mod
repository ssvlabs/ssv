// Pins github.com/ssvlabs/ssv-spec to the Alan (pre-Boole) release for `make spec-test-alan`, which
// downloads it and reads its spec-test vectors (see ibft/storage specGoModFilename). Nothing is built
// against this file, so it lists ssv-spec only: requiring anything that depends on a newer ssv-spec
// (e.g. ./ssvsigner) would make the go command raise this pin.
module github.com/ssvlabs/ssv

go 1.26.0

require github.com/ssvlabs/ssv-spec v1.2.2
