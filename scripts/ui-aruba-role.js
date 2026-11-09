// Drives the Aruba AOS-CX role path in the console against a FRESH nacd
// (empty database): register an Aruba switch, corp -> operator Quarantine ->
// Restore, and check that every step stays on the role path (Aruba-User-Role,
// no Tunnel-* attributes, VLAN unchanged) and that the console never presents
// a CoA-ACK as traffic verification. The switch is simulated: RADIUS through
// the FreeRADIUS adapter API with the service token, and a local UDP responder
// that ACKs CoA-Requests like the lab AOS-CX did. Not for a production
// instance: it adds records.
//
//   NAC_URL=http://127.0.0.1:8080 NAC_ADMIN_EMAIL=admin@nac.local \
//   NAC_ADMIN_PASSWORD=<password> NAC_SERVICE_TOKEN=<token> node scripts/ui-aruba-role.js [outdir]
//
// Needs Playwright with Chromium. Exits 1 if any step fails.
const { chromium } = require('playwright');
const dgram = require('dgram'), crypto = require('crypto');
const BASE = process.env.NAC_URL || 'http://127.0.0.1:8080', SVC = process.env.NAC_SERVICE_TOKEN;
const EMAIL = process.env.NAC_ADMIN_EMAIL || 'admin@nac.local', PW = process.env.NAC_ADMIN_PASSWORD;
const SECRET = 'lab-radius-secret-0123456789', PORT = '1/1/1';
if (!SVC || !PW) { console.error('set NAC_SERVICE_TOKEN and NAC_ADMIN_PASSWORD'); process.exit(2); }

// Fake AOS-CX dynamic-authorization server: records each CoA, answers CoA-ACK.
const coas = [];
const sock = dgram.createSocket('udp4');
sock.on('message', (m, r) => {
  const attrs = {}; let role = null;
  for (let i = 20; i + 2 <= m.length;) {
    const t = m[i], l = m[i + 1]; if (l < 2) break;
    const v = m.subarray(i + 2, i + l);
    if (t === 26 && v.readUInt32BE(0) === 14823 && v[4] === 1) role = v.subarray(6, 4 + v[5]).toString();
    else attrs[t] = v; i += l;
  }
  coas.push({ role, tunnelVLAN: attrs[81] ? attrs[81].toString().replace(/^\0/, '') : null, port: attrs[87] && attrs[87].toString() });
  const hdr = Buffer.from([44, m[1], 0, 20]);
  const auth = crypto.createHash('md5').update(Buffer.concat([hdr, m.subarray(4, 20), Buffer.from(SECRET)])).digest();
  sock.send(Buffer.concat([hdr, auth]), r.port, r.address);
});

const attrs = { 'User-Name': 'wired-endpoint-01', 'Calling-Station-Id': '00-50-00-00-02-00', 'NAS-IP-Address': '127.0.0.1',
  'NAS-Port-Id': PORT, 'TLS-Client-Cert-Serial': '2e1b47dc86a00b7f7250500b4a21f454b2efd819',
  'TLS-Client-Cert-Issuer': '/CN=NAC Lab Disposable CA', 'TLS-Client-Cert-Common-Name': 'wired-endpoint-01' };
async function radius() { // -> { role, vlan, result }
  const body = Object.fromEntries(Object.entries(attrs).map(([k, v]) => [k, { type: 'string', value: [v] }]));
  const r = await fetch(BASE + '/api/v1/radius/authorize?client=127.0.0.1', { method: 'POST',
    headers: { Authorization: 'Basic ' + Buffer.from('freeradius:' + SVC).toString('base64') }, body: JSON.stringify(body) });
  const j = await r.json(), v = (k) => (j[k] || {}).value?.[0];
  return { role: v('reply:Aruba-User-Role'), vlan: v('reply:Tunnel-Private-Group-Id'), result: v('control:Tmp-String-1') };
}
const results = []; const step = (name, ok, extra = '') => { results.push([ok ? 'PASS' : 'FAIL', name, extra]); };
const wait = (ms) => new Promise((r) => setTimeout(r, ms));

(async () => {
  await new Promise((r) => sock.bind(0, '127.0.0.1', r));
  const b = await chromium.launch(); const p = await b.newPage({ viewport: { width: 1280, height: 1000 } });
  let nativeDialogs = 0; const errs = [];
  p.on('dialog', d => { nativeDialogs++; d.dismiss(); }); p.on('pageerror', e => errs.push(String(e)));
  const fill = async (label, v) => p.locator(`div:has(> label:text-is("${label}")) > input`).first().fill(String(v));
  const main = async () => p.textContent('main');
  try {
    await p.goto(BASE + '/ui/'); await p.fill('input[type=email]', EMAIL); await p.fill('input[type=password]', PW);
    await p.click('button[type=submit]'); await p.waitForSelector('#who a');
    const tok = (await (await fetch(BASE + '/api/v1/auth/login', { method: 'POST', body: JSON.stringify({ email: EMAIL, password: PW }) })).json()).token;
    const api = (m, path, body) => fetch(BASE + '/api/v1' + path, { method: m, headers: { Authorization: 'Bearer ' + tok },
      body: body && JSON.stringify(body) }).then((r) => r.json());

    // 1. Register an Aruba switch in the console.
    await p.goto(BASE + '/ui/#/switches'); await p.waitForSelector('text=Register a switch');
    await p.locator('div:has(> label:text-is("Platform")) > select').selectOption('aruba-aos-cx');
    step('Switch form: Aruba hides VLAN and eAPI fields, shows role fields',
      !(await p.isVisible('label:text-is("Quarantine VLAN")')) && !(await p.isVisible('label:text-is("eAPI URL (session observation)")')) &&
      await p.isVisible('label:text-is("Quarantine role (local role on the switch)")'));
    await fill('Name', 'aos-cx-1'); await fill('RADIUS source IP (as FreeRADIUS sees it)', '127.0.0.1');
    await fill('RADIUS shared secret', SECRET); await fill('FreeRADIUS address for the switch config', '10.70.99.2');
    await fill('CoA UDP port', sock.address().port);
    await p.click('button:has-text("Register")'); await p.waitForSelector('text=Switch aos-cx-1');
    const cfg = await p.textContent('pre');
    step('Onboarding: AOS-CX reference config with local roles, VLAN unchanged',
      cfg.includes('port-access role nac-quarantine') && cfg.includes('radius dyn-authorization enable') && !/Tunnel|vlan 20/.test(cfg));
    step('Switch detail: no eAPI actions for Aruba', !(await p.isVisible('button:has-text("Probe management (eAPI)")')));

    // 2. Device, binding and policy (API; covered in the console by ui-walkthrough.js).
    const ep = await api('POST', '/endpoints', { mac: '00:50:00:00:02:00', hostname: 'wired-endpoint-01' });
    await api('POST', '/identity-bindings', { endpoint_id: ep.id, issuer: attrs['TLS-Client-Cert-Issuer'], serial: attrs['TLS-Client-Cert-Serial'] });
    await api('POST', '/policies', { name: 'bound-corp', priority: 10, enabled: true, action: 'allow', segment: 'corp',
      conditions: [{ field: 'identity.bound', operator: 'eq', values: ['true'] }] });
    const pv = await api('GET', '/policy-draft/preview'); await api('POST', '/policy-draft/publish', { expected_digest: pv.draft_digest });
    const r1 = await radius();
    step('RADIUS: corp is Aruba-User-Role nac-corp, no VLAN', r1.role === 'nac-corp' && !r1.vlan, JSON.stringify(r1));

    // 3. Session view states the role contract.
    await p.goto(BASE + '/ui/#/sessions'); await p.click('tr.click >> nth=0'); await p.waitForSelector('text=Enforcement on this switch');
    const sv = await main();
    step('Session: role requested, enforcement=role, switch not observed',
      sv.includes('nac-corp') && sv.includes('Aruba-User-Role') && sv.includes('unavailable') && sv.includes('not traffic verification'));

    // 4. Quarantine via in-page confirm.
    await p.click('button.danger:has-text("Quarantine")'); await p.waitForSelector('#confirm-dialog[open]');
    const warn = await p.textContent('#confirm-dialog .dialog-msg');
    await p.click('#confirm-ok'); await wait(1500); await p.reload(); await p.waitForSelector('text=Enforcement on this switch');
    const q = coas[coas.length - 1] || {};
    step('Quarantine confirm warns: role change, VLAN/IP unchanged, ACK is not verification',
      warn.includes('Aruba-User-Role') && warn.includes('do not change') && warn.includes('does not verify') && !warn.includes('58 s'));
    step('CoA: Aruba-User-Role nac-quarantine on 1/1/1, no Tunnel-*', q.role === 'nac-quarantine' && !q.tunnelVLAN && q.port === PORT, JSON.stringify(q));
    const qv = await main();
    step('Console: CoA acked (not observed), role shown', qv.includes('quarantine → role nac-quarantine') && qv.includes('acked') &&
      qv.includes('not verification'));
    const r2 = await radius();
    step('RADIUS: re-authentication keeps the quarantine role, no VLAN', r2.role === 'nac-quarantine' && !r2.vlan, JSON.stringify(r2));
    const lc = (await api('GET', '/sessions?state=active')).sessions[0];
    const lcd = await api('GET', '/sessions/' + lc.id);
    step('A later re-authentication does not turn a role CoA into "observed"', lcd.last_coa.state === 'acked', lcd.last_coa.state);

    // 5. Restore.
    await p.click('button.primary:has-text("Restore")'); await p.waitForSelector('#confirm-dialog[open]');
    await p.click('#confirm-ok'); await wait(1500);
    const rs = coas[coas.length - 1] || {};
    step('Restore CoA: Aruba-User-Role nac-corp, no Tunnel-*', rs.role === 'nac-corp' && !rs.tunnelVLAN, JSON.stringify(rs));
    const r3 = await radius();
    step('RADIUS: after Restore the identity gets nac-corp', r3.role === 'nac-corp' && !r3.vlan, JSON.stringify(r3));
    if (process.argv[2]) { await p.reload(); await p.waitForSelector('text=Enforcement on this switch');
      await p.screenshot({ path: process.argv[2] + '/ui-aruba-role.png', fullPage: true }); }
  } catch (e) { step('script', false, String(e).slice(0, 300)); }
  step('No native dialogs, no page errors', nativeDialogs === 0 && errs.length === 0, `native=${nativeDialogs} errors=${errs.join(';')}`);
  for (const r of results) console.log(r.join(' | '));
  await b.close(); sock.close();
  if (results.some((r) => r[0] === 'FAIL')) process.exit(1);
})();
