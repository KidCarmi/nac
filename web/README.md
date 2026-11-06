# NAC operator console

A minimal console for the wired pilot, embedded in the nacd binary and served
at `/ui/` (`web/embed.go`). It is plain HTML, CSS and JavaScript with no build
step and no third-party code. It is a client of the REST API (`/api/v1`): it
enforces nothing that the API does not.

Views: Overview, Switches, Sessions (with session detail), Devices, Policy and
Events. See [the operator walkthrough](../docs/pilot/operator-walkthrough.md).

Security:
- served with a same-origin-only Content-Security-Policy
- every API value rendered with `textContent`
- the JWT is held in `sessionStorage`, so it is cleared when the tab closes
