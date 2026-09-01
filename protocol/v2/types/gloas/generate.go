package gloas

// The SSV Gloas types are SSZ-encoded by pk910/dynamic-ssz: fastssz cannot merkleize the progressive
// BlindedExecutionPayloadEnvelope, and the rest compose go-eth2-client's dynamic-ssz spec/gloas types
// (aliased in spec_aliases.go). Regenerate with `go generate ./...`.
//
// Hand-written type files are snake_case.go; the generated encoders are flatcase *_ssz.go — do not edit
// those. Generation overwrites *_ssz.go in place (no `rm` first) so the package stays compilable for
// dynssz-gen to load it: the hand-written Encode/Decode wrappers reference the generated MarshalSSZ.
//go:generate go tool -modfile=../../../../tool.mod dynssz-gen -config generate.yaml
