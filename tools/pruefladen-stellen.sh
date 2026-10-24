#!/bin/sh
# Makes a signed TEST store reachable over HTTPS on this machine.
#
# This belongs to no release, same as tools/pruefladen-tls.sh: it stands up the
# store an API can really fetch from while there is no official NGC store.
#
# Rescued on 24.10.2026 off the first single machine, where it lived in a home
# directory (/home/berry/ngcapp/install-store.sh) and was in no repository.
# Only the paths and the name became arguments; every step still checks first,
# so running it twice changes nothing.
#
#   sudo sh tools/pruefladen-stellen.sh <tls-dir> <store-dir> [name]
#
#   tls-dir    the output directory of tools/pruefladen-tls.sh
#   store-dir  the signed files: catalog.json plus the .ngc packages it names
#   name       DNS name, default store.ngc.test - must be the name the
#              certificate was built for, or Apache serves a chain for a
#              different name and the API is right to refuse
#
# Needs root: it writes into the system trust store, /etc/hosts and Apache.
set -e

T="${1:?tls-dir missing - the output of tools/pruefladen-tls.sh}"
S="${2:?store-dir missing - the directory holding catalog.json}"
NAME="${3:-store.ngc.test}"

[ "$(id -u)" = 0 ] || { echo "Needs root." >&2; exit 2; }

for f in "$T/ca.crt" "$T/store.crt" "$T/store.key" "$S/catalog.json"; do
  [ -f "$f" ] || { echo "missing: $f" >&2; exit 2; }
done

# The certificate has to be the one for this name. Checked here, because the
# alternative is an API that refuses later and a finding that reads like NGC's
# fault.
openssl x509 -in "$T/store.crt" -noout -ext subjectAltName | grep -q "DNS:$NAME" || {
  echo "$T/store.crt is not valid for $NAME:" >&2
  openssl x509 -in "$T/store.crt" -noout -ext subjectAltName >&2
  exit 2
}

# 1. The CA root into the system trust store. The API builds its own client
#    (appstore.New) with no RootCAs of its own, so it uses this pool, and the
#    chain is really judged (point 45).
install -m 0644 "$T/ca.crt" "/usr/local/share/ca-certificates/ngc-test-store-ca.crt"
update-ca-certificates >/dev/null

# 2. The name. The API resolves through the system resolver.
grep -q "[[:space:]]$NAME\([[:space:]]\|$\)" /etc/hosts || echo "127.0.0.1 $NAME" >> /etc/hosts

# 3. The files, readable by www-data, out of the web root of the shell.
mkdir -p /srv/ngc-store
cp "$S/catalog.json" /srv/ngc-store/
# Every package the catalog names, not a hard-coded file name: the store grows
# by apps, and a missing package is a 404 the API reports as a broken store.
for p in "$S"/*.ngc; do
  [ -e "$p" ] || continue
  cp "$p" /srv/ngc-store/
done
chmod 0644 /srv/ngc-store/*

mkdir -p /etc/ngc/tls
install -m 0644 "$T/store.crt" /etc/ngc/tls/store.crt
install -m 0600 "$T/store.key" /etc/ngc/tls/store.key

# 4. Its own vhost on 443, beside the one of the shell.
cat > /etc/apache2/sites-available/ngc-test-store.conf <<X
# A plain file server: a store is files on any web server (draft 4.4).
<VirtualHost *:443>
    ServerName $NAME
    DocumentRoot "/srv/ngc-store"
    SSLEngine on
    SSLCertificateFile    /etc/ngc/tls/store.crt
    SSLCertificateKeyFile /etc/ngc/tls/store.key
    <Directory "/srv/ngc-store">
        Require all granted
        Options -Indexes -FollowSymLinks
        AllowOverride None
    </Directory>
</VirtualHost>
X
a2ensite ngc-test-store >/dev/null
apache2ctl configtest
systemctl reload apache2

# Self-check: the store has to answer over the name, with the chain the system
# now trusts. Without this the script reports success on a vhost nobody can
# fetch from.
curl -fsS "https://$NAME/catalog.json" >/dev/null
echo "STORE-INSTALL-OK ($NAME, $(ls /srv/ngc-store | tr '\n' ' '))"
