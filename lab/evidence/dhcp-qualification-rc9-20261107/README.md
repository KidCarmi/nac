# pilot-rc9 independent wired NAC qualification

Start with the [report](report.md) and [rerun instructions](rerun.md). This package pins KidCarmi/nac PR #1 commit `3e92642110f97d3f42a43652d33479e82cfbcc35` and independently built, installed, running nacd SHA-256 `d2d93e3ddc40a7eadcddfd3275f3d8a7aa5610ca07b43eb219bb9d59267a3749`.

The [topology export](NAC-Pilot-DHCP.unl), [sanitized configuration](configs.sanitized.md), [runner and dependency snapshot](qualification), exact rc9 [clean install](runs/candidate-3e92642-clean-rc9-20261107/findings.md) and admission/outage/restart/observer/accounting/console evidence under [runs](runs), and [SHA-256 verifier](verify_package.py) support reproduction. The [rc8 package](baseline-rc8.md) remains a separate frozen baseline.

No vendor disk, private key, shared secret, admin password, Git bundle or source transport archive is included. Captures contain disposable lab identity and network metadata; treat them as internal evidence. The four console-run captures were scanned for known plaintext credentials with zero matches; earlier runs have their own scan records. The console run's continuous probe failed when the endpoint disk filled; timestamped one-shot TCP probes, DHCP/RADIUS/EAPOL captures and a separate repaired-runner smoke probe are retained with that limitation stated explicitly.
