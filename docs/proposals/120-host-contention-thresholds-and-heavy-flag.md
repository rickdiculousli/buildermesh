---
id: 120
title: Host contention reads ok, warn or red from measured thresholds, the overview names the most contended box, and an agent that starves its box is flagged heavy
kind: feature
size: M
judge: opus
status: accepted
lens: ux
proposed_by: coordinator
proposed_on: 2026-11-23
---

## Opportunity
117 records each box's contention and each agent's CPU and memory every minute but colours nothing, because no threshold has been measured: one PSI reading on k16 was `some avg60=11` during four Gradle-heavy leads, and memory `some` of 10% means thrashing while I/O `some` reaches it under ordinary builds. An operator on the overview still can't see that a box is contended or which agent causes it.

## Proposal
Built once 117 has been released and `bmusage.host.>` holds at least three days of k16's history with leads running, and the Mac's at least one day.

In:
- **Calibration first.** From that history, the distribution of each pressure series (cpu, mem, io `some avg60`) per box, and of macOS load per core, in the minutes with and without a lead working. Thresholds per resource and OS, ok / warn / red, are fixed in code from it (as 108's are) and recorded in this spec with the minute counts before building on them. A resource whose history never left ok is given none and reads uncoloured.
- **A common severity** per box and minute: the worst resource's level. macOS memory uses its kernel pressure level (2 warn, 4 critical).
- **The hosts tab's head line** coloured by resource.
- **A ninth KPI tile `Hosts`:** the box with the worst severity now, its worst resource, and its two heaviest agents by share during its warn-or-worse minutes in the window (`k16 CPU wait 11% · bluke-002 52%, bluke-004 20%`), linking `#/hosts`. With every box ok: `all ok` and the busiest box's CPU.
- **A strip per box under the fleet timeline:** each bucket coloured by its worst minute's severity, so a stall lines up with the lanes.
- **`heavy`**, a flag announced like 108's: an agent's share is its CPU over the box's CPU in use that minute (memory likewise); it is flagged `overworking` with rule `heavy` when its share is over a measured fraction during warn-or-worse minutes of that resource, for a measured count of the last 15 minutes. The fraction and count are set in the calibration so that the history's list of would-be flags is short and right. Words: `held 52% of k16's CPU while it waited 14% (10 of 15 min)`. `RuleOf` learns the words, `mesh-status`'s rule list and the internals' rules table gain `heavy`, the daemon announces it as it announces the others, and it joins the band and the agents table's `watch`.
- **`bmesh build` warns of a contended box.** When the box a lead would start on reads red in its last recorded minute (no reading in the last 5 minutes says nothing), each lead it starts there carries a warning in `build`'s `warnings`, beside the command clashes, on the `warning:` lines a coordinator reports as printed: `k16 is red: CPU wait 14% · bluke-002 52%, bluke-004 20%` (the worst resource and its two heaviest agents, as the `Hosts` tile words them). The lead still starts, and the operator decides; the manual's `bmesh build` section gains it.

Out: alerts beyond the band and the build warning, any action on the agent, refusing or moving a build off a contended box.

Judge: opus, because the thresholds are judged on real data and the flag joins 108's plumbing.

## What you'll see
A coloured head line per box, a `Hosts` tile saying which box is contended and who is causing it, a contention strip under the timeline, and a `▲ overworking` row in the needs-you band when one agent starves its box. A coordinator starting leads on a box already red is told so with the build's other warnings.

## Verification
- The calibration's table (per box, resource and lead/no-lead minutes: percentiles and minute counts) and the list of agents `heavy` would have flagged in the history, in the commit that sets the thresholds.
- `heavy`'s table tests beside 108's: under the fraction, a box at ok, too few minutes, and the memory case stay quiet.
- `build` tests: a box whose last minute reads red puts the warning in the output, one at warn or ok and one whose last reading is over 5 minutes old put none, and the lead starts either way.
- `check.mjs`'s `kpis=` becomes 9 (the manual's check table says 6 today, `check.mjs` checks 8: both say 9 after), and it adds `strips=` on the overview; on 117's seed both fail.

## Shown

## Risks and the alternative considered
The alternative was guessing thresholds in 117 from one reading; it lost because a flag that fires on every busy afternoon gets ignored, and 108's flags were calibrated on the fleet first.

## Upgrading
nothing

## Needs from the operator
