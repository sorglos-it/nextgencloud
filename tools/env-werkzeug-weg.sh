#!/usr/bin/env bash
# Takes one tool out of the PATH and checks that tools/recovery-env.sh NAMES
# the missing one.
#
# Why it earns a place: recovery-env.sh carries a need= list, and a list is a
# promise. A check that only runs where everything is installed never tests
# that promise. This builds a replacement PATH holding exactly the tools of
# that list plus the shell basics, removes one, and reads what comes back.
# The list is read OUT OF THE SCRIPT, so a tool added there is covered without
# anyone remembering to add it here too.
#
# Rescued on 24.10.2026 off the first single machine, where it lived in
# ~/task6fix1/n9.sh and was in no repository.
#
#   bash tools/env-werkzeug-weg.sh [werkzeug ...]
#
# Without arguments it tries grep, sed and tpm2_readpublic - one shell tool,
# one stream tool, one TPM tool. The first pass removes nothing and must be
# green: a run that is red with everything present measures nothing afterwards.
#
# The removals ACCUMULATE, and that is deliberate: the output then shows the
# list growing ("Es fehlt: grep", then "grep sed", then "grep sed
# tpm2_readpublic"), which also measures that recovery-env.sh names ALL the
# missing tools and not just the first one it trips over.
#
# Needs tools/recovery-tools.sh to have run once, because recovery-env.sh wants
# dist/recovery/werkzeuge.txt. Without it the first pass is red and the script
# stops there instead of blaming the need= list.
set -u

cd "$(dirname "$0")/.."
env_script="tools/recovery-env.sh"
[ -f "$env_script" ] || { echo "nicht gefunden: $env_script" >&2; exit 2; }

d="$(mktemp -d)"
out="$(mktemp)"
trap 'rm -rf -- "$d" "$out"' EXIT

need="$(sed -n '/^need=/,/"$/p' "$env_script" | sed 's/^need=//' | tr -d '"')"
[ -n "$need" ] ||
	{ echo "ABBRUCH: keine need=-Liste in $env_script gefunden - ist sie umbenannt?" >&2; exit 1; }

extra="basename dirname cat head tr wc mkdir rm ls cp mv date sort seq sha256sum sleep uname chmod env bash"
for t in $need $extra; do
	p="$(command -v "$t" 2>/dev/null)" && ln -sf "$p" "$d/$t"
done
printf 'Verknuepfungen im Ersatz-PATH: %s (need= nennt %s)\n' \
	"$(ls "$d" | wc -l)" "$(printf '%s\n' $need | wc -l)"

# Not "${@:-...}": quoted, that collapses the default into ONE word and the
# loop then looks for a tool called "grep sed tpm2_readpublic".
if [ "$#" -gt 0 ]; then liste="$*"; else liste="grep sed tpm2_readpublic"; fi

fehler=0
for weg in NICHTS $liste; do
	[ "$weg" = NICHTS ] || rm -f "$d/$weg"
	PATH="$d" bash "$env_script" > "$out" 2>&1 && rc=0 || rc=$?
	erste="$(head -1 "$out")"
	printf -- '-- ohne [%s] EXIT=%s | %s\n' "$weg" "$rc" "$erste"

	if [ "$weg" = NICHTS ]; then
		# With everything present the run has to be green, or every line below
		# is about a broken environment and not about the need= list.
		[ "$rc" = 0 ] ||
			{ echo "   ABBRUCH: mit allen Werkzeugen schon rot - hier ist nichts zu messen" >&2; exit 1; }
		continue
	fi

	# Red alone is not enough. The point is that the output NAMES the tool;
	# a run that fails without saying what is missing leaves the reader
	# searching, which is exactly the fault this measures.
	if [ "$rc" = 0 ]; then
		echo "   FEHLT DER RIEGEL: ohne $weg bleibt der Lauf gruen"
		fehler=1
	elif ! grep -q -- "$weg" "$out"; then
		echo "   NICHT BENANNT: rot, aber $weg steht nicht in der Ausgabe"
		fehler=1
	fi
done

[ "$fehler" = 0 ] || { echo "env-werkzeug-weg: ROT"; exit 1; }
echo "env-werkzeug-weg: ok"
