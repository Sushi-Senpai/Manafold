// Minimal CDP driver: navigate the real builder page, drive real input events,
// capture screenshots. Talks raw DevTools Protocol over the browser WS.
import fs from "node:fs";

const OUT = process.env.OUT_DIR || "/home/johna/shots";
fs.mkdirSync(OUT, { recursive: true });
const BASE = "http://localhost:3210";

const target = await (await fetch(
  `http://localhost:9222/json/new?${encodeURIComponent(BASE + "/decks/demo")}`,
  { method: "PUT" },
)).json();
const ws = new WebSocket(target.webSocketDebuggerUrl);
let id = 0;
const pending = new Map();
const listeners = [];
ws.addEventListener("message", (ev) => {
  const msg = JSON.parse(ev.data);
  if (msg.id && pending.has(msg.id)) {
    const { resolve, reject } = pending.get(msg.id);
    pending.delete(msg.id);
    msg.error ? reject(new Error(JSON.stringify(msg.error))) : resolve(msg.result);
  } else if (msg.method) {
    for (const l of listeners) l(msg);
  }
});
await new Promise((r) => ws.addEventListener("open", r));
function send(method, params = {}) {
  const mid = ++id;
  return new Promise((resolve, reject) => {
    pending.set(mid, { resolve, reject });
    ws.send(JSON.stringify({ id: mid, method, params }));
  });
}
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
await send("Page.enable");
await send("Runtime.enable");

async function setViewport(width, height) {
  await send("Emulation.setDeviceMetricsOverride", { width, height, deviceScaleFactor: 1, mobile: false });
}
async function evalJS(expression) {
  const r = await send("Runtime.evaluate", { expression, returnByValue: true, awaitPromise: true });
  if (r.exceptionDetails) throw new Error(JSON.stringify(r.exceptionDetails));
  return r.result.value;
}
async function goto(url) {
  const done = new Promise((resolve) => {
    const l = (m) => {
      if (m.method === "Page.loadEventFired") { listeners.splice(listeners.indexOf(l), 1); resolve(); }
    };
    listeners.push(l);
  });
  await send("Page.navigate", { url });
  await done;
}
async function shot(name) {
  await sleep(200);
  const { data } = await send("Page.captureScreenshot", { format: "png", captureBeyondViewport: false });
  fs.writeFileSync(`${OUT}/${name}.png`, Buffer.from(data, "base64"));
  console.log("wrote", name);
}
async function rectOf(fn) {
  return await evalJS(`(() => { const el = ${fn}; if (!el) return null;
    const r = el.getBoundingClientRect();
    return { x: r.x + r.width/2, y: r.y + r.height/2, top:r.top, left:r.left, width:r.width, height:r.height }; })()`);
}
async function mouseMove(x, y) { await send("Input.dispatchMouseEvent", { type: "mouseMoved", x, y, buttons: 0 }); }
async function click(x, y) {
  await send("Input.dispatchMouseEvent", { type: "mouseMoved", x, y, buttons: 0 });
  await send("Input.dispatchMouseEvent", { type: "mousePressed", x, y, button: "left", buttons: 1, clickCount: 1 });
  await send("Input.dispatchMouseEvent", { type: "mouseReleased", x, y, button: "left", buttons: 0, clickCount: 1 });
}
async function waitFor(fnBody, tries = 50) {
  for (let i = 0; i < tries; i++) {
    if (await evalJS(`(() => { ${fnBody} })()`)) return true;
    await sleep(200);
  }
  throw new Error("waitFor timed out: " + fnBody);
}

// ---- 1 + 2. three-column layout / type-grouped decklist ----
await setViewport(1560, 1300);
await goto(`${BASE}/decks/demo`);
await waitFor(`return !!document.body.innerText.match(/Creatures \\(4\\)/) && !!document.body.innerText.match(/Commander . 1/i);`);
await sleep(700);
await shot("01-three-column-layout");
await setViewport(1560, 2700);
await sleep(400);
await shot("02-decklist-type-grouped");

// ---- 3. image panel on hover of a decklist row card name ----
await setViewport(1560, 1300);
await sleep(300);
const rift = await rectOf(`[...document.querySelectorAll('.order-2 span')].find(s => s.textContent.trim() === 'Cyclonic Rift')`);
await mouseMove(rift.left + 5, rift.top + rift.height / 2);
await sleep(1300);
await shot("03-image-panel-on-hover");
await mouseMove(20, 20);
await sleep(1400);
await shot("07-image-panel-commander-at-rest");

// ---- 5a. action menu opens on click of the row kebab; 5b scroll closes it ----
const kebab = await rectOf(`(() => {
  const btns = [...document.querySelectorAll('.order-2 button[aria-label="Card actions"]')];
  return btns.find(b => { const li = b.closest('li'); return li && li.textContent.includes('Solemn Simulacrum'); }) || null; })()`);
console.log("kebab", kebab);
if (kebab) {
  await click(kebab.x, kebab.y);
  await sleep(450);
  await shot("05a-action-menu-open-on-click");
  await send("Input.dispatchMouseEvent", { type: "mouseWheel", x: 700, y: 500, deltaX: 0, deltaY: 320 });
  await evalJS(`window.scrollBy(0, 360); true`);
  await sleep(500);
  await shot("05b-action-menu-closed-after-scroll");
  await evalJS(`window.scrollTo(0,0); true`);
  await sleep(200);
}

// ---- 4. add a card by clicking the search result row ----
await setViewport(1560, 1300);
await sleep(300);
const searchInput = await rectOf(`document.querySelector('input[aria-label="Card search"]')`);
await click(searchInput.x, searchInput.y);
await send("Input.insertText", { text: "Beast" });
await waitFor(`return !!document.querySelector('ul[role="listbox"] li[role="option"]');`);
await sleep(400);
await shot("04a-search-before-click");
const beforeCount = await evalJS(`document.body.innerText.match(/Mainboard . (\\d+)/i)?.[1] || document.body.innerText.match(/MAINBOARD . (\\d+)/)?.[1] || null`);
console.log("mainboard before", beforeCount);
const resRow = await rectOf(`(() => { const li = [...document.querySelectorAll('ul[role="listbox"] li[role="option"]')].find(n => n.textContent.includes('Beast Within')); return li; })()`);
console.log("resRow", resRow);
await click(resRow.left + resRow.width / 2 - 60, resRow.top + resRow.height / 2);
await sleep(300);
await shot("04b-click-to-add-confirmation");
await sleep(1400);
const afterCount = await evalJS(`document.body.innerText.match(/Mainboard . (\\d+)/i)?.[1] || document.body.innerText.match(/MAINBOARD . (\\d+)/)?.[1] || null`);
console.log("mainboard after", afterCount);
await shot("04c-after-add-count");

// ---- 6. responsive single column ----
await setViewport(760, 1700);
await sleep(700);
await shot("06-responsive-single-column");

console.log("RESULT", JSON.stringify({ beforeCount, afterCount }));
ws.close();
process.exit(0);
