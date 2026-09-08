# QA fault menu — pass M3 runbook

**This branch (`qa/gloas-m3-fault-menu`) is QA instrumentation for pass M3 of the Glamsterdam
(Gloas / ePBS) QA programme. It is never merged into `stage` or `main`.** It exists to give one
operator, for one restart, one deliberate misbehaviour, so the rest of the cluster can be checked
against the message-validation and value-check rules pass M3 exists to exercise. Full spec:
`docs/qa-glamsterdam-test-plan-passes.md` §6 (ssv-scout repo). Code: `qa/faults/`, `qa/faultnet/`.

## 1. The menu is 19 values, not 20

`docs/qa-glamsterdam-test-plan-passes.md` §6.3 lists 20 candidate values, but `envelope-round-3`
(EPE-09) is deliberately **not** in the registry (`qa/faults/faults.go`). The programme owner
excluded it: the §6 envelope-QBFT mechanism it targets is being replaced upstream, and the EPE
scenario cards are due a refresh once that lands. Re-adding it is a decision for a later branch,
not a bug in this one.

Consequence for the tester: **`FAULT=envelope-round-3` aborts startup**, exactly like any other
unknown value (`qa/faults/faults.go` `Parse`). That is intended. Do not treat it as a regression —
there is no EPE-09 row to run in this pass.

## 2. Selecting a fault

Set `FAULT=<value>` in the environment of **one** operator (config field `QAFault`, env key
`FAULT`) and restart it. Changing the fault always means changing `FAULT` and restarting; nothing
here is a runtime toggle.

On boot, the node logs one of two banners:

- No fault: `qa fault instrumentation present, no fault active`
- A fault: `🧪 QA FAULT INSTRUMENTATION ACTIVE — this node misbehaves on purpose`, carrying
  `qa_fault`, `behaviour`, `site`, and `scenarios` fields.

Confirm the banner names the value you set before recording anything else — if it does not, you
restarted the wrong operator or mistyped the value.

An **unknown** `FAULT` value aborts startup with `qa fault menu: unknown FAULT "...", known
values: ...`. This is deliberate (`qa/faults/faults.go` `Parse`): a typo must fail loudly, not
quietly run a clean node that gets recorded as a passing fault run.

## 3. Confirming a fault fired

Every injection site calls `faults.Fired`, which logs:

```
🧪 qa fault injected
```

with fields `qa_fault`, plus whatever the site adds (`slot`, `role`, and so on). Confirm it with:

```bash
scout.py --env mini logs query --service ssv-node --operators <n> --search "🧪 qa fault injected" --jsonl --fields time,slot,qa_fault,role
```

**An honest-side silence is only evidence when this line is present on the faulted operator.** If
the injection line is missing, the fault never fired — check the banner and the duty schedule
before concluding the honest side is behaving correctly. See also §13, point 1: whether this line
tracks the actual broadcast is itself an open verification item, not yet confirmed on a live
enclave.

## 4. Fault menu

One row per value, in registry order (`qa/faults/faults.go` `menu`). The "Honest-side oracle"
column is carried verbatim (or split per value, where §6.6 gives one line for two values) from
`docs/qa-glamsterdam-test-plan-passes.md` §6.6, so you do not need both documents open.

| `FAULT=` value | Scenario IDs | What the faulted node does | Honest-side oracle (§6.6, verbatim) |
|---|---|---|---|
| `vote-112b` | ATT-02 | Proposes a pre-Gloas 112-byte `BeaconVote` at a Gloas slot. | "failed to decode gloas beacon vote"; round change; decide under the next leader. **Grep for the real string instead — see the errata note at the end of this document: the value check actually emits `failed decoding gloas beacon vote` (`protocol/v2/ssv/value_check.go:112`), not the `to decode` wording above (`docs/qa-glamsterdam-test-plan-passes.md` §6.6).** |
| `vote-index-2` | ATT-03 | Proposes `AttestationDataIndex = 2`. | "gloas attestation data index out of range"; round change. |
| `vote-index-flip` | FLT-05 | Proposes the wrong but valid index (0 becomes 1, 1 becomes 0). | "the wrong valid index is accepted (by design); record the on-chain result." |
| `double-vote-index` | ATT-04 | Signs index 0, then asks the local signer for index 1 on the *same slot* — no wire traffic at all. | "the local signer refuses the second signature." **Read this on the faulted node, not the honest ones — see §7.** |
| `ptc-qbft` | PTC-07, MSG-03 | Sends a QBFT consensus message under role 7 (PTC), reusing this node's role-7 `MessageID`. | "consensus messages for role 7 rejected" (half of the combined PTC-07/MSG-03 line; the other half covers `two-entries`, below). |
| `two-entries` | MSG-03 | Sends two entries in one role-7 partial-signature container. Only the forged copy is sent — see §9. | "2-entry containers rejected" (other half of the PTC-07/MSG-03 line above). |
| `ptc-3-per-epoch` | MSG-07 | Sends the honest PTC partial for slot S, plus two forged copies for S+1 and S+2 of the same epoch. | §6.6 says "the third PTC partial per epoch is ignored." **This needs an erratum — see §8: it is the assignment gate, not the count rule, that ignores them.** |
| `block-wrong-version` | PRO-07 | Proposes a Gloas block stamped with the Fulu (previous fork) data version. | "value check rejects; round change; honest completion." |
| `prefs-conflict` | PRF-07, FLT-07 | Emits a preference whose fee recipient differs from the cluster's, so its signing root never matches. | "the first-seen preference stays pinned." (PRF-07's line; FLT-07 has no separate line of its own in §6.6.) |
| `prefs-34-apart` | MSG-06 | Alternates the preference root for proposal slots 34 slots apart (fee recipient derived from the slot). | "the second slot does not open a new 4-root budget." |
| `prefs-5-roots` | MSG-05, FLT-07 | Emits five distinct preference roots for one slot (budget is four). | "roots 1 to 4 accepted; root 5 ignored; repeated root ignored." |
| `prefs-early` | MSG-04 | Emits preferences 65 slots early — one slot past the 64-slot (2-epoch) lookahead allowance. | "early and late messages ignored with the earliness/lateness reason." (shared MSG-04 line with `prefs-late`.) |
| `prefs-late` | MSG-04 | Emits preferences three slots late — one slot past the 2-slot lateness allowance. **The delayed copy can wait up to ~66 slots (~13 minutes) to send** — preferences are emitted for every still-upcoming proposal slot in the lookahead, not just the nearest one, so keep the fault value active past `proposal slot + 3` or you will see nothing; silence before then is not a result. | "early and late messages ignored with the earliness/lateness reason." (shared MSG-04 line with `prefs-early`.) |
| `prefs-replay` | FLT-11 | Replays one valid preference at a high rate across ~66 slots. **Read §6 before running this one.** | "validation memory stays flat; honest peers keep their scores." |
| `auth-no-builders` | MSG-10 | Gives itself one synthetic direct-builder entry, so it broadcasts request-auth partials (type 9) that the rest of the cluster — which has no `Builders` configured — cannot service. | "receivers without Builders log the hard-fail; §5 unaffected." The hard-fail is `errors.New("no builders configured")` (`protocol/v2/ssv/runner/proposer_preferences_request_auth.go:123`). |
| `envelope-foreign-root` | EPE-04, FLT-06 | Proposes an execution-payload envelope with a foreign `BeaconBlockRoot`. | "envelope beacon block root does not match the decided block ... ; round change" (the foreign-root half of the combined EPE-04/FLT-06 line). |
| `envelope-builder-index` | EPE-04 | Proposes an envelope with a non-self-build `BuilderIndex`. | "... or envelope builder index is not self-build; round change" (the builder-index half of the combined EPE-04/FLT-06 line). |
| `role7-prefork` | MSG-02 | Sends role 7, 8 and 9 messages for a fixed pre-fork slot (cloned from any outgoing **validator-role** partial-signature message this node sends — PTC, preferences, envelope, aggregator or validator-registration — so the value fires without depending on `vr-postfork`. Committee-role partials are deliberately skipped: a committee MsgID carries a committee ID rather than a validator pubkey, so the forged clone could not be published at all and would only add unwire-backed `🧪` lines to this log). | "pre-fork role 7/8/9 messages rejected; post-fork VR partials rejected." (shared MSG-02 line with `vr-postfork`; this value exercises the first half.) |
| `vr-postfork` | MSG-02 | Keeps the validator-registration heartbeat running at Gloas slots (does not stop emitting role-4 messages after the fork). | "pre-fork role 7/8/9 messages rejected; post-fork VR partials rejected." (shared MSG-02 line with `role7-prefork`; this value exercises the second half.) |

**Excluded from the menu, not a bug:** `envelope-round-3` (EPE-09) — see §1.

## 5. Standing caveat — the permissive value check

Five values swap the faulted node's own value check for a permissive one, so that node also
*accepts* invalid proposals from others, not just sends them: `vote-112b`, `vote-index-2`,
`block-wrong-version`, `envelope-foreign-root`, `envelope-builder-index`.

This has no visible effect on a fault run — `FAULT` is fixed for the life of the process, so no
other operator sends this node a *different* invalid proposal to accept during the same run — but
it changes how long the swap lasts, and that is worth knowing before you read a log:

- **Committee runner** (`vote-112b`, `vote-index-2`) and **envelope runner**
  (`envelope-foreign-root`, `envelope-builder-index`): the value check is rebuilt every duty in
  each runner's `executeDuty` (`protocol/v2/ssv/runner/committee.go` `executeDuty`;
  `protocol/v2/ssv/runner/envelope.go` `executeDuty`, line 267 — `produceBlindedEnvelope` is where
  the permissive swap itself happens, not the rebuild). The permissive swap is **duty-scoped** — a
  fresh, honest checker replaces it on the very next duty.
- **Proposer runner** (`block-wrong-version`): the value check is set once, at construction, and
  is never rebuilt per duty. Once `block-wrong-version` fires, the permissive check **stays for
  that runner's lifetime** — i.e. until the node restarts.

## 6. Standing caveat — `prefs-replay`

`prefs-replay` (FLT-11) is the one value with real operational cost, beyond the fault itself:

- **It costs the faulted operator gossipsub score**, and may get it pruned from the mesh. This is
  an *observation to record*, not a defect — FLT-11's own oracle is "honest peers keep their
  scores," which is about the honest side, not this one.
- **The series is ~13 minutes long** (15,841 sends, 50 ms apart) and **cannot be cancelled short of
  restarting the node** — `qa/faultnet` has no cancellation path once a series starts.
- **Only the first send of a series is logged**, plus one summary line
  (`🧪 qa fault: repeated send series finished`, carrying `sent`/`planned`) when it ends.
  Intermediate sends are deliberately silent, so the log buffer the evidence is read from is not
  swamped by ~15,800 lines.
- **20 messages/second is a floor, not a bound.** A validator can have several lookahead proposer-
  preferences runners in flight at once; their series overlap, and the realized send rate can
  exceed 20/s. Do not treat 20/s as an upper bound when reading gossipsub or CPU metrics during
  this run.

## 7. `double-vote-index` (ATT-04) — read the oracle on the faulted node

This is the one fault with **no wire component at all**. It signs index 0, then immediately asks
the local signer for index 1 on the same slot. **Read the oracle on the node running
`FAULT=double-vote-index`**, not on the honest operators — there is nothing for them to see.

On the local-signer path, the second signature attempt fails with:

```
could not sign beacon object: slashable attestation (HighestAttestationVote), not signing
```

(`ssvsigner/ekm/slashing_protector.go:91`, wrapped by `protocol/v2/ssv/runner/runner_signatures.go:60`.)
A remote (Web3Signer) signer wraps the underlying error differently, so search for the substring
`slashable attestation` rather than the full string — it matches either path. The result (both the
first index and the failed second attempt) is logged via `🧪 qa fault injected` on the faulted
node, with fields `first_index`, `second_index`, `second_sign_err`.

## 8. `ptc-3-per-epoch` (MSG-07) — what it actually exercises, and the card erratum

`ptc-3-per-epoch` does **not** exercise the per-epoch duty-count rule its name suggests. It
exercises the **PTC assignment gate**, which the `qa/faultnet` code comments call "§7" — the SIP's
own §7, Message validation (see `docs/qa-glamsterdam-test-plan.md` §3.7), not a section of this
document.

A validator holds exactly one PTC (role 7) duty slot per epoch. `validateBeaconDuty`
(`message/validation/partial_validation.go:189`) runs **before** `validateDutyCount`
(`message/validation/partial_validation.go:217`). For role 7, `validateBeaconDuty`'s
`RolePTCAttester` branch (`message/validation/common_checks.go:238`) refuses both forged copies
with `ErrNoDuty` — there is no genuine duty for the validator at S+1 or S+2, so the assignment gate
rejects them first. `validateDutyCount`, and the per-epoch limit it enforces
(`common_checks.go:131-135` — 2, meaning "one duty per epoch plus a reorg margin"), is **never
reached** by this or any other sending-side fault. `ErrTooManyDutiesPerEpoch` for role 7 is
reachable only across a genuine duty re-fetch, which no forged message can trigger.

**For role 7, the assignment gate subsumes the per-epoch limit.** What `ptc-3-per-epoch` actually
proves is that the honest side enforces the one-PTC-duty-per-epoch *assignment* correctly — a
different, related, and still-useful result, but not the one the value's name implies.

**Card erratum:** `docs/qa-glamsterdam-test-plan-passes.md` §6.6 states the MSG-07 oracle as "the
third PTC partial per epoch is ignored." That describes the visible outcome, but the mechanism is
`ErrNoDuty` (assignment gate), not `ErrTooManyDutiesPerEpoch` (duty-count rule). **This needs an
erratum** in the passes document and the MSG-07 scenario card before the QA programme relies on the
stated oracle to mean "the count rule fired."

## 9. `two-entries` (MSG-03) — why only the forged copy is sent

`two-entries` sends **only the forged (2-entry) message**, not the honest one, so the faulted
operator contributes no honest PTC partial for that slot. Pass M3 runs at size 7 (f = 2), so the
other six operators still reach quorum without it.

Why: `validatePartialSigMessagesByDutyLogic` checks `validatePartialSignatureMessageLimit` (the
"already have a pre-consensus message for this signer+slot" rule) before the entry-count rule this
fault targets — but only once a `signerState` already exists for that (signer, slot). If the honest
message went first, it would create that state, and the forged copy would then be refused on the
wrong rule (`ErrTooManyPartialSigMessage`) instead of the one under test
(`ErrTooManySignaturesInPartialSigMessage`). Sending only the forgery keeps no `signerState` in
place, so `validateSlotTime` and `validateDutyCount` pass and the entry-count check at the bottom
of `validatePartialSigMessagesByDutyLogic` is what actually fires.

## 10. Building the instrumented image

Standard build:

```bash
make build   # -> ./bin/ssvnode
```

For ssv-mini, the image needs a **distinct tag** so it can be selected per operator (see §12) —
build it separately from the stock image so `FAULT=none` operators keep running the plain image:

```bash
docker build -t node/ssv-fault .
```

## 11. Full-menu boot smoke (build + startup check)

Verify every value is accepted and correctly named in the boot banner, before touching an enclave —
it is the cheapest way to catch a typo in the registry. Enumerate the menu from the code, not from
a hardcoded list, so this loop cannot drift from `qa/faults/faults.go`:

```bash
go build ./...
go test ./...

for f in $(go run ./qa/faults/cmd/list); do
  echo "== $f"
  FAULT=$f timeout 20 ./bin/ssvnode start-node --config /path/to/config.yaml 2>&1 \
    | grep -m1 "QA FAULT INSTRUMENTATION ACTIVE" || echo "NO BANNER for $f"
done
```

Expected: 19 banners, no `NO BANNER` line, one per line of `go run ./qa/faults/cmd/list` output.

**Status in this environment: outstanding.** There is no node config and no enclave available
here. `go build ./...` and the package test suites were run instead (see the task report); the
loop above still needs to be run once, on a machine with a real `config.yaml`, before pass M3
starts.

## 12. ssv-mini end-to-end smoke

Bring up a size-4 enclave (the M3 size-7 rig is not needed to prove the plumbing) with the
instrumented image on one operator and `FAULT=two-entries`:

```bash
venv/bin/python scout.py --env mini logs query --service ssv-node --operators 3 --search "🧪 QA FAULT INSTRUMENTATION ACTIVE" --jsonl --fields time,qa_fault
venv/bin/python scout.py --env mini logs query --service ssv-node --operators 3 --search "🧪 qa fault injected" --summary
venv/bin/python scout.py --env mini logs query --service ssv-node --operators 0,1,2 --search "too many signatures in a partial-signature message" --summary
```

Expected: the banner once, the injection line once per PTC duty of operator 3, and the rejection on
all three honest operators. If the honest side is silent while the injection line is present, the
message never reached them — check the broadcast topic (see §13) before blaming validation.

**Status in this environment: outstanding.** There is no running Kurtosis enclave here. This step
also needs the per-operator image/env override described in the companion ssv-mini plan (a size-7
enclave today runs one image and one `env_vars` map for every SSV node) — until that lands, each
fault costs a cold resync via `kurtosis service update --env`, which wipes the log buffer.

## 13. Closed-by-source-reading verification points — still confirm on the first live run

Two items were open questions before the final-review fix wave; both are now **CLOSED by source
reading**, not merely deferred. Neither `qa/faults`' nor `qa/faultnet`'s unit tests can exercise a
live enclave, so still confirm each once, on the first real run — but they are no longer unknowns
to chase, and a silent honest side is not itself reason to suspect either one.

1. **The network decorator is on the runner broadcast path it is meant to intercept — CLOSED.**
   `cli/operator/node.go` sets `valOpts.Network = faultnet.Wrap(...)`, and `valOpts` (as
   `ValidatorOptions`) is the only `Network` handed to `validator.NewController` — every runner and
   every QBFT controller it builds shares this one wrapped instance, reached through the shared
   `BaseRunner` broadcast helpers (`signAndBroadcastPartialSigMsgs`, `signAndBroadcastPostConsensusMsg`
   in `protocol/v2/ssv/runner/runner.go`), not through `CommitteeRunner` specifically. Separately,
   `cli/operator/node.go`'s startup type-assertions (`p2pv1.PeersIndexProvider`, `p2pv1.HostProvider`,
   `p2pv1.HealthChecker`) run against the raw, pre-`Wrap` `p2pNetwork` variable, not `valOpts.Network`
   — so those assertions still succeed regardless of the decorator, and `*faultnet.Network` promoting
   every method but `BroadcastAtSlot` through its embedded `network.P2PNetwork` (rather than
   hand-writing passthroughs) is what makes both of those true at once. Confirm on the first live run
   by checking that `🧪 qa fault injected` lines appear carrying the roles the active fault targets,
   and that their counts track `📤 broadcast message to topic` counts for the faulted operator, for
   every wire fault (`ptc-qbft`, `two-entries`, `ptc-3-per-epoch`, `prefs-5-roots`, `prefs-early`,
   `prefs-late`, `role7-prefork`). `role7-prefork` used to be an exception here — before the second
   review cycle's fix, it also fired on committee-role partials it could never actually publish
   (their executor bytes are not a validator pubkey), so its injection count ran ahead of a wire
   count of zero for those. That is now fixed at the source (`qa/faultnet/plan.go`'s
   `forgeGloasRoles` skips `RoleCommittee` and `RoleAggregatorCommittee`), so `role7-prefork` tracks
   like every other wire fault above and needs no separate caveat.

   `prefs-replay` is excluded from that list, by design, not by bug: only the first send of its
   series announces a `🧪 qa fault injected` line, with every further repeat silent until the
   closing summary line (`🧪 qa fault: repeated send series finished`, carrying `sent`/`planned` —
   see §6). Counting `🧪` lines for `prefs-replay` will always look like 1, regardless of how many
   sends actually went out — **compare the summary line's `sent` field against the `📤` count
   instead.** `sent` counts what `libp2p`'s `Topic.Publish` actually accepted, and since the second
   review cycle's fix (`qa/faultnet/plan.go`'s `perturbForRepeat` now spreads its per-repeat counter
   across four bytes instead of one — a single byte aliased every 256 repeats, well inside the
   series' ~66-slot span, and a gossipsub duplicate makes `Publish` return `nil` rather than an
   error, so `sent` used to count sends the mesh had already silently dropped) those two counts
   should agree; a persistently large gap between them now means real gossipsub dedupe on the wire,
   which is itself worth reporting, not a sign the counting method is wrong.
2. **The synthetic builder URL in `auth-no-builders` cannot degrade block production — CLOSED.**
   Source-level tracing confirms the URL cannot reach the `produceBlockV4` request body in a way
   that breaks the SIP's §4, Proposer (`docs/qa-glamsterdam-test-plan.md` §3.4): an unresolved auth
   is omitted from the produceBlockV4 body entirely rather than sent malformed (see
   `protocol/v2/ssv/runner/observability.go`'s builder-telemetry comment), and
   `gloas.ResolveBuilderConfig` only validates URL scheme and host (plus auth-data shape and the
   builder-pubkey list) — it never attempts to reach the URL, so a synthetic, unreachable one loads
   cleanly and never touches proposal construction. Confirm on the first live run that the faulted
   operator still proposes normally.

## 14. Runbook corrections from the final-review fix wave

Four notes added while closing the whole-branch review's fix wave. None of these change any code;
they change what a tester should conclude from what the code already does.

### 14.1 The injection line is not a wire oracle for four leader-gated faults

For `vote-112b`, `vote-index-2`, `vote-index-flip` and `block-wrong-version`, `faults.Fired` is
emitted from `executeDuty`, which runs on **every** operator, **every** slot — but the malformed
value only actually reaches the wire when this node is the round-1 QBFT leader, roughly one slot in
seven at pass M3's size-7 rig. **"Injection line present, honest side silent" is not evidence of
acceptance for these four values on its own** — it is equally what a slot where this node never led
looks like. Before recording a verdict from one of these four, confirm from the QBFT logs that this
node actually led round 1 for that duty; only then does the honest side's silence mean anything.

### 14.2 REJECT-producing faults cost gossip score, and it survives the restart between menu values

`ptc-qbft`, `two-entries`, `role7-prefork` and `vr-postfork` all produce message-validation REJECTs
on the honest side. Gossipsub peer scoring keys on the libp2p peer id, which comes from
`NetworkPrivateKey` — unchanged across a `FAULT` restart — so a REJECT-heavy value run early in a
session can leave the operator's score degraded (fewer peers, a thinner mesh) for every value run
after it, including non-REJECT ones. **Run the REJECT-producing values LAST in a session**, or check
operator 5's peer count and mesh health before each value if that ordering isn't possible — a silent
honest side late in a run may be a graylisted sender, not a clean pass.

### 14.3 `prefs-5-roots` does not exercise §6.6's "repeated root ignored" clause

`prefs-5-roots`'s oracle in §4's table is "roots 1 to 4 accepted; root 5 ignored; repeated root
ignored" — but nothing in this value ever repeats a root. Its five clones (the honest message plus
four `Clone`s, each with a distinct `SigningRoot` byte-flip) are deterministic and never re-sent, so
the §5 runner's suppression of an identical re-emission is never exercised. This value proves the
first two clauses (four accepted, the fifth ignored by budget); the third clause needs a
value that re-sends an already-accepted root, which nothing in the current menu does.

### 14.4 `prefs-early`'s margin is one slot minus 50 ms of clock tolerance

`prefsEarlySlots` targets exactly one slot past the 2-epoch (64-slot) lookahead allowance, and
`validateSlotTime`'s clock-error tolerance eats 50 ms of that margin. A one-off run where the early
copy is **not** rejected most likely means the broadcast landed inside the last 50 ms of a slot
boundary, not that earliness validation has regressed — re-run before concluding the latter.

## Errata for the pass document

Four discrepancies between `docs/qa-glamsterdam-test-plan-passes.md` §6.6 (or the MSG-06 design
intent) and the code were found while writing this runbook. All four are recorded here so they
travel back to whoever maintains that document; none is a defect in this branch.

1. **ATT-02's oracle string is stale.** §6.6 gives the oracle as `failed to decode gloas beacon
   vote`. The value check that actually produces the round-change (`protocol/v2/ssv/value_check.go:112`,
   `gloasVoteChecker.CheckValue`) emits `failed decoding gloas beacon vote` — different wording
   ("decoding" vs. "to decode"). The `to decode` phrasing exists in the code, but only in an
   unrelated Debug line in the committee observer
   (`protocol/v2/ssv/validator/committee_observer.go:438`, "failed to decode gloas beacon vote from
   proposal"), which is not the M3 oracle's source. A tester grepping the §6.6 string verbatim
   against `ssv-node` logs will find nothing on the actual rejection path and may record a false
   negative for ATT-02. §6.6 needs a wording fix.
2. **MSG-07's oracle names the wrong mechanism.** §6.6 gives the oracle as "the third PTC partial
   per epoch is ignored," describing `ptc-3-per-epoch`'s visible outcome. The mechanism is the PTC
   assignment gate (`ErrNoDuty`), not the per-epoch duty-count rule (`ErrTooManyDutiesPerEpoch`) the
   wording implies — see §8 above for the full writeup. §6.6 and the MSG-07 scenario card need an
   erratum.
3. **`prefs-34-apart` does not probe MSG-06.** It alternates the preference root between two
   proposal slots 34 slots apart, but emits exactly **one** root per slot — no root budget is ever
   exhausted at either slot, so the ring-buffer aliasing MSG-06 is meant to probe (does a slot-34-
   apart signer-state entry alias or evict another slot's budget?) is never exercised. As built, this
   value is a slot-varying variant of `prefs-conflict`, not an MSG-06 probe. It needs a redesign
   before MSG-06's e2e half can be claimed: 4 roots at slot S (exhausting the budget), 1 root at slot
   S+34, then a 5th root back at slot S, to actually test whether the S+34 entry aliased or evicted
   S's budget. **Do not record an MSG-06 verdict from the current `prefs-34-apart` as built.**
4. **`prefs-late`'s oracle depends on the distinct signing root, not just the delay.** After the
   final-review fix wave (FIX 2), `prefs-late` does reach the lateness rule (`messageLateness`) — but
   only because its delayed copy carries a second, distinct signing root (see plan.go's `prefsLate`
   doc comment for the two traps this avoids: `ErrNoDuty` from a backdated payload slot, and a
   duplicate-signing-root rejection from an unperturbed root). A future edit that "simplifies" the
   fault back to an exact duplicate of the honest message would silently move its oracle from
   lateness to a duplicate-root rejection. Note this here so nobody makes that change without
   noticing what it does to the value's oracle. **Separately, the wait before that delayed copy
   sends is not fixed at "a few slots."** `DelaySlots` is computed as `body.Slot + prefsLateSlots -
   now`, and `body.Slot` is the honest message's own proposal slot — but proposer-preferences are
   emitted once per still-upcoming proposal slot across the whole lookahead, not only for the
   nearest one, so `body.Slot` can be up to ~64 slots ahead of `now` at emission time. The delayed
   copy can therefore take up to ~66 slots (~13 minutes) to actually send. A tester who stops
   watching before `proposal slot + 3` has passed will see nothing and may record a false negative;
   silence before then is not a result for this value.
