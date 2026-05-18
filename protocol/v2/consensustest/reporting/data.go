// Package reporting writes the consensustest comparison report data as a
// small JavaScript bootstrap file (`data.js`) consumed by the static UI
// in `stresstest-report/` (index.html + app.js + styles.css).
//
// Splitting data from rendering means iteration on the UI is just
// "edit app.js / styles.css → refresh browser" — no test rerun needed.
// Re-running the test (`make stresstest`) only regenerates
// data.js with fresh stats.
//
// The static UI files are checked into git at `stresstest-report/`;
// `data.js` is gitignored.
//
// Usage from a test:
//
//	sweepResults := []ct.SweepResult{ct.RunSweep(t, ct.DefaultSweeps(...)[0]), ...}
//	c := reporting.Comparison{
//	    Title: "OBFT vs QBFT", Description: "...",
//	    Sweeps: sweepResults,
//	    BaselineIterations: 100, UnstableIterations: 10,
//	    Wallclock: elapsed,
//	}
//	reporting.WriteReportData(c, "./stresstest-report")
package reporting

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
)

// Comparison bundles all data needed for one consolidated report.
// Each sweep with a single point renders as a detail section (summary
// matrix + four charts); each multi-point sweep renders as a trend
// section (three line charts with the swept axis on X). Selection is
// done in the JS app based on `len(points)`.
type Comparison struct {
	Title       string
	Description string
	Sweeps      []ct.SweepResult
	// BaselineIterations / UnstableIterations carry the per-group iteration
	// counts the test driver applied (Baseline-group scenarios at Baseline,
	// every other scenario at Unstable). Per-cell counts also live on
	// BatchCell.Iterations, which is what the UI reads for rendering.
	BaselineIterations int
	UnstableIterations int
	Wallclock          time.Duration
}

// Applicable reports whether the cell ran ≥ 1 iteration. n/a cells (the
// scenario doesn't translate to that protocol) carry Iterations=0.
func Applicable(c ct.BatchCell) bool { return c.Iterations > 0 }

// WriteReportData writes <dir>/data.js containing a `window.REPORT_DATA = {...}`
// assignment consumed by the static UI in <dir>. The companion files
// (index.html, app.js, styles.css) are checked into git at
// `stresstest-report/` and not touched by this function.
//
// If <dir>/data.js already exists with a parseable payload, the new
// run's points are MERGED into the existing data by Fields-tuple
// rather than overwriting. This lets multiple `make stresstest` runs
// at different (n, K) operating points compose into one report: each
// run contributes its (n, K) slice and the UI's pickers select across
// the matrix. Sweeps are matched by Name; within a sweep, points are
// matched by their Fields map (so a (n=4, K=4) point from run 1 and a
// (n=4, K=2) point from run 2 coexist; a re-run at (n=4, K=4)
// replaces its slice with the freshest data).
//
// If the existing data.js is unparseable or has incompatible shape
// (e.g. handwritten / from a much older format), the new run
// overwrites it cleanly.
func WriteReportData(c Comparison, dir string) error {
	if len(c.Sweeps) == 0 {
		return fmt.Errorf("reporting: WriteReportData: no sweeps")
	}
	// Sweep names become DOM IDs in the UI; duplicates would collide.
	seen := make(map[string]bool, len(c.Sweeps))
	for _, sw := range c.Sweeps {
		if seen[sw.Sweep.Name] {
			return fmt.Errorf("reporting: WriteReportData: duplicate sweep name %q", sw.Sweep.Name)
		}
		seen[sw.Sweep.Name] = true
	}

	payload := buildPayload(c)
	path := filepath.Join(dir, "data.js")
	// Merge with existing data.js if any — unparseable existing data is
	// treated as "no prior data" (overwritten cleanly).
	if existing, err := readReportData(path); err == nil && existing != nil {
		payload = mergePayloads(*existing, payload)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("reporting: marshal: %w", err)
	}
	out := append([]byte("window.REPORT_DATA = "), body...)
	out = append(out, ';', '\n')

	// Atomic write: write to a sibling temp file then rename onto the
	// real path. If the process is killed mid-write, the temp file is
	// partial / orphaned and the real data.js stays intact — without
	// this, os.WriteFile would open-O_TRUNC the real file first and a
	// kill before the write completes would leave it truncated, which
	// readReportData on the next run would treat as "no prior data"
	// and silently overwrite (losing every prior (n, K) slice). Rename
	// is atomic on POSIX, so any reader either sees the old file or
	// the fully-written new one.
	//
	// Per-process-unique temp suffix via os.CreateTemp so two parallel
	// `make stresstest` invocations writing into the same REPORT_DIR
	// don't race on a fixed "data.js.tmp" name and corrupt each
	// other's mid-write payloads. (The Fields-tuple merge would still
	// race at the read-then-write boundary, so concurrent runs into
	// the same dir aren't a supported workflow — but a single fixed
	// suffix would turn a soft "last write wins" race into a hard
	// "torn write" failure, which is worth avoiding cheaply.)
	tmpFile, err := os.CreateTemp(dir, "data.js.*.tmp")
	if err != nil {
		return fmt.Errorf("reporting: create tmp: %w", err)
	}
	tmp := tmpFile.Name()
	if _, err := tmpFile.Write(out); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("reporting: write tmp %s: %w", tmp, err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("reporting: close tmp %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		// Clean up the temp file on rename failure so we don't leave
		// half-written debris in REPORT_DIR.
		_ = os.Remove(tmp)
		return fmt.Errorf("reporting: rename %s -> %s: %w", tmp, path, err)
	}
	return nil
}

// readReportData parses an existing data.js (the `window.REPORT_DATA =
// {...};` shape WriteReportData emits). Returns (nil, nil) when the
// file is absent or unparseable — the caller treats those cases as
// "no prior data" and overwrites cleanly.
func readReportData(path string) (*reportPayload, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	// Strip the JS wrapper: leading `window.REPORT_DATA = ` and trailing `;`
	// plus optional whitespace / newline.
	const prefix = "window.REPORT_DATA = "
	s := string(raw)
	i := strings.Index(s, prefix)
	if i < 0 {
		return nil, nil
	}
	body := strings.TrimSpace(s[i+len(prefix):])
	body = strings.TrimSuffix(body, ";")
	body = strings.TrimSpace(body)
	var p reportPayload
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		return nil, nil // treat as "no prior data"
	}
	return &p, nil
}

// mergePayloads composes `prev` and `next` into a single payload. Within
// each sweep, points are merged by Fields-tuple (e.g. {N=4, K=4, BTT=300,
// σ=0.5}); the freshest occurrence wins on duplicate, and new
// (n, K)-or-other-axis combinations are appended. New sweeps not present
// in `prev` are appended; sweeps unique to `prev` are kept as-is.
// Scenarios and Protocols carry forward from `next` (the freshest
// catalog is canonical); top-level metadata (Title, Description,
// Iterations counts, Wallclock) is also taken from `next` since it
// describes the most recent run that produced merged output.
//
// CAVEAT on BaselineIterations / UnstableIterations / Wallclock after
// merge: these reflect ONLY next's run, not the merged content. Prev-
// origin points may carry distributions of a different per-cell iter
// count if next ran at a different budget than prev did. cellPayload.
// Iterations is per-point and stays authoritative; renderers should
// read that for "how many samples back this cell" rather than the
// top-level fields. The top-level values are useful only as "what
// budget did the latest run TRY to apply", not "what budget produced
// every cell in this file".
func mergePayloads(prev, next reportPayload) reportPayload {
	out := reportPayload{
		Title:              next.Title,
		Description:        next.Description,
		BaselineIterations: next.BaselineIterations,
		UnstableIterations: next.UnstableIterations,
		Wallclock:          next.Wallclock,
		// Scenarios/Protocols are UNIONED across prev+next, not just
		// taken from next. Reason: if the catalog changes between runs
		// (scenario added, renamed, or removed), taking only next's
		// list silently hides cells in PRIOR (n, K, ...) points for
		// scenarios that aren't in the freshest run's catalog. Union
		// keeps every scenario whose data lives somewhere in the
		// merged sweeps so the heatmap still renders it. Conflict
		// resolution: next-side metadata (Title / Group / Note) wins
		// on Name match (treat the freshest run as canonical for the
		// scenario's documentation).
		Scenarios: mergeScenarios(prev.Scenarios, next.Scenarios),
		Protocols: mergeProtocols(prev.Protocols, next.Protocols),
	}
	// Build sweep-by-name map from prev, then walk next sweeps: matching
	// names get their points merged; new sweeps are appended.
	prevByName := make(map[string]int, len(prev.Sweeps))
	for i, sw := range prev.Sweeps {
		prevByName[sw.Name] = i
	}
	usedPrev := make(map[string]bool, len(prev.Sweeps))
	for _, nsw := range next.Sweeps {
		if pi, ok := prevByName[nsw.Name]; ok {
			usedPrev[nsw.Name] = true
			merged := mergeSweepPoints(prev.Sweeps[pi], nsw)
			out.Sweeps = append(out.Sweeps, merged)
		} else {
			out.Sweeps = append(out.Sweeps, nsw)
		}
	}
	// Append sweeps that existed in prev but not in next (e.g. an old
	// sweep was removed from the catalog — preserve historical data).
	for _, psw := range prev.Sweeps {
		if !usedPrev[psw.Name] {
			out.Sweeps = append(out.Sweeps, psw)
		}
	}
	return out
}

// mergeScenarios returns the union of `prev` and `next` keyed by Name.
// Entries unique to `prev` come first (preserves catalog order from
// earlier runs); entries from `next` either update an existing one's
// Title/Group/Adversarial/Note (freshest metadata wins) or get
// appended after the prev-only entries. Result ordering: prev-only
// scenarios stay where they were, next-side scenarios in next's
// catalog order. The UI iterates this list to render heatmap rows;
// preserving prev-only scenarios stops their old cells from being
// silently hidden.
//
// Orphan-prev scenarios (in prev but not in next) get a one-line
// stderr warning so a silent catalog rename doesn't show up as two
// near-identical heatmap rows (old name with stale cells, new name
// with fresh ones). Removals are legitimate but easy to mistake for
// renames at the data.js layer — surfacing it lets the operator
// decide.
func mergeScenarios(prev, next []scenarioPayload) []scenarioPayload {
	nextByName := make(map[string]bool, len(next))
	for _, s := range next {
		nextByName[s.Name] = true
	}
	for _, s := range prev {
		if !nextByName[s.Name] {
			log.Printf("reporting: merge: scenario %q present in existing data.js but not in this run's catalog — kept as orphan (rename suspected if the new catalog has a near-twin)", s.Name)
		}
	}
	byName := make(map[string]int, len(prev)+len(next))
	out := make([]scenarioPayload, 0, len(prev)+len(next))
	for _, s := range prev {
		byName[s.Name] = len(out)
		out = append(out, s)
	}
	for _, s := range next {
		if i, ok := byName[s.Name]; ok {
			out[i] = s // freshest metadata wins
		} else {
			byName[s.Name] = len(out)
			out = append(out, s)
		}
	}
	return out
}

// mergeProtocols returns the union of `prev` and `next` by name string.
// Mirrors mergeScenarios's approach; protocols are just strings here so
// "freshest metadata wins" reduces to "names match by string equality".
func mergeProtocols(prev, next []string) []string {
	seen := make(map[string]bool, len(prev)+len(next))
	out := make([]string, 0, len(prev)+len(next))
	for _, p := range prev {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, p := range next {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// mergeSweepPoints merges next's points into prev's points by
// Fields-tuple. next-side points replace prev-side points with the same
// Fields (fresh data wins); new Fields combinations are appended.
// next's sweep metadata (Title, Description, etc.) takes precedence.
//
// Merged Points are re-sorted by fieldsKey post-merge so axis order is a
// deterministic function of the merged Fields-tuple set, independent of
// the run-ordering that produced the file. Without this, alternating
// runs at e.g. LAYERS_K=4 then LAYERS_K=2,3 leave Points in arrival
// order ([K=4, K=2, K=3]), which is fine for the current UI (which
// looks up by Fields rather than iterating Points in order) but a
// latent footgun for any future sort assumption.
func mergeSweepPoints(prev, next sweepPayload) sweepPayload {
	out := sweepPayload{
		Name:        next.Name,
		Title:       next.Title,
		Params:      next.Params,
		Description: next.Description,
		AxisLabel:   next.AxisLabel,
	}
	// Index next points by Fields-tuple so we can find replaced ones.
	nextKeys := make(map[string]int, len(next.Points))
	for i, pt := range next.Points {
		nextKeys[fieldsKey(pt.Fields)] = i
	}
	usedNext := make(map[int]bool, len(next.Points))
	for _, ppt := range prev.Points {
		key := fieldsKey(ppt.Fields)
		if ni, ok := nextKeys[key]; ok {
			out.Points = append(out.Points, next.Points[ni])
			usedNext[ni] = true
		} else {
			out.Points = append(out.Points, ppt)
		}
	}
	for i, npt := range next.Points {
		if !usedNext[i] {
			out.Points = append(out.Points, npt)
		}
	}
	sort.SliceStable(out.Points, func(i, j int) bool {
		return fieldsKey(out.Points[i].Fields) < fieldsKey(out.Points[j].Fields)
	})
	return out
}

// fieldsKey returns a stable string identity for a point's Fields map.
// Keys are sorted lexicographically; values formatted via `%g` for the
// shortest round-trippable representation. Two points merge iff their
// fieldsKey strings are byte-identical.
//
// CONTRACT for sweep builders: axis values emitted into Fields must be
// byte-identical floats across runs. Today every axis value is a
// literal (BTT-as-int-ms, σ as literal 0.1/0.3/..., InstabilityLevel
// as small int, etc.) so `%g` produces stable strings. If a future
// sweep ever computes a Fields value (e.g. derived from BTT/2 ratio
// or sqrt), it MUST round to a canonical form before storing — `%g`
// would otherwise serialize the last-ulp drift differently across
// runs and silently split one logical point into two.
func fieldsKey(fields map[string]float64) string {
	if len(fields) == 0 {
		return ""
	}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('|')
		}
		fmt.Fprintf(&b, "%s=%g", k, fields[k])
	}
	return b.String()
}

// ---- payload DTOs ----------------------------------------------------
//
// Field naming follows JS conventions (camelCase) so the UI consumes
// `window.REPORT_DATA.successRate` directly. Optional fields use
// omitempty so the JSON stays compact and "no data" is distinguishable
// from "value zero" in the UI (e.g., a cell with no successful sims
// omits decisionTime entirely; the UI treats missing as "no samples").

type reportPayload struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	// BaselineIterations / UnstableIterations carry the per-group counts the
	// driver applied. Per-cell iteration counts live on cellPayload.Iterations
	// (which is what the UI's CDF rendering keys off of).
	BaselineIterations int               `json:"baselineIterations,omitempty"`
	UnstableIterations int               `json:"unstableIterations,omitempty"`
	Wallclock          string            `json:"wallclock"`
	Scenarios          []scenarioPayload `json:"scenarios"`
	Protocols          []string          `json:"protocols"`
	Sweeps             []sweepPayload    `json:"sweeps"`
}

type scenarioPayload struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Group       string `json:"group,omitempty"`
	Adversarial bool   `json:"adversarial,omitempty"`
	Note        string `json:"note,omitempty"`
}

type sweepPayload struct {
	Name        string         `json:"name"`
	Title       string         `json:"title"`
	Params      []string       `json:"params,omitempty"`
	Description string         `json:"description,omitempty"`
	AxisLabel   string         `json:"axisLabel,omitempty"`
	Points      []pointPayload `json:"points"`
}

type pointPayload struct {
	Label string `json:"label"`
	// Fields exposes the numeric axis values for this point (K, BTT in ms,
	// Sigma, …) so the UI can look up points by exact value without
	// parsing Label. Mirrors SweepPoint.Fields verbatim.
	Fields map[string]float64 `json:"fields,omitempty"`
	Cells  []cellPayload      `json:"cells"`
}

type cellPayload struct {
	Scenario    string  `json:"scenario"`
	Protocol    string  `json:"protocol"`
	Iterations  int     `json:"iterations"`
	SuccessRate float64 `json:"successRate"`
	// DecisionTimes is the sorted ms-integer decision-time sample for
	// every successful sim. Length = SuccessRate × Iterations. Emitted
	// so the UI can render a CDF directly instead of just summary
	// percentiles. omitted when no sim decided.
	DecisionTimes []int `json:"decisionTimes,omitempty"`
	// DecidingBroadcastTimes is index-aligned with DecisionTimes: entry
	// i is T_broadcast_max (slot-anchored, in ms; = max(BFTStart,
	// T_commit − B_k) per the spec's runtime clamp) for the deciding
	// layer of the i-th decision sample. Reported for diagnostic /
	// per-sample timeline rendering. The UI's BFT_start picker uses
	// pre-computed cells (one per BFT_start sweep point) rather than
	// post-hoc filtering on this array for OBFT-family. Zero values
	// for protocols with no slot-anchored broadcast (QBFT family,
	// PSigs) — those use pipeline-shift in the UI and don't consult
	// this field. Omitted when no sim decided.
	DecidingBroadcastTimes []int               `json:"decidingBroadcastTimes,omitempty"`
	DecisionTime           *percentilesPayload `json:"decisionTime,omitempty"`
	ClusterBandwidth       *percentilesPayload `json:"clusterBandwidth,omitempty"`
	PerKindBandwidth       map[string]float64  `json:"perKindBandwidth,omitempty"`
	MissReasons            map[string]int      `json:"missReasons,omitempty"`
}

type percentilesPayload struct {
	P50  float64 `json:"p50"`
	P90  float64 `json:"p90"`
	P99  float64 `json:"p99"`
	Mean float64 `json:"mean"`
}

func buildPayload(c Comparison) reportPayload {
	scenarios := extractScenarios(c.Sweeps)
	protocols := extractProtocols(c.Sweeps)

	pl := reportPayload{
		Title:              c.Title,
		Description:        c.Description,
		BaselineIterations: c.BaselineIterations,
		UnstableIterations: c.UnstableIterations,
		Wallclock:          c.Wallclock.String(),
		Protocols:          protocols,
	}
	for _, sc := range scenarios {
		pl.Scenarios = append(pl.Scenarios, scenarioPayload{
			Name: sc.Name, Title: sc.DisplayTitle(), Group: sc.Group,
			Adversarial: sc.IsAdversarial(),
			Note:        sc.Note,
		})
	}
	for _, sw := range c.Sweeps {
		swp := sweepPayload{
			Name:        sw.Sweep.Name,
			Title:       sw.Sweep.DisplayTitle(),
			Params:      sw.Sweep.Params,
			Description: sw.Sweep.Description,
			AxisLabel:   sw.Sweep.AxisLabel,
		}
		for i, rep := range sw.Reports {
			pt := pointPayload{
				Label:  sw.Sweep.Points[i].Label,
				Fields: sw.Sweep.Points[i].Fields,
			}
			for _, cell := range rep.Cells {
				pt.Cells = append(pt.Cells, buildCell(cell))
			}
			swp.Points = append(swp.Points, pt)
		}
		pl.Sweeps = append(pl.Sweeps, swp)
	}
	return pl
}

func buildCell(c ct.BatchCell) cellPayload {
	out := cellPayload{
		Scenario:    c.Scenario,
		Protocol:    c.Protocol,
		Iterations:  c.Iterations,
		SuccessRate: c.SuccessRate,
	}
	if c.DecisionTime.Len() > 0 {
		out.DecisionTime = &percentilesPayload{
			P50:  c.DecisionTime.Percentile(50),
			P90:  c.DecisionTime.Percentile(90),
			P99:  c.DecisionTime.Percentile(99),
			Mean: c.DecisionTime.Mean(),
		}
		// Sorted integer-ms samples for the UI CDF chart, paired with the
		// per-sample deciding-layer broadcast deadline (T_broadcast_max
		// for the sample's deciding layer). Both arrays are sorted by
		// decision time jointly so any UI consumer of the per-sample
		// arrays (legacy pipeline-shift fallback, per-sample timeline
		// rendering) can read the i-th broadcast deadline alongside
		// the i-th decision time.
		// One entry per successful sim; absent values (missed sims) are
		// implicit via Iterations - len(DecisionTimes).
		n := len(c.DecisionTime)
		idx := make([]int, n)
		for i := range idx {
			idx[i] = i
		}
		sort.SliceStable(idx, func(i, j int) bool {
			return c.DecisionTime[idx[i]] < c.DecisionTime[idx[j]]
		})
		out.DecisionTimes = make([]int, n)
		// DecidingBroadcastTime is index-aligned with DecisionTime
		// (aggregateCellIters appends both inside the Decided branch
		// together), so missing alignment here would be an aggregator
		// bug, not a data shape we should silently tolerate.
		hasBroadcast := len(c.DecidingBroadcastTime) == n
		if hasBroadcast {
			out.DecidingBroadcastTimes = make([]int, n)
		}
		for i, k := range idx {
			out.DecisionTimes[i] = int(c.DecisionTime[k] + 0.5)
			if hasBroadcast {
				out.DecidingBroadcastTimes[i] = int(c.DecidingBroadcastTime[k] + 0.5)
			}
		}
	}
	if c.ClusterBandwidth.Len() > 0 {
		out.ClusterBandwidth = &percentilesPayload{
			P50:  c.ClusterBandwidth.Percentile(50),
			P90:  c.ClusterBandwidth.Percentile(90),
			P99:  c.ClusterBandwidth.Percentile(99),
			Mean: c.ClusterBandwidth.Mean(),
		}
	}
	if len(c.PerKindBandwidth) > 0 {
		// Emit median per kind. Sorted keys for deterministic output.
		kinds := make([]string, 0, len(c.PerKindBandwidth))
		for k := range c.PerKindBandwidth {
			kinds = append(kinds, k)
		}
		sort.Strings(kinds)
		out.PerKindBandwidth = make(map[string]float64, len(kinds))
		for _, k := range kinds {
			d := c.PerKindBandwidth[k]
			if d.Len() == 0 {
				continue
			}
			out.PerKindBandwidth[k] = d.Median()
		}
	}
	if len(c.MissReasons) > 0 {
		out.MissReasons = make(map[string]int, len(c.MissReasons))
		for k, v := range c.MissReasons {
			out.MissReasons[k] = v
		}
	}
	return out
}

// extractScenarios returns the UNION of scenarios across every (sweep,
// report) in the comparison, preserving first-seen order. Unions —
// rather than reading just sweeps[0].Reports[0] — because a sweep
// point's scenario list can be filtered relative to the overall
// catalog (e.g. p2pBaselineSweep's level>0 points keep only Baseline-
// group scenarios per the rationale in sweep.go). A naive
// "first-report wins" extraction would silently drop every non-
// Baseline scenario from a fresh data.js if the first report happened
// to be a filtered slice, which the UI then renders as a near-empty
// heatmap with no error.
func extractScenarios(sweeps []ct.SweepResult) []ct.Scenario {
	seen := make(map[string]int)
	var out []ct.Scenario
	for _, sw := range sweeps {
		for _, rep := range sw.Reports {
			for _, sc := range rep.Config.Scenarios {
				if _, ok := seen[sc.Name]; ok {
					continue
				}
				seen[sc.Name] = len(out)
				out = append(out, sc)
			}
		}
	}
	return out
}

// extractProtocols returns the UNION of protocol names across every
// (sweep, report). Mirrors extractScenarios's union semantic for the
// same reason: defensive against any sweep emitting a filtered subset.
func extractProtocols(sweeps []ct.SweepResult) []string {
	seen := make(map[string]bool)
	var out []string
	for _, sw := range sweeps {
		for _, rep := range sw.Reports {
			for _, p := range rep.Config.Protocols {
				name := p.Name()
				if seen[name] {
					continue
				}
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	return out
}
