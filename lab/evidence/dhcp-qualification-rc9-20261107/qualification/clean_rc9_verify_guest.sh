set -eu
date -u +VERIFY_UTC=%FT%TZ
systemctl is-active nacd freeradius postgresql nac-freeradius-reload.path
sha256sum /usr/local/bin/nacd
mainpid=$(systemctl show -p MainPID --value nacd)
sha256sum /proc/${mainpid}/exe
curl -fsS http://127.0.0.1:8080/readyz
printf '\n'
curl -fsS http://127.0.0.1:8080/version
printf '\n'
curl -ksS -o /dev/null -w 'CONSOLE_HTTP=%{http_code}\n' https://127.0.0.1:8443/ui/
df -h /
