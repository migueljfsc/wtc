// wtc timeline — vanilla JS, no dependencies, no build step (hard decision).
"use strict";

const $ = (id) => document.getElementById(id);
const timeline = $("timeline");
const moreBtn = $("more");
const statusEl = $("status");

let nextCursor = "";
let refreshTimer = null;

const token = $("token");
token.value = localStorage.getItem("wtc-token") || "";
token.addEventListener("change", () => {
  localStorage.setItem("wtc-token", token.value);
  load(true);
});

for (const id of ["f-env", "f-service", "f-kind", "f-since"]) {
  $(id).addEventListener("change", () => load(true));
}
let qTimer;
$("f-q").addEventListener("input", () => {
  clearTimeout(qTimer);
  qTimer = setTimeout(() => load(true), 300);
});
$("refresh").addEventListener("click", () => load(true));
moreBtn.addEventListener("click", () => load(false));

function params(cursor) {
  const p = new URLSearchParams();
  const set = (k, v) => v && p.set(k, v);
  set("q", $("f-q").value.trim());
  set("env", $("f-env").value.trim());
  set("service", $("f-service").value.trim());
  set("kind", $("f-kind").value);
  const hours = parseInt($("f-since").value, 10);
  p.set("since", new Date(Date.now() - hours * 3600e3).toISOString());
  p.set("limit", "100");
  if (cursor) p.set("cursor", cursor);
  return p;
}

async function load(reset) {
  if (reset) nextCursor = "";
  statusEl.textContent = "loading…";
  statusEl.className = "";
  let resp;
  try {
    resp = await fetch("/api/events?" + params(reset ? "" : nextCursor), {
      headers: { Authorization: "Bearer " + token.value },
    });
  } catch (e) {
    statusEl.textContent = "server unreachable";
    statusEl.className = "err";
    return;
  }
  if (resp.status === 401) {
    statusEl.textContent = "unauthorized — set the API token";
    statusEl.className = "err";
    timeline.replaceChildren();
    moreBtn.hidden = true;
    return;
  }
  if (!resp.ok) {
    statusEl.textContent = "error " + resp.status;
    statusEl.className = "err";
    return;
  }
  const body = await resp.json();
  if (reset) {
    timeline.replaceChildren();
    delete timeline.dataset.day;
  }
  render(body.events || []);
  nextCursor = body.next_cursor || "";
  moreBtn.hidden = !nextCursor;
  statusEl.textContent = new Date().toLocaleTimeString();
}

function render(events) {
  if (!events.length && !timeline.children.length) {
    timeline.replaceChildren(el("div", "empty", "no events in this window"));
    return;
  }
  for (const ev of events) {
    const ts = new Date(ev.ts);
    const day = ts.toLocaleDateString(undefined, {
      weekday: "short", year: "numeric", month: "short", day: "numeric",
    });
    if (timeline.dataset.day !== day) {
      timeline.append(el("div", "day", day));
      timeline.dataset.day = day;
    }
    timeline.append(row(ev, ts));
  }
}

function row(ev, ts) {
  const r = el("div", "ev " + (ev.status || ""));
  r.append(el("span", "t", ts.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" })));

  const env = el("span", "chip env-" + (ev.env || "none"), ev.env || "—");
  r.append(env);
  r.append(el("span", "chip kind", ev.kind + (ev.status === "failed" ? " ✗" : ev.status === "started" ? " …" : ev.status === "degraded" ? " ⚠" : "")));

  const title = el("span", "title");
  if (ev.url) {
    const a = document.createElement("a");
    a.href = ev.url;
    a.target = "_blank";
    a.rel = "noopener";
    a.textContent = ev.title;
    title.append(a);
  } else {
    title.textContent = ev.title;
  }
  const meta = [ev.service, ev.actor].filter(Boolean).join(" · ");
  if (meta) title.append(" ", el("span", "meta", meta));
  r.append(title);
  return r;
}

function el(tag, cls, text) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text !== undefined) e.textContent = text;
  return e;
}

// Gentle auto-refresh of the first page while the tab is visible.
function schedule() {
  clearInterval(refreshTimer);
  refreshTimer = setInterval(() => {
    if (!document.hidden) load(true);
  }, 60e3);
}

load(true);
schedule();
