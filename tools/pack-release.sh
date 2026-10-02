#!/bin/sh
# pack-release.sh - builds the release archive of the NGC web interface.
#
# Usage: pack-release.sh <ref> <seq> [outdir]
#   <ref>    a git ref or commit holding apps/web. For a PUBLISHED release this
#            must be a TAG: a branch moves, and "the same source" is then a
#            different thing tomorrow (web went 62d618e -> fe5a0e4 inside one
#            day while this was written).
#   <seq>    the number of this release, a positive integer (store.Release.Seq).
#            ngc-web.sh refuses a release whose seq is below the installed one
#            in /var/lib/ngc/release.seq, and the same seq may come again to
#            repair an installation (shared/ulh.md).
#   [outdir] where to write; default dist/release/<version>/ (dist/ is ignored)
#
# It writes two files and nothing else:
#   ngc-web-all.tar        the tree of apps/web without tests/, no prefix
#   release.payload.json   the UNSIGNED payload of a store.Release
#
# IT HOLDS NO KEY AND SIGNS NOTHING. Signing is a separate, human step on the
# machine that holds the private release key: tools/sign-release.sh. Why apart:
# a packer that could sign is a packer that could leak the key, and this one
# runs wherever somebody builds.
#
# WHAT IT DOES NOT DECIDE. Whether the archive is COMPLETE is not judged here.
# tools/release/closure.go follows the references of the four entrypoints and
# names what is missing; it runs in go test, hence in tools/check.sh, hence
# before every commit. This script checks the SHAPE: the one entry point
# ngc-web.sh looks for, no tests/, no path leading out, nothing but files and
# folders. The line at the end says so, because a builder who skips the tests
# should at least be told.
#
# WHY git archive AND NOT tar. A tar of the working tree carries the clock, the
# umask, the order the file system hands out, and whatever the checkout did to
# the bytes. git archive takes order from the tree, owner root/root and the mode
# from the git entry. Two flags close what is left, both measured, not assumed
# (plan 2026-10-02, section 0.9):
#   --mtime               a TREE has no date, so git archive takes the clock and
#                         two runs differ. Pinned to the commit date of <ref>.
#   core.autocrlf=false   measured: without it VERSION comes out as "1.2.3\r\n"
#                         and index.php differs from its own blob, so a Windows
#                         build and a Linux build of one source give two
#                         checksums. THE CAUSE, measured: git archive reads
#                         .gitattributes from the tree IT ARCHIVES, and
#                         .gitattributes lies in the repository root while
#                         apps/web has none - so with "web:apps/web" the
#                         "* text=auto eol=lf" rule is outside the archived tree
#                         and does not apply, and the global core.autocrlf=true
#                         wins. The same commit archived from the ROOT gives LF.
#                         "-c core.eol=lf" is NOT set, because it was measured
#                         to do nothing here; a configuration line that has no
#                         effect reads like a proof to the next person.
#
# WHAT IS LEFT OUT, and both exclusions have a measurement behind them:
#   tests/
#     templates/apache-vhost.conf says it itself: "tests/ the browser tests,
#     with node_modules/ in them. A release does not carry them."
#   public/assets/js/net/testlink.js
#     A shortcut only the browser tests use. Measured on the real tree: every
#     mention of it in SHIPPED code is a COMMENT (public/assets/js/apps/store.js:239,
#     auth/keys.js:37, auth/login.js:50), so the reference graph reaches it from
#     nowhere; the live imports all sit under apps/web/tests/ - attacks.spec.js:361,
#     world.spec.js:606 and :896, channels.spec.js:20 :31 :48 :65 :83 :110 :134
#     and on - every one of them
#     await import("/assets/js/net/testlink.js"), and tests/ does not travel.
#     Decided on 02.10.2026 by the web strand: a file only the tests import has
#     no business in a release. It is left out HERE and NOT listed in
#     tools/release/extra-web.txt, because a line there owes a reason and the
#     only reason available would have been invented. The pathspec names the one
#     FILE: public/assets/js/net/ holds shipped code as well.
set -eu
LC_ALL=C
TZ=UTC
export LC_ALL TZ

self=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

die() {
	echo "pack-release: $1" >&2
	exit 1
}

[ $# -ge 2 ] && [ $# -le 3 ] || die "usage: pack-release.sh <ref> <seq> [outdir]"
ref=$1
seq=$2
case $seq in
'' | *[!0-9]*) die "seq is not a number: $seq" ;;
esac
[ "$seq" -gt 0 ] || die "seq must be above zero"

# THE GIT GUARD, FIRST, AND WITH THE RIGHT MESSAGE. A worktree outside the
# repository needs safe.directory, and without it git refuses - but the next
# command would then say "no such commit", which points at the wrong cause and
# costs an hour. Measured in a worktree under the temp folder with its .git on a
# NAS. It is set for THIS process only; nothing global is touched.
if ! git rev-parse --git-dir >/dev/null 2>&1; then
	die "git refuses to work here. A worktree outside the repository needs the guard, for this call only:
  export GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=safe.directory GIT_CONFIG_VALUE_0=\"\$PWD\"
Never set safe.directory globally. The error git gives is:
$(git rev-parse --git-dir 2>&1 || true)"
fi

git rev-parse --verify -q "$ref^{commit}" >/dev/null || die "no such commit: $ref"
commit=$(git rev-parse --verify "$ref^{commit}")
git cat-file -e "$ref:apps/web/VERSION" 2>/dev/null || die "$ref carries no apps/web/VERSION"

# THE VERSION GATE IS store.ValidVersion AND NOTHING LAXER
# (shared/store/manifest.go:224: ^(0|[1-9][0-9]{0,5})\.(…)\.(…)$). verify.sh does
# NOT check the version, so a version that only looks like one is packed,
# signed, published and refused at the USER as store.ErrRelease. Written out as
# three fields so a plain sh can do it: each one is 0, or 1 to 6 digits not
# starting with 0.
version=$(git cat-file blob "$ref:apps/web/VERSION" | tr -d ' \t\r\n')
check_part() {
	case $1 in
	0) return 0 ;;
	[1-9]) return 0 ;;
	[1-9][0-9] | [1-9][0-9][0-9] | [1-9][0-9][0-9][0-9]) return 0 ;;
	[1-9][0-9][0-9][0-9][0-9] | [1-9][0-9][0-9][0-9][0-9][0-9]) return 0 ;;
	esac
	return 1
}
badversion() {
	die "VERSION is \"$version\", which store.ValidVersion rejects (three numbers, each 0 or up to six digits without a leading zero). verify.sh would not catch it and the user would."
}
case $version in
*[!0-9.]* | *..* | .* | *.) badversion ;;
esac
major=${version%%.*}
rest=${version#*.}
case $rest in
*.*) ;;
*) badversion ;;
esac
minor=${rest%%.*}
patch=${rest#*.}
case $patch in
*.*) badversion ;;
esac
check_part "$major" && check_part "$minor" && check_part "$patch" || badversion

out=${3:-dist/release/$version}
mkdir -p -- "$out" || die "cannot create $out"
out=$(CDPATH= cd -- "$out" && pwd)
work=$(mktemp -d) || die "mktemp failed"
trap 'rm -rf -- "$work"' EXIT
trap 'exit 1' HUP INT TERM

# The commit date, so one commit always gives one date - also when it is built
# again next year. It is the archive's mtime AND store.Release.Issued: issued
# sits inside the payload, so the clock of whoever builds would break the
# reproducibility of the payload as well. Validate only asks for > 0.
stamp=$(git show -s --format=%ct "$commit")
case $stamp in
'' | *[!0-9]*) die "the commit has no usable date: $ref" ;;
esac

git -c core.autocrlf=false archive --format=tar --mtime="@$stamp" \
	"$commit:apps/web" ':(exclude)tests' \
	':(exclude)public/assets/js/net/testlink.js' >"$work/ngc-web-all.tar" ||
	die "git archive failed"

tar -tf "$work/ngc-web-all.tar" >"$work/entries" || die "the archive cannot be read"
tr -d '\r' <"$work/entries" >"$work/entries.lf"

# The one file ngc-web.sh checks itself (line 619). Everything else is the
# graph's business, not this script's.
grep -qxF -- 'public/index.php' "$work/entries.lf" ||
	die "public/index.php is not in the archive; ngc-web.sh refuses such a release (line 619)"

grep -q '^tests/' "$work/entries.lf" && die "the archive carries files below tests/"

# Safe unpacking of NGC (shared/README.md), said here as well as in ngc-web.sh:
# a path that leads out, or anything that is not a file or a folder, must not be
# built in the first place.
while IFS= read -r entry; do
	case $entry in
	/* | .. | ../* | */../* | */..) die "the archive holds a path that leads out: $entry" ;;
	esac
done <"$work/entries.lf"
if tar -tvf "$work/ngc-web-all.tar" | tr -d '\r' | grep -qv '^[-d]'; then
	die "the archive holds links or device files"
fi

size=$(wc -c <"$work/ngc-web-all.tar" | tr -d ' ')
# The checksum as BYTES, base64url without padding, the way enc.Bytes reads it.
# Not through hex and back: that is one more place to get an escape wrong, and
# this has to match a sha256sum on the server exactly.
openssl dgst -sha256 -binary -- "$work/ngc-web-all.tar" >"$work/sum.bin" ||
	die "the checksum could not be built"
[ "$(wc -c <"$work/sum.bin" | tr -d ' ')" = 32 ] || die "the checksum is not 32 bytes"
sha=$(base64 -w0 <"$work/sum.bin" | tr '+/' '-_' | tr -d '=')
[ ${#sha} -eq 43 ] || die "the checksum is not 43 characters of base64url"
# Two tools, one answer, or the release is not built: sha256sum is what verify.sh
# runs on the server.
want=$(od -An -tx1 <"$work/sum.bin" | tr -d ' \n')
got=$(sha256sum -- "$work/ngc-web-all.tar" | cut -d' ' -f1)
[ "$want" = "$got" ] || die "openssl and sha256sum disagree about the archive"

# store.Release, and only the members the contract names: no "notes", because an
# empty member must be absent entirely (strict JSON, shared/README.md).
printf '{"seq":%s,"issued":%s,"version":"%s","files":[{"part":"ngc-web","arch":"all","url":"ngc-web-all.tar","size":%s,"sha256":"%s"}]}' \
	"$seq" "$stamp" "$version" "$size" "$sha" >"$work/release.payload.json"

cp -f -- "$work/ngc-web-all.tar" "$out/ngc-web-all.tar"
cp -f -- "$work/release.payload.json" "$out/release.payload.json"

echo "ngc-web $version, release $seq, from commit $commit"
case $ref in
"$commit") ;;
*) echo "  NOTE: $ref is a name, and a name moves. Publish from a tag." ;;
esac
echo "  $out/ngc-web-all.tar          $size bytes"
echo "  $out/release.payload.json     not signed yet"
echo "Completeness is NOT checked here. Run it:"
echo "  go test ./tools/release/"
echo "Then sign it on the machine that holds the release key:"
echo "  tools/sign-release.sh $out <key.pem>"
