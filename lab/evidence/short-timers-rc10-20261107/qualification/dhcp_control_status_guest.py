"""Read active NAC build and service health from the isolated DHCP control guest."""
import sys
sys.path.insert(0, '/root/nac-qualification')
from dhcp_candidate_serial_guest import Console

console = Console()
try:
    print(console.run("date -u +%FT%T.%3NZ; "
                      "systemctl is-active nacd freeradius postgresql; "
                      "curl -fsS --max-time 4 http://127.0.0.1:8080/version; "
                      "printf '\ninstalled='; sha256sum /usr/local/bin/nacd; "
                      "printf 'running='; sha256sum /proc/$(systemctl show -p MainPID --value nacd)/exe",
                      timeout=20))
finally:
    console.close()
