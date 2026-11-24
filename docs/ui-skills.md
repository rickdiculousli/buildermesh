# UI skills for the map

The design skills vendored in `.agents/skills/` (linked from `.claude/skills/`) for work on the map, the console `bmesh serve` serves from `internal/web/`. bmesh's plugin embeds only `skills/`, so none of these reach nodes on the mesh.

## Profile

- **Platform and stack.** Web, no framework, no build step: `index.html`, `map.css` and the ES modules in `js/` (about 115 KB of vanilla JS: `core.js` helpers, `state.js` shared state, `live.js`, `drawer.js`, a module per view, `check.js`, and `main.js` with the views and frames tables; charts drawn as hand-built SVG, live updates over `EventSource`), embedded in the Go binary by `internal/web/web.go`, plus `/js/mesh-table.js`, the node-state table the server renders from `internal/mesh`. A new tab is a module, a `VIEWS` row in `main.js` and a nav link; a new state is a row in `internal/mesh`'s `States` with its `ICON` and `--<state>` colour. Go builds against `go 1.26`; no JS or CSS dependencies.
- **Design system.** CSS custom properties on `:root` in `map.css`, taken from the console mock `docs/proposals/assets/106-console-mock.html`: dark first, a light palette under `prefers-color-scheme: light`, eight categorical steps in fixed order, status colours only beside an icon and a label, system sans with every number, path and time in monospace. The behaviour contract is `docs/manual.md`'s Map section.
- **Kind of UI.** A dense operator console: KPI tiles, a needs-you band, timelines, stacked and line charts, tables, drill-down pages. Desktop first, stacking under 1000px and 480px. Motion is limited to live redraws.
- **How UI is checked.** Go tests on the API (`internal/web/*_test.go`); the page itself only in a browser against a running `bmesh serve`. No screenshot tests.
- **What was there.** No project skills. User-level on the Mac only: `impeccable`, `apple-design`, `material-3`, `security-audit`. The session also offers Claude's built-in `dataviz` skill, which the map's palette rules already follow.

## Picks and precedence

When they disagree, the earlier wins:

1. **The project's own contract.** `docs/manual.md`'s Map section and the tokens in `map.css`. A finding that asks to change a documented choice (the dark-first palette, system sans plus monospace, the 12px type, borders over shadows) is a proposal, not a fix.
2. **`web-design-guidelines`: the rulebook.** Framework-free web rules (WCAG 2.2, responsive, forms, typography, performance, theming, navigation state), with vanilla HTML, CSS and JS samples. Ignore §11 Progressive Web Apps (the map is not installable), §2.1 mobile-first (the map is desktop-first by design) and the `rem`-for-body-text rule where the manual fixes px sizes; §1, §7 and §8 apply in full.
3. **`data-viz`: charts.** Anything drawn as a chart (the timelines, tokens by role, context per call, outliers, burndown, specs, quota): the chart type for the data relationship, marks and channels, data-ink, truthful axes and baselines (Cairo's lies checklist), redundant encoding beside colour, chart alt text, and Few's dashboard rules. Its frontmatter sets `user-invocable: false` and `disable-model-invocation: true`, so it never loads by itself: read `.agents/skills/data-viz/SKILL.md` and the reference its mode names before changing or reviewing a chart. Ignore its advice to swap in Okabe-Ito or Viridis: the map's categorical steps are its own validated palette (precedence 1).
4. **`better-interface` with its seven `better-*` siblings: the critique.** One ranked review (HIGH, MEDIUM, LOW) across accessibility, layout, writing, typography, colour, surfaces and motion, every finding at `file:line` with before and after, ending `Block` or `Approve`; rerunnable by any reviewer. Siblings are vendored because it loads all of them for its main job. It writes fixes in the project's own idiom, so ignore samples in Tailwind or `motion/react`.
5. **`interface-design`: taste and direction.** Craft for dashboards and data interfaces: one focal point per view, density in px, weight over size, quiet layering, states, a pre-component checkpoint. Ignore its "default typography: system/Inter fonts" item (system sans is the map's documented choice) and its offer to write `.interface-design/system.md` (the manual and `map.css` are the system).

No feel-and-motion pick: the map has no gestures or choreography.

| Skill | Source | Commit | License | Left out |
|---|---|---|---|---|
| `web-design-guidelines` | `ehmo/platform-design-skills`, `skills/web` | `dc2be825d8b439caea78e9eaa8fb3ac23b0ff3e9` | MIT (repo root, copied in) | nothing |
| `better-interface`, `better-accessibility`, `better-layout`, `better-writing`, `better-typography`, `better-colors`, `better-ui`, `better-motion` | `jakubkrehel/skills`, `skills/<name>` | `672ae44ace6aaed65b86cbb1e2fd6fbea0e79578` | MIT (repo root, copied into each) | `agents/openai.yaml` |
| `data-viz` | `ryanthedev/design-for-ai`, `skills/data-viz` | `37d7ae63d1afa45eb57ec90bc246cef6dd60c033` | MIT, declared in the repo's README and `plugin.json`; the repo ships no license file, so the standard MIT text was added | nothing |
| `interface-design` | `dammyjay93/interface-design`, `.claude/skills/interface-design` | `2f9be3206855bcb2d1d0af262c8bae25cba6658d` | MIT (repo root, copied in) | `agents/openai.yaml` |

## Rejections

- **Impeccable** (`pbakaus/impeccable`): its Setup runs a downloaded binary, and its critique counts a skipped detector as a failed run; one critique loads about 64 KB. A user-level copy already exists on the Mac and would shadow a project copy of the same name.
- **ui-skills `improve-ui`** (`ibelick/ui-skills`): sound read-only audit against the project's own system, but it drops accessibility findings unless asked and stops at three; `better-interface` covers its job and more.
- **ui-skills `baseline-ui`**: Tailwind, `motion/react` and `cn` rules; another stack.
- **ui-skills `fixing-accessibility`**: covered by `better-accessibility` and the rulebook's §1.
- **data-visualization** (`inference-sh/skills`): no license; its workflow runs charts through a hosted CLI that needs a login.
- **web-interface-guidelines** (`vercel-labs/web-interface-guidelines`): MIT and terse, but framed for React and Next (`<Link>`, hydration, `onKeyDown`); the vanilla rulebook fits better.
- **frontend-design**, **Hallmark**, **Taste Skill**, **UI UX Pro Max**: fresh or marketing surfaces, or scripts and browsers for their main job.

Engineering-only, for later: none needed for a framework-free page.

## Flags

- `better-interface` sends change-scoped reviews (a branch, diff or PR) to `interface-review`, which is user-invoked and not vendored: that link is dead here, so run `better-interface` on the surface instead.
- `better-motion/slow-motion.md` shows a snippet to paste into a browser console to slow animations; guidance only, nothing runs it.
- `interface-design` prefers an inline render tool and offers to write `.interface-design/system.md`; it also names `/interface-design:*` commands that are not in the folder (dead links).
- `data-viz` cites a sibling, `usability-principles.md` (the plugin's `usability` skill), for its Gestalt section only; not vendored, so that link is dead. The rest of design-for-ai (its workflow commands, `scripts/detect.mjs`, `scripts/dealer.mjs`) is left out.
- Impeccable's binary, hooks and detector were the reason for its rejection; nothing of it is vendored.
- No skill read has hooks in its frontmatter, telemetry, uploads or text that overrides the agent's or the user's instructions.
