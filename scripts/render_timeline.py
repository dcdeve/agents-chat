#!/usr/bin/env python3
"""Render an agents-chat export as a self-contained HTML conversation view.

Usage: render_timeline.py EXPORT.json OUTPUT.html
"""
import json
import pathlib
import sys

TEMPLATE = r"""<!doctype html>
<html lang="es">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Sesión agents-chat</title>
<style>
:root {
  --bg: #f6f5f2; --panel: #ffffff; --ink: #1d1d1f; --muted: #6b6b70; --line: #e4e2dc;
  --pill: #efede8; --warn: #b4441c; --ok: #2f7d4f; --code: #f1efea;
  --c0: #c2562f; --c1: #2a6fb0; --c2: #7a4fb5; --c3: #2f8a6b; --sys: #8a8a8f;
  color-scheme: light;
}
@media (prefers-color-scheme: dark) {
  :root:not([data-theme="light"]) {
    --bg: #17171a; --panel: #212125; --ink: #ececef; --muted: #9a9aa2; --line: #33333a;
    --pill: #2b2b31; --warn: #f08a5d; --ok: #6cc497; --code: #2a2a30;
    --c0: #f0875c; --c1: #6aa8e8; --c2: #b28bea; --c3: #5cc7a0; --sys: #8a8a93;
    color-scheme: dark;
  }
}
:root[data-theme="dark"] {
  --bg: #17171a; --panel: #212125; --ink: #ececef; --muted: #9a9aa2; --line: #33333a;
  --pill: #2b2b31; --warn: #f08a5d; --ok: #6cc497; --code: #2a2a30;
  --c0: #f0875c; --c1: #6aa8e8; --c2: #b28bea; --c3: #5cc7a0; --sys: #8a8a93;
  color-scheme: dark;
}
* { box-sizing: border-box; }
body { margin: 0; background: var(--bg); color: var(--ink);
  font: 14px/1.5 -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; }
.wrap { max-width: 1180px; margin: 0 auto; padding: 28px 20px 60px; }
h1 { font-size: 22px; margin: 0 0 4px; letter-spacing: -0.01em; }
.sub { color: var(--muted); font-size: 13px; }
.sub code { font-size: 12px; }
code { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; background: var(--code);
  padding: 1px 5px; border-radius: 4px; font-size: 12.5px; }
.agents { display: grid; grid-template-columns: repeat(auto-fit, minmax(230px, 1fr)); gap: 12px; margin: 22px 0; }
.agent { background: var(--panel); border: 1px solid var(--line); border-radius: 12px; padding: 14px;
  border-top: 4px solid var(--c); }
.agent .name { font-weight: 650; display: flex; align-items: center; gap: 8px; }
.agent .role { font-size: 11px; text-transform: uppercase; letter-spacing: .06em; color: var(--c);
  border: 1px solid var(--c); border-radius: 999px; padding: 0 7px; }
.agent .meta { color: var(--muted); font-size: 12.5px; margin-top: 4px; }
.stats { display: flex; flex-wrap: wrap; gap: 8px; margin-bottom: 18px; }
.stat { background: var(--panel); border: 1px solid var(--line); border-radius: 10px; padding: 8px 14px; }
.stat b { font-size: 18px; display: block; font-variant-numeric: tabular-nums; }
.stat span { color: var(--muted); font-size: 12px; }
.section-title { font-size: 13px; text-transform: uppercase; letter-spacing: .07em; color: var(--muted);
  margin: 26px 0 10px; }
.lanes { background: var(--panel); border: 1px solid var(--line); border-radius: 12px; padding: 14px 16px; overflow-x: auto; }
.lane { display: grid; grid-template-columns: 110px 1fr; align-items: center; gap: 10px; height: 30px; min-width: 640px; }
.lane .lbl { font-size: 12.5px; font-weight: 600; color: var(--c); }
.track { position: relative; height: 18px; background: var(--pill); border-radius: 4px; }
.tick { position: absolute; top: 2px; width: 2px; height: 14px; background: var(--c); opacity: .55; border-radius: 1px; }
.tick.msg { width: 6px; top: 0; height: 18px; opacity: 1; border-radius: 2px; }
.tick.down { background: var(--warn); width: 3px; top: -2px; height: 22px; opacity: 1; }
.axis { display: grid; grid-template-columns: 110px 1fr; gap: 10px; min-width: 640px; color: var(--muted); font-size: 11px; }
.axis .ends { display: flex; justify-content: space-between; }
.legend { color: var(--muted); font-size: 12px; margin-top: 8px; }
.legend i { display: inline-block; width: 10px; height: 10px; border-radius: 2px; margin: 0 4px 0 12px; vertical-align: -1px; }
.layout { display: grid; grid-template-columns: minmax(0, 1fr) 320px; gap: 20px; align-items: start; }
@media (max-width: 900px) { .layout { grid-template-columns: 1fr; } }
.toolbar { display: flex; flex-wrap: wrap; gap: 6px; margin-bottom: 12px; }
.toolbar button { background: var(--panel); color: var(--ink); border: 1px solid var(--line); border-radius: 999px;
  padding: 5px 12px; font: inherit; font-size: 13px; cursor: pointer; }
.toolbar button[aria-pressed="true"] { background: var(--ink); color: var(--bg); border-color: var(--ink); }
.feed { display: flex; flex-direction: column; gap: 10px; }
.msg { display: flex; gap: 10px; max-width: 760px; }
.msg.right { flex-direction: row-reverse; margin-left: auto; }
.avatar { flex: none; width: 34px; height: 34px; border-radius: 50%; background: var(--c); color: #fff;
  display: grid; place-items: center; font-weight: 700; font-size: 13px; }
.bubble { background: var(--panel); border: 1px solid var(--line); border-radius: 14px; padding: 10px 13px;
  min-width: 0; border-left: 3px solid var(--c); }
.msg.right .bubble { border-left: 1px solid var(--line); border-right: 3px solid var(--c); }
.bubble.auto { background: transparent; border-style: dashed; }
.head { font-size: 12.5px; color: var(--muted); margin-bottom: 4px; display: flex; flex-wrap: wrap; gap: 6px; align-items: baseline; }
.head .from { color: var(--c); font-weight: 650; }
.head .tag { font-size: 10.5px; text-transform: uppercase; letter-spacing: .05em; border: 1px solid var(--line);
  border-radius: 4px; padding: 0 5px; }
.body { white-space: pre-wrap; overflow-wrap: anywhere; }
.body.clamp { max-height: 9.5em; overflow: hidden; -webkit-mask-image: linear-gradient(#000 70%, transparent); mask-image: linear-gradient(#000 70%, transparent); }
.more { background: none; border: 0; color: var(--c1); cursor: pointer; padding: 4px 0 0; font: inherit; font-size: 12.5px; }
.acks { margin-top: 6px; font-size: 12px; color: var(--ok); }
.acks.none { color: var(--warn); }
.event { align-self: center; text-align: center; font-size: 12.5px; color: var(--muted); background: var(--pill);
  border-radius: 999px; padding: 4px 14px; max-width: 92%; }
.event b { color: var(--ink); font-weight: 600; }
.event.warn { color: var(--warn); }
.event.good { color: var(--ok); }
.event.quiet { background: transparent; padding: 0 10px; font-size: 11.5px; }
.time { font-variant-numeric: tabular-nums; }
.side { position: sticky; top: 16px; display: flex; flex-direction: column; gap: 12px; }
.card { background: var(--panel); border: 1px solid var(--line); border-radius: 12px; padding: 14px; }
.card h3 { margin: 0 0 6px; font-size: 14px; }
.state { display: inline-block; font-size: 11px; text-transform: uppercase; letter-spacing: .05em; border-radius: 4px;
  padding: 1px 6px; background: var(--pill); }
.state.completed { color: var(--ok); }
.steps { list-style: none; margin: 10px 0 0; padding: 0; font-size: 12.5px; }
.steps li { display: grid; grid-template-columns: 62px 1fr; gap: 6px; padding: 3px 0; border-top: 1px solid var(--line); }
.steps li span:first-child { color: var(--muted); font-variant-numeric: tabular-nums; }
.empty { color: var(--muted); font-style: italic; }
</style>
</head>
<body>
<div class="wrap">
  <h1>Sesión agents-chat</h1>
  <div class="sub" id="sub"></div>
  <div class="agents" id="agents"></div>
  <div class="stats" id="stats"></div>

  <div class="section-title">Actividad por agente</div>
  <div class="lanes" id="lanes"></div>

  <div class="layout">
    <div>
      <div class="section-title">Conversación</div>
      <div class="toolbar" id="toolbar"></div>
      <div class="feed" id="feed"></div>
    </div>
    <aside class="side">
      <div class="section-title" style="margin-bottom:0">Tareas</div>
      <div id="tasks"></div>
    </aside>
  </div>
</div>
<script>
const DATA = __DATA__;
const ev = DATA.events;
const COLORS = ["var(--c0)", "var(--c1)", "var(--c2)", "var(--c3)"];
const agents = {};
const order = [];
ev.filter(e => e.type === "agent_registered").forEach((e, i) => {
  const d = e.data;
  const m = /--model\s+(\S+?)[;\s]/.exec(d.configuration + " ");
  agents[d.id] = { name: d.name, product: d.product, version: d.version,
    model: m ? m[1] : "modelo por defecto", color: COLORS[i % COLORS.length], idx: i };
  order.push(d.id);
});
const coordinator = order[0];
const who = id => agents[id] || { name: id === "human" ? "humano" : id === "system" ? "sistema" : (id || "?").slice(0, 8), color: "var(--sys)" };
const esc = s => String(s ?? "").replace(/[&<>"']/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
const hhmm = t => new Date(t).toLocaleTimeString("es", { hour: "2-digit", minute: "2-digit", second: "2-digit" });
const short = id => (id || "").slice(0, 8);
const el = (tag, cls, html) => { const n = document.createElement(tag); if (cls) n.className = cls; if (html != null) n.innerHTML = html; return n; };

// Header
const t0 = new Date(ev[0].at), t1 = new Date(ev[ev.length - 1].at);
const mins = Math.round((t1 - t0) / 60000);
document.getElementById("sub").innerHTML =
  `Sesión <code>${esc(short(DATA.session_id))}</code> · estado <b>${esc(DATA.status)}</b> · ${t0.toLocaleDateString("es")} ` +
  `${hhmm(t0)} → ${hhmm(t1)} (${mins} min) · ${ev.length} eventos`;

const agentsBox = document.getElementById("agents");
order.forEach(id => {
  const a = agents[id];
  const n = el("div", "agent");
  n.style.setProperty("--c", a.color);
  n.innerHTML = `<div class="name">${esc(a.name)}<span class="role">${id === coordinator ? "coordinador" : "worker"}</span></div>
    <div class="meta">${esc(a.product)} ${esc(a.version)}</div><div class="meta">${esc(a.model)}</div>`;
  agentsBox.appendChild(n);
});

// Acks and messages
const acks = {};
ev.filter(e => e.type === "ack").forEach(e => (acks[e.data] = acks[e.data] || []).push(e));
const messages = ev.filter(e => e.type === "message");
const isAuto = b => /^(Task \S+ assigned to you|Review requested for task)/.test(b);
const count = t => ev.filter(e => e.type === t).length;
const stats = [
  [messages.filter(m => !isAuto(m.data.body)).length, "mensajes escritos"],
  [messages.filter(m => isAuto(m.data.body)).length, "notificaciones automáticas"],
  [count("ack"), "ACK"],
  [count("task_created"), "tareas"],
  [count("review_submitted"), "revisiones"],
  [count("task_completed"), "completadas"],
  [count("wait_timeout"), "esperas vacías"],
  [count("agent_unreachable"), "unreachable"],
];
document.getElementById("stats").innerHTML = stats.map(([n, l]) => `<div class="stat"><b>${n}</b><span>${l}</span></div>`).join("");

// Activity lanes
const span = Math.max(1, t1 - t0);
const pos = t => ((new Date(t) - t0) / span * 100).toFixed(2) + "%";
const lanes = document.getElementById("lanes");
order.forEach(id => {
  const a = agents[id];
  const row = el("div", "lane");
  row.style.setProperty("--c", a.color);
  const ticks = ev.filter(e => e.actor === id && e.type === "call").map(e => `<div class="tick" style="left:${pos(e.at)}" title="${esc(e.data && e.data.op)} ${hhmm(e.at)}"></div>`)
    .concat(ev.filter(e => e.actor === id && e.type === "message").map(e => `<div class="tick msg" style="left:${pos(e.at)}" title="mensaje ${hhmm(e.at)}"></div>`))
    .concat(ev.filter(e => e.type === "agent_unreachable" && e.data.agent_id === id).map(e => `<div class="tick down" style="left:${pos(e.at)}" title="unreachable ${hhmm(e.at)}"></div>`));
  row.innerHTML = `<div class="lbl">${esc(a.name)}</div><div class="track">${ticks.join("")}</div>`;
  lanes.appendChild(row);
});
lanes.insertAdjacentHTML("beforeend", `<div class="axis"><div></div><div class="ends"><span>${hhmm(t0)}</span><span>${hhmm(new Date(+t0 + span / 2))}</span><span>${hhmm(t1)}</span></div></div>
  <div class="legend"><i style="background:var(--c1);opacity:.55"></i>llamada MCP<i style="background:var(--c1)"></i>mensaje<i style="background:var(--warn)"></i>lease vencido (unreachable)</div>`);

// Tasks
const tasks = {};
const taskName = {};
ev.forEach(e => {
  const task = e.data && e.data.task;
  if (e.type === "task_created" && e.data.id) { tasks[e.data.id] = { title: e.data.title, acceptance: e.data.acceptance, steps: [] }; }
  const id = (task && task.id) || (e.type === "task_created" && e.data.id);
  if (!id || !e.type.startsWith("task_") && !e.type.startsWith("review_") && e.type !== "artifact_published") return;
  const t = tasks[id] = tasks[id] || { title: task && task.title, steps: [] };
  if (task) { t.title = task.title || t.title; t.state = task.state; t.assignee = task.assignee || t.assignee; }
  t.steps.push(e);
});
// review_submitted and artifact_published carry artifact ids, not task ids.
const artifactTask = {};
ev.forEach(e => {
  if (e.type === "review_requested" && e.data.task) artifactTask[e.data.artifact_id] = e.data.task.id;
  if (e.type === "artifact_published" && e.data.task_id) artifactTask[e.data.id] = e.data.task_id;
});
ev.forEach(e => {
  if (e.type === "review_submitted" || (e.type === "artifact_published" && !(e.data.task && e.data.task.id))) {
    const tid = artifactTask[e.data.artifact_id || e.data.id];
    if (tid && tasks[tid]) tasks[tid].steps.push(e);
  }
});
Object.values(tasks).forEach(t => t.steps.sort((a, b) => a.seq - b.seq));
Object.entries(tasks).forEach(([id, t]) => (taskName[id] = t.title || short(id)));
const stepLabel = e => ({
  task_created: "creada", task_assigned: "asignada a " + who(e.data.task ? e.data.task.assignee : e.data.assignee).name,
  task_accepted: "aceptada", task_updated: "estado → " + (e.data.task ? e.data.task.state : "?"),
  artifact_published: "artefacto publicado", review_requested: "pide revisión",
  review_submitted: "revisión: " + (e.data.verdict || "?"), task_completed: "completada",
}[e.type] || e.type) + ` <span style="color:var(--muted)">· ${esc(who(e.actor).name)}</span>`;
const tasksBox = document.getElementById("tasks");
const tlist = Object.entries(tasks);
if (!tlist.length) tasksBox.innerHTML = `<div class="card empty">Sin tareas.</div>`;
tlist.forEach(([id, t]) => {
  const c = el("div", "card");
  c.innerHTML = `<h3>${esc(t.title || short(id))}</h3>
    <span class="state ${esc(t.state)}">${esc(t.state || "proposed")}</span>
    ${t.assignee ? ` <span style="color:${who(t.assignee).color};font-size:12.5px;font-weight:600">${esc(who(t.assignee).name)}</span>` : ""}
    <ul class="steps">${t.steps.map(s => `<li><span>${hhmm(s.at)}</span><span>${stepLabel(s)}</span></li>`).join("")}</ul>`;
  tasksBox.appendChild(c);
});

// Feed
const filters = { conv: "Conversación", all: "Todo (incluye llamadas y esperas)" };
let mode = "conv";
const toolbar = document.getElementById("toolbar");
Object.entries(filters).forEach(([k, label]) => {
  const b = el("button", null, esc(label));
  b.setAttribute("aria-pressed", k === mode);
  b.onclick = () => { mode = k; [...toolbar.children].forEach(x => x.setAttribute("aria-pressed", x === b)); render(); };
  toolbar.appendChild(b);
});
const tname = e => { const id = e.data && e.data.task ? e.data.task.id : e.data && e.data.id; return esc(taskName[id] || short(id)); };
function eventLine(e) {
  const a = who(e.actor).name;
  switch (e.type) {
    case "session_created": return ["", `Sesión creada`];
    case "agent_registered": return ["quiet", `Registrado <b>${esc(e.data.name)}</b> (${esc(e.data.product)})`];
    case "session_started": return ["good", `Sesión iniciada`];
    case "session_closed": return ["", `Sesión cerrada`];
    case "task_created": return ["", `<b>${esc(a)}</b> creó la tarea <b>${tname(e)}</b>`];
    case "task_assigned": return ["", `<b>${esc(a)}</b> asignó <b>${tname(e)}</b> a <b>${esc(who(e.data.assignee || (e.data.task && e.data.task.assignee)).name)}</b>`];
    case "task_accepted": return ["good", `<b>${esc(a)}</b> aceptó <b>${tname(e)}</b>`];
    case "task_updated": return ["", `<b>${esc(a)}</b> → ${esc(e.data.task && e.data.task.state)} en <b>${tname(e)}</b>`];
    case "artifact_published": return ["", `<b>${esc(a)}</b> publicó un artefacto <code>${esc(e.data.kind)}</code> · digest <code>${esc((e.data.digest || "").slice(0, 10))}…</code>`];
    case "review_requested": return ["", `<b>${esc(a)}</b> pidió revisión de <b>${tname(e)}</b>`];
    case "review_submitted": return [e.data.verdict === "approved" ? "good" : "warn", `<b>${esc(a)}</b> revisó: <b>${esc(e.data.verdict)}</b>${e.data.reason ? " — " + esc(e.data.reason) : ""}`];
    case "task_completed": return ["good", `✓ <b>${tname(e)}</b> completada`];
    case "agent_unreachable": return ["warn", `⚠ <b>${esc(who(e.data.agent_id).name)}</b> sin llamadas en 90 s (unreachable)`];
    case "activity": return ["quiet", `${esc(a)} · actividad: ${esc(e.data)}`];
    case "leadership_proposed": case "leadership_changed": case "vote_cast": return ["", `<b>${esc(a)}</b> · ${esc(e.type)}`];
    case "call": return ["quiet", `${esc(a)} · ${esc(e.data && e.data.op)}`];
    case "wait_timeout": return ["quiet", `${esc(a)} · espera vacía`];
    case "delivery": return ["quiet", `${esc(a)} · recibió ${e.data.length} mensaje(s)`];
    default: return ["quiet", `${esc(a)} · ${esc(e.type)}`];
  }
}
const CONV = new Set(["session_created", "session_started", "session_closed", "task_created", "task_assigned", "task_accepted",
  "task_updated", "artifact_published", "review_requested", "review_submitted", "task_completed", "agent_unreachable",
  "leadership_proposed", "leadership_changed", "vote_cast"]);
function render() {
  const feed = document.getElementById("feed");
  feed.innerHTML = "";
  ev.forEach(e => {
    if (e.type === "ack") return;
    if (e.type === "message") {
      const d = e.data, from = who(d.sender), auto = isAuto(d.body);
      const n = el("div", "msg" + (d.sender === coordinator ? " right" : ""));
      n.style.setProperty("--c", from.color);
      const to = (d.recipients || []).map(r => esc(who(r).name)).join(", ");
      const ack = (acks[d.id] || []).map(x => `${esc(who(x.actor).name)} ${hhmm(x.at)}`);
      n.innerHTML = `<div class="avatar">${esc(from.name.split(/[-_ ]/).map(p => p[0]).join("").slice(0, 2).toUpperCase())}</div>
        <div class="bubble${auto ? " auto" : ""}">
          <div class="head"><span class="from">${esc(from.name)}</span><span>→ ${to}</span>
            <span class="time">${hhmm(e.at)}</span>${auto ? `<span class="tag">automático</span>` : ""}</div>
          <div class="body">${esc(d.body)}</div>
          <div class="acks${ack.length ? "" : " none"}">${ack.length ? "✓ ACK " + ack.join(" · ") : "sin ACK"}</div>
        </div>`;
      feed.appendChild(n);
      const body = n.querySelector(".body");
      requestAnimationFrame(() => {
        if (body.scrollHeight > 160) {
          body.classList.add("clamp");
          const more = el("button", "more", "Ver todo");
          more.onclick = () => { const c = body.classList.toggle("clamp"); more.textContent = c ? "Ver todo" : "Ver menos"; };
          body.after(more);
        }
      });
      return;
    }
    if (mode === "conv" && !CONV.has(e.type)) return;
    const [cls, html] = eventLine(e);
    feed.appendChild(el("div", "event " + cls, `<span class="time">${hhmm(e.at)}</span> · ${html}`));
  });
}
render();
</script>
</body>
</html>
"""


def main():
    if len(sys.argv) != 3:
        sys.exit(__doc__)
    data = json.loads(pathlib.Path(sys.argv[1]).read_text())
    blob = json.dumps(data, ensure_ascii=False).replace("</", "<\\/")
    pathlib.Path(sys.argv[2]).write_text(TEMPLATE.replace("__DATA__", blob))


if __name__ == "__main__":
    main()
