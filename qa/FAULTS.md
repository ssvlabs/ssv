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
| `vote-112b` | ATT-02 | Proposes a pre-Gloas 112-byte `BeaconVote` at a Gloas slot. | "failed to decode gloas beacon vote"; round change; decide under the next leader. |
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
| `prefs-late` | MSG-04 | Emits preferences three slots late — one slot past the 2-slot lateness allowance. | "early and late messages ignored with the earliness/lateness reason." (shared MSG-04 line with `prefs-early`.) |
| `prefs-replay` | FLT-11 | Replays one valid preference at a high rate across ~66 slots. **Read §6 before running this one.** | "validation memory stays flat; honest peers keep their scores." |
| `auth-no-builders` | MSG-10 | Gives itself one synthetic direct-builder entry, so it broadcasts request-auth partials (type 9) that the rest of the cluster — which has no `Builders` configured — cannot service. | "receivers without Builders log the hard-fail; §5 unaffected." The hard-fail is `errors.New("no builders configured")` (`protocol/v2/ssv/runner/proposer_preferences_request_auth.go:123`). |
| `envelope-foreign-root` | EPE-04, FLT-06 | Proposes an execution-payload envelope with a foreign `BeaconBlockRoot`. | "envelope beacon block root does not match the decided block ... ; round change" (the foreign-root half of the combined EPE-04/FLT-06 line). |
| `envelope-builder-index` | EPE-04 | Proposes an envelope with a non-self-build `BuilderIndex`. | "... or envelope builder index is not self-build; round change" (the builder-index half of the combined EPE-04/FLT-06 line). |
| `role7-prefork` | MSG-02 | Sends role 7, 8 and 9 messages for pre-fork slots (cloned from this node's own validator-registration partial). | "pre-fork role 7/8/9 messages rejected; post-fork VR partials rejected." (shared MSG-02 line with `vr-postfork`; this value exercises the first half.) |
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
  (`envelope-foreign-root`, `envelope-builder-index`): the value check is rebuilt every duty
  (`protocol/v2/ssv/runner/committee.go` `executeDuty`, `protocol/v2/ssv/runner/envelope.go`
  `produceBlindedEnvelope`). The permissive swap is **duty-scoped** — a fresh, honest checker
  replaces it on the very next duty.
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

## 13. Open verification points — no unit test closes these

Two items are known to be unverified on a live enclave. Neither can be closed by `qa/faults`'
or `qa/faultnet`'s unit tests; they need a real run.

1. **The network decorator is on the committee-runner broadcast path.** Confirm by comparing
   `🧪 qa fault injected` counts against `📤 broadcast message to topic` counts for the faulted
   operator — they should track together for every wire fault (`ptc-qbft`, `two-entries`,
   `ptc-3-per-epoch`, `prefs-5-roots`, `prefs-early`, `prefs-late`, `prefs-replay`,
   `role7-prefork`). Deferred since the decorator was first written.
2. **The synthetic builder URL in `auth-no-builders` does not degrade block production on the
   faulted node.** Source-level tracing says the URL cannot reach the `produceBlockV4` request
   body in a way that breaks the SIP's §4, Proposer (`docs/qa-glamsterdam-test-plan.md` §3.4) — but
   that is a trace, not a run. Confirm the faulted operator still proposes normally once this runs
   on ssv-mini.
