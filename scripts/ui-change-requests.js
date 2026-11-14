// Drives the review-only change-request pages of the console against a FRESH
// nacd (empty database): propose the policy draft as a change request,
// validate, simulate (historical replay), reproduce, replace content with the
// draft (new revision, back to draft), cancel. It checks the review-only
// labels and that nothing changed the active policy. RADIUS traffic is
// simulated through the FreeRADIUS adapter API with the service token. Not
// for a production instance: it adds records.
//
//   NAC_URL=http://127.0.0.1:8080 NAC_ADMIN_EMAIL=admin@nac.local \
//   NAC_ADMIN_PASSWORD=<initial> NAC_SERVICE_TOKEN=<token> node scripts/ui-change-requests.js [outdir]
//
// Needs Playwright with Chromium. Exits 1 if any step fails.
const { chromium } = require('playwright');
const BASE = process.env.NAC_URL || 'http://127.0.0.1:8080', SVC = process.env.NAC_SERVICE_TOKEN;
const EMAIL = process.env.NAC_ADMIN_EMAIL || 'admin@nac.local', PW = process.env.NAC_ADMIN_PASSWORD;
const OUT = process.argv[2];
if (!SVC || !PW) { console.error('set NAC_SERVICE_TOKEN and NAC_ADMIN_PASSWORD'); process.exit(2); }
const attrs = { 'User-Name': 'wired-endpoint-01', 'Calling-Station-Id': '00-50-00-00-02-00', 'NAS-IP-Address': '127.0.0.1',
  'NAS-Port-Id': 'Ethernet1', 'TLS-Client-Cert-Serial': '2e1b47dc86a00b7f7250500b4a21f454b2efd819',
  'TLS-Client-Cert-Issuer': '/CN=NAC Lab Disposable CA', 'TLS-Client-Cert-Common-Name': 'wired-endpoint-01' };
let token = '';
async function api(method, path, body) {
  const r = await fetch(BASE + '/api/v1' + path, { method, headers: { Authorization: 'Bearer ' + token, 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body) });
  const t = await r.text(); let j = null; try { j = JSON.parse(t); } catch (_) { /* not JSON */ }
  if (!r.ok) throw new Error(method + ' ' + path + ': ' + r.status + ' ' + t);
  return j;
}
async function radius() {
  const body = Object.fromEntries(Object.entries(attrs).map(([k, v]) => [k, { type: 'string', value: [v] }]));
  const r = await fetch(BASE + '/api/v1/radius/authorize?client=127.0.0.1', { method: 'POST',
    headers: { Authorization: 'Basic ' + Buffer.from('freeradius:' + SVC).toString('base64') }, body: JSON.stringify(body) });
  const j = await r.json(); return (j['reply:Tunnel-Private-Group-Id'] || {}).value?.[0];
}
const results = []; const step = (name, ok, extra = '') => { results.push([ok ? 'PASS' : 'FAIL', name, extra]); };
(async () => {
  const b = await chromium.launch(); const p = await b.newPage({ viewport: { width: 1280, height: 1100 } });
  let nativeDialogs = 0; const errs = [];
  p.on('dialog', d => { nativeDialogs++; d.dismiss(); }); p.on('pageerror', e => errs.push(String(e)));
  const flash = async () => ((await p.locator('#flash > div').last().textContent().catch(() => '')) || '').trim();
  const shot = async (name) => { if (OUT) await p.screenshot({ path: OUT + '/' + name + '.png', fullPage: true }); };
  try {
    // Setup through the API: switch, device, binding, a published policy and one recorded decision.
    const login = await (await fetch(BASE + '/api/v1/auth/login', { method: 'POST', body: JSON.stringify({ email: EMAIL, password: PW }) })).json();
    token = login.token;
    await api('POST', '/switches', { name: 'access-sw-1', radius_source_ip: '127.0.0.1', radius_secret: 'lab-radius-secret-0123456789', coa_port: 9,
      segment_vlans: { corp: 10, quarantine: 20, guest: 30 }, access_ports: ['Ethernet1'] });
    const ep = await api('POST', '/endpoints', { mac: '00:50:00:00:02:00', hostname: 'wired-endpoint-01' });
    await api('POST', '/identity-bindings', { endpoint_id: ep.id, issuer: attrs['TLS-Client-Cert-Issuer'], serial: attrs['TLS-Client-Cert-Serial'] });
    const corp = await api('POST', '/policies', { name: 'bound-corp', priority: 10, enabled: true, action: 'allow', segment: 'corp',
      conditions: [{ field: 'identity.bound', operator: 'eq', values: ['true'] }] });
    const pv = await api('GET', '/policy-draft/preview');
    await api('POST', '/policy-draft/publish', { expected_digest: pv.draft_digest });
    step('Setup: endpoint gets corp VLAN 10 under the active policy', (await radius()) === '10');
    // The draft now proposes guest instead of corp (not published).
    await api('PUT', '/policies/' + corp.id, { name: 'bound-corp', priority: 10, enabled: true, action: 'allow', segment: 'guest',
      conditions: [{ field: 'identity.bound', operator: 'eq', values: ['true'] }] });
    const activeBefore = (await api('GET', '/policy-versions')).versions.find((v) => v.active);

    // Console: sign in, Policy -> Propose as change request.
    await p.goto(BASE + '/ui/'); await p.fill('input[type=email]', EMAIL); await p.fill('input[type=password]', PW);
    await p.click('button[type=submit]'); await p.waitForSelector('#who a');
    await p.goto(BASE + '/ui/#/policy'); await p.click('#propose-cr'); await p.waitForSelector('#cr-create');
    step('Changes page shows the review-only label', (await p.textContent('#cr-label')).includes('Review-only'));
    await p.fill('#cr-title', 'bound devices to guest'); await p.fill('#cr-intent', 'pilot review');
    await p.click('#cr-create'); await p.waitForSelector('#cr-validate');
    const crID = p.url().split('/').pop();
    step('Create from the draft: detail shows state draft, revision 1 and the label',
      (await p.textContent('main')).includes('draft') && (await p.textContent('#cr-detail-label')).includes('does not change the active policy'));
    step('Simulate is disabled before validation', await p.isDisabled('#cr-simulate'));
    await shot('01-change-request-draft');

    await p.click('#cr-validate'); await p.waitForTimeout(700);
    step('Validate: recorded, state validated', (await flash()).includes('Validation recorded') && (await p.textContent('main')).includes('validated'), await flash());
    await p.click('#cr-simulate'); await p.waitForSelector('#cr-sim-rows table');
    const rowsText = await p.textContent('#cr-sim-rows');
    step('Simulate: historical replay labelled, one changed case corp -> guest',
      rowsText.includes('not live-network verification') && rowsText.includes('corp') && rowsText.includes('guest'), rowsText.slice(0, 120));
    await shot('02-simulation');
    await p.click('button.cr-reproduce >> nth=0'); await p.waitForTimeout(600);
    step('Reproduce: identical output from the frozen input set', (await flash()).startsWith('Reproduced: identical output'), await flash());

    await p.click('#cr-rebase'); await p.waitForSelector('#confirm-dialog[open]'); await p.click('#confirm-ok'); await p.waitForTimeout(700);
    const d = await api('GET', '/change-requests/' + crID);
    step('Replace content with the draft: revision 2, back to draft, old simulation reported stale',
      d.revision === 2 && d.state === 'draft' && d.simulations[0].drift.state === 'stale', JSON.stringify(d.simulations[0].drift));
    await p.click('#cr-cancel'); await p.waitForSelector('#confirm-dialog[open]'); await p.click('#confirm-ok'); await p.waitForTimeout(700);
    step('Cancel: read-only afterwards', await p.isDisabled('#cr-validate') && await p.isDisabled('#cr-cancel'));
    await shot('03-cancelled');

    const activeAfter = (await api('GET', '/policy-versions')).versions.find((v) => v.active);
    step('The active policy is unchanged (review-only)', activeAfter.number === activeBefore.number && activeAfter.digest === activeBefore.digest);
    step('The endpoint still gets corp VLAN 10', (await radius()) === '10');
    step('No native browser dialogs; no page errors', nativeDialogs === 0 && errs.length === 0, errs.join('; '));
  } catch (e) {
    step('walkthrough aborted', false, String(e));
  } finally {
    await b.close();
  }
  for (const [s, n, x] of results) console.log(s.padEnd(5), n, x ? '- ' + x : '');
  const failed = results.filter((r) => r[0] === 'FAIL').length;
  console.log(`${results.length - failed}/${results.length} passed`);
  process.exit(failed ? 1 : 0);
})();
