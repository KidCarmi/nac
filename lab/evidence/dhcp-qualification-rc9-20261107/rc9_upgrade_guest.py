"""Stage and run the documented rc9 installer on the isolated NAC control node."""
import os
import sys

sys.path.insert(0, '/root/nac-qualification')
from dhcp_candidate_serial_guest import Console

ACTION = os.environ.get('RC9_ACTION', '')
STAGE = '/root/nac-pilot-rc9'
NAME = 'nac-pilot-pilot-rc9.tar.gz'
EXPECTED = '29991b27cef9b635c5679bff8df56be25827e539586b545ef56e6f5098cce5d2'
BIN = 'd2d93e3ddc40a7eadcddfd3275f3d8a7aa5610ca07b43eb219bb9d59267a3749'
COMMIT = '3e92642110f97d3f42a43652d33479e82cfbcc35'
if ACTION not in {'preflight', 'stage', 'install_start', 'install_check', 'verify', 'status'}:
    raise SystemExit('set RC9_ACTION=preflight|stage|install_start|install_check|verify|status')

console = Console()
try:
    if ACTION == 'preflight':
        print(console.run('date -u +%FT%T.%3NZ; systemctl is-active nacd freeradius postgresql; '
                          'printf "installed="; sha256sum /usr/local/bin/nacd; '
                          'printf "running="; sha256sum /proc/$(systemctl show -p MainPID --value nacd)/exe; '
                          'printf "env="; sha256sum /etc/nac/nacd.env; '
                          'test -f /root/nac-pilot-install/pki/ca.crt && echo EXISTING_LAB_PKI=present; '
                          'curl -fsS --max-time 4 http://127.0.0.1:8080/version', timeout=20))
    elif ACTION == 'stage':
        print(console.run(f'test ! -e {STAGE} && install -d -m 0700 {STAGE} && '
                          'test -f /root/nac-pilot-install/pki/ca.crt && test -f /etc/nac/nacd.env'))
        for name in (NAME, NAME + '.sha256'):
            console.run("python3 -c 'import urllib.request; "
                        f"urllib.request.urlretrieve(\"http://10.70.99.254:18090/{name}\",\"{STAGE}/{name}\")'", timeout=45)
        print(console.run(f'cd {STAGE} && sha256sum -c {NAME}.sha256 && sha256sum {NAME}'))
        observed = console.run(f'sha256sum {STAGE}/{NAME}').split()[0]
        if observed != EXPECTED:
            raise RuntimeError('rc9 bundle hash mismatch')
        print(console.run(f'cd {STAGE} && tar xzf {NAME} && cat nac-pilot-pilot-rc9/BUNDLE-MANIFEST.txt', timeout=40))
        observed_bin = console.run(f'sha256sum {STAGE}/nac-pilot-pilot-rc9/bin/nacd').split()[0]
        if observed_bin != BIN:
            raise RuntimeError('rc9 nacd hash mismatch')
        print('RC9_NACD_SHA256', observed_bin)
    elif ACTION == 'install_start':
        print(console.run("test ! -e /root/nac-rc9-install.rc && test -f /root/nac-pilot-rc9/nac-pilot-pilot-rc9/bin/nacd && "
                          "nohup bash -c 'cd /root/nac-pilot-rc9/nac-pilot-pilot-rc9 && "
                          "bash deployments/pilot/install.sh --proxy http://10.70.99.254:3128 "
                          "--ca /root/nac-pilot-install/pki/ca.crt "
                          "--server-cert /root/nac-pilot-install/pki/server.crt "
                          "--server-key /root/nac-pilot-install/pki/server.key; "
                          "printf \"%s\\n\" \"$?\" > /root/nac-rc9-install.rc' "
                          "> /root/nac-rc9-install.log 2>&1 </dev/null & echo RC9_INSTALL_STARTED"))
    elif ACTION == 'install_check':
        print(console.run('date -u +%FT%T.%3NZ; ls -l /root/nac-rc9-install.rc /root/nac-rc9-install.log 2>&1; '
                          'if test -f /root/nac-rc9-install.rc; then printf "INSTALL_RC="; cat /root/nac-rc9-install.rc; '
                          'else echo INSTALL_RUNNING; fi; tail -30 /root/nac-rc9-install.log; '
                          "ps -eo pid,etime,args | grep -E '[i]nstall.sh|[a]pt-get' || true", timeout=20))
    elif ACTION == 'verify':
        installed = console.run('sha256sum /usr/local/bin/nacd').split()[0]
        running = console.run('sha256sum /proc/$(systemctl show -p MainPID --value nacd)/exe').split()[0]
        if installed != BIN or running != BIN:
            raise RuntimeError(f'rc9 hash mismatch installed={installed} running={running}')
        report = console.run('cat /etc/nac/install-report.txt; systemctl is-active nacd freeradius postgresql; '
                             'curl -fsS --max-time 4 http://127.0.0.1:8080/version; '
                             'printf "env="; sha256sum /etc/nac/nacd.env', timeout=30)
        if f'running nacd: commit {COMMIT[:7]}' not in report and f'running nacd: commit {COMMIT}' not in report:
            raise RuntimeError('installer report lacks running rc9 commit')
        if console.run('cat /root/nac-rc9-install.rc').strip() != '0':
            raise RuntimeError('rc9 installer failed')
        print('RC9_INSTALLED_NACD_SHA256', installed)
        print('RC9_RUNNING_NACD_SHA256', running)
        print(report)
    elif ACTION == 'status':
        print(console.run('date -u +%FT%T.%3NZ; systemctl is-active nacd freeradius postgresql; '
                          'printf "installed="; sha256sum /usr/local/bin/nacd; '
                          'printf "running="; sha256sum /proc/$(systemctl show -p MainPID --value nacd)/exe; '
                          'curl -fsS --max-time 4 http://127.0.0.1:8080/readyz', timeout=20))
finally:
    console.close()


