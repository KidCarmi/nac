// Drives docs/pilot/operator-walkthrough.md in the console against a FRESH
// nacd (empty database): Account password change, switch onboarding, device
// record and identity binding, policy preview/publish/restore, quarantine
// CoA. The switch's RADIUS traffic is simulated through the FreeRADIUS
// adapter API with the service token, so no switch is needed. Not for a
// production instance: it changes the admin password and adds records.
//
//   NAC_URL=http://127.0.0.1:8080 NAC_ADMIN_EMAIL=admin@nac.local //   NAC_ADMIN_PASSWORD=<initial> NAC_SERVICE_TOKEN=<token> node scripts/ui-walkthrough.js [outdir]
//
// Needs Playwright with Chromium. Exits 1 if any step fails.
const { chromium } = require('playwright');
const BASE = process.env.NAC_URL || 'http://127.0.0.1:8080', SVC = process.env.NAC_SERVICE_TOKEN;
const EMAIL = process.env.NAC_ADMIN_EMAIL || 'admin@nac.local', PW = process.env.NAC_ADMIN_PASSWORD;
const NEWPW = process.env.NAC_NEW_PASSWORD || 'walkthrough-passphrase-' + Date.now();
if (!SVC || !PW) { console.error('set NAC_SERVICE_TOKEN and NAC_ADMIN_PASSWORD'); process.exit(2); }
const attrs = { 'User-Name': 'wired-endpoint-01', 'Calling-Station-Id': '00-50-00-00-02-00', 'NAS-IP-Address': '127.0.0.1',
  'NAS-Port-Id': 'Ethernet1', 'TLS-Client-Cert-Serial': '2e1b47dc86a00b7f7250500b4a21f454b2efd819',
  'TLS-Client-Cert-Issuer': '/CN=NAC Lab Disposable CA', 'TLS-Client-Cert-Common-Name': 'wired-endpoint-01' };
async function radius() {
  const body = Object.fromEntries(Object.entries(attrs).map(([k, v]) => [k, { type: 'string', value: [v] }]));
  const r = await fetch(BASE + '/api/v1/radius/authorize?client=127.0.0.1', { method: 'POST',
    headers: { Authorization: 'Basic ' + Buffer.from('freeradius:' + SVC).toString('base64') }, body: JSON.stringify(body) });
  const j = await r.json(); return (j['reply:Tunnel-Private-Group-Id'] || {}).value?.[0] || (j['control:Tmp-String-1'] || {}).value?.[0];
}
const results = []; const step = (name, ok, extra = '') => { results.push([ok ? 'PASS' : 'FAIL', name, extra]); };
(async () => {
  const b = await chromium.launch(); const p = await b.newPage({ viewport: { width: 1280, height: 1000 } });
  let nativeDialogs = 0; const errs = [];
  p.on('dialog', d => { nativeDialogs++; d.dismiss(); }); p.on('pageerror', e => errs.push(String(e)));
  const fill = async (label, v) => p.locator(`div:has(> label:text-is("${label}")) > input`).first().fill(String(v));
  const confirmOK = async () => { await p.waitForSelector('#confirm-dialog[open]'); await p.click('#confirm-ok'); };
  const flash = async () => (await p.textContent('#flash')).trim().split('\n').pop();
  try {
    // First sign-in, then Account password change.
    await p.goto(BASE + '/ui/'); await p.fill('input[type=email]', EMAIL); await p.fill('input[type=password]', PW);
    await p.click('button[type=submit]'); await p.waitForSelector('#who a');
    await p.click('#who a'); await p.waitForSelector('text=Change password');
    const pw = p.locator('form input[type=password]');
    await pw.nth(0).fill(PW); await pw.nth(1).fill(NEWPW); await pw.nth(2).fill(NEWPW);
    await p.click('button:has-text("Change password")'); await p.waitForTimeout(800);
    step('Account: change password', (await p.textContent('#flash')).includes('Password changed'));
    const relog = await fetch(BASE + '/api/v1/auth/login', { method: 'POST', body: JSON.stringify({ email: EMAIL, password: PW }) });
    step('Account: old password rejected', relog.status === 401, 'status ' + relog.status);
    // 1. Onboard the switch.
    await p.goto(BASE + '/ui/#/switches'); await p.waitForSelector('text=Register a switch');
    await fill('Name', 'access-sw-1'); await fill('RADIUS source IP (as FreeRADIUS sees it)', '127.0.0.1');
    await fill('RADIUS shared secret', 'lab-radius-secret-0123456789'); await fill('FreeRADIUS address for the switch config', '10.70.99.2');
    await fill('CoA UDP port', '9');
    await p.click('button:has-text("Register")'); await p.waitForSelector('text=Switch access-sw-1');
    const cfgText = await p.textContent('pre');
    step('Onboarding: register switch, EOS config generated', cfgText.includes('dot1x') && cfgText.includes('vlan 10'));
    // 2. Endpoint authenticates (valid, unbound) -> denied with explanation.
    const v0 = await radius(); step('RADIUS: valid but unbound certificate is denied', v0 === 'nac-reject', String(v0));
    await p.goto(BASE + '/ui/#/endpoints'); await p.waitForSelector('text=Add a device record');
    await fill('MAC (as the switch reports it)', '00:50:00:00:02:00'); await fill('Hostname', 'wired-endpoint-01');
    await p.click('button:has-text("Add device")').catch(async () => p.locator('form button.primary').first().click());
    await p.waitForTimeout(600);
    await p.goto(BASE + '/ui/#/sessions'); await p.click('tr.click >> nth=0'); await p.waitForSelector('button:has-text("Bind")');
    await p.click('button:has-text("Bind")'); await p.waitForTimeout(700);
    step('Identity binding from the session', (await p.textContent('#flash')).includes('Bound. It applies'), await flash());
    // 3. Policy: add rule bound -> corp, preview, publish (in-page confirm).
    await p.goto(BASE + '/ui/#/policy'); await p.click('button:has-text("+ rule")');
    await fill('Name', 'bound-corp'); await fill('Priority (lower first)', '10');
    await p.click('button:has-text("+ condition")');
    const row = p.locator('form div.row').filter({ has: p.locator('select') }).first();
    await row.locator('select').nth(0).selectOption('identity.bound'); await row.locator('select').nth(1).selectOption('eq');
    await row.locator('input').nth(1).fill('true');
    await p.click('button:has-text("Add rule")'); await p.waitForTimeout(700);
    await p.click('button:has-text("Preview changes")'); await p.waitForSelector('button:has-text("Publish this draft")');
    const prev = await p.textContent('main');
    step('Policy: preview shows the predicted change', /prediction/i.test(prev) && prev.includes('corp'));
    await p.click('button:has-text("Publish this draft")'); await confirmOK(); await p.waitForTimeout(800);
    step('Policy: publish via in-page confirm', (await p.textContent('#flash')).includes('Published.'), await flash());
    const v1 = await radius(); step('RADIUS: bound certificate gets corp VLAN 10', v1 === '10', String(v1));
    // 4. Session view: three separate facts; quarantine CoA (no switch -> timeout, override recorded).
    await p.goto(BASE + '/ui/#/sessions'); await p.click('tr.click >> nth=0'); await p.waitForSelector('text=3. Endpoint reachability');
    const sv = await p.textContent('main');
    step('Session: authorization / observed / reachability shown separately',
      sv.includes('1. Switch authorization') && sv.includes('2. Observed on the switch') && sv.includes('not_observed'));
    await p.click('button.danger:has-text("Quarantine")'); await p.waitForSelector('#confirm-dialog[open]');
    const warn = await p.textContent('#confirm-dialog .dialog-msg');
    await p.click('#confirm-ok'); await p.waitForTimeout(1500);
    step('CoA: quarantine requested via in-page confirm (warning shown)', warn.includes('DHCP renewal'));
    const v2 = await radius(); step('RADIUS: next authentication applies the quarantine (VLAN 20)', v2 === '20', String(v2));
    // 5. Restore the previous policy version.
    await p.goto(BASE + '/ui/#/policy'); await p.waitForSelector('text=Versions');
    await p.locator('button:has-text("Restore")').first().click(); await confirmOK(); await p.waitForTimeout(800);
    step('Policy: restore an earlier version via in-page confirm', /restor/i.test(await flash()), await flash());
    if (process.argv[2]) await p.screenshot({ path: process.argv[2] + '/ui-walkthrough.png', fullPage: true });
  } catch (e) { step('script', false, String(e).slice(0, 200)); }
  step('No native dialogs, no page errors', nativeDialogs === 0 && errs.length === 0, `native=${nativeDialogs} errors=${errs.join(';')}`);
  for (const r of results) console.log(r.join(' | '));
  await b.close();
  if (results.some((r) => r[0] === 'FAIL')) process.exit(1);
  console.log('new admin password: ' + NEWPW);
})();
