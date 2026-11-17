# Slice A walkthrough run record

| Field | Value |
|---|---|
| Code commit under test | `44e447d4ad89557e4da6a58a714138cec0ddbeb7` (PR #10 head when run; this record is added in a docs-only commit on top) |
| Invocation | `DEMO_RESET=1 NAC_DB_DSN=<disposable local PostgreSQL> scripts/demo-change-requests.sh docs/evidence/slice-a-walkthrough-44e447d` |
| Build | `go build ./cmd/nacd` by the demo script (development build, go1.25.0 linux/amd64; not a pilot bundle). An identical separate build gave nacd SHA-256 `e7048aedd92d1fc041f7c82a9262b1acd0f829c8fc8e046bf725058388956ef3` |
| Environment | Cloud build container. PostgreSQL 16 (local), Node v22.23.4, Playwright 1.63.0 (Chromium). Fresh database, generated demo credentials (not recorded). |
| Result | **12/12 PASS**: see `stdout.txt` |
| Run at | 2026-11-14T21:56:55Z |
| Status | UNREVIEWED evidence for review. Not a lab result and not candidate acceptance. |

## What this run does and does not show

- **Shows:**
  - the review-only change-request flow in the console and API, end to end;
  - the active policy version and digest are unchanged;
  - NAC's **simulated** RADIUS authorization reply stays corp VLAN 10.
- **Does not show:** anything about a network endpoint. The RADIUS requests
  are manufactured and posted to the FreeRADIUS adapter API with the service
  token. No EAP-TLS, switch port, CoA delivery, DHCP or endpoint traffic is
  exercised. **Endpoint reachability: NOT RUN.**

## Files

| File | What |
|---|---|
| `stdout.txt` | The script's complete output: the 12 checks and the final `GET /api/v1/change-requests/{id}` |
| `01-change-request-draft.png` | The change request after "Propose as change request": draft, revision 1, review-only label, Simulate disabled |
| `02-simulation.png` | After validate and simulate: the labelled historical replay, one case corp → guest, coverage |
| `03-cancelled.png` | After replace-content (revision 2, old simulation stale) and cancel: read-only |

## SHA-256 manifest

```
62f0c5ad576be8d6b67f93210b795dd4d7a7e50143dc574699d3ba962f560b9c  stdout.txt
410a23caaf42ea5d3cfc793342257a9cd367db7e1566dfc1c46f613579840814  01-change-request-draft.png
7a0c71f1818518d35b9efa401652580bb12cdfe7efb6064eb223ddac3ac7fff3  02-simulation.png
7a00ad0991ba1ff0b7fc4235f7dd6336b0e5ab37b2923fde4110827d5b6035d7  03-cancelled.png
```
