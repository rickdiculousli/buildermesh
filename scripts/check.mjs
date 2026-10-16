// check.mjs <url> <width> <height> [png]: loads the map at url (with
// ?check=1 added when missing) in headless Chromium at width x height over
// the DevTools protocol, waits for the self-check title to show a loaded
// tree on a live connection, prints the title, writes a screenshot of the
// viewport to png when given, and exits: 0 once the title showed, 1 when it
// did not within 20 s (the last title is printed all the same), 2 on a
// usage or browser error. No --dump-dom: the page's /events stream never
// ends, so a plain headless load never returns.
//
// Chromium is $CHROME, else the newest headless shell or Chromium in the
// Playwright cache ($PLAYWRIGHT_BROWSERS_PATH, ~/.cache/ms-playwright or
// ~/Library/Caches/ms-playwright); stderr names the one it runs. Needs Node
// 22 or later (global fetch and WebSocket).
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
  await send("Page.enable");
  await send("Page.navigate", { url: url.href });
  // The page writes its title 100 ms after each change; wait for a tree on
  // a live connection, then (for up to 3 s) for the title to hold still.
  let t = "", ready = false;
  for (let i = 0; i < 100 && !ready; i++) {
    await sleep(200);
    t = await title();
    ready = /trees=[1-9]/.test(t) && /conn=\S*live/.test(t);
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
  if (!ready) console.error("check.mjs: no loaded tree on a live connection within 20 s");
  code = ready ? 0 : 1;
  ws.close();
} catch (e) {
  console.error(`check.mjs: ${e.message}`);
} finally {
  proc.kill();
  for (let i = 0; i < 20 && !exited; i++) await sleep(100);
  rmSync(profile, { recursive: true, force: true });
}
process.exit(code);
