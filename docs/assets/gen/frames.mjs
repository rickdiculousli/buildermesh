// frames.mjs <svg> <outprefix> <width> <dark|light> <t1,t2,...> [reduce]
// Renders an SVG (as an <img>, as GitHub shows it) in headless Chromium at
// each time t (seconds) into <outprefix>-<t>.png, freezing CSS animations.
import { spawn } from "node:child_process";
import { mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
const [svg, out, w, scheme, times, reduce] = process.argv.slice(2);
const chrome = join(process.env.HOME, "Library/Caches/ms-playwright/chromium_headless_shell-1243/chrome-headless-shell-mac-arm64/chrome-headless-shell");
const profile = mkdtempSync(join(process.env.HOME, ".cache/bmesh-scratch/tmp/fr-"));
const proc = spawn(chrome, ["--headless", "--no-sandbox", "--hide-scrollbars", "--remote-debugging-port=0", `--user-data-dir=${profile}`, "about:blank"], { stdio: "ignore" });
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
let port = "";
for (let i = 0; !port && i < 100; i++) { await sleep(150); try { port = readFileSync(join(profile, "DevToolsActivePort"), "utf8").split("\n")[0].trim(); } catch {} }
const page = (await (await fetch(`http://127.0.0.1:${port}/json`)).json()).find((t) => t.type === "page");
const ws = new WebSocket(page.webSocketDebuggerUrl);
await new Promise((r) => ws.addEventListener("open", r));
let id = 0; const pending = new Map();
ws.addEventListener("message", (e) => { const m = JSON.parse(e.data); if (m.id && pending.has(m.id)) { pending.get(m.id)(m); pending.delete(m.id); } });
const send = (method, params = {}) => new Promise((r) => { const i = ++id; pending.set(i, r); ws.send(JSON.stringify({ id: i, method, params })); });
const bg = scheme === "light" ? "#ffffff" : "#0d1117";
await send("Emulation.setEmulatedMedia", { features: [{ name: "prefers-color-scheme", value: scheme }, { name: "prefers-reduced-motion", value: reduce ? "reduce" : "no-preference" }] });
await send("Emulation.setDeviceMetricsOverride", { width: +w, height: 100, deviceScaleFactor: 2, mobile: false });
// Inline the SVG as an <object>-free <img> via data URL, like GitHub's <img>.
const data = "data:image/svg+xml;base64," + readFileSync(resolve(svg)).toString("base64");
// An <img> SVG's animations aren't reachable from the page; render the SVG
// document directly instead so getAnimations() works, at the same width.
const html = `<html><body style="margin:0;background:${bg}"><div id=__frame_wrap style="width:${w}px"></div><script>
fetch(${JSON.stringify(data)}).then(r=>r.text()).then(t=>{document.getElementById('__frame_wrap').innerHTML=t;const s=document.querySelector('svg');s.style.width='100%';s.style.height='auto';s.style.display='block';document.title='ready';});</script></body></html>`;
await send("Page.enable"); await send("Runtime.enable");
await send("Page.navigate", { url: "data:text/html;base64," + Buffer.from(html).toString("base64") });
for (let i = 0; i < 50; i++) { await sleep(100); const r = await send("Runtime.evaluate", { expression: "document.title" }); if (r.result.result.value === "ready") break; }
await sleep(300);
const hRes = await send("Runtime.evaluate", { expression: "Math.ceil(document.getElementById('__frame_wrap').getBoundingClientRect().height)" });
const h = hRes.result.result.value;
await send("Emulation.setDeviceMetricsOverride", { width: +w, height: h, deviceScaleFactor: 2, mobile: false });
for (const t of times.split(",")) {
  await send("Runtime.evaluate", { expression: `document.getAnimations().forEach(a=>{a.pause();a.currentTime=${+t * 1000};});document.querySelectorAll('svg').forEach(s=>{if(s.pauseAnimations){s.pauseAnimations();s.setCurrentTime(${+t});}});` });
  await sleep(80);
  const shot = await send("Page.captureScreenshot", { format: "png" });
  const f = `${out}-${t}.png`; writeFileSync(f, Buffer.from(shot.result.data, "base64")); console.log(f);
}
const n = await send("Runtime.evaluate", { expression: "document.getAnimations().length" });
console.error(`animations: ${n.result.result.value}, height ${h}`);
ws.close(); proc.kill();
