// NAC pilot console. Plain JavaScript, no dependencies. Every value from the
// API is rendered with textContent (never innerHTML), so certificate fields,
// switch names or reasons cannot inject markup.
"use strict";

const $main = document.getElementById("main");
// Render a page: like replaceChildren, but optional sections may be null.
const show = (...kids) => $main.replaceChildren(...kids.flat().filter((k) => k !== null && k !== undefined && k !== false));
let token = sessionStorage.getItem("nac.token") || "";
let me = sessionStorage.getItem("nac.email") || "";
let pollTimer = null;

// ---------- DOM helpers ----------
function h(tag, attrs, ...kids) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs || {})) {
    if (v === undefined || v === null || v === false) continue;
    if (k.startsWith("on")) el.addEventListener(k.slice(2), v);
    else if (k === "class") el.className = v;
    else el.setAttribute(k, v === true ? "" : v);
  }
  for (const kid of kids.flat()) {
    if (kid === undefined || kid === null || kid === false) continue;
    el.append(kid instanceof Node ? kid : document.createTextNode(String(kid)));
  }
  return el;
}
const txt = (v) => (v === undefined || v === null || v === "" ? "—" : String(v));
const when = (t) => (t ? new Date(t).toLocaleString() : "—");
const ago = (t) => {
  if (!t) return "never";
  const s = Math.round((Date.now() - new Date(t)) / 1000);
  if (s < 60) return s + "s ago"; if (s < 3600) return Math.round(s / 60) + "m ago";
  if (s < 86400) return Math.round(s / 3600) + "h ago"; return Math.round(s / 86400) + "d ago";
};
function chip(text, kind) { return h("span", { class: "chip " + (kind || "") }, text); }
function stateChip(s) {
  const map = { yes: "ok", no: "bad", unknown: "warn", pending: "warn", active: "ok", rejected: "bad", ended: "",
    accept: "ok", reject: "bad", info: "", error: "bad", present: "ok", missing: "warn", stale: "warn",
    observed: "ok", acked: "ok", nak: "bad", timeout: "bad", failed: "bad", sent: "warn", requested: "warn",
    fresh: "ok", unavailable: "warn", connected: "ok", absent: "bad", matched: "ok", not_matched: "", insufficient_evidence: "warn",
    disabled: "", not_reached: "", not_observed: "warn" };
  return chip(s, map[s] ?? "");
}
// ask shows an in-page confirmation and resolves true/false. It replaces the
// native window.confirm(), which blocks the whole browser (and automation)
// and can be silently suppressed by the browser. Stable ids for automation:
// #confirm-dialog, #confirm-ok, #confirm-cancel.
function ask(message, okLabel = "Confirm") {
  return new Promise((resolve) => {
    document.getElementById("confirm-dialog")?.remove();
    const msg = h("p", { class: "dialog-msg" }, message);
    const cancel = h("button", { id: "confirm-cancel", type: "button" }, "Cancel");
    const ok = h("button", { id: "confirm-ok", type: "button", class: "primary" }, okLabel);
    const dlg = h("dialog", { id: "confirm-dialog", "aria-labelledby": "confirm-title" },
      h("h2", { id: "confirm-title" }, "Please confirm"), msg, h("div", { class: "row" }, cancel, ok));
    let done = false;
    const finish = (v) => { if (done) return; done = true; if (dlg.open) dlg.close(); dlg.remove(); resolve(v); };
    cancel.addEventListener("click", () => finish(false));
    ok.addEventListener("click", () => finish(true));
    dlg.addEventListener("cancel", (e) => { e.preventDefault(); finish(false); }); // Escape
    document.body.append(dlg);
    dlg.showModal();
    ok.focus();
  });
}

function flash(msg, kind) {
  const box = document.getElementById("flash");
  const d = h("div", { class: kind || "info" }, msg);
  box.append(d);
  setTimeout(() => d.remove(), kind === "err" ? 12000 : 5000);
}
function panel(title, ...kids) { return h("section", { class: "panel" }, title ? h("h2", {}, title) : null, ...kids); }
function kv(pairs) {
  return h("dl", { class: "kv" }, pairs.flatMap(([k, v]) => [h("dt", {}, k), h("dd", {}, v instanceof Node ? v : txt(v))]));
}
function table(cols, rows, onRow) {
  return h("table", {}, h("thead", {}, h("tr", {}, cols.map((c) => h("th", {}, c)))),
    h("tbody", {}, rows.length ? rows.map((r) => {
      const tr = h("tr", { class: onRow ? "click" : "" }, r.cells.map((c) => h("td", {}, c instanceof Node ? c : txt(c))));
      if (onRow) tr.addEventListener("click", () => onRow(r));
      return tr;
    }) : h("tr", {}, h("td", { colspan: cols.length, class: "muted" }, "Nothing yet."))));
}
function field(label, input) { return h("div", {}, h("label", {}, label), input); }

// ---------- API ----------
async function api(method, path, body) {
  const opts = { method, headers: { Authorization: "Bearer " + token } };
  if (body !== undefined) { opts.headers["Content-Type"] = "application/json"; opts.body = JSON.stringify(body); }
  const res = await fetch("/api/v1" + path, opts);
  if (res.status === 401) { logout(); throw new Error("Your session expired. Sign in again."); }
  const ct = res.headers.get("Content-Type") || "";
  const data = ct.includes("json") ? await res.json() : await res.text();
  if (!res.ok) {
    const msg = data && data.detail ? data.title + ": " + data.detail : "HTTP " + res.status;
    throw new Error(msg);
  }
  return data;
}
async function act(fn, okMsg) {
  try { const r = await fn(); if (okMsg) flash(okMsg); return r; }
  catch (e) { flash(e.message, "err"); return null; }
}

// ---------- routing ----------
const routes = {
  "": overview, overview, switches, switch: switchDetail, sessions, session: sessionDetail,
  endpoints, policy, events, account,
};
function go(hash) { location.hash = hash; }
async function render() {
  clearInterval(pollTimer);
  if (!token) return loginView();
  document.getElementById("nav").hidden = false;
  document.getElementById("who").replaceChildren(h("a", { href: "#/account", title: "Account and password" }, me), " ",
    h("button", { onclick: logout }, "Sign out"));
  const [name, arg] = location.hash.replace(/^#\/?/, "").split("/");
  document.querySelectorAll("nav a").forEach((a) => a.classList.toggle("active", a.getAttribute("href") === "#/" + (name || "overview")));
  const view = routes[name] || overview;
  try { await view(arg); } catch (e) { show(panel("Error", h("p", { class: "bad" }, e.message))); }
}
window.addEventListener("hashchange", render);

function logout() {
  token = ""; me = "";
  sessionStorage.removeItem("nac.token"); sessionStorage.removeItem("nac.email");
  document.getElementById("nav").hidden = true;
  document.getElementById("who").replaceChildren();
  loginView();
}

function loginView() {
  const email = h("input", { type: "email", autocomplete: "username", required: true });
  const pw = h("input", { type: "password", autocomplete: "current-password", required: true });
  const form = h("form", { class: "panel login", onsubmit: async (e) => {
    e.preventDefault();
    try {
      const res = await fetch("/api/v1/auth/login", { method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email: email.value, password: pw.value }) });
      if (!res.ok) throw new Error("Sign-in failed: check email and password.");
      const d = await res.json();
      token = d.token; me = email.value;
      sessionStorage.setItem("nac.token", token); sessionStorage.setItem("nac.email", me);
      render();
    } catch (err) { flash(err.message, "err"); }
  } }, h("h1", {}, "Sign in"), field("Email", email), field("Password", pw), h("p", {}, h("button", { class: "primary", type: "submit" }, "Sign in")));
  show(form);
}

// ---------- Account ----------
async function account() {
  const d = await api("GET", "/auth/me");
  const cur = h("input", { type: "password", autocomplete: "current-password", required: true });
  const nw = h("input", { type: "password", autocomplete: "new-password", required: true, minlength: 12 });
  const rep = h("input", { type: "password", autocomplete: "new-password", required: true, minlength: 12 });
  const form = h("form", { onsubmit: async (e) => {
    e.preventDefault();
    if (nw.value !== rep.value) { flash("The new passwords differ; nothing was changed.", "err"); return; }
    const r = await act(() => api("POST", "/auth/password", { current_password: cur.value, new_password: nw.value }),
      "Password changed. Every other session of this account is signed out.");
    if (r) { token = r.token; sessionStorage.setItem("nac.token", token); render(); }
  } }, field("Current password", cur), field("New password (at least 12 characters)", nw), field("Repeat new password", rep),
    h("p", {}, h("button", { class: "primary", type: "submit" }, "Change password")));
  show(panel("Account", kv([["Email", d.email], ["Role", d.role], ["Permissions", d.permissions.join(", ")],
    ["Password last changed", d.password_changed_at ? when(d.password_changed_at) : "never (initial password)"]])),
  panel("Change password", h("p", { class: "muted" },
    "Changing the password signs out every other session of this account. Forgot it? On the control node run: sudo nac-reset-password " + d.email),
  form));
}

// ---------- Overview ----------
async function overview() {
  const [health, sw, rejects] = await Promise.all([api("GET", "/radius/health"), api("GET", "/switches"),
    api("GET", "/auth-events?result=reject&limit=10")]);
  const ev = (e) => e ? h("span", {}, h("span", { class: "muted" }, when(e.occurred_at) + " — "), e.reason) : "none";
  show(
    h("h1", {}, "Overview"),
    panel("RADIUS integration", kv([
      ["Last NAC decision", ev(health.last_decision)],
      ["Last certificate (EAP-TLS) reject", ev(health.last_eap_tls_reject)],
      ["Last outage reject", ev(health.last_outage_reject)],
      ["Outage log import", h("span", {}, stateChip(health.outage_log_import.state || "unknown"), " ", txt(health.outage_log_import.detail))],
      ["FreeRADIUS client file", h("span", {}, stateChip(health.radius_client_file.state || "unknown"), " ", txt(health.radius_client_file.detail))],
      ["Loaded by FreeRADIUS", h("span", {}, stateChip(health.freeradius_load?.state || "unknown"), " ", txt(health.freeradius_load?.detail))],
    ]), h("p", { class: "note" }, health.outage_semantics)),
    panel("Switches", table(["Switch", "Configured", "Reachable", "Auth request seen", "Verified (record)"],
      sw.switches.map((s) => ({ id: s.id, cells: [s.name, stateChip(s.status.configured.state), stateChip(s.status.reachable.state),
        stateChip(s.status.auth_request_seen.state), stateChip(s.status.verified.state)] })), (r) => go("#/switch/" + r.id))),
    panel("Recent rejections", eventsTable(rejects.events)));
}

// ---------- Switches ----------
async function switches() {
  const d = await api("GET", "/switches");
  show(h("h1", {}, "Switches"),
    panel(null, table(["Name", "RADIUS source", "Segments", "Configured", "Reachable", "Auth seen", "Verified (record)"],
      d.switches.map((s) => ({ id: s.id, cells: [s.name, h("code", {}, s.radius_source_ip), segmentsOf(s),
        stateChip(s.status.configured.state), stateChip(s.status.reachable.state), stateChip(s.status.auth_request_seen.state),
        stateChip(s.status.verified.state)] })), (r) => go("#/switch/" + r.id))),
    switchForm(null));
}
const vlans = (m) => Object.entries(m || {}).sort((a, b) => a[1] - b[1]).map(([k, v]) => k + "=" + v).join(", ");
const isRole = (sw) => sw?.enforcement === "role";
// A role switch maps segments to local roles; its VLAN never changes.
const segmentsOf = (sw) => isRole(sw)
  ? "roles " + Object.entries(sw.segment_roles || {}).map(([k, v]) => k + "=" + v).join(", ")
  : vlans(sw.segment_vlans);

function switchForm(sw) {
  const f = {
    name: h("input", { value: sw?.name || "", placeholder: "access-sw-1" }),
    src: h("input", { value: sw?.radius_source_ip || "", placeholder: "10.70.99.1" }),
    nas: h("input", { value: sw?.nas_ip || "", placeholder: "same as source" }),
    secret: h("input", { type: "password", autocomplete: "new-password", placeholder: sw ? "leave empty to keep" : "16+ random characters" }),
    platform: h("select", {},
      h("option", { value: "arista-eos", selected: (sw?.platform || "arista-eos") === "arista-eos" }, "Arista EOS (VLAN assignment)"),
      h("option", { value: "aruba-aos-cx", selected: sw?.platform === "aruba-aos-cx" }, "Aruba AOS-CX (local role; VLAN and IP unchanged)")),
    rcorp: h("input", { value: sw?.segment_roles?.corp || "nac-corp" }),
    rquar: h("input", { value: sw?.segment_roles?.quarantine || "nac-quarantine" }),
    avlan: h("input", { type: "number", min: 1, max: 4094, value: sw?.access_vlan ?? 10 }),
    corp: h("input", { type: "number", min: 1, max: 4094, value: sw?.segment_vlans?.corp ?? 10 }),
    quar: h("input", { type: "number", min: 1, max: 4094, value: sw?.segment_vlans?.quarantine ?? 20 }),
    ports: h("input", { value: (sw?.access_ports || ["Ethernet1"]).join(", ") }),
    server: h("input", { value: sw?.radius_server_ip || "", placeholder: "FreeRADIUS IP the switch sends to" }),
    coa: h("input", { type: "number", value: sw?.coa_port || 3799 }),
    eapi: h("input", { value: sw?.eapi_url || "", placeholder: "optional: https://10.70.99.1" }),
    eu: h("input", { value: sw?.eapi_username || "", placeholder: "optional" }),
    ep: h("input", { type: "password", autocomplete: "new-password", placeholder: sw?.eapi_password_set ? "leave empty to keep" : "optional" }),
  };
  // Segments other than corp/quarantine (added through the API) are not
  // editable here and must survive a save unchanged.
  const extraSegments = Object.fromEntries(Object.entries(sw?.segment_vlans || {}).filter(([k]) => k !== "corp" && k !== "quarantine"));
  const extraRoles = Object.fromEntries(Object.entries(sw?.segment_roles || {}).filter(([k]) => k !== "corp" && k !== "quarantine"));
  const aruba = () => f.platform.value === "aruba-aos-cx";
  const submit = async (e) => {
    e.preventDefault();
    const body = { name: f.name.value.trim(), platform: f.platform.value, radius_source_ip: f.src.value.trim(), nas_ip: f.nas.value.trim(),
      radius_secret: f.secret.value, coa_port: Number(f.coa.value),
      access_ports: f.ports.value.split(",").map((s) => s.trim()).filter(Boolean), radius_server_ip: f.server.value.trim() };
    if (aruba()) {
      Object.assign(body, { segment_roles: { ...extraRoles, corp: f.rcorp.value.trim(), quarantine: f.rquar.value.trim() },
        access_vlan: Number(f.avlan.value), clear_eapi: true });
    } else {
      Object.assign(body, { segment_vlans: { ...extraSegments, corp: Number(f.corp.value), quarantine: Number(f.quar.value) },
        eapi_url: f.eapi.value.trim(), eapi_username: f.eu.value.trim(), eapi_password: f.ep.value, clear_eapi: false });
    }
    const r = await act(() => sw ? api("PUT", "/switches/" + sw.id, body) : api("POST", "/switches", body),
      sw ? "Switch updated." : "Switch registered. Apply the generated switch configuration next.");
    if (r) go("#/switch/" + r.id);
  };
  const vlanFields = h("div", { class: "form" }, field("Corp VLAN", f.corp), field("Quarantine VLAN", f.quar),
    Object.keys(extraSegments).length ? field("Other segments (kept on save; edit through the API)",
      h("div", { class: "mono" }, vlans(extraSegments))) : null,
    field("eAPI URL (session observation)", f.eapi), field("eAPI username", f.eu), field("eAPI password", f.ep));
  const roleFields = h("div", { class: "form" }, field("Corp role (local role on the switch)", f.rcorp),
    field("Quarantine role (local role on the switch)", f.rquar), field("Access VLAN (unchanged by NAC)", f.avlan),
    h("p", { class: "note" }, "NAC returns the role name in Aruba-User-Role. The role and its policy must exist on the switch; " +
      "what the quarantine role blocks is switch configuration, not NAC. NAC does not observe AOS-CX sessions."));
  const sync = () => {
    vlanFields.style.display = aruba() ? "none" : ""; roleFields.style.display = aruba() ? "" : "none";
    if (!sw && aruba() && f.ports.value === "Ethernet1") f.ports.value = "1/1/1";
    if (!sw && !aruba() && f.ports.value === "1/1/1") f.ports.value = "Ethernet1";
  };
  f.platform.addEventListener("change", sync);
  sync();
  return panel(sw ? "Edit switch" : "Register a switch",
    h("form", { onsubmit: submit },
      h("div", { class: "form" },
        field("Name", f.name), field("Platform", f.platform), field("RADIUS source IP (as FreeRADIUS sees it)", f.src), field("NAS-IP-Address for CoA", f.nas),
        field("RADIUS shared secret", f.secret),
        field("Access ports (comma-separated)", f.ports), field("FreeRADIUS address for the switch config", f.server),
        field("CoA UDP port", f.coa)),
      vlanFields, roleFields,
      h("p", {}, h("button", { class: "primary", type: "submit" }, sw ? "Save" : "Register"))));
}

async function switchDetail(id) {
  const sw = await api("GET", "/switches/" + id);
  const cfg = await api("GET", "/switches/" + id + "/config");
  const st = sw.status;
  const item = (label, s) => h("div", { class: "panel" }, h("h3", {}, label, " ", stateChip(s.state)), h("p", {}, txt(s.detail)),
    s.note ? h("p", { class: s.historical ? "warn" : "" }, s.note) : null, s.at ? h("p", { class: "muted" }, (s.historical ? "recorded " : "") + when(s.at)) : null);
  const sessions = await api("GET", "/sessions?switch_id=" + id);
  const pre = h("pre", {}, cfg);
  show(
    h("h1", {}, "Switch ", sw.name),
    h("div", { class: "grid" }, item("1. Configured", st.configured), item("2. Reachable (management)", st.reachable),
      item("3. Authentication request seen", st.auth_request_seen), item("4. Verified test (record)", st.verified)),
    panel("Actions", h("div", { class: "row" },
      isRole(sw) ? null : h("button", { onclick: async () => { if (await act(() => api("POST", "/switches/" + id + "/probe"), "Probe finished.")) render(); } }, "Probe management (eAPI)"),
      isRole(sw) ? null : h("button", { onclick: async () => { if (await act(() => api("POST", "/switches/" + id + "/observe"), "Switch state refreshed.")) render(); } }, "Refresh session observation"),
      h("button", { class: "primary", onclick: async () => { if (await act(() => api("POST", "/switches/" + id + "/verify"), "Verification recorded.")) render(); } }, "Verify (after a test login)"),
      h("button", { class: "danger", onclick: async () => {
        if (!await ask("Delete switch " + sw.name + "? Its sessions are removed and FreeRADIUS stops accepting it.")) return;
        if (await act(() => api("DELETE", "/switches/" + id), "Switch deleted.") !== null) go("#/switches");
      } }, "Delete")),
      h("p", { class: "muted" }, "FreeRADIUS client file: ", txt(st.radius_client_file.detail)),
      h("p", {}, "Loaded by FreeRADIUS: ", stateChip(st.freeradius_load.state), " ", txt(st.freeradius_load.detail))),
    panel(isRole(sw) ? "Generated AOS-CX reference configuration (lab: ArubaOS-CX Virtual 10.07)" : "Generated EOS configuration (validated on EOS 4.29.2F)",
      h("p", { class: "note" }, "The RADIUS key is a placeholder. Replace <RADIUS_SHARED_SECRET> with the secret you registered, or download the configuration with the key."),
      h("div", { class: "row" },
        h("button", { onclick: () => navigator.clipboard.writeText(cfg).then(() => flash("Copied.")) }, "Copy"),
        h("button", { onclick: async () => {
          if (!await ask("Show the configuration including the RADIUS secret? This is recorded in the audit log.")) return;
          const full = await act(() => api("GET", "/switches/" + id + "/config?include_secret=true"));
          if (full) pre.textContent = full;
        } }, "Show with secret")),
      pre),
    panel("Sessions on this switch", sessionsTable(sessions.sessions)),
    switchForm(sw));
  // Until FreeRADIUS confirms the load, check the status only, and re-render
  // once it settles, unless the operator is typing in the form below.
  if (st.configured.state === "pending") pollTimer = setInterval(async () => {
    const cur = await api("GET", "/switches/" + id);
    if (cur.status.configured.state !== "pending" && !document.activeElement?.closest("form")) render();
  }, 3000);
}

// ---------- Sessions ----------
function observedCell(s) {
  const o = s.observed;
  if (o.freshness !== "fresh") return h("span", {}, stateChip(o.freshness), " ", o.status ? o.status + " VLAN " + txt(o.vlan) : "");
  const m = o.matches_requested;
  return h("span", {}, chip(o.status + " VLAN " + txt(o.vlan), m === false ? "bad" : "ok"), m === false ? " mismatch" : "");
}
function sessionsTable(list) {
  return table(["Last auth", "Switch / port", "MAC", "Identity (cert CN)", "State", "Requested", "Observed on switch", "Last CoA"],
    list.map((s) => ({ id: s.id, cells: [ago(s.last_auth_at), s.switch_name + " " + s.nas_port_id, h("code", {}, s.mac),
      s.cert_cn || h("span", { class: "muted" }, "no validated certificate"), stateChip(s.state),
      s.requested.vlan ? (s.requested.segment + " VLAN " + s.requested.vlan + (s.override ? " (override)" : "")) : txt(s.requested.action),
      observedCell(s), s.last_coa ? h("span", {}, s.last_coa.kind, " ", stateChip(s.last_coa.state)) : "—"] })),
    (r) => go("#/session/" + r.id));
}
async function sessions() {
  const q = h("input", { placeholder: "MAC, certificate CN or port", value: sessionStorage.getItem("nac.q") || "" });
  const state = h("select", {}, ["", "active", "rejected", "ended"].map((v) => h("option", { value: v }, v || "any state")));
  state.value = sessionStorage.getItem("nac.state") || "";
  const out = h("div");
  const load = async () => {
    sessionStorage.setItem("nac.q", q.value); sessionStorage.setItem("nac.state", state.value);
    const d = await api("GET", "/sessions?q=" + encodeURIComponent(q.value) + "&state=" + state.value);
    out.replaceChildren(sessionsTable(d.sessions));
  };
  show(h("h1", {}, "Sessions"),
    panel(null, h("form", { class: "row", onsubmit: (e) => { e.preventDefault(); load(); } }, q, state, h("button", { type: "submit" }, "Search")), out));
  await load();
  pollTimer = setInterval(load, 5000);
}

async function sessionDetail(id) {
  const [d, epList] = await Promise.all([api("GET", "/sessions/" + id), api("GET", "/endpoints?limit=200")]);
  const eps = epList.items || [];
  const ld = d.last_decision;
  const coaBusy = d.last_coa && ["requested", "sent"].includes(d.last_coa.state);
  const role = d.enforcement?.strategy === "role";
  // On a role switch nothing re-authenticates: the action sends a role CoA
  // for the role the active policy selects, so it is labelled for what it does.
  const reauthLabel = role ? "Apply active policy" : "Re-authenticate (apply active policy)";
  const coaBtn = (kind, label, cls, question) => h("button", { class: cls, disabled: d.state !== "active" || coaBusy, onclick: async () => {
    const live = d.liveness.state === "connected" ? "" : "\n\nNote: " + d.liveness.note;
    const pause = role ? "\n\nThe CoA asks the switch to change this session's local role (Aruba-User-Role). The VLAN and the endpoint's " +
      "address do not change and the switch does not re-authenticate. A CoA-ACK means the switch accepted the request; NAC does not " +
      "observe AOS-CX sessions and does not verify that traffic is restricted. What the role blocks is switch configuration: in the lab, " +
      "new connections to tested peers were denied, but existing TCP connections were not torn down. Check the switch and test traffic." : "\n\nThe switch re-authenticates the session. In the pilot lab (vEOS-lab 4.29.2F) this interrupted endpoint " +
      "traffic for up to about 58 s, also when the VLAN did not change. After a VLAN change the endpoint needs an address " +
      "in the new VLAN: in the lab, Windows Server 2025 got one within about 65 s, but a Linux (systemd-networkd) endpoint " +
      "only when its DHCP lease expired, which can take the remaining lease time. Check the endpoint, not only this page.";
    if (!await ask((question || label + " this session") + " on " + d.switch_name + " " + d.nas_port_id + " (" + d.mac + ")?" + live + pause, label)) return;
    if (await act(() => api("POST", "/sessions/" + id + "/" + kind), "Requested. Following the CoA…")) render();
  } }, label);

  const inputs = ld ? Object.entries(ld.inputs).sort().map(([k, v]) => ({ cells: [h("code", {}, k), txt(v.value), stateChip(v.status), txt(v.source),
    v.authenticated ? "yes" : "no", v.observed_at ? when(v.observed_at) : "—", txt(v.note)] })) : [];
  const trace = ld ? ld.trace.map((r) => ({ cells: [r.priority, r.rule, r.action, stateChip(r.outcome),
    h("div", {}, (r.conditions || []).map((c) => h("div", { class: "mono" }, c.field + " " + c.operator + " " + (c.values || []).join("|") +
      " → " + c.outcome + " (observed: " + txt(c.observed) + ", " + c.evidence + ")")))] })) : [];

  const bind = (() => {
    if (d.binding || !d.cert_serial) return null;
    const sel = h("select", {}, eps.length === 0 ? h("option", { value: "" }, "no device records") : null,
      (eps || []).map((e) => h("option", { value: e.id }, e.mac + " " + (e.hostname || ""))));
    return panel("Bind this certificate to a device",
      h("p", {}, "The certificate is valid but not bound to a device record, so identity.bound is false. Binding uses issuer ", h("code", {}, d.cert_issuer),
        " and serial ", h("code", {}, d.cert_serial), "."),
      h("div", { class: "row" }, sel, h("button", { class: "primary", onclick: async () => {
        if (await act(() => api("POST", "/sessions/" + id + "/bind", { endpoint_id: sel.value }), "Bound. It applies at the next authentication.")) render();
      } }, "Bind"), h("a", { href: "#/endpoints" }, "create a device record")));
  })();

  show(
    h("h1", {}, "Session ", h("code", {}, d.mac), " on ", d.switch_name, " ", d.nas_port_id, " ", stateChip(d.state)),
    panel("Why", h("ul", {}, d.explanation.map((l) => h("li", {}, l)))),
    h("div", { class: "grid" },
      panel("1. Switch authorization (requested by NAC)", kv([["Action", d.requested.action], ["Segment", d.requested.segment], role ? ["Role", d.requested.role] : ["VLAN", d.requested.vlan],
        ["Override", d.override ? d.override + " by " + d.override_by : "none"], ["Decision", d.requested.decision_id], ["At", when(d.requested.at)]])),
      panel("2. Observed on the switch", kv([["Still connected?", h("span", {}, stateChip(d.liveness.state), " ", d.liveness.note)],
        ["Freshness", stateChip(d.observed.freshness)], ["Status", d.observed.status], ["VLAN", d.observed.vlan],
        ["Source", d.observed.source], ["At", when(d.observed.at)], ["Note", d.observed.note]])),
      panel("3. Endpoint reachability", kv([["State", stateChip(d.endpoint_reachability.state)],
        ["Note", d.endpoint_reachability.note]])),
      panel("Identity and device", kv([["Certificate CN", d.cert_cn], ["Serial", d.cert_serial ? h("code", {}, d.cert_serial) : "—"],
        ["Issuer", d.cert_issuer], ["Outer identity (untrusted)", d.user_name], ["Binding", d.binding ? d.binding.id + " rev " + d.binding.revision : "none"],
        ["Device", d.endpoint ? d.endpoint.mac + " " + (d.endpoint.hostname || "") : "none"], ["Session started", when(d.started_at)], ["Last auth", when(d.last_auth_at)]]))),
    panel("Enforcement on this switch", kv([["Platform", d.enforcement.platform], ["Strategy", d.enforcement.strategy],
      ["Mechanism", d.enforcement.mechanism], ["Observation", d.enforcement.observation],
      ["Limitations", h("ul", {}, (d.enforcement.limitations || []).map((l) => h("li", {}, l)))]])),
    panel("Change access (CoA)", h("p", { class: "muted" },
      "The requested state is recorded first and applies at the next authentication even if the switch refuses the CoA. ",
      role ? "On this switch NAC changes the session's local role; the VLAN and the endpoint's address stay the same. An ACK is not traffic verification."
        : "Changing the VLAN requires the endpoint to get a new IP address (DHCP). How soon depends on its DHCP client and lease; see the pilot's known limitations."),
      h("div", { class: "row" }, coaBtn("quarantine", "Quarantine", "danger"), coaBtn("restore", "Restore", "primary"),
        coaBtn("reauthenticate", reauthLabel, "", role ? "Apply the active policy to this session" : null)),
      d.coa_requests.length ? table(["Requested", "Action", "By", "State", "Attempts", "Error-Cause", "Detail"],
        d.coa_requests.slice().reverse().map((c) => ({ cells: [when(c.created_at), c.kind + (c.requested_role ? " → role " + c.requested_role : c.requested_vlan ? " → VLAN " + c.requested_vlan : ""),
          c.created_by, stateChip(c.state), c.attempts, txt(c.error_cause), txt(c.detail)] }))) : null),
    bind,
    ld ? panel("Decision inputs (decision " + ld.id + ", policy version " + ld.policy_version + ")",
      table(["Input", "Value", "Evidence", "Source", "Authenticated", "Observed at", "Note"], inputs)) : null,
    ld ? panel("Rule evaluation (first match wins)", table(["Priority", "Rule", "Action", "Outcome", "Conditions"], trace),
      h("p", {}, "Result: ", stateChip(ld.result), " ", txt(ld.reason))) : null,
    panel("Timeline", h("ol", { class: "timeline" }, d.timeline.map((t) => h("li", {}, h("span", { class: "muted" }, when(t.at)), " ",
      chip(t.stage), " ", stateChip(t.result), " ", t.summary, " ", h("span", { class: "muted" }, "(" + t.source + ")"))))));
  // A role CoA-ACK is final (no re-authentication follows), so only a VLAN
  // CoA keeps polling after the ACK.
  if (coaBusy || (!role && d.last_coa && d.last_coa.state === "acked")) pollTimer = setInterval(render, 1500);
}

// ---------- Devices ----------
async function endpoints() {
  const [epList, binds] = await Promise.all([api("GET", "/endpoints?limit=200"), api("GET", "/identity-bindings")]);
  const eps = epList.items || [];
  const byEp = {};
  for (const b of binds.bindings) (byEp[b.endpoint_id] ||= []).push(b);
  const mac = h("input", { placeholder: "00:50:00:00:02:00" }), host = h("input", { placeholder: "laptop-01" }), type = h("input", { placeholder: "laptop" });
  const posture = h("select", {}, ["", "compliant", "at_risk", "noncompliant"].map((v) => h("option", { value: v }, v || "no posture")));
  show(h("h1", {}, "Devices and identity bindings"),
    panel(null, table(["MAC", "Hostname", "Type", "Posture", "Posture observed", "Bound certificates"],
      eps.map((e) => ({ cells: [h("code", {}, e.mac), e.hostname, e.device_type, e.posture,
        e.posture_observed_at ? ago(e.posture_observed_at) + " (" + e.posture_source + ")" : "—",
        h("div", {}, (byEp[e.id] || []).map((b) => h("div", {}, h("code", {}, b.serial), " ", b.subject_cn || "", " ",
          h("button", { class: "danger", onclick: async () => {
            if (!await ask("Remove this binding? The device loses identity-based access at its next authentication.")) return;
            if (await act(() => api("DELETE", "/identity-bindings/" + b.id), "Binding removed.") !== null) render();
          } }, "unbind"))))] })))),
    panel("Add a device record",
      h("form", { onsubmit: async (e) => {
        e.preventDefault();
        const body = { mac: mac.value.trim(), hostname: host.value.trim(), device_type: type.value.trim() };
        if (posture.value) body.posture = posture.value;
        if (await act(() => api("POST", "/endpoints", body), "Device added. Bind its certificate from its session.")) render();
      } }, h("div", { class: "form" }, field("MAC (as the switch reports it)", mac), field("Hostname", host), field("Device type", type), field("Posture", posture)),
        h("p", {}, h("button", { class: "primary", type: "submit" }, "Add device")))));
}

// ---------- Policy ----------
function ruleEditor(rule, inputs, onSaved) {
  const name = h("input", { value: rule?.name || "" }), prio = h("input", { type: "number", value: rule?.priority ?? 100 });
  const action = h("select", {}, ["allow", "quarantine", "deny"].map((a) => h("option", { value: a }, a)));
  action.value = rule?.action || "allow";
  const segment = h("input", { value: rule?.segment ?? "corp" });
  const enabled = h("input", { type: "checkbox" }); enabled.checked = rule ? rule.enabled : true;
  const condBox = h("div");
  const addCond = (c) => {
    const f = h("select", {}, inputs.inputs.map((i) => h("option", { value: i.field }, i.field)));
    f.value = c?.field || "identity.bound";
    const op = h("select", {}, inputs.operators.map((o) => h("option", { value: o }, o))); op.value = c?.operator || "eq";
    const vals = h("input", { value: (c?.values || []).join(","), placeholder: "value[,value]" });
    const attr = h("input", { placeholder: "attr key", value: c?.field?.startsWith("attr.") ? c.field.slice(5) : "" });
    if (c?.field?.startsWith("attr.")) f.value = "attr.<key>";
    const row = h("div", { class: "row" }, f, attr, op, vals, h("button", { type: "button", onclick: () => row.remove() }, "remove"));
    row.read = () => ({ field: f.value === "attr.<key>" ? "attr." + attr.value.trim() : f.value, operator: op.value,
      values: ["exists", "absent"].includes(op.value) ? [] : vals.value.split(",").map((s) => s.trim()).filter(Boolean) });
    condBox.append(row);
  };
  (rule?.conditions || []).forEach(addCond);
  return h("form", { onsubmit: async (e) => {
    e.preventDefault();
    const body = { name: name.value.trim(), priority: Number(prio.value), enabled: enabled.checked, action: action.value,
      segment: action.value === "deny" ? "" : segment.value.trim(), description: rule?.description || "",
      conditions: [...condBox.children].map((r) => r.read()) };
    const r = await act(() => rule ? api("PUT", "/policies/" + rule.id, body) : api("POST", "/policies", body), "Draft updated (not yet published).");
    if (r) onSaved();
  } },
    h("div", { class: "form" }, field("Name", name), field("Priority (lower first)", prio), field("Action", action), field("Segment", segment),
      field("Enabled", enabled)),
    h("label", {}, "Conditions (all must match; missing or stale evidence never matches eq/ne/in)"), condBox,
    h("p", { class: "row" }, h("button", { type: "button", onclick: () => addCond() }, "+ condition"),
      h("button", { class: "primary", type: "submit" }, rule ? "Save rule" : "Add rule"),
      rule ? h("button", { type: "button", class: "danger", onclick: async () => {
        if (!await ask("Delete rule " + rule.name + " from the draft?")) return;
        if (await act(() => api("DELETE", "/policies/" + rule.id), "Rule removed from draft.") !== null) onSaved();
      } }, "Delete rule") : null));
}

async function policy() {
  const [draft, versions, acts, inputs] = await Promise.all([api("GET", "/policy-draft"), api("GET", "/policy-versions"),
    api("GET", "/policy-activations"), api("GET", "/policy-inputs")]);
  const editor = h("div");
  const previewBox = h("div");
  const v = draft.validation;
  const doPreview = async () => {
    const p = await act(() => api("GET", "/policy-draft/preview"));
    if (!p) return;
    const note = h("input", { placeholder: "change note (shown in history)" });
    previewBox.replaceChildren(panel("Preview of the draft",
      h("p", { class: "note" }, p.label),
      kv([["Compared with", "active version " + p.active_version], ["Recorded cases examined", p.cases_examined + " (latest decision per session since " + when(p.since) + ")"],
        ["Changed decisions", p.changed], ["Cases with insufficient evidence", p.insufficient_evidence]]),
      table(["Session subject", "Recorded", "Active version", "Draft", "Insufficient evidence", "Stale inputs"],
        p.rows.map((r) => ({ cells: [r.case.subject, when(r.case.occurred_at), r.active_action + " " + (r.active_segment || ""),
          h("span", { class: r.changed ? "warn" : "" }, r.draft_action + " " + (r.draft_segment || ""),
            r.override_applies ? " (operator quarantine override applies; policy alone: " + (r.policy_changed ? "changes" : "unchanged") + ")" : ""),
          (r.insufficient_evidence || []).join(", ") || "—",
          (r.stale_inputs || []).join(", ") || "—"] }))),
      h("p", { class: "muted" }, "Publishing applies to authentications from now on. Existing sessions keep their access until they re-authenticate; use Re-authenticate on a session (Apply active policy on a role switch) to apply it now."),
      !draft.differs_from_active ? h("p", { class: "warn" }, "The draft is identical to the active version: edit a rule before publishing.") : null,
      h("div", { class: "row" }, note, h("button", { class: "primary", disabled: !p.validation.valid || !draft.differs_from_active, onclick: async () => {
        if (!await ask("Publish the previewed draft as a new active version?")) return;
        const r = await act(() => api("POST", "/policy-draft/publish", { expected_digest: p.draft_digest, note: note.value }), "Published.");
        if (r) render();
      } }, "Publish this draft"))));
  };
  show(h("h1", {}, "Policy"),
    panel("Draft " + (draft.differs_from_active ? "(differs from active version " + draft.active_version + ")" : "(same as active version " + draft.active_version + ")"),
      table(["Priority", "Rule", "Enabled", "Conditions", "Action", "Segment"], draft.rules.map((r) => ({ rule: r, cells: [r.priority, r.name, r.enabled ? "yes" : "no",
        h("div", {}, r.conditions.map((c) => h("div", { class: "mono" }, c.field + " " + c.operator + " " + (c.values || []).join("|")))), r.action, txt(r.segment)] })),
        (row) => editor.replaceChildren(panel("Edit rule", ruleEditor(row.rule, inputs, render)))),
      h("p", {}, "Default action when no rule applies: ", h("strong", {}, draft.default_action)),
      h("div", {}, v.errors.map((e) => h("div", { class: "bad" }, "✗ " + e)), v.warnings.map((w) => h("div", { class: "warn" }, "⚠ " + w)),
        v.valid ? h("div", { class: "ok" }, "✓ draft is valid") : null),
      h("p", { class: "row" }, h("button", { onclick: () => editor.replaceChildren(panel("Add rule", ruleEditor(null, inputs, render))) }, "+ rule"),
        h("button", { class: "primary", onclick: doPreview }, "Preview changes"))),
    editor, previewBox,
    panel("Versions", table(["Version", "Active", "Rules", "Default", "Created", "By", "Note", ""], versions.versions.map((ver) => ({ cells: [ver.number,
      ver.active ? chip("active", "ok") : "", ver.rules.length, ver.default_action, when(ver.created_at), ver.created_by, txt(ver.note),
      ver.active ? "" : h("button", { onclick: async () => {
        if (!await ask("Restore version " + ver.number + "? It becomes active for new authentications and the draft is reset to it. Sessions under an operator quarantine stay quarantined.")) return;
        if (await act(() => api("POST", "/policy-versions/" + ver.number + "/restore", { note: "restored from console" }), "Version " + ver.number + " restored.")) render();
      } }, "Restore")] })))),
    panel("Activation history", table(["When", "Kind", "Version", "Previous", "By", "Note"], acts.activations.map((a) => ({ cells: [when(a.activated_at), a.kind,
      a.version_number, txt(a.previous_version_number), a.activated_by, txt(a.note)] })))),
    panel("Decision inputs you can use", table(["Field", "Allowed sources", "Meaning"], inputs.inputs.map((i) => ({ cells: [h("code", {}, i.field),
      i.sources.join(", "), i.description + (i.max_age ? " (fresh for " + i.max_age + ")" : "")] })))));
}

// ---------- Events ----------
function eventsTable(list) {
  return table(["When", "Stage", "Result", "Port / MAC", "Reason", "Source"], list.map((e) => ({ e, cells: [when(e.occurred_at), chip(e.stage),
    stateChip(e.result), (e.nas_port_id || "") + " " + (e.mac || ""), e.reason, e.source] })),
    (r) => r.e.session_id && go("#/session/" + r.e.session_id));
}
async function events() {
  const stage = h("select", {}, ["", "eap-tls", "nac-decision", "outage", "coa", "observation", "operator", "accounting"].map((s) => h("option", { value: s }, s || "all stages")));
  const out = h("div");
  const load = async () => out.replaceChildren(eventsTable((await api("GET", "/auth-events?limit=200&stage=" + stage.value)).events));
  stage.addEventListener("change", load);
  show(h("h1", {}, "Events"), panel(null, h("div", { class: "row" }, stage), out));
  await load();
}

render();
