#!/usr/bin/env bash
# Generates a DISPOSABLE test PKI for automated tests and first-time lab
# trials: a test CA, a FreeRADIUS server certificate, one endpoint client
# certificate, and a client certificate from a second, UNTRUSTED CA (to test
# certificate rejection). Never use these for production.
#
#   scripts/test-pki.sh <output-dir> [endpoint-cn]
set -euo pipefail
out=${1:?usage: test-pki.sh <output-dir> [endpoint-cn]}
cn=${2:-wired-endpoint-01}
mkdir -p "$out"
cd "$out"
exec 3>&2 2>pki.log  # openssl progress noise goes to pki.log
trap 'cat pki.log >&3' ERR

cat > ext.cnf <<'CNF'
[server]
basicConstraints = CA:FALSE
keyUsage = digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth
[client]
basicConstraints = CA:FALSE
keyUsage = digitalSignature, keyEncipherment
extendedKeyUsage = clientAuth
CNF

mkca() { # name subject
  openssl req -x509 -newkey rsa:2048 -nodes -days 3650 -subj "$2" -keyout "$1.key" -out "$1.pem" \
    -addext "basicConstraints=critical,CA:TRUE" -addext "keyUsage=critical,keyCertSign,cRLSign"
}
mkcert() { # name subject ca ext
  openssl req -newkey rsa:2048 -nodes -subj "$2" -keyout "$1.key" -out "$1.csr"
  openssl x509 -req -in "$1.csr" -CA "$3.pem" -CAkey "$3.key" -CAcreateserial -days 825 \
    -extfile ext.cnf -extensions "$4" -out "$1.pem"
  rm -f "$1.csr"
}

mkca ca "/CN=NAC Test CA"
mkca untrusted-ca "/CN=Untrusted Test CA"
mkcert server "/CN=nac-radius.test" ca server
mkcert client "/CN=$cn" ca client
mkcert rogue "/CN=rogue-endpoint" untrusted-ca client
chmod 600 ./*.key
echo "serial of client.pem: $(openssl x509 -in client.pem -noout -serial | cut -d= -f2)"
echo "issuer of client.pem: $(openssl x509 -in client.pem -noout -issuer -nameopt compat | sed 's/^issuer=//')"
