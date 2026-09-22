#!/bin/sh
# Prints the pin NGC gives a TLS key (trust.TLSPin): base64url without
# padding of the SHA-256 of the certificate's SubjectPublicKeyInfo.
# Usage: pin.sh <certificate.pem>
set -eu
[ $# -eq 1 ] || { echo "usage: pin.sh <certificate.pem>" >&2; exit 2; }
# A pipeline fails only with its last command, so the key is read on its
# own: a certificate that cannot be read must not turn into a pin.
key=$(openssl x509 -in "$1" -noout -pubkey) || { echo "pin: cannot read $1" >&2; exit 1; }
pin=$(printf '%s\n' "$key" | openssl pkey -pubin -outform DER |
	openssl dgst -sha256 -binary | base64 -w0 | tr '+/' '-_' | tr -d '=')
# A step above that failed left nothing to hash; the SHA-256 of nothing is
# no key's pin.
[ ${#pin} -eq 43 ] && [ "$pin" != 47DEQpj8HBSa-_TImW-5JCeuQeRkm5NMpJWZG3hSuFU ] ||
	{ echo "pin: cannot read $1" >&2; exit 1; }
echo "$pin"
