#!/bin/sh
# Builds an own CA and a server certificate for a TEST store.
#
# This belongs to no release. It builds a *test* store, the thing an API can
# really fetch from while nobody has an official NGC store yet (the store key
# in the installer is still a placeholder, points 56 and 57).
#
# Rescued on 24.10.2026 off the first single machine, where it lived in a home
# directory (/home/berry/ngcapp/make-store-tls.sh) and was in no repository.
# That machine is a hand build and is being switched off; the script is here so
# the test store can be rebuilt instead of invented again. Only the paths and
# the name became arguments - the steps and the self-check are unchanged.
#
#   sh tools/pruefladen-tls.sh [name] [output-dir]
#
#   name        DNS name of the store, default store.ngc.test. ".test" is
#               reserved for exactly this (RFC 2606), so it can never collide
#               with a real domain.
#   output-dir  where the keys and certificates land, default ./pruefladen-tls
#
# The chain is verified here with openssl before anything is installed: a
# certificate that does not verify locally would make every later measurement a
# measurement of the CA, not of the API.
set -e

NAME="${1:-store.ngc.test}"
D="${2:-./pruefladen-tls}"

case "$NAME" in
  *.*) ;;
  *) echo "The name needs at least one dot: $NAME" >&2; exit 2 ;;
esac

# Not "rm -rf $D": a mistyped argument would take a directory with it. A fresh
# directory is required instead, so nothing is ever silently overwritten.
if [ -e "$D" ]; then
  echo "$D exists. Remove it yourself or name another directory." >&2
  exit 2
fi
mkdir -p "$D"
cd "$D"

umask 077

openssl req -x509 -newkey rsa:3072 -sha256 -days 365 -nodes \
  -keyout ca.key -out ca.crt \
  -subj "/CN=NGC Test Store CA" \
  -addext "basicConstraints=critical,CA:TRUE,pathlen:0" \
  -addext "keyUsage=critical,keyCertSign,cRLSign"

openssl req -newkey rsa:2048 -sha256 -nodes \
  -keyout store.key -out store.csr \
  -subj "/CN=$NAME"

cat > ext.cnf <<X
basicConstraints=critical,CA:FALSE
keyUsage=critical,digitalSignature,keyEncipherment
extendedKeyUsage=serverAuth
subjectAltName=DNS:$NAME
X

openssl x509 -req -in store.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -sha256 -days 365 -out store.crt -extfile ext.cnf

chmod 0644 ca.crt store.crt store.csr ext.cnf

# Self-check of the preparation: without this the script could hand on a chain
# that nothing accepts, and the API's refusal would look like a finding.
openssl verify -CAfile ca.crt store.crt
openssl x509 -in store.crt -noout -ext subjectAltName
echo "TLS-PREP-OK ($NAME in $D)"
