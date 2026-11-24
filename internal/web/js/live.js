// live: the /events connection.
import { $, check } from "./core.js";
import { mesh } from "./state.js";
import { drawRecent, remember } from "./drawer.js";
import { checkSoon } from "./check.js";

function parseFrame(data) {
  try {
    const f = JSON.parse(data);
    return f && typeof f.subject === "string" ? f : null;
  } catch (e) {
    return null;
  }
}

let connState = "connecting";

function setConn(s) {
  connState = s;
  showConn();
  if (check) {
    if (check.conn[check.conn.length - 1] !== s) check.conn.push(s);
    checkSoon();
  }
}

// showConn writes the connection state: "● live · <host> hub" once live.
export function showConn() {
  const c = $("conn");
  c.textContent = connState === "live" ? "● live" + (mesh.host ? " · " + mesh.host + " hub" : "") : connState;
  c.classList.toggle("down", connState === "reconnecting");
  c.classList.toggle("up", connState === "live");
}

const RETRY_MS = [1000, 2000, 5000, 10000];
let retries = 0;

// connect follows /events, calling on.open at each connection and on.frame
// with each live frame. The browser retries a dropped stream itself; a
// stream it gave up on (an error status while the daemon starts) is opened
// again here with backoff.
export function connect(on) {
  const es = new EventSource("/events");
  es.onopen = () => {
    // Every connection replays the last day, and live frames were missed
    // while it was down.
    retries = 0;
    mesh.recent = [];
    drawRecent();
    setConn("live");
    on.open();
  };
  es.onerror = () => {
    setConn("reconnecting");
    if (es.readyState !== EventSource.CLOSED) return;
    setTimeout(() => connect(on), RETRY_MS[Math.min(retries++, RETRY_MS.length - 1)]);
  };
  // Replayed frames only fill the drawer: they already happened.
  es.addEventListener("replay", (e) => {
    const f = parseFrame(e.data);
    if (!f) return;
    if (check) {
      check.replay++;
      checkSoon();
    }
    remember(f);
  });
  es.addEventListener("live", (e) => {
    const f = parseFrame(e.data);
    if (!f) return;
    if (check) {
      check.live++;
      checkSoon();
    }
    remember(f);
    on.frame(f);
  });
}
