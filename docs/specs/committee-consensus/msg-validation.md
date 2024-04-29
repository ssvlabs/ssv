# Message validation - Committee consensus

## Specification
- Based on knowledge base pull request https://github.com/bloxapp/knowledge-base-internal/pull/20

## Design considerations

- How to validate msg id differently, depending on if its relating to a cluster or validator
- Need to make sure rules restrict that a cluster can do up to 1 consensus per slot or max 32 per epoch - min(#active_validator * 2, 32) same for post consensus
- Make sure that in attestation post-consensus we don't accept the same validator index more than twice per epoch
- How backward compitablity affects on the design, so that post fork its easy to cleanup

## Design Suggestions

- Create new message validator to use after fork to not affect on current validation
- for stage test make sure we have clear logs on msg rejections/ignored 