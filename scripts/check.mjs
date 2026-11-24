// check.mjs <url> <width> <height> [png]: loads the map at url (with ?check=1
// added when missing) in headless Chromium at width x height, waits for the
// self-check title to show a loaded tree on a live connection, prints it, and
// writes a screenshot of the viewport to png when given. Exit 0 ok; 1 not
// loaded within 20 s or a RULES rule failed; 2 usage or browser error.
// CHECK_COLOR_SCHEME=dark|light sets prefers-color-scheme. Chromium is
// $CHROME, else the newest headless shell or Chromium in the Playwright cache
// ($PLAYWRIGHT_BROWSERS_PATH, ~/.cache/ms-playwright, ~/Library/Caches/ms-playwright).
// Node 22 or later. No --dump-dom: the page's /events stream never ends.
import { spawn } from "node:child_process";
import { existsSync, mkdtempSync, readdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { homedir, tmpdir } from "node:os";
import { join } from "node:path";

const [rawURL, w, h, png] = process.argv.slice(2);
if (!rawURL || !(+w > 0) || !(+h > 0)) {
  console.error("usage: node scripts/check.mjs <url> <width> <height> [png]");
  process.exit(2);
}
const url = new URL(rawURL);
url.searchParams.set("check", "1");

// chromium finds the browser to run, or exits naming where it looked.
function chromium() {
  if (process.env.CHROME) return process.env.CHROME;
  const roots = [process.env.PLAYWRIGHT_BROWSERS_PATH, join(homedir(), ".cache/ms-playwright"),
    join(homedir(), "Library/Caches/ms-playwright")].filter(Boolean);
  // Headless shells first (they start faster), each newest revision first.
  const builds = [
    ["chromium_headless_shell-", ["chrome-headless-shell-linux64/chrome-headless-shell",
      "chrome-headless-shell-mac-arm64/chrome-headless-shell", "chrome-headless-shell-mac-x64/chrome-headless-shell"]],
    ["chromium-", ["chrome-linux64/chrome", "chrome-linux/chrome",
      "chrome-mac-arm64/Chromium.app/Contents/MacOS/Chromium", "chrome-mac/Chromium.app/Contents/MacOS/Chromium"]],
  ];
  for (const [prefix, bins] of builds) {
    for (const root of roots) {
      let dirs = [];
      try { dirs = readdirSync(root).filter((d) => d.startsWith(prefix) && /^\d+$/.test(d.slice(prefix.length))); } catch { continue; }
      dirs.sort((a, b) => b.slice(prefix.length) - a.slice(prefix.length));
      for (const d of dirs) {
        for (const bin of bins) {
          const p = join(root, d, bin);
          if (existsSync(p)) return p;
        }
      }
    }
  }
  console.error(`check.mjs: no Chromium: set CHROME, or install one into the Playwright cache (looked in ${roots.join(", ")})`);
  process.exit(2);
}

// RULES: per view ("all" = every view), what the final title must show. Each
// rule reads the title key `key=value`: `test` is ">= 1", "== N", "!= N" or
// "has a,b,c" (value is "name:count,..." and must name each). A rule with no
// `required` passes when its key is missing; a `required` one fails.
const SEGS = "working,landing,held,silent,asking,blocked";
const RULES = {
  all: [
    { key: "bandHeld", test: "== 0", msg: "a held node is in the needs-you band" },
    { key: "bandLanding", test: "== 0", msg: "a landing node is in the needs-you band" },
    { key: "older", test: ">= 1", required: true, msg: "no stale row in the band's older fold" },
    { key: "digest", test: "!= 1", msg: "the strip shows a digest older than twice digest_every" },
    { key: "landDup", test: "== 0", required: true, msg: "a landing's repeated land event shows twice" },
    { key: "stateGaps", test: "== 0", required: true, msg: "a state in the node-state table has no icon or colour token" },
  ],
  map: [
    { key: "segs", test: `has ${SEGS}`, required: true, msg: "no $ segment on the timeline" },
    { key: "kpis", test: "== 8", required: true, msg: "the KPI row hasn't 8 tiles" },
    ...[["roles", "no series on the tokens-by-role chart"], ["agents", "no rows in the agents table"],
      ["ctx", "no points on the context-per-call chart"], ["quota", "no points on the quota chart"],
      ["flags", "no flag row in the needs-you band"], ["flagsRunaway", "the seed's runaway has no band row"],
      ["outliers", "no points on the outliers chart"], ["burndown", "no marks on the burndowns"],
    ].map(([key, msg]) => ({ key, test: ">= 1", required: true, msg })),
  ],
  hosts: [["hosts", "no host card drawn"], ["procs", "no live process rows on the hosts tab"]]
    .map(([key, msg]) => ({ key, test: ">= 1", required: true, msg })),
  timeline: [{ key: "segs", test: `has ${SEGS}`, required: true, msg: "no $ segment on the timeline" }],
};

const val = (t, key) => (new RegExp(`(?:^| )${key}=(\\S*)`).exec(t) || [])[1];
// failures lists the message of every rule of the title's view that fails.
function failures(t) {
  const view = (/ view=(\S+) /.exec(t) || [])[1];
  const out = [];
  for (const r of [...RULES.all, ...(RULES[view] || [])]) {
    const v = val(t, r.key);
    const [op, arg] = r.test.split(" ");
    if (v === undefined) {
      if (r.required) out.push(...(op === "has" ? arg.split(",").map((st) => r.msg.replace("$", st)) : [r.msg]));
      continue;
    }
    if (op === "has") {
      const drawn = new Set(v.split(",").map((s) => s.split(":")[0]));
      for (const st of arg.split(",")) if (!drawn.has(st)) out.push(r.msg.replace("$", st));
    } else if (!{ ">=": (a, b) => a >= b, "==": (a, b) => a === b, "!=": (a, b) => a !== b }[op](+v, +arg)) out.push(r.msg);
  }
  return out;
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const chrome = chromium();
console.error(`check.mjs: chromium ${chrome}`);
const profile = mkdtempSync(join(tmpdir(), "bmesh-check-"));
// Port 0: Chromium picks a free port and writes it to DevToolsActivePort.
const proc = spawn(chrome, ["--headless", "--no-sandbox", "--disable-gpu", "--hide-scrollbars", "--no-first-run",
  "--remote-debugging-port=0", `--user-data-dir=${profile}`, "about:blank"], { stdio: "ignore" });
let exited = false, spawnErr = "";
proc.on("exit", () => { exited = true; });
// A missing or non-executable $CHROME is an error event, not an exit.
proc.on("error", (e) => { exited = true; spawnErr = e.message; });
let code = 2;
try {
  let port = "";
  for (let i = 0; !port; i++) {
    if (exited) throw new Error(`chromium exited at start${spawnErr ? `: ${spawnErr}` : ""}`);
    if (i > 100) throw new Error("chromium did not open its DevTools port in 20 s");
    await sleep(200);
    try { port = readFileSync(join(profile, "DevToolsActivePort"), "utf8").split("\n")[0].trim(); } catch { /* not yet */ }
  }
  const targets = await (await fetch(`http://127.0.0.1:${port}/json`)).json();
  const page = targets.find((t) => t.type === "page");
  if (!page) throw new Error("chromium has no page target");
  const ws = new WebSocket(page.webSocketDebuggerUrl);
  await new Promise((resolve, reject) => {
    ws.addEventListener("open", resolve);
    ws.addEventListener("error", () => reject(new Error("DevTools websocket failed")));
  });
  let id = 0;
  const waiting = new Map();
  // A closed socket (Chromium gone) fails every call still waiting.
  ws.addEventListener("close", () => {
    for (const settle of waiting.values()) settle({ error: { message: "DevTools websocket closed" } });
    waiting.clear();
  });
  ws.addEventListener("message", (m) => {
    const d = JSON.parse(m.data);
    if (d.id && waiting.has(d.id)) {
      waiting.get(d.id)(d);
      waiting.delete(d.id);
    }
  });
  const send = (method, params = {}) => new Promise((resolve, reject) => {
    if (ws.readyState !== WebSocket.OPEN) return reject(new Error(`${method}: DevTools websocket closed`));
    const n = ++id;
    waiting.set(n, (d) => (d.error ? reject(new Error(`${method}: ${d.error.message}`)) : resolve(d.result)));
    ws.send(JSON.stringify({ id: n, method, params }));
  });
  const title = async () => (await send("Runtime.evaluate", { expression: "document.title", returnByValue: true })).result.value;
  await send("Emulation.setDeviceMetricsOverride", { width: +w, height: +h, deviceScaleFactor: 2, mobile: +w < 700 });
  // CHECK_COLOR_SCHEME=dark|light picks prefers-color-scheme; unset, Chromium's own (light).
  if (process.env.CHECK_COLOR_SCHEME) {
    await send("Emulation.setEmulatedMedia", { features: [{ name: "prefers-color-scheme", value: process.env.CHECK_COLOR_SCHEME }] });
  }
  await send("Page.enable");
  await send("Page.navigate", { url: url.href });
  // The page writes its title 100 ms after each change; wait for a tree and
  // the view's own drawing (a timeline, or a tab's panels) on a live
  // connection, then (for up to 3 s) for the title to hold still.
  let t = "", ready = false;
  for (let i = 0; i < 100 && !ready; i++) {
    await sleep(200);
    t = await title();
    const view = (/ view=(\S+) /.exec(t) || [])[1];
    const up = (k) => +val(t, k) >= 1;
    ready = up("trees") && /conn=\S*live/.test(t) && (up("lanes") || ["routines", "specs", "hosts", "tokens"].includes(view))
      && (view !== "map" || up("agents") && up("outliers") && up("burndown")) && (view !== "specs" || up("burndown"))
      && (view !== "tokens" || up("agents"))
      && (view !== "hosts" || up("hosts") && up("procs"));
  }
  if (ready) {
    for (let last = "", i = 0; last !== t && i < 10; i++) {
      last = t;
      await sleep(300);
      t = await title();
    }
  }
  console.log(`${w}x${h} ${url.href}\n  ${t || "(no title)"}`);
  if (png) {
    const shot = await send("Page.captureScreenshot", { format: "png", captureBeyondViewport: false });
    writeFileSync(png, Buffer.from(shot.data, "base64"));
    console.log(`  screenshot ${png}`);
  }
  if (!ready) console.error("check.mjs: no loaded tree and timeline on a live connection within 20 s");
  const fails = failures(t);
  for (const f of fails) console.error(`check.mjs: ${f}`);
  code = ready && !fails.length ? 0 : 1;
  ws.close();
} catch (e) {
  console.error(`check.mjs: ${e.message}`);
} finally {
  proc.kill();
  for (let i = 0; i < 20 && !exited; i++) await sleep(100);
  rmSync(profile, { recursive: true, force: true });
}
process.exit(code);
