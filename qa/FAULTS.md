# QA fault menu — pass M3 runbook

**This branch (`qa/gloas-m3-fault-menu`) is QA instrumentation for pass M3 of the Glamsterdam
(Gloas / ePBS) QA programme. It is never merged into `stage` or `main`.** It exists to give one
operator, for one restart, one deliberate misbehaviour, so the rest of the cluster can be checked
against the message-validation and value-check rules pass M3 exists to exercise. Full spec:
`docs/qa-glamsterdam-test-plan-passes.md` §6 (ssv-scout repo); design and decisions D1–D6:
`docs/superpowers/plans/2026-09-28-gloas-m3-fault-menu-v2-addendum.md` (ssv-scout repo). Code:
`qa/faults/`, `qa/faultnet/`, `qa/faultval/`, plus the runner hooks named in §4.

**Base.** `origin/epbs-gloas` @ `8d4f83fa7`, rebased 2026-09-28 from the now-orphaned `613dd52c9`
(the v1 menu, executed 2026-09-08 as `97f7d5826`). Every `file:line` below is for this
tree; line numbers drift, so grep the quoted string, not the line.

**Rig assumed throughout.** Pass M3's size-7 ssv-mini enclave: operator 5 runs the fault, the
honest operators are 0, 1, 2, 3, 4 and 6. Operator 4 (gas limit) and operator 6 (remote signer)
are degraded at the start of §6.5 and restored in its steps 4 and 5.

## 1. The menu is 33 values; three names are retired and abort startup

`qa/faults/faults.go` holds **33** values (`go run ./qa/faults/cmd/list` prints them, in menu
order). v1 had 19; the re-cut adds 16 and retires 2.

**Retired, by design:** `envelope-foreign-root`, `envelope-builder-index` and `envelope-round-3`
(EPE-04, FLT-06, EPE-09 in v1). Generation 3 of SIP §6 removed the code they targeted — the
execution-payload-envelope QBFT runner (`protocol/v2/ssv/runner/envelope.go`) no longer exists on
this base, and the envelope is now an entry in the proposer's post-consensus packet. A value with
no injection site is worse than an absent one: it boots, prints the banner, injects nothing and
"passes". So all three are gone from the registry, and **`FAULT=<any of the three>` aborts
startup** exactly like a typo (`qa/faults/faults.go` `Parse`; pinned by
`TestRetiredValuesAbortStartup`). That is intended; it is not a regression. Their scenarios are
now served by the §4 `payload-root-*` values (EPE-04, FLT-06) and the §6 `postcons-*` values
(EPE-09).

**Added:** `vote-index-1-same-slot`, `block-wrong-slot`, `block-wrong-proposer-index`,
`payload-root-on-external`, `payload-root-zero-selfbuild`, `payload-root-fabricated`,
`prefs-early-epoch`, `auth-9-entries`, `auth-mixed-signer`, `auth-repeat-root-in-packet`,
`postcons-block-only`, `postcons-3-entries`, `postcons-mixed-index`, `postcons-foreign-root`,
`vr-prefork-slot-replay`, `proposer-round-3`.

**Redesigned or changed in behaviour:** `vote-index-flip` (it was broken on the new base: every
operator now checks its own start value in `decide`, `protocol/v2/ssv/runner/runner.go:767`, so
the flipped value never left the node), `prefs-34-apart` (moved to `qa/faultnet`, now a real MSG-06
probe), `prefs-5-roots` (adds a delayed repeat), `two-entries` (now also sends the honest copy),
`role7-prefork` (roles 7 and 8 only, throttled), `vr-postfork` (fourth gate fixed, one-per-epoch
cadence).

`payload-root-on-external` stays registered but is **not executable on ssv-mini** (decision D1,
§4.3 and §14.7).

## 2. Selecting a fault

Set `FAULT=<value>` in the environment of **one** operator (config field `QAFault`, env key
`FAULT`) and restart it. On ssv-mini use `make fault FAULT=<value> OP=5`: it archives the
operator's logs, applies the value, and fails if the node does not come back with it. Do not use
`kurtosis service update` directly — it is a cold resync that wipes the log buffer the oracle is
read from. Changing the fault always means a restart; nothing here is a runtime toggle.

On boot, the node logs one of two banners:

- No fault: `qa fault instrumentation present, no fault active`
- A fault: `🧪 QA FAULT INSTRUMENTATION ACTIVE — this node misbehaves on purpose`, carrying
  `qa_fault`, `behaviour`, `site`, `scenarios` and `no_local_validation` fields.

Confirm the banner names the value you set before recording anything else — if it does not, you
restarted the wrong operator or mistyped the value.

An **unknown** `FAULT` value — including the three retired names — aborts startup with
`qa fault menu: unknown FAULT "...", known values: ...`. A typo must fail loudly, not quietly run
a clean node that gets recorded as a passing fault run.

## 2b. The `FAULT_NO_LOCAL_VALIDATION` modifier — making the honest side observable

Set `FAULT_NO_LOCAL_VALIDATION=true` (config field `QANoLocalValidation`) alongside a `FAULT`
value. It is a **modifier, not a menu value**: `FAULT` selects exactly one misbehaviour, so a menu
entry would force a choice between running `prefs-early` and observing the honest reaction to it.
As a separate variable it composes with every value.

**Why it exists.** A node validates its own outbound gossip before publishing
(`go-libp2p-pubsub` `Topic.Publish` → `ValidateLocal`); a message its own validator refuses is
**never published**. The 2026-09-09 ladder found six values whose rule fired only on the faulted
operator, with every honest operator silent (`prefs-early` 11/11, `prefs-late` 15/15, `ptc-qbft`
20/20, `two-entries` 19/19, `role7-prefork` 3006/3009, `prefs-replay` 75,512). The rule was proven
at the sender; the receivers were never tested. The modifier removes the sender's pubsub gate so
the forged message reaches the mesh.

**Boot banner.** With the modifier on, the node logs, before the fault banner:

```
🧪 QA MODIFIER: LOCAL PUBSUB VALIDATION DISABLED — this node does not validate messages on the
gossip path, so its own forged messages reach the mesh and it also stops rejecting invalid INBOUND
messages. Do not read this node's receive path as evidence.
```

An unrecognised value aborts startup, exactly as an unknown `FAULT` does.

**Per-value requirement** (column "NLV" in §4). There are four cases:

| NLV | Meaning | Values |
|---|---|---|
| **required** | op5's own gate refuses the forged message, so without the modifier nothing reaches the honest side and only the sender-side lines of §3 exist. | `ptc-qbft`, `two-entries`, `ptc-3-per-epoch`, `prefs-34-apart`, `prefs-5-roots`, `prefs-early`, `prefs-early-epoch`, `prefs-late`, `prefs-replay`, `auth-9-entries`, `auth-mixed-signer`, `auth-repeat-root-in-packet`, `postcons-3-entries`, `postcons-mixed-index`, `role7-prefork`, `vr-postfork`, `vr-prefork-slot-replay`, `proposer-round-3` |
| **not needed** | The message is wire-valid; the rule under test is a value check or a runner check. Message validation never decodes the consensus value. Leave it off. | `vote-112b`, `vote-index-2`, `vote-index-flip`, `vote-index-1-same-slot`, `block-wrong-version`, `block-wrong-slot`, `block-wrong-proposer-index`, `payload-root-on-external`, `payload-root-zero-selfbuild`, `payload-root-fabricated`, `prefs-conflict`, `auth-no-builders` |
| **must be off** | Wire-valid packet whose oracle is a runner-level drop; keep op5's receive path normal. | `postcons-block-only`, `postcons-foreign-root` |
| n/a | No wire component. | `double-vote-index` |

`vr-postfork` note: the addendum's value table and the implementer draft list it as "no". That is
wrong for the honest oracle — its role-4 partial at a Gloas slot is a REJECT on op5's own gate too
(`own outbound message rejected by local validation`), so it never leaves op5 without the modifier.
Run it with the modifier to evidence MSG-02 gate 2 on the receivers.

**Scope, and the two limits that matter when reading results.**

1. It wraps **only** the pubsub gate (`cli/operator/node.go:377`,
   `cfg.P2pNetworkConfig.MessageValidator = faultval.Wrap(...)`), never the runner's own validator.
   Runner-level rejections stay live — `no duty assigned` and `no builders configured` are real M3
   oracles and must keep firing.
2. It is **accept-all**, not accept-own. The faulted operator also stops rejecting invalid
   **inbound** messages, and — because the inner validator is bypassed — it also stops logging the
   `own outbound message …` lines of §3. **With the modifier on, op5's receive path is not an
   observation point; read the honest operators only.**

With the modifier unset, `Wrap` returns the production validator unchanged (asserted by identity in
`qa/faultval`'s tests).

**Debug logging is required on the honest operators.** Every message-validation oracle in this
runbook — `rejecting invalid message` (`message/validation/errors.go:264`) and `ignoring invalid
message` (`errors.go:266`) — is logged at **Debug**. So are the runner-level drop lines
(`could not handle message, dropping message`, `protocol/v2/ssv/validator/queue_validator.go:265`
and `committee_queue.go:276`), `skipped stashed proposer-preferences partial on replay`,
`📬 got post-consensus message`, the `did not build the envelope` line and `peer scores`. An honest
operator at Info shows none of them; that silence is not a result.

## 3. Confirming a fault fired

Every injection site calls `faults.Fired`, which logs (Warn):

```
🧪 qa fault injected
```

with `qa_fault` plus whatever the site adds. The `qa/faultnet` values add `slot` (topic slot),
`role`, and for a partial-signature body `payload_slot` and `entries`, plus `label` for multi-row
values — count per `label` to confirm every row went out.

```bash
scout.py --env mini logs query --service ssv-node --operators 5 --search "🧪 qa fault" --jsonl --fields time,msg,slot,qa_fault,role,label,payload_slot,reason
```

**The not-applicable line (new).** A value that has nothing to act on at a slot says so, instead of
staying silent (`faults.NotApplicable`, Warn):

```
🧪 qa fault not applicable
```

with `qa_fault`, `reason` and `slot`. It is **not** an injection — it proves the rig is alive and
the value was inapplicable on that slot. It is emitted by:

| Value | When | `reason` |
|---|---|---|
| `payload-root-on-external` | every self-build proposer slot — **every slot on ssv-mini** (D1) | `self-build slot: no external bid to stamp` |
| `payload-root-zero-selfbuild` | external-bid slot (never on ssv-mini) | `external bid: payload_root is already zero` |
| `payload-root-fabricated` | external-bid slot (never on ssv-mini) | `external bid: no payload_root to fabricate` |
| `postcons-block-only` | external-bid Gloas proposer slot | starts `external-bid slot:` (field `self_build=false`) |
| `postcons-foreign-root` | external-bid Gloas proposer slot | starts `external-bid slot:` (field `self_build=false`) |

`vote-index-1-same-slot` uses a variant of the same idea: when op5 has no same-slot view it still
logs `🧪 qa fault injected` with `applied=false, reason="no same-slot view"` and changes nothing.

**The §6.5 step-3 rig check, restated.** For the duty the value targets, **neither a `🧪 qa fault
injected` nor a `🧪 qa fault not applicable` line on op5 means a broken rig** — not a value to
wait out, and not an honest-side result. For the proposer-slot values, "the duty" means a
proposer duty of one of op5's validators, which is rare; check op5's proposer schedule first.

**Sender-side evidence (new since the rebase).** With the modifier **off**, op5's own gate logs its
refusal of its own forged message (`message/validation/errors.go:237-252`):

- `own outbound message rejected by local validation` (Warn, `:241`) — a REJECT rule fired;
- `own outbound message ignored by local validation` (Debug, `:248`) — a routine IGNORE:
  early, late, duplicated, slot or round already advanced, round too high
  (`routineSelfIgnores`, `:190-199`);
- `own outbound message ignored by local validation (unexpected)` (Warn, `:250`) — any other
  IGNORE, e.g. `ErrNoDuty`, `ErrValidatorRegistrationRetired`, the distinct-root budget;
- `outbound message dropped by local validation` (Debug, `network/topics/controller.go:208`).

These prove the sender enforces the rule; they are **not** the honest-side oracle. With the
modifier **on**, all of them are 0 for the value's role (the gate is bypassed); if they appear, the
modifier is not active.

**`🧪` does not prove wire exit.** `p2pNetwork.BroadcastAtSlot` publishes asynchronously
(`network/topics/controller.go`, `topic.Publish` in a goroutine), and every `Fired` call runs
before or around that hand-off. A local refusal never returns an error to the caller. Wire exit is
proven only by the honest-side oracle (or, with the modifier off, disproven by the lines above).

**Other `🧪` lines.**

- `🧪 qa fault: forged-send budget exhausted, sending honest messages only from now on` (Warn,
  once, `qa_fault`, `max_sets`; `qa/faultnet/budget.go:141`) — `role7-prefork`, `auth-9-entries`,
  `auth-mixed-signer` reached their cap (§14.2). After it the value sends honest traffic only.
- `🧪 qa fault: repeated send series finished` (Warn, `sent`, `planned`;
  `qa/faultnet/faultnet.go:139`) — `prefs-replay` (§6).
- Boot: the four `auth-*` values log **one** `🧪 qa fault injected` at boot with
  `builder_url=http://qa-fault-builder.invalid` (`cli/operator/node.go:276`), when the synthetic
  builder is installed. A missing boot line for these values means no type-9 packet will ever be
  emitted — a broken rig. The 2026-09-09 record of "zero" lines for `auth-no-builders` was a query
  window that started after boot.
- `🧪 qa fault could not build RoundChange(3)` / `… could not broadcast RoundChange(3)` (Warn) —
  `proposer-round-3` hit an error and did **not** fire.
- `qa fault: forged send failed, honest broadcast unaffected` (Warn) — a forged `faultnet` send
  failed; the honest message was still sent.

## 4. Fault menu

One row per value, in registry order (`qa/faults/faults.go` `menu`). The `Scenarios` column is the
registry string verbatim. **NLV** = `FAULT_NO_LOCAL_VALIDATION` (§2b). **Leader** = the malformed
value reaches the wire only when op5 proposes its own start value (§14.1). **Score** = what the
honest side does at message validation: REJECT (costs op5 gossip score, §14.2), IGNORE (no score
cost), or none (wire-valid; the rule is a value or runner check).

| `FAULT=` value | Scenarios | What the faulted node does | NLV | Leader | Score |
|---|---|---|---|---|---|
| `vote-112b` | ATT-02 | Proposes a pre-Gloas 112-byte `BeaconVote` at a Gloas slot. | not needed | yes | none |
| `vote-index-2` | ATT-03 | Proposes `AttestationDataIndex = 2`. | not needed | yes | none |
| `vote-index-flip` | FLT-05 | Proposes the wrong but valid index (0↔1); own check drops only the same-slot view. | not needed | yes | none |
| `vote-index-1-same-slot` | ATT-10 | Proposes index 1 on the block its own view knows to be the duty slot's (pins that root). | not needed | yes | none |
| `double-vote-index` | ATT-04 | Signs index 0, then asks the local signer for index 1 on the same slot. No wire traffic. | n/a | no | none |
| `ptc-qbft` | PTC-07, MSG-03 | Sends a QBFT proposal under role 7 (PTC). | required | no | REJECT (unthrottled) |
| `two-entries` | MSG-03 | Sends its honest role-7 partial **and** a 2-entry clone. | required | no | REJECT (unthrottled) |
| `ptc-3-per-epoch` | MSG-07 | Sends the honest PTC partial for S plus forged copies for S+1 and S+2. | required | no | IGNORE |
| `block-wrong-version` | PRO-07 | Proposes a Gloas block stamped with the Fulu data version. | not needed | yes | none |
| `block-wrong-slot` | PRO-10 | Proposes a Gloas block with slot = duty slot + 1. | not needed | yes | none |
| `block-wrong-proposer-index` | PRO-10 | Proposes a Gloas block with proposer_index = duty validator index + 1. | not needed | yes | none |
| `payload-root-on-external` | PRO-10, EPE-04 | Stamps a non-zero PayloadRoot on a **natural** external bid; not applicable on self-build slots. **Not executable on ssv-mini.** | not needed | yes | none |
| `payload-root-zero-selfbuild` | PRO-10, EPE-04 | Proposes a self-build value with a zero PayloadRoot. | not needed | yes | none |
| `payload-root-fabricated` | EPE-04, FLT-06 | Proposes a self-build value with PayloadRoot[0] ^= 0xff; passes every value check. | not needed | yes | none |
| `prefs-conflict` | PRF-07, FLT-07 | Emits a preference whose fee recipient differs from the cluster's. | not needed | no | none |
| `prefs-34-apart` | MSG-06 | Fills the 4-root budget at P, opens P+34, sends a 5th root at P, repeats the P+34 root. | required | no | IGNORE |
| `prefs-5-roots` | MSG-05, FLT-07 | Five distinct roots for one slot, then a byte-distinct repeat of the honest root. | required | no | IGNORE |
| `prefs-early` | MSG-04 | Extra copy stamped 65 slots ahead of the emission slot (always crosses the epoch bound). | required | no | IGNORE |
| `prefs-early-epoch` | MSG-04 | Probe one slot before the epoch earliness bound, control exactly at it, control proof. | required | no | IGNORE |
| `prefs-late` | MSG-04 | Extra copy with a second distinct root, sent to arrive at P+3. | required | no | IGNORE |
| `prefs-replay` | FLT-11 | Replays one valid preference every 50 ms across 66 slots. **Read §6.** | required | no | IGNORE |
| `auth-no-builders` | MSG-10 | Boots with one synthetic direct-builder entry, so it sends type-9 request-auth partials nobody can service. | not needed | no | none |
| `auth-9-entries` | MSG-03, MSG-10 | 9-entry type-9 packet, then an 8-root packet, then a 9th root. Capped at 3 sets. | required | no | REJECT (3) |
| `auth-mixed-signer` | MSG-03, MSG-10 | Type-9 packets with two validator indices, and with two signers. Capped at 3 sets. | required | no | REJECT (6) |
| `auth-repeat-root-in-packet` | MSG-10 | Type-9 packets that repeat a root inside one packet, repeat an accepted root, overflow the budget. | required | no | IGNORE |
| `postcons-block-only` | EPE-09 | Self-build slot: replaces the proposer post-consensus packet with the block entry only. | **must be off** | no | none |
| `postcons-3-entries` | EPE-09, MSG-03 | Every Gloas proposer slot: honest packet, then a second packet with three entries. | required | no | REJECT (unthrottled) |
| `postcons-mixed-index` | EPE-09, MSG-03 | Every Gloas proposer slot: honest packet, then a 2-entry packet whose entry 1 has index + 1. | required | no | REJECT (unthrottled) |
| `postcons-foreign-root` | EPE-09 | Self-build slot: replaces the packet with one whose envelope root is corrupted (first byte ^ 0xff). | **must be off** | no | none |
| `role7-prefork` | MSG-02 | Clones an outgoing validator-role partial into roles 7 and 8 at the last pre-fork slot. Throttled. | required | no | REJECT (8) |
| `vr-postfork` | MSG-02 | Keeps the validator-registration heartbeat running at Gloas slots, one validator per epoch. | required | no | REJECT (1/epoch, unthrottled) |
| `vr-prefork-slot-replay` | MSG-02 (gate 3) | From epoch fork+1: clones its role-7 partial as a role-4 partial stamped with the pre-fork slot. | required | no | IGNORE |
| `proposer-round-3` | TIM-08 | After a proposer instance's round-2 expiry, broadcasts `RoundChange(3)`. | required | no | IGNORE |

Common honest-side wrappers, so the reasons below can be grepped bare:

- **Value-check values (§4.1, §4.3):** the honest drop is Debug `could not handle message,
  dropping message` (`protocol/v2/ssv/validator/committee_queue.go:276` for committee,
  `queue_validator.go:265` for proposer) with `error` = `proposal fullData invalid: <reason>`
  (`protocol/v2/qbft/instance/proposal.go:138`) or `proposal invalid: <reason>` (`proposal.go:84`).
  Then a round change and a decide under the round-2 leader.
- **Message-validation values:** Debug `rejecting invalid message` / `ignoring invalid message`
  (`message/validation/errors.go:264`/`:266`) with the error text, rendered `<text>, got <g>, want
  <w>` where the rule sets them.

### 4.1 §2 — the committee vote

**Faulted-node negative check (all four value-check vote faults):**
`input data invalid: …` (`protocol/v2/ssv/runner/runner.go:767`) must be **0** on op5. If it
appears, op5 refused its own faulted start value in `decide` and nothing reached the wire — the
seam is broken. **Decided index:** read Debug `signed attestation data` (`committee.go:515`, field
`attestation_data.index`), not `built gloas attestation vote` (`committee.go:1215`), which is each
operator's own beacon-node vote logged before the fault and before the decision.

**`vote-112b` (ATT-02).** Evidence: `🧪 qa fault injected` every committee duty. Honest oracle:
`failed decoding gloas beacon vote` (`protocol/v2/ssv/value_check.go:118`); the full expected
`error` is `proposal fullData invalid: failed decoding gloas beacon vote: unexpected end of SSZ:
not enough data for fixed fields (have 112, needed 120)`. Round change, round-2 decide. **§6.6
still says `failed to decode gloas beacon vote`; that wording exists only in the exporter observer
(`committee_observer.go`, Debug, `… from proposal`) and is not on the rejection path — grep the
`decoding` string.** Permissive own check, duty-scoped (§5).

**`vote-index-2` (ATT-03).** Evidence: `attestation_data_index=2`. Honest oracle:
`attestation data index 2 must be 0 or 1` (spec `types/consensus_data.go:190`, reached through
`bv.Validate()` at `value_check.go:121`). ATT-03 step 8 greps `attestation data index <n> must be
0 or 1`; scout search is literal, so grep `index 2 must be 0 or 1`. The same error code
(`GloasBeaconVoteInvalidIndexErrorCode`) also carries the ATT-10 same-slot string — attribute a drop
by its text, not its code. Permissive own check, duty-scoped (§5).

**`vote-index-flip` (FLT-05).** Evidence: `honest_index`, `attestation_data_index`, `block_root`,
`same_slot_view` (`committee.go:1310-1322`). `same_slot_view=true` is leg A, `false` leg B.
- Leg A (same-slot head — every slot on ssv-mini): honest oracle
  `gloas attestation data index 1 for a same-slot block` (`value_check.go:132`), round change,
  round-2 decide. Only slots where op5 flipped 0→1 give it; a 1→0 flip is never rejected.
- Leg B (older head): no rejection — the index is trusted from the leader. "The wrong valid index
  is accepted (by design); record the on-chain result." Not observable on ssv-mini.
- Own check: op5 keeps an honest checker **without** the same-slot view (`DropSameSlotView`), not a
  permissive one. It still rejects out-of-range indices, slashable votes and epoch mismatches.

**`vote-index-1-same-slot` (ATT-10).** A separate registry value, not an alias: it forces index 1
on the root of op5's own same-slot view (`committee.go:1324-1345`). Evidence, one line every slot:
`applied=true` with `honest_index`, `attestation_data_index=1`, `block_root`, `root_substituted`;
or `applied=false, reason="no same-slot view"` (value and check unchanged). Honest oracle:
`gloas attestation data index 1 for a same-slot block` (`value_check.go:132`), round change,
round-2 decide. **Leg-C trap:** at a height where op5 leads round r ≥ 2 after an unrelated round-1
failure, op5 re-proposes the faulted start value, and the honest side logs the same string with
`qbft_round=r`. That is a fresh proposal, not a re-rejection; exclude those heights. On ssv-mini
this value is equivalent to `vote-index-flip` leg A.

**`double-vote-index` (ATT-04).** See §7.

### 4.2 §3 — PTC on the wire

**`ptc-qbft` (PTC-07, MSG-03).** A round-1 proposal with no FullData under op5's role-7
`MessageID` (`qa/faultnet/plan.go` `forgeConsensusMsg`). Honest oracle: REJECT
`unexpected consensus message for this role, got PTC_ATTESTER_RUNNER`
(`message/validation/errors.go:107`, set at `consensus_validation.go:164-169`). Sender-side
(modifier off): `own outbound message rejected by local validation` with the same text. Not
throttled — see §14.2.

**`two-entries` (MSG-03).** Honest oracle, per honest node per PTC duty: REJECT `too many
signatures in a partial-signature message, got 2, want 1` (`errors.go:134`, raised
`message/validation/partial_validation.go:139-146`). `got more partial signature messages of a
certain type than allowed` (`errors.go:90`) must be **0** — if it fires, the fault is malformed.
See §9 for why the honest copy is now sent too. Not throttled — see §14.2. (PTC-07 also uses this
value; the registry string is still `MSG-03`.)

**`ptc-3-per-epoch` (MSG-07).** See §8. Honest oracle: each forged copy (S+1, S+2) gets IGNORE
`no duty for this epoch` (`errors.go:85`, returned at `common_checks.go:241`). Sender-side
(modifier off): `own outbound message ignored by local validation (unexpected)` with the same
text.

### 4.3 §4 — the block and its value-check pins

Site: `protocol/v2/ssv/runner/proposer.go` `gloasProposalInput`, one call to `applyProposalFault`
(`proposer_qa_fault.go`) just before `Encode()` (`proposer.go:354-362`); `block-wrong-version`
keeps its own block (`proposer.go:380-385`). The `🧪` line therefore comes **before** `🧊 got gloas
beacon block proposal`. Every fired line carries `honest_self_build`. No §4 value changes the bid,
so `self_build` in the `🧊` line is always the beacon node's. All §4 values fire only on op5's
proposer duties.

| Value | Evidence fields | Honest oracle (verbatim) | Own check |
|---|---|---|---|
| `block-wrong-version` | `stamped_version=fulu` | `value version does not match slot fork` (`value_check.go:367`, pin 2) | permissive, runner lifetime |
| `block-wrong-slot` | `honest_block_slot`, `stamped_block_slot`, `honest_self_build` | `gloas block slot does not match duty slot` (`value_check.go:378`, pin 3) — not pin 1, not pin 4 | permissive, runner lifetime |
| `block-wrong-proposer-index` | `honest_proposer_index`, `stamped_proposer_index`, `honest_self_build` | `gloas block proposer index does not match duty validator index` (`value_check.go:384`, pin 4) — not pin 3 | permissive, runner lifetime |
| `payload-root-on-external` | fired: `honest_self_build=false`, `synthetic_external=false`, `builder_index`, `stamped_payload_root`; else not-applicable | `gloas payload_root presence does not match self-build bid` (`value_check.go:390`, pin 5) — BLD rig only | permissive, only once it fires |
| `payload-root-zero-selfbuild` | `honest_self_build=true`, `real_payload_root`, `stamped_payload_root` (zero) | `gloas payload_root presence does not match self-build bid` (`value_check.go:390`, pin 5), then `✅ published execution payload envelope` (`proposer.go:856`) under the round-2 leader | permissive, runner lifetime |
| `payload-root-fabricated` | `honest_self_build=true`, `real_payload_root`, `stamped_payload_root` | none — see below | **honest (no swap)** |

After each pin rejection: round change and a round-2 decide; read the decided build source from
Debug `decided gloas block build source` (`proposer.go:793`). Pin 5's string is **shared** by
`payload-root-on-external` and `payload-root-zero-selfbuild`; attribute the direction from op5's
`honest_self_build` field.

**`payload-root-on-external` — NOT EXECUTABLE until a BLD rig exists (D1).** It never forges a bid;
it stamps only a natural external bid. ssv-mini has no external builder, so every proposer slot is
self-build and the value logs `🧪 qa fault not applicable`, `reason="self-build slot: no external
bid to stamp"`, every such slot. That line proves the rig is alive; it is not an injection. The
external half of PRO-10 and EPE-04 stays `info` until P0.5 provides a builder rig.

**`payload-root-fabricated` (EPE-04 leg b, FLT-06 leg A) — the oracle is an absence plus a Debug
line, not a Warn.** The value passes all five pins everywhere, so it is decided in round 1 when op5
leads, and every operator signs an envelope nobody holds the payload for. Expected (pinned by
`TestProposerRunnerGloasFabricatedPayloadRootDoesNotPublish`):
- 0 pin strings; decided in round 1;
- `✅ successfully submitted block proposal` (`proposer.go:657`);
- **0** `✅ published execution payload envelope` on every node;
- **0** Warn `could not match the produced envelope to the decided value, not publishing it`
  (`proposer.go:881`) — that Warn needs an error, which this value never produces;
- Debug `envelope signature reconstructed; this operator did not build the envelope, not
  publishing` (`proposer.go:833`) on every node, op5 included — **needs Debug logs**;
- metric `ssv.runner.envelope.build_match` (`protocol/v2/ssv/runner/observability.go:136`) with
  `envelope_build_match="other"` +1 per operator, `"self"` +0;
- chain: the block is canonical, and the next slot shows `payload_present=false`.
EPE-04 and FLT-06 as written (the Warn oracle) would record a FAIL for a correct node — see the
errata.

### 4.4 §5 — proposer preferences and the request-auth overlay

Every `qa/faultnet` value here needs the modifier. Honest-side strings come from
`validateDistinctRootBudget` (`message/validation/partial_validation.go:391-415`):
`<label>, <n> distinct root(s) seen and <m> new` (`:411`) and `<label>, no new signing root`
(`:406`), label `proposer-preferences` or `request-auth`. All are IGNORE.

**`prefs-conflict` (PRF-07, FLT-07).** Runner hook `applyPreferenceFault`
(`protocol/v2/ssv/runner/proposer_preferences.go:601`, `faults.Fired` at `:636`): `fee[0] ^= 0xff`,
so op5 holds one standing root per slot; the other six partials still reach quorum. Oracle:
"the first-seen preference stays pinned" — a CL-side observation, no node string. Cluster-side:
`✔️ successfully submitted proposer preferences` (`proposer_preferences.go:491`). No budget line
(each signer has its own budget).

**`prefs-34-apart` (MSG-06) — redesigned, now a real probe.** The runner fee-recipient hook is
gone; the value lives in `qa/faultnet/plan.go` `prefs34Apart`. It fires only when the honest
emission for P is in the epoch before epoch(P) (the steady emission hold) and `P mod 32 ≤ 29`.
Rows (labels): the honest root plus 3 more at P, 100 ms apart (`p` ×3); a root at P+34 at slot 1
of epoch(P) (`p+34`); a 5th root at P at slot 2 (`p-5th`); a byte-distinct repeat of the P+34 root
at slot 3 (`p+34-repeat`). Honest oracle, per honest node per probed P:
- `slot=P`: exactly 1 `proposer-preferences, 4 distinct root(s) seen and 1 new`;
- `slot=P+34`: exactly 1 `proposer-preferences, no new signing root` (positive proof the P+34 row
  was recorded), 0 `message was sent before slot starts`, 0 `no duty for this epoch`;
- `proposer-preferences, <0..3> distinct root(s) seen`: 0.
A 34-slot ring (the regression MSG-06 targets) would evict P, and the 5th root would be accepted
with no line.

**`prefs-5-roots` (MSG-05, FLT-07).** Honest root, 4 clones 100/200/300/400 ms later, then 2 s
later a byte-distinct repeat of the honest root (`label=repeat`). Oracle, per honest node per slot:
1 `proposer-preferences, 4 distinct root(s) seen and 1 new` (clauses 1-2) and 1
`proposer-preferences, no new signing root` (clause 3 — the same-peer repeat, a REJECT before
`635f697b7`). No REJECT. MSG-05's "five gas-limit values" item cannot be produced (roots are
flipped directly).

**`prefs-early` (MSG-04).** Extra copy stamped 65 slots ahead of the emission slot. The earliness
rule is now an epoch bound (a preference for epoch E+2 is expected from the start of E+1), so 65
slots always crosses it; at the steady emission slot (j = 15) the copy is ≈ 17 slots early.
Oracle: `message was sent before slot starts, got early by ≈ 3m24s − δ`. Only a first tick after a
restart emits at another j: early by (32 − j)·12 s − δ, or 396 s − δ at j = 31. See §14.4.

**`prefs-early-epoch` (MSG-04, decision D2).** Probes the bound at T = first slot of
epoch(now)+2. Rows: `probe` (sent in slot 31 of the emission epoch), `control` (slot 0 of the next
epoch, exactly at the bound), `control-proof` (control root repeated 2 s later). The honest
message is still sent. Oracle, per honest node per set: 1 `message was sent before slot starts,
got early by ≈ 12s − δ` for `slot=T`; the control accepted with no line; 1 `proposer-preferences,
no new signing root` for `slot=T` (proof the control was recorded). Under the old 64-slot rule the
probe would be accepted: 0 early lines = FAIL. It fires on every type-8 emission, so a validator
with several proposals next epoch produces several sets for the same T — count per set.

**`prefs-late` (MSG-04).** Extra copy with a second distinct root (`SigningRoot[0] ^= 0x01`), sent
to arrive at P+3. Oracle: `current time is above duty's start …` (`errors.go:76`; the "+34/+3"
wording is stale text) followed by `, got late by <d>` (`common_checks.go:39`), d ≈ 9 s + x. **The
send delay is 17-48 slots in steady state** — preferences are emitted for every upcoming proposal
slot in the lookahead — so keep the value active past `P + 3` or you will see nothing; silence
before then is not a result. The distinct root is load-bearing (errata point 4).

**`prefs-replay` (FLT-11).** See §6.

**`auth-no-builders` (MSG-10).** Config fault: `applyBuilderFault` (`cli/operator/node.go:227`)
installs one synthetic entry (`http://qa-fault-builder.invalid`, `AuthData 0x0102030405060708`), so
op5 sends a 1-entry type-9 packet per proposal slot. Evidence: the single boot `🧪` line with
`builder_url` (§3). Honest oracle: `no builders configured`
(`protocol/v2/ssv/runner/proposer_preferences_request_auth.go:139`), on **two** lines — count both:
- Debug `could not handle message, dropping message` (`queue_validator.go:265`) with that `error`,
  when the receiver's §5 sub-runner has already started;
- Debug `skipped stashed proposer-preferences partial on replay`
  (`proposer_preferences.go:160`) with the same error, when the packet arrived before the
  receiver's duty for P started (common: op5's type-9 goes out first at the shared emission tick).
Search the error text, not either log message. `skipping request-auth partials for roots not
frozen here` never fires on a no-builders receiver.

**The three new auth values.** All need the synthetic builder (the same boot line) and fire only on
a 1-entry honest type-9 packet (`qa/faultnet/plan_auth.go` `isRequestAuth`); with a real
multi-builder config they fall back to identity and log nothing. **Run them on a local-signer
operator:** a remote-signing operator has its builder entries stripped after `applyBuilderFault`
(`node.go:281-288`, Warn `Builders configured with a remote signer: …`), so the boot `🧪` line
appears but no type-9 packet is ever sent.

| Value | Rows (`label`) | Honest oracle, per honest node per set | Cap |
|---|---|---|---|
| `auth-9-entries` | `9-entries` now; `8-root` {r0, a1..a7} +1 s; `9th-root` {a8} +2 s | 1 REJECT `too many signatures in a partial-signature message, got 9, want 8`; 1 `request-auth, 8 distinct root(s) seen and 1 new`; `request-auth, <0..7> distinct root(s) seen` = 0; `no builders configured` = 2 (rows 0 and 2, across both lines); `proposer-preferences, <n> distinct root(s) seen` = 0 | 3 sets |
| `auth-mixed-signer` | `mixed-index` {index vi, vi+1} +100 ms; `mixed-signer` {signer s, s+1} +200 ms | 1 REJECT `validator index differs across partial signatures, got <vi+1>, want <vi>` (`errors.go:116`); 1 REJECT `signer is not expected, got <s>, want <s+1>` (`errors.go:115`); `no builders configured` = 1 (row 0) | 3 sets |
| `auth-repeat-root-in-packet` | `5-roots` {a1..a5} +1 s; `repeat-in-packet` {a6, a6, a7} +2 s; `no-new-root` {a6, a7} +3 s; `over-budget` {a8, a9, a10} +4 s | 1 `request-auth, no new signing root`; 1 `request-auth, 8 distinct root(s) seen and 3 new`; `request-auth, 6 distinct root(s) seen and 3 new` = 0 (a double-counting regression would log it for row 2); `no builders configured` = 3 (rows 0-2) | none (IGNORE only) |

Sender-side (modifier off): `own outbound message rejected by local validation` for the REJECT
rows; `own outbound message ignored by local validation (unexpected)` for the budget rows.

### 4.5 §6 — the proposer post-consensus packet (`postcons-*`)

Site: one hook in `ProposerRunner.ProcessConsensus` (`protocol/v2/ssv/runner/proposer.go:545-555`),
logic in `proposer_postcons_qa_fault.go`. At a Gloas slot the honest packet is `{block}` on an
external-bid slot and `{block, envelope}` on a self-build slot. Pre-Gloas: no injection, no `🧪`
line. Not leader-gated: every operator sends post-consensus.

**Replace vs add.** `postcons-block-only` and `postcons-foreign-root` REPLACE the honest packet
(a second packet from the same peer would hit the per-signer post-consensus limit,
`partial_validation.go:363-368`, the wrong rule and a REJECT). `postcons-3-entries` and
`postcons-mixed-index` send the honest packet FIRST and the forged one SECOND; the forged one fails
structural validation before any signer state is touched, so op5's honest share still counts.

**Evidence** (Warn, op5): `🧪 qa fault injected` with `slot`, `validator_index`, `self_build`,
`honest_entries`, `replaces_honest`, `packets`, `entries`, plus: block-only `block_root`,
`omitted_envelope_root`; foreign-root `block_root`, `honest_envelope_root`, `foreign_root`;
mixed-index `forged_index`. Block-only and foreign-root log `🧪 qa fault not applicable` on every
external-bid slot (§3).

**Honest oracles** (exclude op5 from every count):

- **Leg A — `postcons-block-only`.** Wire accepts (entry cap 2); the runner accepts silently, since
  the envelope root is `Optional: true` (`proposer.go:1037`, check at `runner_validations.go:317-321`).
  Positive evidence: Debug `📬 got post-consensus message` (`runner.go:670`) with `signer=<op5>` and
  `validators=[<idx>]` — **one** element, where an honest self-build packet logs two. `🎯 got
  post-consensus quorum` lists only the first quorum's signers, so op5's absence there proves
  nothing. The cluster still publishes: `✅ published execution payload envelope`
  (`proposer.go:856`). Subject to the arrival race (§14.5). Not a model of a natural node path —
  see the SGN-02 note below.
- **Leg B — `postcons-3-entries` (MSG-03).** REJECT `too many signatures in a partial-signature
  message, got 3, want 2` (`errors.go:134`, raised `partial_validation.go:140`), fields
  `runner_role=PROPOSER`, `operator_ids=[<op5>]`. Count every Gloas proposer slot of op5, not only
  self-build. op5's own runner logs `wrong expected roots count` (`runner_validations.go:289`) —
  op5 only. Not subject to the race.
- **Leg C — `postcons-mixed-index` (MSG-03).** REJECT `validator index differs across partial
  signatures, got <idx+1>, want <idx>` (`errors.go:116`, raised `partial_validation.go:160-169`),
  same fields. op5's own runner logs `unknown validator index` (`runner_signatures.go:133`) —
  op5 only. Not subject to the race.
- **Leg D — `postcons-foreign-root`.** No wire reject (confirm `rejecting invalid message` = 0 for
  op5's PROPOSER packets). Runner reject: Debug `could not handle message, dropping message`
  (`queue_validator.go:265`) with `error` = `process post-consensus message: failed processing post
  consensus message: invalid post-consensus message: unexpected signing root`
  (`runner_validations.go:314`). Search the bare `unexpected signing root`. The whole packet is
  dropped, so op5's block share is lost too. Subject to the arrival race (§14.5).

**Run order: A, D, B, C** (block-only, foreign-root, 3-entries, mixed-index). A and D cost no
score; B and C are unthrottled REJECTs (§14.2). Receivers see only direct mesh peers' messages
(REJECTed messages are not forwarded), so record sender and receiver counts.

**Node-side finding, not a menu issue (SGN-02).** If envelope signing fails, the proposer returns
`failed signing blinded envelope` (`proposer.go:539`) and sends **no** post-consensus packet — so a
remote signer that cannot sign `DomainBeaconBuilder` (operator 6) loses its block share too.
Candidate upstream issue.

### 4.6 §7 — the fork gates and the proposer round cap

**`role7-prefork` (MSG-02 gate 1).** Clones an outgoing **validator-role** partial (proposer,
preferences, PTC, aggregator …) into roles 7 and 8 at the last pre-fork slot
(`qa/faultnet/plan.go` `forgeGloasRoles`; decision D3: no third clone). Committee-role partials are
skipped — their executor is a committee ID and the clone could not be published. **Throttled:** at
most one forged set per epoch and 4 sets per boot (8 REJECTs), then the exhausted line (§3); all 4
sets need ≥ 4 epochs (≈ 26 min). Evidence: fired line with `payload_slot=<fork·32−1>`. Honest
oracle, ≤ 4 of each per honest node: REJECT `invalid role, got PTC_ATTESTER_RUNNER (7) @ slot
<fork·32−1>` and `invalid role, got PROPOSER_PREFERENCES_RUNNER (8) @ slot <fork·32−1>`
(`partial_validation.go:91-94`, `ErrInvalidRole` `errors.go:106`).

**`vr-postfork` (MSG-02 gate 2).** The fault suppresses all four Gloas VR gates: (1) the
event-driven enqueue gate and (2) the periodic scheduling gate in
`operator/duties/validator_registration.go` (`gloasVRDeprecated`, `:226`), (3)
`ValidatorRegistrationRunner.StartNewDuty` and (4) `VRSubmitter.start` in
`protocol/v2/ssv/runner/validator_registration.go` (`vrDeprecatedAtSlot`, `:93`, `:409`). Gate 4
is downstream of the wire and is suppressed for consistency only. **Cadence (D4):** at Gloas epochs
only, the node schedules ONE validator — its lowest-index attesting self share — once per epoch, at
`slot % 32 == validatorIndex % 32` (`vrPostForkCadenceShare`, `:243`); pre-fork epochs keep the
stock 320-slot cadence. The first injection can be up to 31 slots after the window opens.
Evidence chain on op5: Debug `validator registration duty sent` → Debug `signing and broadcasting
validator registration partial sig` → `🧪 qa fault injected` (`slot`), one chain per epoch.
Sender-side (modifier off): `own outbound message rejected by local validation` with `invalid role,
got VALIDATOR_REGISTRATION_RUNNER (4) @ slot <s>`. Honest oracle (modifier on): REJECT `invalid
role, got VALIDATOR_REGISTRATION_RUNNER (4) @ slot <s ≥ fork slot>` (`partial_validation.go:91-94`,
`validRoleAtSlot` `signed_ssv_message.go:162-164`). Honest-silence check: 0 `submitted validator
registrations` (`beacon/goclient/validator.go:52`) at Gloas slots on every node.

**`vr-prefork-slot-replay` (MSG-02 gate 3).** From wall-clock epoch fork+1 only, clones op5's
role-7 partial as a role-4 `ValidatorRegistrationPartialSig` stamped `payload_slot = fork·32−1`,
one entry (`plan.go` `forgePreForkRegistration`). The fork epoch itself logs nothing (self-guard).
Throttled to one clone per epoch, no cap (IGNORE). Evidence: `role=VALIDATOR_REGISTRATION_RUNNER`,
`payload_slot=<fork·32−1>`. Honest oracle, 1 per honest node per epoch: IGNORE `validator
registrations ended with the Gloas fork` (`errors.go:93`, raised `partial_validation.go:202-204`),
no got/want suffix. Sender-side (modifier off): `own outbound message ignored by local validation
(unexpected)`.

**`proposer-round-3` (TIM-08 ignore leg).** Hook `protocol/v2/qbft/instance/timeout.go`
`qaProposerRound3` (`:89`), called on the cutoff branch of `UponRoundTimeout`. On a PROPOSER
instance whose round-2 timer expires (the bump lands on the cutoff, round 3; ssv#3041,
`roundtimer.MaxRound(RoleProposer) = 2`), op5 builds `RoundChange(3)` with `CreateRoundChange`
(valid signature, current justifications) and sends it straight to the network. Its own instance
still gives up at the cutoff. At most once per instance; other roles untouched. **Requires the
modifier:** otherwise op5's own gate IGNOREs round 3 as a routine self-ignore (Debug `own outbound
message ignored by local validation`, `round is too high for this role …`) and nothing leaves.
Evidence on op5: Debug `📢 broadcasting round change message (this round timed out)` with
`qbft_new_round=3`, then `🧪 qa fault injected` (`slot`, `qbft_round=3`, `role=PROPOSER`). Honest
oracle: IGNORE `round is too high for this role, got 3 (PROPOSER role), want 2 (PROPOSER role)`
(`errors.go:82`, formatted `consensus_validation.go:178-182`); `🔄 got round change` with
`qbft_round=3` = 0 on the honest nodes. **Triggering:** the proposer instance must reach its round-2
expiry, so TIM-08 stops the round-1 and round-2 leaders; round 2 expires ≈ 4.0 s after instance
start on this tree. **Window:** its own, last (§14.2, D6).

## 5. Standing caveat — the weakened own value check

`decide` checks the local start value on **every** operator, leader or not
(`protocol/v2/ssv/runner/runner.go:767`), so each value-check fault must weaken op5's own check or
its value never leaves the node. The override is one of three states (`qa/faults/valuecheck.go`
`ValueCheckOverride`):

- **Permissive** (accepts anything): `vote-112b`, `vote-index-2`, `block-wrong-version`,
  `block-wrong-slot`, `block-wrong-proposer-index`, `payload-root-zero-selfbuild`,
  `payload-root-on-external` (only once it has fired).
- **Honest without the same-slot view**: `vote-index-flip`, `vote-index-1-same-slot` (when
  `applied=true`).
- **Honest, unchanged**: `payload-root-fabricated` (it passes every pin by design).

How long the swap lasts:

- **Committee runner** (the four vote values): the value check is rebuilt every duty in
  `executeDuty` (`committee.go:1220-1228`), so the swap is **duty-scoped**.
- **Proposer runner** (the §4 values): the checker is set once, at construction, and the swap
  assigns `r.ValCheck` (`proposer.go:356-358`, `:382`). Once it happens it **stays for that
  runner's lifetime** — until the node restarts.

No visible effect on a fault run — `FAULT` is fixed for the life of the process, so nobody sends
op5 a *different* invalid proposal to accept — but it is why op5's receive path is never evidence
for these values.

## 6. Standing caveat — `prefs-replay`

`prefs-replay` (FLT-11) replays one valid preference: the honest root, re-signed, with a perturbed
`PartialSignature` (a 4-byte per-repeat counter, `perturbForRepeat`, so gossipsub dedup does not
swallow the series).

- **Expected outcome, inverted from v1:** every copy is IGNOREd by the distinct-root budget
  (`proposer-preferences, no new signing root`) **before** signature verification and before slot
  time (`partial_validation.go:239` vs `:251`), so even copies after P+2 are classified as dedup,
  never `got late by`. IGNORE carries no `p4` penalty: the flooder pays nothing in score, and
  nothing is recorded. For role 8 during the flood, `message was sent before slot starts` and
  `got late by` must be 0. FLT-11's "fails signature verification after the budget check" is wrong
  (the outer signature is valid; the inner partial is never verified).
- **The series is ~13 minutes long** (15,841 sends, 50 ms apart) and **cannot be cancelled short
  of restarting the node**.
- **Only the first send of a series is logged**, plus the closing `🧪 qa fault: repeated send
  series finished` (`sent`, `planned`). Intermediate sends are silent so the log buffer is not
  swamped.
- **20 messages/second is a floor, not a bound.** Several lookahead preference runners can be in
  flight at once and their series overlap.
- **Validation memory does not stay flat** (errata #138, ssv#3036): with the modifier on, honest
  peers absorbed ~100k validations each and grew 10-37 MiB, bounded and dose-dependent on mesh
  position. Record the growth; do not write FLT-11's "stays flat" clause as PASS.

## 7. `double-vote-index` (ATT-04) — read the oracle on the faulted node

The one value with **no wire component**. It signs index 0, then immediately asks the local signer
for index 1 on the same slot (`protocol/v2/ssv/runner/committee.go:526-547`,
`signAttesterDuty`). **Read the oracle on op5**, not on the honest operators. The second attempt
fails with:

```
could not sign beacon object: slashable attestation (HighestAttestationVote), not signing
```

Wrapper `protocol/v2/ssv/runner/runner_signatures.go:60`. Local-signer path: eth2-key-manager
`v1.5.7-0.20260901124029-39a18877e131` `signer/sign_attestation.go:39`; remote path:
`ssvsigner/ekm/slashing_protector.go:91`, same text. Search the substring `slashable attestation`.
The refusal is in the `second_sign_err` field of the `🧪 qa fault injected` line (with
`first_index`, `second_index`), so query
`--search "🧪 qa fault injected" --fields time,slot,first_index,second_index,second_sign_err` —
not `--fields …,error`. A `⚠️ duty failed` line never appears: the second signature is discarded on
purpose. The protection predicate is epoch-only, so this value cannot show index-aware protection.

## 8. `ptc-3-per-epoch` (MSG-07) — the assignment gate, not the count rule

`ptc-3-per-epoch` does **not** reach the per-epoch duty-count rule its name suggests (errata #120
stands; decision D5 accepts it). In `validatePartialSigMessagesByDutyLogic`,
`validateBeaconDuty` (`message/validation/partial_validation.go:223`) runs **before**
`validateDutyCount` (`:251`). For role 7 its PTC branch (`common_checks.go:237-243`) refuses both
forged copies with `ErrNoDuty` — there is no genuine duty at S+1 or S+2. The limit
(`dutyLimit` role 7 → `return 2, true`, `common_checks.go:138-142`) is never reached by any sending
fault.

**Expected on this base:** each forged copy gets IGNORE `no duty for this epoch` (`errors.go:85`) —
in validation, not in the runner. The 2026-09-09 observation `no duty assigned`
(`protocol/v2/ssv/runner/error.go:22`, errata #136) is historical and unexplained on this base;
record which string fires. PTC duties are now pre-fetched for the current and next epoch
(`operator/duties/ptc_attestation.go:229-260`), so the card's "restart op4 just before the fault
epoch" variant is close to unreachable.

**MSG-07's count clause is PARTIAL by design (D5).** No burst mode was built. What this value
proves is that the honest side enforces the one-PTC-duty-per-epoch *assignment*; §6.6's "the third
PTC partial per epoch is ignored" is right on class (IGNORE) and wrong on mechanism and on count —
the second **and** third copies are ignored, by the assignment gate.

## 9. `two-entries` (MSG-03) — why the honest copy is sent too

The entry-count rule moved into the structural pass: `maxValidatorRoleSignatures`
(`partial_validation.go:318-327`) returns 1 for role 7 and is checked at `:139-146`, before any duty
logic and before signer state. The honest partial therefore cannot pre-empt it, so the value now
sends the honest role-7 partial **and** the 2-entry clone (`plan.go` `dupEntry`). This restores
op5's PTC contribution; at size 7 f stays 2 with operators 4 and 6 degraded. The same-index rule
(`:160-169`) passes because the duplicate entry keeps the index.

## 10. Building the instrumented image

```bash
make build   # -> ./bin/ssvnode
```

For ssv-mini, the image needs a **distinct tag** so it can be selected per operator (see §12) —
build it separately from the stock image so `FAULT=none` operators keep running the plain image:

```bash
docker build -t node/ssv-fault .
```

Do not build from a git worktree (its `.git` is a file outside the build context); use a
`git clone --local` copy.

## 11. Full-menu boot smoke (build + startup check)

Verify every value is accepted and named in the boot banner, and every retired value aborts,
before touching an enclave. Enumerate the menu from the code so this loop cannot drift:

```bash
go build ./...
go test ./qa/...

for f in $(go run ./qa/faults/cmd/list); do
  echo "== $f"
  FAULT=$f timeout 20 ./bin/ssvnode start-node --config /path/to/config.yaml 2>&1 \
    | grep -m1 "QA FAULT INSTRUMENTATION ACTIVE" || echo "NO BANNER for $f"
done

for f in envelope-foreign-root envelope-builder-index envelope-round-3; do
  FAULT=$f timeout 20 ./bin/ssvnode start-node --config /path/to/config.yaml 2>&1 \
    | grep -m1 'unknown FAULT' || echo "RETIRED VALUE BOOTED: $f"
done
```

Expected: **33** banners, no `NO BANNER` line, and three `unknown FAULT` lines, no `RETIRED VALUE
BOOTED`.

**Status: done 2026-09-28 on a live enclave**, by the equivalent `make fault` path (a standalone
`start-node` needs the beacon and EL clients up before `newNode` prints the banner, and a clone of an
enclave operator must never join its network with that operator's key). ssv-mini `params-gloas-fault.yaml`
(size 4), image `node/ssv-fault` built from this tree, operator 3:

- `make test-fault-switch OP=3 SSV_REPO=../ssv-m3-v2` → `PASS: fault switching archives, applies and verifies`.
- `make fault FAULT=<v> OP=3` over all 33 values from `go run ./qa/faults/cmd/list`: 33/33 exit 0, the boot
  banner's `qa_fault` equals the value every time, 0 FATAL/PANIC; `make fault-off` restores
  `no fault active`; operators 0-2 had 0 restarts throughout.
- `make fault` refuses a retired value at its menu check, so the node-level abort was checked directly with
  `kurtosis service update localnet ssv-node-3 --env FAULT=envelope-round-3`: the container exits 1 with
  `FATAL could not start node … qa fault menu: unknown FAULT "envelope-round-3"`. `make fault-off` cannot
  restore an exited container (its archive step needs a running one); restore with
  `kurtosis service update localnet ssv-node-3 --env FAULT=none`.

This proves boot wiring only. No injection path has been exercised on a live enclave yet: that happens in
the first ladder of pass M3, per §14.2's order.

## 12. ssv-mini end-to-end smoke

Bring up the enclave with the instrumented image on one operator, `FAULT=two-entries` and
`FAULT_NO_LOCAL_VALIDATION=true`, Debug logging on the honest operators:

```bash
venv/bin/python scout.py --env mini logs query --service ssv-node --operators 5 --search "🧪 QA FAULT INSTRUMENTATION ACTIVE" --jsonl --fields time,qa_fault,no_local_validation
venv/bin/python scout.py --env mini logs query --service ssv-node --operators 5 --search "🧪 qa fault injected" --summary
venv/bin/python scout.py --env mini logs query --service ssv-node --operators 0,1,2,3,4,6 --search "too many signatures in a partial-signature message, got 2, want 1" --summary
```

Expected: the banner once (`no_local_validation=true`), the injection line once per PTC duty of
op5, and the REJECT on every honest operator in op5's mesh. If the honest side is silent while the
injection line is present, first check the modifier banner and the honest log level, then the
broadcast topic (§13).

**Status: outstanding** (T9, `make test-fault-switch` on a live enclave). Counting on `--env mini`
is lossy (client-side filtering after `--limit`, a ~92-minute buffer); read cumulative counts from
the archive `make fault` writes.

## 13. Closed-by-source-reading verification points — still confirm on the first live run

1. **The network decorator is on every broadcast path it must intercept — CLOSED.**
   `cli/operator/node.go:483` sets `valOpts.Network = faultnet.Wrap(...)`, the only `Network`
   handed to `validator.NewController`; every runner and QBFT instance shares it via
   `BroadcastAtSlot`. That includes the type-9 request-auth packet (`runner.go:502`) and
   `proposer-round-3`, which calls the instance network directly because `Instance.Broadcast`
   refuses an irrelevant instance. The startup type assertions (`PeersIndexProvider`,
   `HostProvider`, `HealthChecker`) run on the raw pre-`Wrap` network, so they still succeed.
   Confirm on the first live run that `🧪 qa fault injected` lines carry the roles the value
   targets, and — for the `qa/faultnet` values with the modifier on — that their count tracks
   `📤 broadcast message to topic` for op5. Exceptions to that count: `prefs-replay` (compare the
   series line's `sent` field, §6), the four `auth-*` values' single boot line, and the throttled
   values after their exhausted line.
2. **The synthetic builder URL cannot degrade block production — CLOSED.** An unresolved auth is
   omitted from the `produceBlockV4` body rather than sent malformed (see
   `protocol/v2/ssv/runner/observability.go`'s builder-telemetry comment), and
   `gloas.ResolveBuilderConfig` validates only URL scheme and host, never reaches the URL. Holds for
   all four `auth-*` values. Confirm on the first live run that op5 still proposes normally.

## 14. Run notes

### 14.1 The injection line is not a wire oracle for the leader-gated values

For `vote-112b`, `vote-index-2`, `vote-index-flip`, `vote-index-1-same-slot`,
`block-wrong-version`, `block-wrong-slot`, `block-wrong-proposer-index`,
`payload-root-on-external`, `payload-root-zero-selfbuild` and `payload-root-fabricated`,
`faults.Fired` runs on every duty of op5 — but the faulted value reaches the wire only when op5
proposes its own start value: round 1 when op5 leads, or a later round it leads whose round change
carries no prepared justification. That is roughly one duty in seven at size 7, and for the §4
values only op5's validators' proposer duties count. **"Injection line present, honest side
silent" is not evidence of acceptance on its own.** Before recording a verdict, confirm from the
QBFT logs that op5 led the round. `payload-root-fabricated` has an effect only when op5 leads round
1 and the value is decided in round 1.

### 14.2 Run ordering — gossip score is a shared, persistent budget

Gossipsub scoring keys on the libp2p peer ID (`NetworkPrivateKey`), unchanged across a `FAULT`
restart, and `RetainScore` keeps a disconnected peer's score for 100 epochs. A REJECT-heavy value
run early degrades op5's mesh for every value after it (errata #139: an unthrottled
`role7-prefork` got op5 graylisted in 13 minutes with the modifier on).

**Derivation** (`qa/faultnet/budget.go`, from `network/topics/params`):
- P4 per topic = TopicWeight · w4 · n², with w4 = graylistThreshold / (TopicWeight · 20²)
  (`topic_score.go:225`, `maxInvalidMessagesAllowed = 20`). TopicWeight cancels: **P4 = −40·n²**,
  where n = REJECTed messages this peer delivered on the topic, counted per direct peer (REJECTed
  messages are not forwarded).
- Positive score is capped at TopicScoreCap 32.72; thresholds gossip −4000, publish −8000,
  graylist −16000 (`peer_score.go:15-21`). So graylist is n = 20 on one topic and the gossip
  threshold is n ≈ 10. Any negative score prunes the peer from meshes.
- Decay 0.01^(1/100) ≈ **0.955 per epoch**: half-life ≈ 15 epochs (≈ 96 min).
- A rate limit alone fails: one 2-REJECT set per epoch converges to n = 44 and crosses graylist
  after ≈ 13 epochs. Hence a **total** cap per value: `maxRejectsPerRun = 8` (P4 ≈ −2560),
  assuming the worst case that every forged message lands on one topic (ssv-mini).

**Caps in code:** `role7-prefork` 4 sets × 2 = 8; `auth-9-entries` 3 sets × 1 = 3;
`auth-mixed-signer` 3 sets × 2 = 6; `vr-prefork-slot-replay` one per epoch, no cap (IGNORE).
**Not throttled** — bound them by hand, stopping the value once op5 has logged 8 injections:
`ptc-qbft`, `two-entries`, `postcons-3-entries`, `postcons-mixed-index`, `vr-postfork` (one per
epoch by cadence).

**Spacing between two REJECT values:** the carried counter must satisfy
**n_prev · 0.955^t + n_next ≤ 9** (t in epochs). Example: `role7-prefork` (8) then
`auth-mixed-signer` (6) needs t ≥ 22 epochs (≈ 2.3 h); `auth-9-entries` (3) then
`auth-mixed-signer` (6) needs t ≥ 0. Check `peer scores` (Debug, `p4_invalid_message_deliveries`)
on the honest nodes before each REJECT value. A silent honest side late in a run may be a
graylisted sender, not a clean pass.

**Order for the menu loop (§6.5 step 6):**
1. **`vr-postfork` immediately after the fork** (errata #137), for 2 epochs (≥ 2 injections under
   the D4 cadence, n ≤ 3). It is the one REJECT value that runs early, because its window is tied
   to the fork; the IGNORE block that follows gives it hours of decay.
2. **The no-score and IGNORE values**, in any order: the §2 vote values, `double-vote-index`, the
   §4 block and payload-root values, `prefs-conflict`, `auth-no-builders`, `postcons-block-only`,
   `postcons-foreign-root`, `ptc-3-per-epoch`, `prefs-5-roots`, `prefs-34-apart`, `prefs-early`,
   `prefs-early-epoch`, `prefs-late`, `prefs-replay`, `auth-repeat-root-in-packet`, and
   `vr-prefork-slot-replay` (only at wall-clock epoch ≥ fork+1; it self-guards in the fork epoch).
3. **The REJECT values, last**, with the spacing rule above, cheapest first: `auth-9-entries`,
   `auth-mixed-signer`, `postcons-3-entries`, `postcons-mixed-index`, `two-entries`, `ptc-qbft`,
   `role7-prefork`.
4. **The postcons legs run in the order A, D, B, C** (block-only and foreign-root in block 2,
   3-entries then mixed-index in block 3).
5. **`proposer-round-3` LAST, in its own window (D6)**, after operators 4 and 6 are restored, so at
   most three operators are degraded (op5 plus the stopped round-1 and round-2 leaders). Do not
   combine it with any other degradation.

### 14.3 `prefs-5-roots` now exercises clause 3

The delayed byte-distinct repeat of the honest root gives 1 `proposer-preferences, no new signing
root` per honest node per slot, so all three MSG-05 clauses are covered. FLT-07's "repeated root"
row becomes "1 per slot per honest node". (The v1 value never repeated a root.)

### 14.4 `prefs-early`'s margin is ≈ 17 slots, not one slot

Earliness is now bounded by the epoch (`messageEarliness`, `common_checks.go:31`), with a tolerance
of `clockErrorTolerance + earlyMessageMargin` = 50 ms + 1 s (`const.go`). A 65-slot copy at the
steady emission slot is ≈ 17 slots past it, so a one-off miss is not a boundary race any more —
investigate it. `prefs-early-epoch` is the value that probes the bound itself.

### 14.5 The post-consensus arrival race (`postcons` legs A and D)

Once an honest node holds both post-consensus quorums, `hasDutySucceeded()` is true and
`awaitingEnvelope()` false, so a later packet drops with `running duty already succeeded`
(`protocol/v2/ssv/runner/error.go:25`, returned `runner_validations.go:107`) **before** the root
checks. Leg D oracle: `unexpected signing root` + `running duty already succeeded` (both with op5 as
signer) = one per honest operator per self-build slot, and `unexpected signing root` ≥ 1 per
self-build slot across the cluster. Leg A's `📬` evidence is short by the same mechanism.

### 14.6 The fabricated-root oracle is a Debug line plus a metric, not a Warn

For `payload-root-fabricated` the evidence is the Debug `… did not build the envelope, not
publishing` line on every node and `envelope_build_match="other"`, with 0 envelopes published — not
the `could not match the produced envelope …` Warn. See §4.3.

### 14.7 `payload-root-on-external` stays `info` until a BLD rig exists

On ssv-mini it can only log `🧪 qa fault not applicable` (every self-build slot). Record the
not-applicable lines as rig evidence, and record PRO-10's and EPE-04's external half as `info` —
never as PASS or FAIL — until P0.5 provides a builder.

## Known limits and errata for the pass document

Discrepancies between `docs/qa-glamsterdam-test-plan-passes.md` §6.3/§6.6, the scenario cards and
the code, recorded here so they travel back to whoever maintains those documents. None is a defect
in this branch.

1. **ATT-02's §6.6 string is stale (errata #119/#134, still unapplied).** §6.6 says `failed to
   decode gloas beacon vote`; the rejection path emits `failed decoding gloas beacon vote`
   (`value_check.go:118`). The ATT-02 card is correct.
2. **ATT-03's card step 8 greps a placeholder.** `attestation data index <n> must be 0 or 1` never
   matches a literal search; grep `index 2 must be 0 or 1`. The ATT-03 PASS on `4d96af393` used the
   old v1 string and needs a re-run on this build (errata #229).
3. **MSG-07's count clause is PARTIAL (D5, errata #120).** No value reaches
   `ErrTooManyDutiesPerEpoch`; the stop is `ErrNoDuty` (§8). Record MSG-07 as PARTIAL for the
   count text.
4. **`prefs-34-apart` now DOES probe MSG-06.** The redesign (§4.4) supersedes errata #121, which
   said the v1 value could not. MSG-06's e2e half can now be claimed from this value.
5. **`prefs-late`'s oracle depends on the distinct signing root, not just the delay.** An exact
   duplicate of the honest message would land on `proposer-preferences, no new signing root`
   (IGNORE, budget) instead of the lateness rule. Do not "simplify" the value.
6. **Errata #137 named the wrong cause.** The ungated check in `VRSubmitter.start` is downstream of
   the wire (it is now gated anyway, `validator_registration.go:409`). The likely cause of v1's zero
   `vr-postfork` injections was the 320-slot registration cadence against a 13-minute window;
   decision D4's one-validator-per-epoch cadence fixes that. Correct #137 and the MSG-02 run note.
7. **EPE-04 / FLT-06 name the wrong fabricated-root oracle.** They expect the `could not match the
   produced envelope …` Warn; the correct oracle is the Debug `did not build the envelope` line plus
   the `build_match` metric and 0 published envelopes (§4.3, §14.6). As written they would record a
   FAIL for a correct node.
8. **FLT-11's expectation is inverted.** The replay is IGNOREd by the budget with no score cost
   (§6), and its Background's "fails signature verification" is wrong.
9. **PTC-07's two-entry oracle is stale.** `got more partial signature messages of a certain type
   than allowed` can no longer be the two-entry result; only `too many signatures in a
   partial-signature message` can (§9). Its citations (`errors.go`, `consensus_validation.go`)
   have moved.
10. **MSG-10 undercounts.** Count `no builders configured` across both the queue drop line and the
    stash-replay line (§4.4).
11. **MSG-02 still expects a third role clone.** Decision D3 dropped it; the value sends roles 7
    and 8 only. Edit MSG-02 steps 6 and 75 and the §6.3/§6.6 rows, and add gate 3
    (`vr-prefork-slot-replay`, `validator registrations ended with the Gloas fork`) to §6.6.
12. **TIM-08 does not say it needs `FAULT_NO_LOCAL_VALIDATION`.** `proposer-round-3` requires it
    (§4.6).
13. **EPE-09's field names and chain.** Use the evidence fields and the `unexpected signing root`
    chain in §4.5, and account for the `running duty already succeeded` race (§14.5).
14. **PRO-07 / PRO-10 / ATT-04 still use `kurtosis service update`.** Use `make fault … OP=5`.
