#!/usr/bin/env sh
# Raises the pinned base image of the terminal app - the ONLY way the FROM line
# of apps/server-controller/templates/terminal/Containerfile may be changed.
#
# WHY A TOOL AND NOT A HAND: the line carries a digest, and a digest typed by
# hand is a number nobody measured. Everything below exists to make the number
# measured: it is fetched from the registry, and then it is VERIFIED against
# the bytes it names - sha256 over the manifest body has to equal the digest
# the registry put in its header. Only then is the file touched.
#
# WHO RUNS IT AND WHEN - the question a pinned base creates and does not answer
# by itself:
#
#   A pinned base receives no security fix. Debian publishes one, the tag moves,
#   and this digest does not. So somebody has to run this, and the proposal is:
#   whoever cuts a release runs it BEFORE the release and commits the change as
#   its own commit, so that the base of every shipped image is a decision of
#   that release and is visible in its diff. Nothing automatic: a digest that a
#   job raises on its own is a tag again, only slower.
#
#   Who may change it: whoever may sign a release. The digest decides what runs
#   in the container of the terminal app, and that app's manifest says
#   needs: ["root"] - so it is the same power as signing, and it belongs to the
#   same person.
#
# NOT MEASURED, and it is the usual limit of this machine: that podman actually
# fetches this digest and refuses a mismatch. There is no podman and no docker
# here. What IS measured is the digest itself (fetched and re-hashed, below)
# and that the build refuses a Containerfile without one
# (runtime.CheckBase, apps/server-controller/classes/runtime/build_test.go).
set -eu

repo=${NGC_BASE_REPO:-library/debian}
tag=${NGC_BASE_TAG:-13-slim}
file=$(dirname "$0")/../apps/server-controller/templates/terminal/Containerfile
write=no
[ "${1:-}" = "--write" ] && write=yes

for t in curl sha256sum; do
	command -v "$t" >/dev/null 2>&1 || { echo "pin-base: $t is missing" >&2; exit 1; }
done
[ -r "$file" ] || { echo "pin-base: cannot read $file" >&2; exit 1; }

tmp=$(mktemp)
trap 'rm -f "$tmp" "$tmp.head"' EXIT

token=$(curl -fsS "https://auth.docker.io/token?service=registry.docker.io&scope=repository:$repo:pull" |
	sed -n 's/.*"token":"\([^"]*\)".*/\1/p')
# The token is a precondition, not a result: without it the next call answers
# 401 with a body, and a sed over that body would hand out something that looks
# like a digest.
[ -n "$token" ] || { echo "pin-base: no pull token for $repo" >&2; exit 1; }

accept='application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.docker.distribution.manifest.v2+json'
curl -fsS -D "$tmp.head" -o "$tmp" \
	-H "Authorization: Bearer $token" -H "Accept: $accept" \
	"https://registry-1.docker.io/v2/$repo/manifests/$tag"

said=$(tr -d '\r' < "$tmp.head" | sed -n 's/^[Dd]ocker-[Cc]ontent-[Dd]igest: *//p' | tail -n 1)
own=sha256:$(sha256sum "$tmp" | cut -d' ' -f1)

# THE VERIFICATION, and it is the whole point of the tool: the registry's word
# against the bytes it sent. A proxy that rewrote the body would be caught here,
# and so would a header read out of the wrong place.
[ -n "$said" ] || { echo "pin-base: the registry sent no digest header" >&2; exit 1; }
if [ "$said" != "$own" ]; then
	echo "pin-base: the registry says $said, the bytes hash to $own - NOTHING WRITTEN" >&2
	exit 1
fi
case $own in
	sha256:????????????????????????????????????????????????????????????????) ;;
	*) echo "pin-base: $own is no sha256 digest" >&2; exit 1 ;;
esac

# Both architectures NGC builds for have to be in there, or the one digest does
# not serve both --arch builds and the second one fails on a build machine.
for arch in amd64 arm64; do
	grep -q "\"architecture\":\"$arch\"" "$tmp" ||
		{ echo "pin-base: the manifest names no $arch - NOTHING WRITTEN" >&2; exit 1; }
done

want="FROM docker.io/$repo:$tag@$own"
have=$(sed -n 's/^FROM .*/&/p' "$file" | head -n 1)
echo "pin-base: measured $own over $(wc -c < "$tmp" | tr -d ' ') bytes, amd64 and arm64 present"
echo "pin-base: in the file: $have"
echo "pin-base: would be   : $want"
[ "$have" = "$want" ] && { echo "pin-base: unchanged"; exit 0; }
[ "$write" = yes ] || { echo "pin-base: nothing written (pass --write)"; exit 0; }

# Written through a new file and a rename: a run broken off in the middle must
# not leave a Containerfile with half a FROM line in it.
new=$file.new
awk -v want="$want" 'sub(/^FROM .*/, want) {n++} {print} END {if (n!=1) exit 1}' "$file" > "$new" ||
	{ rm -f "$new"; echo "pin-base: the file has no single FROM line - NOTHING WRITTEN" >&2; exit 1; }
mv "$new" "$file"
echo "pin-base: written. Commit it on its own and say which release it is for."
