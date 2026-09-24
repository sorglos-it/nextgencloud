# Recovery-Modus, Arbeitspaket 0: Testumgebung

> **Für Agenten:** PFLICHT-SUBSKILL: `superpowers:subagent-driven-development` (empfohlen) oder
> `superpowers:executing-plans`, Aufgabe für Aufgabe. Die Schritte sind Kästchen (`- [ ]`).

**Ziel:** Auf der Debian-VM `ngc-recovery` steht eine Testumgebung, in der das Versiegeln und Öffnen
eines Geheimnisses mit einer TPM-Policy aus sieben Zweigen (PolicyOR) gegen swtpm und simulierte
Root-Sticks nachweisbar funktioniert — das eine Abnahmekriterium von Paket 0.

**Aufbau:** Acht Shell-Skripte in `tools/`, die aufeinander aufbauen: Werkzeuge feststellen,
Umgebung prüfen, einrichten, swtpm steuern, QEMU starten, Policy bauen und öffnen, alles testen,
alles als CI-Job fahren.
Der Digest jedes Policy-Zweigs und das spätere Öffnen entstehen aus **einer** gemeinsamen Funktion
mit identischer Befehlsfolge — nur so kann der beim Öffnen erzeugte Digest den versiegelten treffen.
Alle Laufzeitdaten (swtpm-Zustand, SoftHSM2-Token, Schlüssel, Kontexte, PIN-Dateien) liegen unter
`dist/recovery/` und damit nie in Git. Keine Rust-Zeile in diesem Paket; die kommt mit Paket 1.

**Technik:** Debian 13, `tpm2-tools` gegen `swtpm` im Socket-Modus, `SoftHSM2` über `pkcs11-tool`
als Ersatz für vier Root-Sticks plus einen Reservestick, `qemu-system-x86_64` mit Secure-Boot-OVMF,
Bash.

## Verbindliche Vorgaben

Aus `docs/recovery-modus.md` und `CLAUDE.md`. Sie gelten für **jede** Aufgabe dieses Plans, auch
wenn sie dort nicht wiederholt werden.

- Vorgabe ist `docs/recovery-modus.md`. Abschnitt 11, Paket 0, Abnahme: „Seal/Unseal mit PolicyOR
  aus 7 Zweigen auf swtpm". Das ist das **einzige** Abnahmekriterium dieses Pakets.
- **Nie ein Sicherheitsmerkmal abschwächen.** Geht etwas mit den vorhandenen Werkzeugen nicht:
  stoppen und den Koordinator fragen. Kein Ersatzweg, der weniger beweist.
- **Fail-closed.** Jeder Fehler bricht ab. Jedes Skript beginnt mit `set -euo pipefail`. In diesem
  Paket steht **nirgends** `|| true`, `|| :`, `2>/dev/null` oder ein anderer Notausgang, der einen
  fehlgeschlagenen Befehl verdeckt. Einzige Ausnahme ist die eine Aufräumzeile im `trap` (siehe
  unten); sie ist dort begründet.
- **Sitzungen und Objekte aufräumen.** Jedes Skript, das TPM-Sitzungen anlegt, setzt als Erstes
  `trap 'for h in -t -l -s; do tpm2_flushcontext "$h" >/dev/null 2>&1 || :; done' EXIT`.
  `-t` allein räumt nur transiente Objekte ab; eine Sitzung bleibt damit im TPM stehen. Genau das
  passiert in jedem Negativfall, weil die Freigabe am Ende der Funktion nur auf dem Erfolgspfad
  erreicht wird — deshalb zusätzlich `-l` (geladene) und `-s` (gesicherte Sitzungen). Sonst wäre
  nach ein paar Negativfällen kein Sitzungsplatz mehr frei und der nächste Fall scheiterte aus dem
  falschen Grund. Diese eine Zeile darf Fehler schlucken, weil sie nur aufräumt und nie etwas
  prüft; die Hilfsfunktion `flush` im Ablauf bleibt bei `-t`, damit sie die laufende Sitzung nicht
  wegwirft.
- **Alles nach stderr.** Jede Meldung, jeder Hinweis und jede Fehlermeldung des Skripts geht mit
  `>&2` nach stderr. Auf stdout steht nur das, was ein aufrufendes Skript weiterverarbeitet.
  Sonst verschwinden Meldungen hinter `>/dev/null` und `| grep -q`. Die Ausgabe der aufgerufenen
  Werkzeuge wird mit `>/dev/null` (nur stdout!) stillgelegt, nie mit `2>/dev/null`.
- **Keine Geheimnisse ins Log:** keine PINs, Schlüssel oder Nonces ausgeben, auch nicht gekürzt.
- **PINs nie als Kommandozeilenargument.** Sie stehen sonst in `/proc/<pid>/cmdline` und in der
  Prozessliste. PINs liegen in Dateien mit Rechten 0600 unter `dist/recovery/pin/`; die Stufe
  `test` sieht „PIN-Quelle: Datei" ohnehin vor (`docs/recovery-modus.md`, Abschnitt 4).
  Test-Schlüssel und Test-PINs dürfen nach Abschnitt 13 der Vorgabe im Repo liegen — hier liegen
  sie trotzdem außerhalb von Git und heißen überall ausdrücklich TEST.
- **SHA-384 überall.** PCR-Bank, Sitzungen (`tpm2_startauthsession -g sha384`), Namensalgorithmus
  geladener Stick-Schlüssel (`tpm2_loadexternal -g sha384`), Objekte (`tpm2_create -g sha384`).
  SHA-256 ist in diesem Paket nur die zweite, unbenutzte PCR-Bank von swtpm.
- **Sitzungen gesalzen und verschlüsselt.** Jede Sitzung wird mit
  `tpm2_startauthsession --tpmkey-context 0x81000001` gegen den Primärschlüssel gesalzen und danach mit
  `tpm2_sessionconfig --enable-encrypt --enable-decrypt <sitzung>` auf Parameterverschlüsselung
  gestellt — auch die Trial-Sitzungen, damit das Muster genau so nach Paket 3 wandert. Die Vorgabe
  (Abschnitt 6) verlangt das ohne Einschränkung auf echte Hardware.
- **`expiration` ist immer 0** (`tpm2_policysigned -t 0`). Keine Autorisierung gilt länger als die
  eine Sitzung, in der sie entsteht.
- **Objektplätze freiräumen.** swtpm hält nur zwei transiente Objekte. Vor jedem Befehl, der ein
  Objekt lädt, steht deshalb `tpm2_flushcontext -t`; im Plan macht das die Hilfsfunktion `flush`.
  Der Primärschlüssel liegt fest unter `0x81000001` und wird nie als Kontextdatei herumgereicht.
- **Kein `noda`.** Versiegelte Objekte tragen den Wörterbuchschutz. Statt dessen wird der Lockout
  mit `tpm2_dictionarylockout` auf die Vorgabe gesetzt: 3 Versuche, 24 h Sperre.
- Die Policy hat **sieben** Zweige (ein Normalstart-Zweig plus sechs Stick-Paare aus vier Sticks),
  also die Form der Stufe `enterprise`. Entwickelt wird in Stufe `test`; das Abnahmekriterium prüft
  trotzdem die große Form, weil das TPM höchstens acht Zweige je PolicyOR kann und das früh
  feststehen muss.
- Ordner- und Dateinamen englisch, klein, Trenner `-`. Neue Ordner nur mit Namen aus dem
  Projektstruktur-Standard. Alles Erzeugte nach `dist/`.
- Code, Bezeichner, Kommentare und Commit-Nachrichten **englisch**; Ausgaben für Thomas und jede
  Fehlermeldung **deutsch** und so, dass sie sagt, was als Nächstes zu tun ist.
- Vor **jedem** Commit `tools/check.sh`. Jeder Commit trägt den Trailer
  `Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>`. Branch `recovery`, kleine Commits, am
  Ende CHANGELOG-Eintrag, Tag und ein Pull Request nach `main`.
- Kommentare erklären, warum etwas so ist, nicht was die Zeile tut.
- Jede Abweichung von `docs/recovery-modus.md` wird als offener Punkt in `CHANGELOG.md` unter
  „Offen" eingetragen (Vorgabe, Abschnitt 13).

## Umgebung

Die VM `ngc-recovery` (Proxmox, Debian 13, 4 Kerne, 8 GB RAM, 60 GB Platte) hat Netz für `apt` und
den Benutzer `berry` mit `sudo` ohne Passwort. **Ohne KVM:** der Host reicht keine Virtualisierung
durch, QEMU läuft emuliert und dadurch langsam — der QEMU-Testfall braucht darum spürbar länger
als die TPM-Fälle. **Das Repo bleibt auf Thomas' Rechner.** Die VM bekommt keinen Zugang zum privaten Repo und keine
Zugangsdaten; sie bekommt nur die Skripte. Vor jedem Lauf werden sie gespiegelt:

```bash
tar -c tools | ssh ngc-recovery 'mkdir -p ~/ngc && tar -x -C ~/ngc'
ssh ngc-recovery 'cd ~/ngc && tools/recovery-test.sh'
```

Jedes Skript findet seinen Platz selbst (`cd "$(dirname "$0")/.."`); das Arbeitsverzeichnis heißt
auf der VM also `~/ngc/dist/recovery`. Geschrieben, `tools/check.sh` geprüft und committet wird auf
Thomas' Rechner, wo Go 1.27 steht — auf der VM wird nur ausgeführt.

## Dateien

| Datei | Aufgabe |
| --- | --- |
| `tools/recovery-tools.sh` | prüft Fassungen und Optionen der Werkzeuge gegen die geprüften Fakten und schreibt das Ergebnis nach `dist/recovery/werkzeuge.txt` |
| `tools/recovery-env.sh` | prüft, ob alle Werkzeuge da sind und ob das TPM eine SHA-384-PCR-Bank hat; nennt sonst genau, was fehlt |
| `tools/recovery-setup.sh` | `install` (einmalig, mit sudo und Netz) und `prepare` (Token, Schlüssel, PIN-Dateien, ohne sudo) |
| `tools/recovery-swtpm.sh` | startet, stoppt und setzt benannte Instanzen des TPM-Simulators zurück |
| `tools/recovery-qemu.sh` | `check`: QEMU mit Secure-Boot-OVMF und **eigener** swtpm-Instanz; `stop` |
| `tools/recovery-policy.sh` | baut die sieben Zweige, versiegelt, öffnet, erhöht den Zähler, fährt die Negativfälle |
| `tools/recovery-test.sh` | fährt alle Fälle durch, auch die, die scheitern müssen |
| `tools/recovery-ci.sh` | der CI-Job: Werkzeuge prüfen, Testumgebung herrichten, alles testen, Protokoll schreiben |
| `docs/recovery-abnahme.md` | Abnahmeprotokoll: die Paket-Abnahmen und die 18 Kriterien |

Arbeitsverzeichnis `dist/recovery/` (nicht in Git):

| Ordner | Inhalt |
| --- | --- |
| `tpm/<instanz>/` | Zustand und Sockets einer swtpm-Instanz (`main` für die Policy, `qemu` für QEMU) |
| `softhsm/` | SoftHSM2-Konfiguration und Token |
| `keys/` | öffentliche Stick-Schlüssel |
| `pin/` | PIN-Dateien, Rechte 0600 |
| `ovmf/` | beschreibbare Kopie der Firmware-Variablen |
| `work/` | Sitzungen, Zweig-Digests, versiegeltes Objekt, TPM-Kontexte |
| `ci/` | Protokolle der CI-Läufe |

`werkzeuge.txt` liegt direkt in `dist/recovery/`, weil die Datei den ganzen Ordner beschreibt und
jeden Lauf überdauert.

---
## Geprüfte Fakten von der VM

Der Koordinator hat diese Punkte am 23.09.2026 auf `ngc-recovery` selbst durchgemessen. Sie sind
**keine Annahmen**. Die beiden belegenden Skripte liegen dort unter `~/smoke/run.sh` (openssl) und
`~/smoke/stick.sh` (SoftHSM2); beide enden mit `GRUEN`. Wer eine Befehlszeile dieses Plans ändern
will, prüft zuerst dort.

| Punkt | Festgestellt |
| --- | --- |
| Fassungen | tpm2-tools 5.7, swtpm 0.7.1, SoftHSM2 2.6.1, QEMU 10.0.13, OVMF mit `OVMF_CODE_4M.secboot.fd` |
| `xxd` | eigenes Paket, muss mit installiert werden |
| `sudo` | nachinstalliert; der Benutzer `berry` darf ohne Passwort |
| **KVM** | **nicht vorhanden** — der Proxmox-Host reicht keine Virtualisierung durch (`modprobe kvm_intel` → Ein-/Ausgabefehler). QEMU läuft emuliert, **ohne** `accel=kvm`. Das ist hinzunehmen und steht als offener Punkt am Ende |
| Nonce | `tpm2_policynoncetpm` gibt es nicht. `tpm2_policysigned` hat `-x/--nonce-tpm`, `-t/--expiration`, `--raw-data`, `-q/--qualification`, `--ticket`, `--timeout` |
| Öffnen | zwei Schritte: `tpm2_policysigned … -g sha384 -t 0 -x --raw-data=to_sign.bin` (bei SHA-384 genau 52 Byte: 48 Byte Nonce + 4 Byte Ablauf), signieren, dann `tpm2_policysigned … -g sha384 -t 0 -x -s sig.bin -f ecdsa` |
| Digest bauen | Trial-Sitzung **ohne** `-x`, mit einer Platzhalter-Signatur: `tpm2_policysigned -S trial.ctx -c stick.ctx -g sha384 -s dummy.sig -f ecdsa -t 0`. Dass der so gebaute Digest stimmt, ist belegt — das Öffnen mit `-x` gelingt damit |
| Replay | dieselbe Signatur in einer neuen Sitzung wird vom TPM **abgelehnt** — belegt |
| Digest lesen | `tpm2_getpolicydigest` (nicht `tpm2_policygetdigest`) |
| Sitzungen | `tpm2_startauthsession` kennt `--policy-session`, `-g`, `--tpmkey-context` (Salz) und `--bind-context`; `tpm2_sessionconfig --enable-encrypt --enable-decrypt` gibt es |
| PolicyNV | nimmt Optionen plus die Vergleichsart als Argument: `-i datei … eq` |
| Öffnen des Objekts | `tpm2_unseal -c seal.ctx -p "session:sess.ctx"` |
| **Objektplätze** | swtpm hält nur **zwei** transiente Objekte, und jeder tpm2-Aufruf lädt seinen Kontext neu, ohne ihn freizugeben. Folge 1: der Primärschlüssel wird mit `tpm2_evictcontrol -C o -c primary.ctx 0x81000001` fest abgelegt und danach überall als `-C 0x81000001` benutzt. Folge 2: **vor jedem** Befehl, der ein Objekt lädt, `tpm2_flushcontext -t`. Ohne das bricht alles mit „out of memory for object contexts" ab |
| SoftHSM2 | kennt nur `CKM_ECDSA` (roh), **kein** ECDSA-SHA384. Also außerhalb hashen (`openssl dgst -sha384 -binary`) und den Hash mit `--mechanism ECDSA --signature-format openssl` signieren lassen — genau wie ein echter HSM-Stick. `tpm2_policysigned … -f ecdsa` nimmt das Ergebnis an |
| Öffentlicher Stick-Schlüssel | `pkcs11-tool … --read-object --type pubkey --label stick1 --output-file stick1.der`, dann `openssl ec -pubin -inform DER -in stick1.der -pubout -out stick1.pem`, laden mit `tpm2_loadexternal -C o -G ecc -g sha384 -u stick1.pem -c stick1.ctx` |
| PCR-Bank | `swtpm_setup --tpm2 --tpm-state <dir> --createek --pcr-banks sha256,sha384 --overwrite` legt sha384 an; `--create-ek-cert`/`--create-platform-cert` braucht root und bleibt weg |
| Simulator starten | `swtpm socket --tpm2 --tpmstate dir=$D --server type=unixio,path=$D/sock --ctrl type=unixio,path=$D/sock.ctrl --flags not-need-init,startup-clear --daemon --pid file=$D/pid`, dann `TPM2TOOLS_TCTI="swtpm:path=$D/sock"`. Für QEMU ist `$D/sock.ctrl` der richtige Socket |

Ungeprüft ist genau **ein** Punkt: ob tpm2-tools die Plattform-PIN aus einer Datei nimmt
(`-p "session:sess.ctx+file:…"`). Aufgabe 1 prüft ihn mit einem kleinen Durchlauf; scheitert er,
wird die PIN **nicht** auf die Kommandozeile gelegt, sondern der Koordinator gefragt.

---

### Aufgabe 1: Werkzeuge feststellen

Die Befehlsfolgen dieses Plans stammen aus einem Lauf auf genau dieser VM (Abschnitt „Geprüfte
Fakten"). Diese Aufgabe stellt sicher, dass die installierten Werkzeuge auch wirklich die sind, für
die der Plan geschrieben wurde: Fassungen, Optionsnamen, SHA-384-PCR-Bank. Dazu prüft sie den
**einen** Punkt, den der Koordinator nicht gemessen hat: ob tpm2-tools die Plattform-PIN aus einer
Datei nimmt.

**Dateien:**
- Anlegen: `tools/recovery-setup.sh` (vorerst nur der Unterbefehl `install`)
- Anlegen: `tools/recovery-tools.sh`

**Schnittstellen:**
- Liefert: `tools/recovery-setup.sh install` — installiert die Pakete (braucht sudo und Netz).
- Liefert: `tools/recovery-tools.sh` — Rückgabe 0, wenn alle geprüften Fakten gelten; schreibt
  `dist/recovery/werkzeuge.txt` mit Fassungen und Prüfergebnis. Sonst Rückgabe 1 mit `STOPP:`.
- Liefert: die PIN-Dateien `dist/recovery/pin/stick.txt`, `so.txt`, `platform.txt` mit Rechten 0600
  und die SoftHSM2-Konfiguration `dist/recovery/softhsm/softhsm2.conf`.

**Wenn etwas nicht stimmt:** **stoppen und den Koordinator fragen.** Nicht ersetzen, nicht umgehen.
Eine selbst gewählte Zahl statt der TPM-Nonce oder eine PIN auf der Kommandozeile wäre genau die
Schwächung, die dieser Plan ausschließt.

- [ ] **Schritt 1: Paketliste anlegen**

Datei `tools/recovery-setup.sh`, ausführbar machen (`chmod +x`):

```bash
#!/usr/bin/env bash
# Sets up the recovery test environment. "install" needs sudo and the network
# and runs once per machine; everything else has to work offline, so the parts
# are separate subcommands - a test run must never call apt-get.
set -euo pipefail
cd "$(dirname "$0")/.."

install() {
	# xxd is its own package on Debian 13, and the test environment falls over
	# without it: every PCR value passes through it.
	sudo apt-get update
	# procps brings pgrep, which the simulator control uses. No Go here: the
	# repository and tools/check.sh stay on the machine that commits.
	sudo apt-get install -y \
		qemu-system-x86 ovmf swtpm swtpm-tools softhsm2 opensc tpm2-tools \
		openssl xxd socat procps
	printf 'Pakete installiert. Weiter mit: tools/recovery-tools.sh\n' >&2
}

case "${1:-}" in
install) install ;;
*) printf 'Aufruf: tools/recovery-setup.sh install\n' >&2; exit 1 ;;
esac
```

- [ ] **Schritt 2: Pakete installieren**

Ausführen: `tools/recovery-setup.sh install`
Erwartet: läuft durch und endet mit `Pakete installiert.` Danach beantwortet
`command -v tpm2_policysigned swtpm qemu-system-x86_64 socat xxd` alle fünf Namen mit einem Pfad.

- [ ] **Schritt 3: Prüfskript schreiben**

Datei `tools/recovery-tools.sh`, ausführbar machen:

```bash
#!/usr/bin/env bash
# Proves that the installed tools are the ones this package was written
# against. Every command sequence here comes from a run on this machine
# (~/smoke/run.sh and ~/smoke/stick.sh); a different release may spell an
# option differently, and a wrong guess would weaken the policy without anyone
# noticing. So the assumptions get proven, not trusted.
set -euo pipefail
cd "$(dirname "$0")/.."
# Cleanup only, never a check: this is the one line in the package allowed to
# swallow an error. -t alone would leave sessions behind, so -l and -s go with
# it - a probe that died mid-way must not cost the next run its session slots.
trap 'for h in -t -l -s; do tpm2_flushcontext "$h" >/dev/null 2>&1 || :; done' EXIT

work="$(pwd)/dist/recovery"
probe="$work/work/probe"
state="$work/tpm/probe"
txt="$work/werkzeuge.txt"
handle=0x81000002        # the probe uses its own handle, not the policy's

say() { printf '%s\n' "$*" >&2; }
stop_and_ask() {
	say ""
	say "STOPP: $1"
	say "Ein Ersatzweg würde ein Sicherheitsmerkmal aus docs/recovery-modus.md schwächen."
	say "Bitte den Koordinator fragen und erst danach weitermachen."
	exit 1
}
# swtpm holds only two transient objects and every tpm2 call loads its context
# afresh without giving the slot back.
flush() { tpm2_flushcontext -t >/dev/null; }

rm -rf "$probe" "$state"
mkdir -p "$probe" "$state" "$work/softhsm/tokens" "$work/keys" "$work/pin"
chmod 700 "$work/pin"
cat > "$work/softhsm/softhsm2.conf" <<CONF
directories.tokendir = $work/softhsm/tokens
objectstore.backend = file
log.level = ERROR
CONF

# TEST PINs in files with mode 0600: a PIN on the command line is readable in
# /proc/<pid>/cmdline by every user on the machine. Stage "test" of the
# specification prescribes "PIN source: file" anyway.
for entry in stick:12345678 so:87654321 platform:1234; do
	f="$work/pin/${entry%%:*}.txt"
	if [ ! -f "$f" ]; then
		printf '%s' "${entry##*:}" > "$f"
		chmod 600 "$f"
	fi
done
pin_platform="$work/pin/platform.txt"

# --- 1. versions ----------------------------------------------------------
version="$(tpm2 --version | sed -n 's/.*version="\([^"]*\)".*/\1/p')"
if [ -z "$version" ]; then
	stop_and_ask "Die Fassung der tpm2-tools ist nicht feststellbar."
fi
major="${version%%.*}"
rest="${version#*.}"
minor="${rest%%.*}"
if [ "$major" -lt 5 ] || { [ "$major" -eq 5 ] && [ "$minor" -lt 7 ]; }; then
	stop_and_ask "tpm2-tools $version ist älter als 5.7, gegen die dieser Plan geschrieben ist."
fi

# --- 2. commands and options ----------------------------------------------

for cmd in tpm2_getpolicydigest tpm2_evictcontrol tpm2_policysigned tpm2_policynv \
	tpm2_policypcr tpm2_policyauthvalue tpm2_policyor tpm2_sessionconfig \
	tpm2_startauthsession tpm2_dictionarylockout tpm2_loadexternal tpm2_verifysignature; do
	if ! command -v "$cmd" >/dev/null; then
		stop_and_ask "Der Befehl $cmd fehlt. Ohne ihn ist die Policy nicht baubar."
	fi
done

# --help of some tools exits non-zero; the grep below is the check that counts.
help_of() {              # $1 = command
	if "$1" --help > "$probe/help.txt" 2>&1; then :; fi
	cat "$probe/help.txt"
}
need_opt() {             # $1 = command, rest = long options that must exist
	local cmd="$1" opt page
	shift
	page="$(help_of "$cmd")"
	for opt in "$@"; do
		if ! printf '%s\n' "$page" | grep -q -- "$opt"; then
			stop_and_ask "$cmd kennt $opt nicht."
		fi
	done
}
need_opt tpm2_policysigned --nonce-tpm --expiration --raw-data
need_opt tpm2_startauthsession --policy-session --tpmkey-context
need_opt tpm2_sessionconfig --enable-encrypt --enable-decrypt
need_opt tpm2_policypcr --pcr-list --pcr
need_opt tpm2_policynv --input
need_opt tpm2_create --sealing-input --policy
need_opt tpm2_dictionarylockout --max-tries --recovery-time --lockout-recovery-time

# --- 3. simulator with a SHA-384 PCR bank ---------------------------------
# The probe brings its own instance: tools/recovery-swtpm.sh only exists from
# task 4 on, and the probe must not depend on what it measures.
swtpm_setup --tpm2 --tpm-state "$state" --createek --pcr-banks sha256,sha384 --overwrite
swtpm socket --tpm2 --tpmstate "dir=$state" --server "type=unixio,path=$state/sock" \
	--ctrl "type=unixio,path=$state/sock.ctrl" --flags not-need-init,startup-clear \
	--daemon --pid "file=$state/swtpm.pid"
for _ in $(seq 50); do
	if [ -S "$state/sock" ]; then break; fi
	sleep 0.1
done
if [ ! -S "$state/sock" ]; then
	say "swtpm ist nicht gestartet. Prüfen: swtpm --version und die Rechte auf $state"
	exit 1
fi
export TPM2TOOLS_TCTI="swtpm:path=$state/sock"
if ! tpm2_getcap pcrs | grep -q sha384; then
	stop_and_ask "Der Simulator hat keine SHA-384-PCR-Bank trotz --pcr-banks sha256,sha384."
fi

# --- 4. the one unmeasured point: does the PIN come out of a file? --------
flush
tpm2_createprimary -C o -g sha384 -G ecc384 -c "$probe/primary.ctx" >/dev/null
tpm2_evictcontrol -C o -c "$probe/primary.ctx" "$handle" >/dev/null
flush
tpm2_startauthsession -S "$probe/trial.ctx" -g sha384 --tpmkey-context "$handle" >/dev/null
tpm2_sessionconfig --enable-encrypt --enable-decrypt "$probe/trial.ctx" >/dev/null
tpm2_policyauthvalue -S "$probe/trial.ctx" >/dev/null
tpm2_getpolicydigest -S "$probe/trial.ctx" -o "$probe/authvalue.digest" >/dev/null
tpm2_flushcontext "$probe/trial.ctx" >/dev/null
printf 'TEST-PROBE-SECRET' > "$probe/secret.bin"
flush
# sensitiveDataOrigin stays CLEAR because the data comes from outside (-i).
tpm2_create -C "$handle" -g sha384 -u "$probe/p.pub" -r "$probe/p.priv" \
	-i "$probe/secret.bin" -L "$probe/authvalue.digest" -p "file:$pin_platform" \
	-a "fixedtpm|fixedparent|adminwithpolicy" >/dev/null
shred -u "$probe/secret.bin"
flush
tpm2_load -C "$handle" -u "$probe/p.pub" -r "$probe/p.priv" -c "$probe/p.ctx" >/dev/null
tpm2_startauthsession --policy-session -S "$probe/sess.ctx" -g sha384 \
	--tpmkey-context "$handle" >/dev/null
tpm2_sessionconfig --enable-encrypt --enable-decrypt "$probe/sess.ctx" >/dev/null
tpm2_policyauthvalue -S "$probe/sess.ctx" >/dev/null
flush
got="$(tpm2_unseal -c "$probe/p.ctx" -p "session:$probe/sess.ctx+file:$pin_platform")"
tpm2_flushcontext "$probe/sess.ctx" >/dev/null
if [ "$got" != TEST-PROBE-SECRET ]; then
	stop_and_ask "tpm2-tools nimmt die PIN nicht aus einer Datei (session:…+file:…). Die PIN kommt nicht auf die Kommandozeile."
fi
tpm2_evictcontrol -C o -c "$handle" >/dev/null

if [ -c /dev/kvm ]; then
	kvm=vorhanden
else
	kvm="nicht vorhanden, QEMU laeuft emuliert"
fi
{
	printf 'Werkzeuge der Recovery-Testumgebung\n'
	printf 'Geprüft am %s auf %s\n\n' "$(date -I)" "$(uname -srm)"
	printf 'tpm2-tools:  %s\n' "$version"
	printf 'swtpm:       %s\n' "$(swtpm --version | head -1)"
	printf 'SoftHSM2:    %s\n' "$(softhsm2-util --version)"
	printf 'QEMU:        %s\n' "$(qemu-system-x86_64 --version | head -1)"
	printf 'OpenSSL:     %s\n' "$(openssl version)"
	printf 'Go:          %s\n' "$go_version"
	printf '\nGeprüft und in Ordnung:\n'
	printf '  tpm2_policysigned kennt --nonce-tpm, --expiration, --raw-data\n'
	printf '  tpm2_startauthsession kennt --policy-session und --tpmkey-context\n'
	printf '  tpm2_sessionconfig kennt --enable-encrypt und --enable-decrypt\n'
	printf '  tpm2_policypcr kennt --pcr-list und --pcr\n'
	printf '  tpm2_policynv kennt --input\n'
	printf '  tpm2_dictionarylockout kennt --max-tries und die beiden Sperrzeiten\n'
	printf '  PCR-Bank sha384 vorhanden\n'
	printf '  Plattform-PIN aus Datei (session:…+file:…) funktioniert\n'
	printf '\nKVM: %s\n' "$kvm"
} > "$txt"

kill "$(cat "$state/swtpm.pid")"
say "Werkzeuge geprüft: $txt"
```

- [ ] **Schritt 4: Prüfskript laufen lassen**

Ausführen: `tools/recovery-tools.sh`
Erwartet: endet mit `Werkzeuge geprüft: …/werkzeuge.txt`. Bricht es mit `STOPP:` ab, ist das
**kein** Grund für eine Notlösung: die Meldung an den Koordinator weitergeben und warten.

- [ ] **Schritt 5: Ergebnis lesen**

Ausführen: `cat dist/recovery/werkzeuge.txt`
Erwartet: tpm2-tools 5.7 oder neuer, und unter „Geprüft und in Ordnung" alle acht Zeilen. Fehlt
eine, ist das Skript gar nicht so weit gekommen.

- [ ] **Schritt 6: Commit**

```bash
tools/check.sh
git add tools/recovery-setup.sh tools/recovery-tools.sh
git commit -m "Verify the installed TPM and PKCS#11 tools match the plan" \
	--trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Aufgabe 2: Umgebungsprüfung

**Dateien:**
- Anlegen: `tools/recovery-env.sh`

**Schnittstellen:**
- Benutzt: `dist/recovery/werkzeuge.txt` aus Aufgabe 1 als Nachweis, dass die Werkzeuge geprüft sind.
- Liefert: `tools/recovery-env.sh` — Rückgabe 0, wenn alle Werkzeuge da sind, sonst 1 mit einer
  deutschen Liste des Fehlenden. Jedes weitere Skript ruft es als Erstes auf.
- Liefert: `tools/recovery-env.sh pcr-bank` — prüft gegen ein **laufendes** TPM, dass eine
  SHA-384-PCR-Bank da ist. Braucht `TPM2TOOLS_TCTI` in der Umgebung.

- [ ] **Schritt 1: Skript anlegen, das noch nichts kann**

Datei `tools/recovery-env.sh`, ausführbar machen:

```bash
#!/usr/bin/env bash
# Checks that the recovery test environment has everything it needs.
set -euo pipefail
exit 1
```

- [ ] **Schritt 2: Laufen lassen, Fehlschlag sehen**

Ausführen: `tools/recovery-env.sh; echo "Rueckgabe: $?"`
Erwartet: `Rueckgabe: 1`

- [ ] **Schritt 3: Prüfung schreiben**

Inhalt von `tools/recovery-env.sh`:

```bash
#!/usr/bin/env bash
# Checks that the recovery test environment has everything it needs. Every
# other script starts with this, so a missing package fails early and says so.
set -euo pipefail
cd "$(dirname "$0")/.."
work="$(pwd)/dist/recovery"

# Every tool the package actually calls, including the ones that only appear
# deep inside recovery-policy.sh - a missing tpm2_policynv must not surface as
# a failing policy branch three tasks later.
need="qemu-system-x86_64 swtpm swtpm_setup softhsm2-util pkcs11-tool openssl xxd socat shred
	pgrep go tpm2 tpm2_startauthsession tpm2_sessionconfig tpm2_flushcontext tpm2_createprimary
	tpm2_evictcontrol tpm2_getpolicydigest tpm2_create tpm2_load tpm2_loadexternal
	tpm2_unseal tpm2_pcrread tpm2_pcrextend tpm2_policypcr tpm2_policysigned
	tpm2_policyauthvalue tpm2_policynv tpm2_policyor tpm2_nvdefine tpm2_nvincrement
	tpm2_nvread tpm2_nvreadpublic tpm2_getcap tpm2_getrandom tpm2_verifysignature
	tpm2_dictionarylockout"
missing=""
for tool in $need; do
	if ! command -v "$tool" >/dev/null; then missing="$missing $tool"; fi
done
# Firmware and the PKCS#11 module are data, not programs: check the files. The
# secure-boot build is the one work package 8 will sign against.
for file in /usr/share/OVMF/OVMF_CODE_4M.secboot.fd /usr/share/OVMF/OVMF_VARS_4M.ms.fd \
	/usr/lib/softhsm/libsofthsm2.so; do
	if [ ! -f "$file" ]; then missing="$missing $(basename "$file")"; fi
done

if [ -n "$missing" ]; then
	printf 'Es fehlt:%s\n' "$missing" >&2
	printf 'Einrichten mit: tools/recovery-setup.sh install\n' >&2
	exit 1
fi

# KVM is deliberately not required: this VM gets no virtualisation from its
# host, so QEMU runs emulated. Everything works, only slower.
if [ ! -f "$work/werkzeuge.txt" ]; then
	printf 'Die Werkzeuge sind noch nicht geprüft: %s fehlt.\n' "$work/werkzeuge.txt" >&2
	printf 'Prüfen mit: tools/recovery-tools.sh\n' >&2
	exit 1
fi

pcr_bank() {
	if [ -z "${TPM2TOOLS_TCTI:-}" ]; then
		printf 'TPM2TOOLS_TCTI ist nicht gesetzt.\n' >&2
		printf 'Setzen mit: export TPM2TOOLS_TCTI="$(tools/recovery-swtpm.sh tcti main)"\n' >&2
		exit 1
	fi
	# The whole policy is SHA-384. swtpm allocates no SHA-384 bank unless it is
	# asked for, and a missing bank would turn into a failing branch instead of
	# a clear message.
	if ! tpm2_getcap pcrs | grep -q sha384; then
		printf 'Dieses TPM hat keine SHA-384-PCR-Bank.\n' >&2
		printf 'Neu einrichten mit: tools/recovery-swtpm.sh reset main\n' >&2
		exit 1
	fi
	printf 'PCR-Bank sha384: vorhanden\n' >&2
}

case "${1:-}" in
"") printf 'Umgebung: vollständig\n' >&2 ;;
pcr-bank) pcr_bank ;;
*) printf 'Aufruf: tools/recovery-env.sh [pcr-bank]\n' >&2; exit 1 ;;
esac
```

- [ ] **Schritt 4: Laufen lassen**

Ausführen: `tools/recovery-env.sh; echo "Rueckgabe: $?"`
Erwartet: `Umgebung: vollständig` und `Rueckgabe: 0`.

- [ ] **Schritt 5: Gegenprobe, dass die Prüfung wirklich prüft**

Ausführen:
```bash
mv dist/recovery/werkzeuge.txt dist/recovery/werkzeuge.weg
tools/recovery-env.sh; echo "Rueckgabe: $?"
mv dist/recovery/werkzeuge.weg dist/recovery/werkzeuge.txt
tools/recovery-env.sh; echo "Rueckgabe: $?"
```
Erwartet: erst `Die Werkzeuge sind noch nicht geprüft` mit `Rueckgabe: 1`, dann wieder 0.

- [ ] **Schritt 6: Commit**

```bash
tools/check.sh
git add tools/recovery-env.sh
git commit -m "Check the recovery test environment" \
	--trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Aufgabe 3: Token und Schlüssel vorbereiten

**Dateien:**
- Ändern: `tools/recovery-setup.sh`

**Schnittstellen:**
- Benutzt: `tools/recovery-env.sh`, die PIN-Dateien und die SoftHSM2-Konfiguration aus Aufgabe 1.
- Liefert: `tools/recovery-setup.sh prepare` — ohne sudo, ohne Netz. Legt **fünf** Token
  `TEST-STICK-1` bis `TEST-STICK-5` an, jedes mit einem eigenen Schlüsselpaar `stickN`
  (EC secp384r1, ID `0N`), und schreibt die öffentlichen Schlüssel nach
  `dist/recovery/keys/stickN.pem`. Stick 1 bis 4 sind das Quorum, **Stick 5 ist der Reservestick**
  aus der Zeremonie (Vorgabe, Abschnitt 8) und dient in Aufgabe 10 als fremder Stick.

- [ ] **Schritt 1: Zeigen, dass es den Unterbefehl noch nicht gibt**

Ausführen: `tools/recovery-setup.sh prepare; echo "Rueckgabe: $?"`
Erwartet: `Aufruf: tools/recovery-setup.sh install` und `Rueckgabe: 1`.

- [ ] **Schritt 2: `prepare` schreiben**

In `tools/recovery-setup.sh` vor dem `case` einfügen:

```bash
prepare() {
	tools/recovery-env.sh
	work="$(pwd)/dist/recovery"
	export SOFTHSM2_CONF="$work/softhsm/softhsm2.conf"
	module=/usr/lib/softhsm/libsofthsm2.so
	pin="$(cat "$work/pin/stick.txt")"
	so_pin="$(cat "$work/pin/so.txt")"

	# Five tokens: four sticks for the quorum plus the spare stick the ceremony
	# in the specification demands. Each stick is its own token with its own
	# key - no clones, exactly like the real sticks.
	for n in 1 2 3 4 5; do
		label="TEST-STICK-$n"
		if ! softhsm2-util --show-slots | grep -q "$label"; then
			softhsm2-util --init-token --free --label "$label" --so-pin "$so_pin" --pin "$pin"
			pkcs11-tool --module "$module" --token-label "$label" --login --pin "$pin" \
				--keypairgen --key-type EC:secp384r1 --label "stick$n" --id "0$n"
		fi
		if [ ! -f "$work/keys/stick$n.pem" ]; then
			pkcs11-tool --module "$module" --token-label "$label" --read-object \
				--type pubkey --label "stick$n" --output-file "$work/keys/stick$n.der"
			openssl ec -pubin -inform DER -in "$work/keys/stick$n.der" -pubout \
				-out "$work/keys/stick$n.pem"
		fi
	done

	# Five sticks, five different keys: if two matched, every "any pair opens"
	# test would pass for the wrong reason.
	distinct=$(for n in 1 2 3 4 5; do
		openssl ec -pubin -in "$work/keys/stick$n.pem" -noout -text | tail -n +2
	done | sort -u | wc -l)
	if [ "$distinct" -lt 5 ]; then
		printf 'Zwei Test-Sticks haben denselben Schlüssel. Das darf nicht sein.\n' >&2
		printf 'Token wegwerfen und neu anlegen: rm -rf %s/softhsm/tokens/* %s/keys/stick*\n' \
			"$work" "$work" >&2
		exit 1
	fi
	printf 'Fünf Test-Sticks bereit (1-4 Quorum, 5 Reserve): %s/keys\n' "$work" >&2
}
```

Und den `case` ergänzen:

```bash
case "${1:-}" in
install) install ;;
prepare) prepare ;;
*) printf 'Aufruf: tools/recovery-setup.sh install|prepare\n' >&2; exit 1 ;;
esac
```

**Zur PIN:** `pkcs11-tool` hat keine Option, die PIN aus einer Datei zu lesen. Die PIN kommt
deshalb aus `dist/recovery/pin/stick.txt` (Rechte 0600) und steht für die Dauer des Aufrufs in der
Prozessliste. Das ist hier tragbar, weil es eine TEST-PIN eines SoftHSM2-Token ist (Vorgabe,
Abschnitt 13); das echte Stick-Modul in Paket 2 benutzt `cryptoki` und hat das Problem nicht. Der
Punkt steht am Ende dieses Plans unter „Offene Entscheidungen für den Koordinator".

- [ ] **Schritt 3: Laufen lassen**

Ausführen: `tools/recovery-setup.sh prepare`
Erwartet: `Fünf Test-Sticks bereit (1-4 Quorum, 5 Reserve)`.

- [ ] **Schritt 4: Nachprüfen, dass fünf verschiedene Schlüssel entstanden sind**

Ausführen:
```bash
for n in 1 2 3 4 5; do
	openssl ec -pubin -in dist/recovery/keys/stick$n.pem -noout -text | tail -n +2
done | sort -u | wc -l
```
Erwartet: `5`. Steht dort weniger, ist die Einrichtung fehlerhaft — nicht weitermachen.

- [ ] **Schritt 5: Zweimal laufen lassen, ohne dass etwas doppelt entsteht**

Ausführen: `tools/recovery-setup.sh prepare && softhsm2-util --show-slots | grep -c TEST-STICK-`
Erwartet: läuft ohne Fehler durch, und die Zahl ist `5` — nicht `10`.

- [ ] **Schritt 6: Prüfen, dass `prepare` ohne sudo auskommt**

Ausführen: `sudo -k; tools/recovery-setup.sh prepare`
Erwartet: läuft durch, ohne nach einem Passwort zu fragen. Fragt es doch, ist noch ein `sudo` oder
`apt-get` in `prepare` gerutscht — das gehört nach `install`.

- [ ] **Schritt 7: Commit**

```bash
tools/check.sh
git add tools/recovery-setup.sh
git commit -m "Prepare five SoftHSM2 tokens that stand in for the root sticks" \
	--trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Aufgabe 4: swtpm steuern

**Dateien:**
- Anlegen: `tools/recovery-swtpm.sh`
- Anlegen: `tools/recovery-test.sh`

**Schnittstellen:**
- Liefert: `tools/recovery-swtpm.sh start|stop|reset|tcti [instanz]`. Die Instanz ist `main`
  (Vorgabe, für die Policy) oder `qemu` (für Aufgabe 5); jede hat ihr eigenes Verzeichnis
  `dist/recovery/tpm/<instanz>/` mit Datensocket `sock` und Steuersocket `sock.ctrl`. `tcti` gibt
  die Zeichenkette für `TPM2TOOLS_TCTI` aus. `reset` wirft den Zustand weg und startet neu.
- Liefert: `tools/recovery-test.sh` — der Testlauf, der mit jeder weiteren Aufgabe wächst.

- [ ] **Schritt 1: Test schreiben, der noch scheitert**

Datei `tools/recovery-test.sh`, ausführbar machen:

```bash
#!/usr/bin/env bash
# Runs every case of work package 0, including the ones that must fail.
set -euo pipefail
cd "$(dirname "$0")/.."
# Cleanup only: a negative case that failed on purpose leaves its session and
# its objects in the TPM - the simulator holds only a handful of either.
trap 'for h in -t -l -s; do tpm2_flushcontext "$h" >/dev/null 2>&1 || :; done' EXIT

fail() { printf 'FEHLGESCHLAGEN: %s\n' "$1" >&2; exit 1; }
ok() { printf 'ok: %s\n' "$1" >&2; }
use_main() {
	TPM2TOOLS_TCTI="$(tools/recovery-swtpm.sh tcti main)"
	export TPM2TOOLS_TCTI
}

# A syntax error in any script must show up here, not three cases later.
for s in tools/recovery-*.sh; do bash -n "$s"; done

tools/recovery-env.sh
tools/recovery-setup.sh prepare
tools/recovery-swtpm.sh reset main
use_main
tools/recovery-env.sh pcr-bank
tpm2_getrandom 8 -o dist/recovery/work/rnd.bin || fail "TPM antwortet nicht"
ok "TPM antwortet, SHA-384-Bank vorhanden"
```

- [ ] **Schritt 2: Laufen lassen, Fehlschlag sehen**

Ausführen: `tools/recovery-test.sh`
Erwartet: bricht ab, weil `tools/recovery-swtpm.sh` fehlt.

- [ ] **Schritt 3: Skript schreiben**

Datei `tools/recovery-swtpm.sh`, ausführbar machen:

```bash
#!/usr/bin/env bash
# Starts, stops and resets the TPM simulator. There are two instances: "main"
# carries the policy tests, "qemu" belongs to the virtual machine - a guest
# moves the PCRs, and one shared simulator would change the very PCR state the
# policy is sealed against.
set -euo pipefail
cd "$(dirname "$0")/.."
work="$(pwd)/dist/recovery"

name="${2:-main}"
case "$name" in
main|qemu) ;;
*) printf 'Unbekannte Instanz: %s (erlaubt: main, qemu)\n' "$name" >&2; exit 1 ;;
esac
dir="$work/tpm/$name"
sock="$dir/sock"
pidfile="$dir/swtpm.pid"

start() {
	mkdir -p "$dir" "$work/work"
	if [ ! -f "$dir/tpm2-00.permall" ]; then
		# --createek is enough here. The EK certificate needs root and
		# /var/lib/swtpm-localca, and checking it against the manufacturer CA
		# belongs to work package 3. The SHA-384 bank has to be asked for.
		swtpm_setup --tpm2 --tpm-state "$dir" --createek --pcr-banks sha256,sha384 --overwrite
	fi
	if [ -f "$pidfile" ] && ps -p "$(cat "$pidfile")" >/dev/null; then return 0; fi
	if [ "$name" = qemu ]; then
		# QEMU initialises the TPM itself over the control channel, so this
		# instance must not be started up in advance.
		swtpm socket --tpm2 --tpmstate "dir=$dir" --server "type=unixio,path=$sock" \
			--ctrl "type=unixio,path=$sock.ctrl" --daemon --pid "file=$pidfile"
	else
		swtpm socket --tpm2 --tpmstate "dir=$dir" --server "type=unixio,path=$sock" \
			--ctrl "type=unixio,path=$sock.ctrl" --flags not-need-init,startup-clear \
			--daemon --pid "file=$pidfile"
	fi
	# The socket appears a moment after the daemon forks; without this wait the
	# first tpm2 command races it.
	for _ in $(seq 50); do
		if [ -S "$sock" ]; then break; fi
		sleep 0.1
	done
	if [ ! -S "$sock" ]; then
		printf 'swtpm %s ist nicht gestartet: %s\n' "$name" "$dir" >&2
		printf 'Prüfen: swtpm --version, Rechte auf dem Verzeichnis, alter Socket im Weg\n' >&2
		exit 1
	fi
	if [ "$name" = main ]; then
		TPM2TOOLS_TCTI="swtpm:path=$sock" tools/recovery-env.sh pcr-bank
	fi
}

stop() {
	pid=""
	if [ -f "$pidfile" ]; then
		pid="$(cat "$pidfile")"
	elif pids="$(pgrep -f "tpmstate dir=$dir")"; then
		# Without a pid file a daemon may still sit on this state directory -
		# so it gets looked up instead of being left running.
		pid="$(printf '%s\n' "$pids" | head -1)"
	fi
	if [ -z "$pid" ]; then
		rm -f "$pidfile"
		return 0
	fi
	if ps -p "$pid" >/dev/null; then
		kill "$pid"
		# Waiting matters: reset removes the state directory right afterwards,
		# and a simulator still running would write it back.
		for _ in $(seq 100); do
			if ! ps -p "$pid" >/dev/null; then break; fi
			sleep 0.1
		done
		if ps -p "$pid" >/dev/null; then
			printf 'swtpm %s endet nicht (PID %s). Von Hand beenden: kill -9 %s\n' \
				"$name" "$pid" "$pid" >&2
			exit 1
		fi
	fi
	rm -f "$pidfile"
}

reset() {
	stop
	rm -rf "$dir"
	if [ "$name" = main ]; then
		# A second run has to start from nothing: sessions, contexts, branch
		# digests and the sealed object all belong to the TPM just thrown away.
		rm -rf "$work/work"
		mkdir -p "$work/work"
	fi
	start
}

case "${1:-}" in
start) start ;;
stop) stop ;;
reset) reset ;;
tcti) printf 'swtpm:path=%s\n' "$sock" ;;
*) printf 'Aufruf: tools/recovery-swtpm.sh start|stop|reset|tcti [main|qemu]\n' >&2; exit 1 ;;
esac
```

- [ ] **Schritt 4: Test laufen lassen**

Ausführen: `tools/recovery-test.sh`
Erwartet: `PCR-Bank sha384: vorhanden` und `ok: TPM antwortet, SHA-384-Bank vorhanden`.

- [ ] **Schritt 5: Zurücksetzen als echten Testfall anhängen**

Ans Ende von `tools/recovery-test.sh`:

```bash
# reset has to really throw the state away, otherwise the second run of this
# test starts on the leftovers of the first. The NV counter is the proof.
tpm2_nvdefine -C o 0x1500016 -a "nt=counter|ownerwrite|ownerread|authread" >/dev/null
tpm2_nvincrement -C o 0x1500016 >/dev/null
tools/recovery-swtpm.sh reset main
use_main
if tpm2_nvreadpublic | grep -q 0x1500016; then fail "reset wirft den TPM-Zustand nicht weg"; fi
if [ -n "$(ls -A dist/recovery/work)" ]; then fail "reset räumt dist/recovery/work nicht"; fi
ok "reset stellt ein jungfräuliches TPM und ein leeres Arbeitsverzeichnis her"
```

Ausführen: `tools/recovery-test.sh`
Erwartet: beide `ok`-Zeilen.

- [ ] **Schritt 6: Commit**

```bash
tools/check.sh
git add tools/recovery-swtpm.sh tools/recovery-test.sh
git commit -m "Drive the TPM simulator in two separate instances" \
	--trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Aufgabe 5: QEMU mit Secure-Boot-OVMF und eigenem TPM

**Dateien:**
- Anlegen: `tools/recovery-qemu.sh`
- Ändern: `tools/recovery-test.sh`

**Schnittstellen:**
- Benutzt: `tools/recovery-swtpm.sh reset|tcti qemu`
- Liefert: `tools/recovery-qemu.sh check` startet QEMU mit der Secure-Boot-Firmware
  `OVMF_CODE_4M.secboot.fd` und `-machine q35,smm=on`, hängt die eigene swtpm-Instanz `qemu` an,
  fragt über QMP `query-tpm` ab, prüft, dass die Firmware wirklich ins TPM gemessen hat, und
  beendet die VM. `tools/recovery-qemu.sh stop` beendet eine hängengebliebene VM.

**Ohne KVM:** Diese VM bekommt keine Virtualisierung durchgereicht, QEMU emuliert also. Deshalb
steht kein `accel=kvm` in der Befehlszeile und die Wartezeiten sind großzügig.

**Was diese Aufgabe nicht beweist:** Secure Boot ist hier nur *startfähig* — eigene PK/KEK/db und
signierte UKI kommen mit Paket 8. Bewiesen wird: die Firmware-Variante mit SMM startet, sieht das
TPM und misst hinein.

- [ ] **Schritt 1: Test anhängen (scheitert)**

Ans Ende von `tools/recovery-test.sh`:

```bash
tools/recovery-qemu.sh check
ok "QEMU mit Secure-Boot-OVMF, eigener TPM-Instanz und Messung der Firmware"
use_main
```

- [ ] **Schritt 2: Laufen lassen, Fehlschlag sehen**

Ausführen: `tools/recovery-test.sh`
Erwartet: bricht ab, weil `tools/recovery-qemu.sh` fehlt.

- [ ] **Schritt 3: Skript schreiben**

Datei `tools/recovery-qemu.sh`, ausführbar machen:

```bash
#!/usr/bin/env bash
# Proves that QEMU boots the secure-boot OVMF with the simulator attached as a
# TPM and that the firmware really measures into it. The signed images come
# with work package 8; here only the plumbing is checked.
set -euo pipefail
cd "$(dirname "$0")/.."
tools/recovery-env.sh
work="$(pwd)/dist/recovery"
vars="$work/ovmf/OVMF_VARS_4M.fd"
qmp="$work/work/qmp.sock"
pidfile="$work/work/qemu.pid"
serial="$work/work/qemu-serial.log"

say() { printf '%s\n' "$*" >&2; }

stop_qemu() {
	if [ ! -f "$pidfile" ]; then return 0; fi
	pid="$(cat "$pidfile")"
	if ps -p "$pid" >/dev/null; then
		kill "$pid"
		for _ in $(seq 100); do
			if ! ps -p "$pid" >/dev/null; then break; fi
			sleep 0.1
		done
		if ps -p "$pid" >/dev/null; then
			say "QEMU endet nicht (PID $pid). Von Hand beenden: kill -9 $pid"
			exit 1
		fi
	fi
	rm -f "$pidfile"
}

check() {
	mkdir -p "$work/ovmf" "$work/work"
	stop_qemu
	# A guest moves the PCRs it is measured into. Sharing the simulator with the
	# policy tests would change exactly the PCR state they seal against.
	tools/recovery-swtpm.sh reset qemu
	sock="$work/tpm/qemu/sock"
	if [ ! -f "$vars" ]; then cp /usr/share/OVMF/OVMF_VARS_4M.ms.fd "$vars"; fi
	rm -f "$qmp" "$serial"

	# -tpmdev emulator speaks swtpm's control protocol, so the chardev points at
	# the control socket, not at the data socket. -nographic and -daemonize do
	# not work together; -display none with the serial line in a file does.
	# No accel=kvm: this host passes no virtualisation through, QEMU emulates.
	qemu-system-x86_64 -machine q35,smm=on -m 1024 -no-reboot -display none \
		-global driver=cfi.pflash01,property=secure,value=on \
		-drive "if=pflash,format=raw,unit=0,readonly=on,file=/usr/share/OVMF/OVMF_CODE_4M.secboot.fd" \
		-drive "if=pflash,format=raw,unit=1,file=$vars" \
		-chardev "socket,id=chrtpm,path=$sock.ctrl" \
		-tpmdev emulator,id=tpm0,chardev=chrtpm \
		-device tpm-tis,tpmdev=tpm0 \
		-serial "file:$serial" \
		-qmp "unix:$qmp,server,nowait" -daemonize -pidfile "$pidfile"

	for _ in $(seq 100); do
		if [ -S "$qmp" ]; then break; fi
		sleep 0.1
	done
	if [ ! -S "$qmp" ]; then
		say "QEMU hat keinen QMP-Socket angelegt: $qmp"
		stop_qemu
		exit 1
	fi

	# Emulated firmware needs its time to come up and measure; 30 seconds are
	# plenty on this machine and cost nothing when it is quicker.
	answer="$({ printf '%s\n' '{"execute":"qmp_capabilities"}' '{"execute":"query-tpm"}'; sleep 30; } \
		| socat - "UNIX-CONNECT:$qmp")"
	stop_qemu

	model="$(printf '%s' "$answer" | grep -o '"model": *"[^"]*"' | head -1 | cut -d'"' -f4)"
	if [ -z "$model" ]; then
		say "QEMU meldet kein TPM."
		say "Prüfen: läuft swtpm (tools/recovery-swtpm.sh start qemu)?"
		say "Zeigt der chardev auf $sock.ctrl und nicht auf $sock?"
		exit 1
	fi
	say "TPM-Gerät an QEMU: $model"

	# The stronger proof: the firmware has extended PCR 0. An untouched TPM has
	# it at all zero, so anything else can only come from the guest.
	TPM2TOOLS_TCTI="$(tools/recovery-swtpm.sh tcti qemu)"
	export TPM2TOOLS_TCTI
	tpm2_pcrread -o "$work/work/qemu-pcr0.bin" sha384:0 >/dev/null
	if [ -z "$(xxd -p -c 96 "$work/work/qemu-pcr0.bin" | tr -d '0\n')" ]; then
		say "Die Firmware hat nicht ins TPM gemessen: PCR 0 ist noch leer."
		say "Prüfen: zeigt der chardev auf das Steuerprotokoll ($sock.ctrl)?"
		say "Sagt $serial etwas? Hat die emulierte Firmware genug Zeit bekommen?"
		exit 1
	fi
	say "Firmware hat gemessen: PCR 0 ist nicht mehr leer"
}

case "${1:-}" in
check) check ;;
stop) stop_qemu ;;
*) printf 'Aufruf: tools/recovery-qemu.sh check|stop\n' >&2; exit 1 ;;
esac
```

- [ ] **Schritt 4: Test laufen lassen**

Ausführen: `tools/recovery-test.sh`
Erwartet: `TPM-Gerät an QEMU: tpm-tis`, `Firmware hat gemessen: PCR 0 ist nicht mehr leer` und
`ok: QEMU mit Secure-Boot-OVMF, eigener TPM-Instanz und Messung der Firmware`. Ohne KVM dauert
dieser eine Fall rund eine Minute.

- [ ] **Schritt 5: Nachprüfen, dass die beiden Instanzen sich nicht ins Gehege kommen**

Ausführen:
```bash
TPM2TOOLS_TCTI="$(tools/recovery-swtpm.sh tcti main)" tpm2_pcrread sha384:0 | tail -1
TPM2TOOLS_TCTI="$(tools/recovery-swtpm.sh tcti qemu)" tpm2_pcrread sha384:0 | tail -1
```
Erwartet: Die Instanz `main` zeigt lauter Nullen, `qemu` nicht. Zeigen beide dasselbe, hängt QEMU
an der falschen Instanz — dann öffnet später kein Zweig mehr reproduzierbar.

- [ ] **Schritt 6: Commit**

```bash
tools/check.sh
git add tools/recovery-qemu.sh tools/recovery-test.sh
git commit -m "Boot the secure-boot OVMF in QEMU with its own simulated TPM" \
	--trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Aufgabe 6: Der Normalstart-Zweig und die gemeinsame Zweig-Funktion

Diese Aufgabe legt die Form fest, an der alles Weitere hängt: **eine** Funktion `policy_branch`
erzeugt den Digest eines Zweigs und öffnet ihn später — in derselben Reihenfolge. Zwei getrennte
Befehlsfolgen würden auseinanderlaufen, und der beim Öffnen erzeugte Digest träfe den versiegelten
nie.

Dazu kommen die beiden PCR-Zustände aus der Vorgabe (Abschnitt 6): PCR 11 trägt im Normalzweig den
Produktiv-UKI, in den Stick-Zweigen den Recovery-UKI. Hier bootet nichts, deshalb werden beide
Zustände als Wertedateien gerechnet und mit `tpm2_policypcr -f` benutzt; das TPM vergleicht sie
gegen seine eigenen PCR und schlägt fehl, wenn sie nicht passen.

**Dateien:**
- Anlegen: `tools/recovery-policy.sh`
- Ändern: `tools/recovery-test.sh`

**Schnittstellen:**
- Liefert: `tools/recovery-policy.sh seal pcr` versiegelt den Text `TEST-DEK` (nie ein echter
  Schlüssel) gegen den Normalstart-Zweig und legt `dist/recovery/work/seal.pub` und `seal.priv` an.
- Liefert: `tools/recovery-policy.sh open pcr` öffnet über den Normalstart-Zweig und gibt das
  Geheimnis auf die Standardausgabe.
- Liefert: `tools/recovery-policy.sh state normal|recovery` bringt den Simulator in einen der beiden
  Boot-Zustände (nur nach `reset` sinnvoll, ein PCR lässt sich nur erweitern).
- Liefert: den festen Primärschlüssel unter `0x81000001`.

- [ ] **Schritt 1: Test anhängen (scheitert)**

Ans Ende von `tools/recovery-test.sh`:

```bash
tools/recovery-swtpm.sh reset main
use_main
tools/recovery-policy.sh state normal
tools/recovery-policy.sh seal pcr
[ "$(tools/recovery-policy.sh open pcr)" = "TEST-DEK" ] || fail "Normalstart-Zweig öffnet nicht"
ok "Zweig 1: Normalstart über PCR 11 = Produktiv-UKI"
```

- [ ] **Schritt 2: Laufen lassen, Fehlschlag sehen**

Ausführen: `tools/recovery-test.sh`
Erwartet: bricht ab, weil `tools/recovery-policy.sh` fehlt.

- [ ] **Schritt 3: Skript mit dem ersten Zweig schreiben**

Datei `tools/recovery-policy.sh`, ausführbar machen:

```bash
#!/usr/bin/env bash
# Builds the sealing policy and opens the sealed secret again. The policy gets
# the shape the specification demands: one branch for the normal boot, six for
# the pairs of four sticks.
#
# The rule this file lives by: the digest of a branch and the act of opening it
# come out of the same function, policy_branch, in the same order. Two separate
# command sequences drift apart, and the digest computed while opening could
# never meet the sealed one.
set -euo pipefail
cd "$(dirname "$0")/.."
tools/recovery-env.sh
# Cleanup only: a branch that failed on purpose leaves its session behind, and
# -t alone would not take that one with it.
trap 'for h in -t -l -s; do tpm2_flushcontext "$h" >/dev/null 2>&1 || :; done' EXIT

work="$(pwd)/dist/recovery"
w="$work/work"
mkdir -p "$w"
# PIN_FILE lets the test hand in a wrong PIN without ever putting one on a
# command line, where the whole machine could read it.
pin_platform="${PIN_FILE:-$work/pin/platform.txt}"
if [ -z "${TPM2TOOLS_TCTI:-}" ]; then
	TPM2TOOLS_TCTI="$(tools/recovery-swtpm.sh tcti main)"
fi
export TPM2TOOLS_TCTI

pcrs="sha384:0,1,2,3,4,5,6,7,11"
handle=0x81000001
say() { printf '%s\n' "$*" >&2; }
# The simulator holds only two transient objects, and every tpm2 call loads its
# context anew without giving the slot back. Without this before each loading
# command everything dies with "out of memory for object contexts".
flush() { tpm2_flushcontext -t >/dev/null; }

# Two boot states, as section 6 of the specification prescribes. Nothing boots
# on the simulator, so both are TEST digests whose names say so.
uki_prod="$(printf 'TEST-UKI-PRODUKTIV' | openssl dgst -sha384 -binary | xxd -p -c 96 | tr -d '\n')"
uki_recovery="$(printf 'TEST-UKI-RECOVERY' | openssl dgst -sha384 -binary | xxd -p -c 96 | tr -d '\n')"

# PCR 11 after a single extend: SHA-384 over the zero start value followed by
# the image digest - exactly what the TPM computes on TPM2_PCR_Extend.
pcr11_after() {          # $1 = image digest, hex
	{ head -c 48 /dev/zero | xxd -p -c 96; printf '%s' "$1"; } \
		| tr -d '\n' | xxd -r -p | openssl dgst -sha384 -binary | xxd -p -c 96 | tr -d '\n'
}

pcr_file() {             # $1 = normal|recovery, $2 = output file
	local digest
	case "$1" in
	normal) digest="$uki_prod" ;;
	recovery) digest="$uki_recovery" ;;
	*) say "Unbekannter PCR-Zustand: $1"; exit 1 ;;
	esac
	tpm2_pcrread -o "$w/pcr07.bin" "sha384:0,1,2,3,4,5,6,7" >/dev/null
	{ cat "$w/pcr07.bin"; pcr11_after "$digest" | xxd -r -p; } > "$2"
}

# Brings the simulator into one of the two states. A PCR can only be extended,
# never set, so the caller resets the simulator first.
set_pcr_state() {        # $1 = normal|recovery
	case "$1" in
	normal) tpm2_pcrextend "11:sha384=$uki_prod" ;;
	recovery) tpm2_pcrextend "11:sha384=$uki_recovery" ;;
	*) say "Unbekannter PCR-Zustand: $1 (erlaubt: normal, recovery)"; exit 1 ;;
	esac
	say "PCR 11 steht auf: $1"
}

# The primary key is made persistent once. Handing it around as a context file
# would occupy one of the two transient slots in every single call.
primary() {
	if tpm2_getcap handles-persistent | grep -q "$handle"; then return 0; fi
	flush
	tpm2_createprimary -C o -g sha384 -G ecc384 -c "$w/primary.ctx" >/dev/null
	tpm2_evictcontrol -C o -c "$w/primary.ctx" "$handle" >/dev/null
	rm -f "$w/primary.ctx"
	flush
}

# Every session is salted against the primary key and uses parameter
# encryption, as section 6 of the specification demands - trial sessions too,
# so the pattern moves to work package 3 unchanged.
new_session() {          # $1 = context file, $2 = trial|policy
	primary
	flush
	if [ "$2" = policy ]; then
		tpm2_startauthsession --policy-session -S "$1" -g sha384 --tpmkey-context "$handle" >/dev/null
	else
		tpm2_startauthsession -S "$1" -g sha384 --tpmkey-context "$handle" >/dev/null
	fi
	tpm2_sessionconfig --enable-encrypt --enable-decrypt "$1" >/dev/null
}

# One branch, one command sequence, for both jobs: mode "trial" builds the
# digest, mode "real" opens. The sticks, the PIN and the counter join this
# function in tasks 7 and 8; the closing PolicyOR stays outside it.
policy_branch() {        # $1 = trial|real, $2 = branch, $3 = session context
	local branch="$2" session="$3"
	if [ "$branch" = pcr ]; then
		pcr_file normal "$w/pcr-normal.bin"
		tpm2_policypcr -S "$session" -l "$pcrs" -f "$w/pcr-normal.bin" >/dev/null
		return 0
	fi
	say "Unbekannter Zweig: $branch"
	exit 1
}

branch_digest() {        # $1 = branch
	new_session "$w/trial.ctx" trial
	policy_branch trial "$1" "$w/trial.ctx"
	tpm2_getpolicydigest -S "$w/trial.ctx" -o "$w/branch-$1.digest" >/dev/null
	tpm2_flushcontext "$w/trial.ctx" >/dev/null
}

seal() {                 # $1 = branch
	primary
	branch_digest "$1"
	printf 'TEST-DEK' > "$w/secret.bin"
	flush
	# noda is deliberately absent: it would switch off the dictionary attack
	# protection and leave the platform PIN guessable without limit.
	# sensitiveDataOrigin stays CLEAR because the data comes from outside (-i).
	tpm2_create -C "$handle" -g sha384 -u "$w/seal.pub" -r "$w/seal.priv" \
		-i "$w/secret.bin" -L "$w/branch-$1.digest" \
		-a "fixedtpm|fixedparent|adminwithpolicy" >/dev/null
	shred -u "$w/secret.bin"
	say "Versiegelt gegen Zweig $1"
}

open_branch() {          # $1 = branch
	primary
	flush
	tpm2_load -C "$handle" -u "$w/seal.pub" -r "$w/seal.priv" -c "$w/seal.ctx" >/dev/null
	new_session "$w/sess.ctx" policy
	policy_branch real "$1" "$w/sess.ctx"
	flush
	# The secret goes to stdout because the test compares it. The real version
	# in work package 6 uses tpm2_unseal -o and never lets it near a terminal.
	tpm2_unseal -c "$w/seal.ctx" -p "session:$w/sess.ctx"
	tpm2_flushcontext "$w/sess.ctx" >/dev/null
}

case "${1:-}" in
state) set_pcr_state "${2:-}" ;;
seal) seal "${2:-}" ;;
open)
	case "${2:-}" in
	pcr) open_branch pcr ;;
	*) say "Aufruf: tools/recovery-policy.sh open pcr"; exit 1 ;;
	esac ;;
*) say "Aufruf: tools/recovery-policy.sh state|seal|open"; exit 1 ;;
esac
```

- [ ] **Schritt 4: Test laufen lassen**

Ausführen: `tools/recovery-test.sh`
Erwartet: `ok: Zweig 1: Normalstart über PCR 11 = Produktiv-UKI`. Bricht etwas mit
`out of memory for object contexts` ab, fehlt vor dem betroffenen Befehl ein `flush`.

- [ ] **Schritt 5: Negativfall — ein geänderter Produktiv-PCR sperrt den Normalzweig**

Ans Ende von `tools/recovery-test.sh`:

```bash
# Criterion 4 in miniature: one more extend stands for an image changed by a
# single byte. The branch must stay shut.
anders="$(printf 'TEST-EIN-BYTE-ANDERS' | openssl dgst -sha384 -binary | xxd -p -c 96 | tr -d '\n')"
tpm2_pcrextend "11:sha384=$anders" >/dev/null
if tools/recovery-policy.sh open pcr >/dev/null; then fail "Normalzweig öffnet trotz geänderter PCR"; fi
ok "geänderte PCR 11 sperrt den Normalzweig"

tools/recovery-swtpm.sh reset main
use_main
tools/recovery-policy.sh state recovery
tools/recovery-policy.sh seal pcr
if tools/recovery-policy.sh open pcr >/dev/null; then fail "Normalzweig öffnet im Recovery-Zustand"; fi
ok "im Recovery-Zustand bleibt der Normalzweig zu"
```

Ausführen: `tools/recovery-test.sh`
Erwartet: alle drei `ok`-Zeilen dieser Aufgabe.

- [ ] **Schritt 6: Commit**

```bash
tools/check.sh
git add tools/recovery-policy.sh tools/recovery-test.sh
git commit -m "Seal against the normal-boot PCR branch" \
	--trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Aufgabe 7: Zweig mit zwei Stick-Signaturen

**Dateien:**
- Ändern: `tools/recovery-policy.sh`, `tools/recovery-test.sh`

**Schnittstellen:**
- Liefert: `tools/recovery-policy.sh seal 12` und `tools/recovery-policy.sh open pair 1 2` —
  der Zweig eines Stick-Paars. Der Zweigname ist immer die kleinere gefolgt von der größeren
  Stick-Nummer, also `12`, `13`, `14`, `23`, `24`, `34`.
- Neue Funktionen im Skript: `load_stick <n>`, `sign_stick <n> <ein> <aus>`, `policysigned_raw`,
  `policysigned_real`, `policysigned_trial`, `dummy_sig`.

**Warum das gegen Replay hält:** `--raw-data` gibt bei SHA-384 genau 52 Byte heraus — 48 Byte
Nonce, die das TPM in **dieser** Sitzung erzeugt hat, und 4 Byte Ablaufzeit. Der Stick signiert
genau diese Bytes. Eine neue Sitzung hat eine neue Nonce, eine mitgeschnittene Signatur passt
nicht mehr. Der Koordinator hat das auf dieser VM belegt; Aufgabe 10 macht daraus einen Testfall.
`-t 0` heißt: keine Autorisierung überlebt ihre Sitzung.

**Warum der Trial-Digest trotzdem stimmt:** In einer Trial-Sitzung prüft das TPM die Signatur nicht
(TPM-2.0-Spec, TPM2_PolicySigned); in den Digest gehen nur `TPM_CC_PolicySigned`, der Name des
Schlüssels und `policyRef` ein. Deshalb wird der Digest **ohne** `-x` und mit einer beliebigen
Platzhalter-Signatur gebaut. Dass der so gebaute Digest der richtige ist, ist auf dieser VM belegt.

- [ ] **Schritt 1: Test anhängen (scheitert)**

Ans Ende von `tools/recovery-test.sh`:

```bash
tools/recovery-policy.sh seal 12
[ "$(tools/recovery-policy.sh open pair 1 2)" = "TEST-DEK" ] || fail "Stick-Paar 1+2 öffnet nicht"
ok "Zweig eines Stick-Paars (1+2) öffnet mit zwei echten Signaturen"
```

- [ ] **Schritt 2: Laufen lassen, Fehlschlag sehen**

Ausführen: `tools/recovery-test.sh`
Erwartet: Abbruch mit `Unbekannter Zweig: 12`.

- [ ] **Schritt 3: Stick-Zugriff einbauen**

In `tools/recovery-policy.sh` unter die Zeile mit `pin_platform=` einfügen:

```bash
keys="$work/keys"
pin_stick="$work/pin/stick.txt"
module=/usr/lib/softhsm/libsofthsm2.so
export SOFTHSM2_CONF="$work/softhsm/softhsm2.conf"
```

Und hinter `new_session` einfügen:

```bash
load_stick() {           # $1 = stick number
	if [ ! -f "$w/stick$1.ctx" ]; then
		flush
		# The name algorithm matters: the key's Name goes into the PolicySigned
		# digest, so a SHA-256 name would silently build a different branch.
		tpm2_loadexternal -C o -G ecc -g sha384 -u "$keys/stick$1.pem" \
			-c "$w/stick$1.ctx" >/dev/null
	fi
}

sign_stick() {           # $1 = stick number, $2 = data file, $3 = signature file
	# SoftHSM2 only offers CKM_ECDSA, the raw operation, so the digest is made
	# outside - exactly how a real HSM stick works as well. The PIN comes from
	# a file with mode 0600; pkcs11-tool has no way to read it itself.
	openssl dgst -sha384 -binary -out "$2.hash" "$2"
	pkcs11-tool --module "$module" --token-label "TEST-STICK-$1" --login \
		--pin "$(cat "$pin_stick")" --sign --mechanism ECDSA \
		--signature-format openssl --input-file "$2.hash" --output-file "$3" >/dev/null
}

# A trial session does not check the signature, the file only has to parse. One
# throwaway signature from a key nobody keeps serves every branch.
dummy_sig() {
	if [ ! -f "$w/dummy.sig" ]; then
		openssl ecparam -name secp384r1 -genkey -noout -out "$w/dummy.key"
		printf 'TEST-DUMMY' | openssl dgst -sha384 -sign "$w/dummy.key" -out "$w/dummy.sig"
		shred -u "$w/dummy.key"
	fi
}

# Hands out the bytes the stick has to sign: 48 byte nonce of this session plus
# 4 byte expiration. A recording of an earlier run does not fit a new session.
policysigned_raw() {     # $1 = session, $2 = key context, $3 = output file
	flush
	tpm2_policysigned -S "$1" -c "$2" -g sha384 -t 0 -x --raw-data="$3" >/dev/null
}

policysigned_real() {    # $1 = session, $2 = key context, $3 = signature file
	flush
	tpm2_policysigned -S "$1" -c "$2" -g sha384 -t 0 -x -s "$3" -f ecdsa >/dev/null
}

policysigned_trial() {   # $1 = session, $2 = key context
	dummy_sig
	flush
	# No -x here: the nonce belongs to the real session only, and the digest
	# does not depend on it.
	tpm2_policysigned -S "$1" -c "$2" -g sha384 -t 0 -s "$w/dummy.sig" -f ecdsa >/dev/null
}
```

- [ ] **Schritt 4: Den Paar-Zweig in die gemeinsame Funktion aufnehmen**

In `policy_branch` die beiden Zeilen `say "Unbekannter Zweig: $branch"` und `exit 1` ersetzen
durch:

```bash
	case "$branch" in
	[1-5][1-5]) ;;
	*) say "Unbekannter Zweig: $branch"; exit 1 ;;
	esac
	pcr_file recovery "$w/pcr-recovery.bin"
	tpm2_policypcr -S "$session" -l "$pcrs" -f "$w/pcr-recovery.bin" >/dev/null
	for n in "${branch%?}" "${branch#?}"; do
		load_stick "$n"
		if [ "$1" = trial ]; then
			policysigned_trial "$session" "$w/stick$n.ctx"
		else
			policysigned_raw "$session" "$w/stick$n.ctx" "$w/tosign-$n.bin"
			sign_stick "$n" "$w/tosign-$n.bin" "$w/stick$n.sig"
			policysigned_real "$session" "$w/stick$n.ctx" "$w/stick$n.sig"
		fi
	done
```

Die erste Zeile der Funktion um `n` ergänzen: `local branch="$2" session="$3" n`.

- [ ] **Schritt 5: Aufruf `open pair i j` ergänzen**

Vor dem `case` am Dateiende einfügen:

```bash
# Sets $branch. Deliberately not a command substitution: an exit inside $( )
# would only end the subshell and the script would carry on with nothing.
branch_of_pair() {       # $1, $2 = stick numbers
	local i="${1:-}" j="${2:-}"
	case "$i" in 1|2|3|4|5) ;; *) say "Stick-Nummer 1 bis 5 erwartet, nicht: $i"; exit 1 ;; esac
	case "$j" in 1|2|3|4|5) ;; *) say "Stick-Nummer 1 bis 5 erwartet, nicht: $j"; exit 1 ;; esac
	if [ "$i" -ge "$j" ]; then say "Erst der kleinere Stick, also: open pair $j $i"; exit 1; fi
	branch="$i$j"
}
```

Und den `open`-Zweig des `case` ersetzen durch:

```bash
open)
	case "${2:-}" in
	pcr) open_branch pcr ;;
	pair) branch_of_pair "${3:-}" "${4:-}"; open_branch "$branch" ;;
	*) say "Aufruf: tools/recovery-policy.sh open pcr|pair <i> <j>"; exit 1 ;;
	esac ;;
```

- [ ] **Schritt 6: Test laufen lassen**

Ausführen: `tools/recovery-test.sh`
Erwartet: `ok: Zweig eines Stick-Paars (1+2) öffnet mit zwei echten Signaturen`

- [ ] **Schritt 7: Zusichern, dass wirklich Sitzungsdaten signiert werden**

Das gehört in den Testlauf, nicht in einen Blick von Hand. Ans Ende von `tools/recovery-test.sh`:

```bash
# 52 bytes: 48 byte nonce of this session plus 4 byte expiration. Two sticks
# in one session get different bytes; if they did not, the signature would not
# hang on the session at all.
[ "$(wc -c < dist/recovery/work/tosign-1.bin)" -eq 52 ] \
	|| fail "die zu signierenden Daten sind nicht 52 Byte lang"
if cmp -s dist/recovery/work/tosign-1.bin dist/recovery/work/tosign-2.bin; then
	fail "beide Sticks bekommen dieselben Daten zum Signieren"
fi
ok "die Sticks signieren je 52 Byte aus ihrer eigenen Sitzung"
```

Ausführen: `tools/recovery-test.sh`
Erwartet: `ok: die Sticks signieren je 52 Byte aus ihrer eigenen Sitzung`. Schlägt einer der beiden
Fälle fehl, hängt die Signatur nicht an der Sitzung — dann **stoppen und den Koordinator fragen**.

- [ ] **Schritt 8: Commit**

```bash
tools/check.sh
git add tools/recovery-policy.sh tools/recovery-test.sh
git commit -m "Open a branch with two real stick signatures" \
	--trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Aufgabe 8: Plattform-PIN, Zähler und Wörterbuchschutz

**Dateien:**
- Ändern: `tools/recovery-policy.sh`, `tools/recovery-test.sh`

**Schnittstellen:**
- Liefert: Jeder Paar-Zweig enthält zusätzlich `tpm2_policyauthvalue` (Plattform-PIN aus
  `dist/recovery/pin/platform.txt`) und `tpm2_policynv` gegen den Zähler `0x1500016`.
- Liefert: `tools/recovery-policy.sh bump` erhöht den Zähler und versiegelt mit `c+1` neu.
- Liefert: `tools/recovery-policy.sh lockout-clear` hebt eine ausgelöste Sperre wieder auf, damit
  der Testlauf weiterlaufen kann.
- Liefert: Der Wörterbuchschutz steht auf der Vorgabe aus Abschnitt 6 — **3 Versuche, 24 h** —,
  gesetzt beim Versiegeln. Der Normalstart-Zweig (`open pcr`) bleibt ohne PIN, weil er beim Booten
  ohne Menschen ablaufen muss.
- Umgebungsvariablen für den Test: `PIN_FILE` (andere PIN-Datei) und `COUNTER_FILE` (Zählerstand von
  Hand vorgeben). Beide gibt es, damit weder PIN noch Zählerstand je auf einer Kommandozeile
  stehen.

- [ ] **Schritt 1: Test anhängen (scheitert)**

Ans Ende von `tools/recovery-test.sh`:

```bash
tools/recovery-policy.sh bump
[ "$(tools/recovery-policy.sh open pair 1 2)" = "TEST-DEK" ] || fail "nach Zähler +1 öffnet nichts"
ok "Zähler +1, neu versiegelt, öffnet wieder"
```

- [ ] **Schritt 2: Laufen lassen, Fehlschlag sehen**

Ausführen: `tools/recovery-test.sh`
Erwartet: Abbruch mit `Aufruf: tools/recovery-policy.sh state|seal|open` — `bump` kennt das Skript
noch nicht.

- [ ] **Schritt 3: Zähler und Sperre einbauen**

In `tools/recovery-policy.sh` unter `handle=0x81000001` einfügen:

```bash
nv=0x1500016
# COUNTER_FILE lets the test hand in an old counter state without putting it on
# a command line.
counter_file="${COUNTER_FILE:-$w/counter.bin}"
```

Und hinter `primary` einfügen:

```bash
# The counter lives in the TPM and disappears with a reset, so every entry
# point makes sure it is there before it uses it. Owner auth is empty here;
# that is a TEST simplification and noted as an open point in CHANGELOG.md.
counter() {
	if ! tpm2_nvreadpublic | grep -q "$nv"; then
		tpm2_nvdefine -C o "$nv" -a "nt=counter|ownerwrite|ownerread|authread" >/dev/null
		# A fresh counter has to be written once before it can be read.
		tpm2_nvincrement -C o "$nv" >/dev/null
	fi
	if [ -z "${COUNTER_FILE:-}" ]; then
		tpm2_nvread -C o "$nv" -s 8 -o "$counter_file" >/dev/null
	fi
}

policy_nv() {            # $1 = session
	tpm2_policynv -S "$1" -C o -i "$counter_file" "$nv" eq >/dev/null
}

# Section 6 of the specification: 3 attempts, then 24 hours locked. This
# replaces the noda attribute, which would have switched the protection off.
lockout_setup() {
	tpm2_dictionarylockout --setup-parameters --max-tries=3 \
		--recovery-time=86400 --lockout-recovery-time=86400 >/dev/null
}

lockout_clear() {
	tpm2_dictionarylockout --clear-lockout >/dev/null
	say "Sperre aufgehoben"
}
```

- [ ] **Schritt 4: PIN und Zähler in den Zweig aufnehmen**

In `policy_branch` hinter die `for`-Schleife über die beiden Sticks anhängen:

```bash
	tpm2_policyauthvalue -S "$session" >/dev/null
	policy_nv "$session"
```

In `seal` die erste Zeile `primary` ergänzen zu:

```bash
	primary
	counter
	lockout_setup
```

und den `tpm2_create`-Aufruf um die PIN erweitern (die Zeile mit `-i` ersetzen):

```bash
	tpm2_create -C "$handle" -g sha384 -u "$w/seal.pub" -r "$w/seal.priv" \
		-i "$w/secret.bin" -L "$w/branch-$1.digest" -p "file:$pin_platform" \
		-a "fixedtpm|fixedparent|adminwithpolicy" >/dev/null
```

In `open_branch` steht als erste Zeile `primary`; direkt darunter kommt `counter`, damit der
Zählerstand auch nach einem `reset` wieder da ist. Den `tpm2_unseal`-Aufruf ersetzen durch:

```bash
	# The normal boot branch carries no PIN: it has to run at boot without a
	# person. Every stick branch does, through the file - never on a command
	# line, where /proc/<pid>/cmdline would show it to the whole machine.
	if [ "$1" = pcr ]; then
		tpm2_unseal -c "$w/seal.ctx" -p "session:$w/sess.ctx"
	else
		tpm2_unseal -c "$w/seal.ctx" -p "session:$w/sess.ctx+file:$pin_platform"
	fi
```

- [ ] **Schritt 5: `bump` schreiben und den `case` ergänzen**

Hinter `open_branch` einfügen:

```bash
bump() {
	primary
	counter
	tpm2_nvincrement -C o "$nv" >/dev/null
	tpm2_nvread -C o "$nv" -s 8 -o "$w/counter.bin" >/dev/null
	rm -f "$w/seal.pub" "$w/seal.priv" "$w/seal.ctx" "$w"/branch-*.digest
	# Reseal against the same branch as before. Task 9 drops this note: from
	# then on there is only one policy, the OR over all seven branches.
	seal "$(cat "$w/sealed-branch")"
	say "Zähler erhöht, neu versiegelt"
}
```

Damit `bump` weiß, gegen welchen Zweig zuletzt versiegelt wurde, schreibt `seal` das mit: in `seal`
hinter `say "Versiegelt gegen Zweig $1"` die Zeile ergänzen

```bash
	printf '%s' "$1" > "$w/sealed-branch"
```

Den `case` ergänzen:

```bash
bump) bump ;;
lockout-clear) lockout_clear ;;
```

- [ ] **Schritt 6: Test laufen lassen**

Ausführen: `tools/recovery-test.sh`
Erwartet: `ok: Zähler +1, neu versiegelt, öffnet wieder`

- [ ] **Schritt 7: Negativfälle anhängen — alter Zählerstand und falsche PIN**

Ans Ende von `tools/recovery-test.sh`:

```bash
# An old counter state: the branch is built against a value the TPM has left
# behind, so PolicyNV refuses.
cp dist/recovery/work/counter.bin dist/recovery/work/counter.alt
tools/recovery-policy.sh bump
if COUNTER_FILE="$(pwd)/dist/recovery/work/counter.alt" tools/recovery-policy.sh open pair 1 2 >/dev/null; then
	fail "alter Zählerstand öffnet"
fi
ok "alter Zählerstand öffnet nicht"

# A wrong platform PIN. The file keeps the PIN off the command line here too.
printf '9999' > dist/recovery/pin/falsch.txt
chmod 600 dist/recovery/pin/falsch.txt
if PIN_FILE="$(pwd)/dist/recovery/pin/falsch.txt" tools/recovery-policy.sh open pair 1 2 >/dev/null; then
	fail "falsche Plattform-PIN öffnet"
fi
ok "falsche Plattform-PIN öffnet nicht"
# The failed attempt counted against the dictionary protection; clear it so the
# following cases start from zero.
tools/recovery-policy.sh lockout-clear
[ "$(tools/recovery-policy.sh open pair 1 2)" = "TEST-DEK" ] || fail "nach lockout-clear öffnet nichts"
ok "richtige PIN öffnet weiterhin"
```

Ausführen: `tools/recovery-test.sh`
Erwartet: alle vier `ok`-Zeilen dieser Aufgabe.

- [ ] **Schritt 8: Commit**

```bash
tools/check.sh
git add tools/recovery-policy.sh tools/recovery-test.sh
git commit -m "Bind the branch to the platform PIN and the NV counter" \
	--trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Aufgabe 9: PolicyOR aus sieben Zweigen — das Abnahmekriterium

**Dateien:**
- Ändern: `tools/recovery-policy.sh`, `tools/recovery-test.sh`

**Schnittstellen:**
- Liefert: `tools/recovery-policy.sh seal` — **ohne** Zweignamen. Versiegelt gegen `tpm2_policyor`
  über sieben Digests in fester Reihenfolge: `pcr`, `12`, `13`, `14`, `23`, `24`, `34`.
- Liefert: `open pcr` und `open pair i j` schließen ihren Ablauf mit demselben `tpm2_policyor` ab.
- Entfällt: die Form `seal <zweig>` und die Datei `sealed-branch`. Es gibt ab hier genau eine
  Policy — die aus der Vorgabe. Weniger Optionen, weniger Wege, etwas falsch zu machen.

**Die Reihenfolge ist Teil des Digests.** Sie steht genau einmal im Skript, in `branches`, und beide
Wege lesen sie von dort. Eine zweite, abweichende Liste im Öffnen-Pfad würde jeden Zweig
unbrauchbar machen.

- [ ] **Schritt 1: Test anhängen (scheitert)**

Ans Ende von `tools/recovery-test.sh`:

```bash
tools/recovery-swtpm.sh reset main
use_main
tools/recovery-policy.sh state recovery
tools/recovery-policy.sh seal
for paar in "1 2" "1 3" "1 4" "2 3" "2 4" "3 4"; do
	# shellcheck disable=SC2086  # the pair is two arguments on purpose
	[ "$(tools/recovery-policy.sh open pair $paar)" = "TEST-DEK" ] || fail "Paar $paar öffnet nicht"
done
ok "alle sechs Stick-Paare öffnen"
if tools/recovery-policy.sh open pcr >/dev/null; then fail "Normalzweig öffnet im Recovery-Zustand"; fi
ok "der Normalzweig bleibt im Recovery-Zustand zu"

tools/recovery-swtpm.sh reset main
use_main
tools/recovery-policy.sh state normal
tools/recovery-policy.sh seal
[ "$(tools/recovery-policy.sh open pcr)" = "TEST-DEK" ] || fail "Normalstart öffnet nicht"
ok "der Normalzweig öffnet im Produktiv-Zustand"
if tools/recovery-policy.sh open pair 1 2 >/dev/null; then fail "Stick-Zweig öffnet im Produktiv-Zustand"; fi
ok "die Stick-Zweige bleiben im Produktiv-Zustand zu"
ok "SIEBEN ZWEIGE IN EINER POLICY — Abnahmekriterium von Paket 0"
```

- [ ] **Schritt 2: Laufen lassen, Fehlschlag sehen**

Ausführen: `tools/recovery-test.sh`
Erwartet: scheitert beim ersten Paar, das nicht der eine versiegelte Zweig ist.

- [ ] **Schritt 3: Zweigliste und PolicyOR einbauen**

In `tools/recovery-policy.sh` unter `pcrs=` einfügen:

```bash
# The branch list, in fixed order, written down exactly once. The order goes
# into the OR digest, so the sealing path and the opening path both read it
# from here - a second, differing list would break every branch.
branches="pcr 12 13 14 23 24 34"
branch_list() {
	local list="" b
	for b in $branches; do list="$list,$w/branch-$b.digest"; done
	printf '%s' "${list#,}"
}
```

`branch_digest` bleibt, wie es ist. Darunter neu:

```bash
policy_digest() {
	local b
	for b in $branches; do branch_digest "$b"; done
	new_session "$w/trial.ctx" trial
	tpm2_policyor -S "$w/trial.ctx" -l "sha384:$(branch_list)" >/dev/null
	tpm2_getpolicydigest -S "$w/trial.ctx" -o "$w/policy.digest" >/dev/null
	tpm2_flushcontext "$w/trial.ctx" >/dev/null
}
```

- [ ] **Schritt 4: `seal`, `open_branch` und `bump` umstellen**

`seal` bekommt keinen Zweig mehr. Die Funktion lautet jetzt vollständig:

```bash
seal() {
	primary
	counter
	lockout_setup
	policy_digest
	printf 'TEST-DEK' > "$w/secret.bin"
	flush
	# noda is deliberately absent: it would switch off the dictionary attack
	# protection and leave the platform PIN guessable without limit.
	# sensitiveDataOrigin stays CLEAR because the data comes from outside (-i).
	tpm2_create -C "$handle" -g sha384 -u "$w/seal.pub" -r "$w/seal.priv" \
		-i "$w/secret.bin" -L "$w/policy.digest" -p "file:$pin_platform" \
		-a "fixedtpm|fixedparent|adminwithpolicy" >/dev/null
	shred -u "$w/secret.bin"
	say "Versiegelt gegen sieben Zweige"
}
```

In `open_branch` hinter `policy_branch real "$1" "$w/sess.ctx"` einfügen:

```bash
	# The same OR as at sealing time, from the same list.
	tpm2_policyor -S "$w/sess.ctx" -l "sha384:$(branch_list)" >/dev/null
```

In `bump` die Zeile `seal "$(cat "$w/sealed-branch")"` durch `seal` ersetzen und in `seal` die
Zeile mit `sealed-branch` streichen. Im `case` `seal) seal "${2:-}" ;;` durch `seal) seal ;;`
ersetzen.

- [ ] **Schritt 5: Ältere Testfälle auf die neue Aufrufform bringen**

In `tools/recovery-test.sh` die beiden älteren Aufrufe `tools/recovery-policy.sh seal pcr` und den
einen `tools/recovery-policy.sh seal 12` durch `tools/recovery-policy.sh seal` ersetzen. Sonst
ändert sich an den älteren Fällen nichts: Der PCR-Zustand steht dort schon richtig — der
Normalstart-Fall aus Aufgabe 6 läuft nach `state normal`, alles ab dem zweiten Block aus Aufgabe 6
nach `state recovery`.

Ausführen: `grep -n "recovery-policy.sh seal" tools/recovery-test.sh`
Erwartet: jede Zeile endet auf `seal`, keine trägt noch einen Zweignamen.

- [ ] **Schritt 6: Test laufen lassen**

Ausführen: `tools/recovery-test.sh`
Erwartet: unter anderem

```
ok: alle sechs Stick-Paare öffnen
ok: der Normalzweig bleibt im Recovery-Zustand zu
ok: der Normalzweig öffnet im Produktiv-Zustand
ok: die Stick-Zweige bleiben im Produktiv-Zustand zu
ok: SIEBEN ZWEIGE IN EINER POLICY — Abnahmekriterium von Paket 0
```

- [ ] **Schritt 7: Nachsehen, dass wirklich sieben Digests in der Policy stehen**

Ausführen: `ls dist/recovery/work/branch-*.digest | wc -l`
Erwartet: `7`. Steht dort weniger, hat `policy_digest` einen Zweig übersprungen und die Policy ist
kleiner als die Abnahme verlangt.

- [ ] **Schritt 8: Commit**

```bash
tools/check.sh
git add tools/recovery-policy.sh tools/recovery-test.sh
git commit -m "Seal against a PolicyOR of seven branches" \
	--trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Aufgabe 10: Die Negativfälle, die wirklich etwas beweisen

Ein Negativtest ist nur dann einer, wenn er aus dem richtigen Grund scheitert. Diese Aufgabe baut
die vier Fälle, bei denen ein echter Schlüssel eine echte Signatur leistet und das TPM sie trotzdem
zurückweist.

| Fall | Was wirklich passiert |
| --- | --- |
| Replay | Eine gültige Stick-Signatur wird mitgeschnitten und in eine **neue** Sitzung eingespielt. Die neue Sitzung hat eine neue Nonce, das TPM lehnt ab |
| Halber Zweig | Nur **ein** Stick des Paars signiert — gültig. Der Zweig ist nie vollständig, der abschließende PolicyOR passt nicht |
| Fremder Stick | Der **fünfte** Stick (der Reservestick) signiert gültig, steht aber in keinem Zweig. PolicySigned geht durch, der PolicyOR nicht |
| Sperre | Drei falsche Plattform-PINs, danach öffnet auch die richtige nicht mehr — bis `lockout-clear` |

**Dateien:**
- Ändern: `tools/recovery-policy.sh`, `tools/recovery-test.sh`

**Schnittstellen:**
- Liefert: `tools/recovery-policy.sh replay <i> <j>` — ein ehrlicher Durchlauf, dann dieselbe
  Signatur in einer frischen Sitzung.
- Liefert: `tools/recovery-policy.sh half <i> <j>` — nur Stick i signiert.
- Liefert: `tools/recovery-policy.sh checkstick <n>` — lässt Stick n eine Testnachricht signieren
  und prüft die Signatur gegen seinen öffentlichen Schlüssel.

**Beide Fälle sagen mit der Rückgabe, warum sie enden** — sonst wäre der Test auch dann grün, wenn
schon der ehrliche Vorlauf kaputt ist und der Fall gar nichts mehr beweist:

| Rückgabe | Bedeutung |
| --- | --- |
| 1 | Das TPM hat abgelehnt. **Nur das ist bestanden.** |
| 2 | Schon der ehrliche Vorlauf hat nicht geöffnet — der Fall beweist nichts, der Test ist rot |
| 0 | Das TPM hat angenommen. Rot, und zwar der schlimme Fall |

Der Testlauf verlangt deshalb ausdrücklich Rückgabe **1**, nicht „irgendetwas außer 0".

- [ ] **Schritt 1: Prüfhilfe und Tests anhängen (scheitern)**

Neben `fail` und `ok` am Kopf von `tools/recovery-test.sh` einfügen:

```bash
# A negative case has to end for the right reason, so the exit code is checked
# exactly, not just "anything but 0".
expect_rc() {            # $1 = expected code, rest = the command
	local want="$1" rc=0
	shift
	if "$@" >/dev/null; then rc=0; else rc=$?; fi
	if [ "$rc" -ne "$want" ]; then
		fail "$* : Rückgabe $rc, erwartet $want"
	fi
}
```

Ans Ende von `tools/recovery-test.sh`:

```bash
tools/recovery-swtpm.sh reset main
use_main
tools/recovery-policy.sh state recovery
tools/recovery-policy.sh seal

# The proof against a recorded run: same signature, new session. Exit code 1
# means the TPM refused; 2 would mean the honest run already failed and the
# case proves nothing.
expect_rc 1 tools/recovery-policy.sh replay 1 2
ok "Replay: eine mitgeschnittene Signatur wird in einer neuen Sitzung abgelehnt"

expect_rc 1 tools/recovery-policy.sh half 1 2
ok "ein Stick allein öffnet nicht, obwohl seine Signatur gültig ist"

# Stick 5 is a real token with a real key. First the proof that it can sign at
# all - without it the next case would pass for the wrong reason.
tools/recovery-policy.sh checkstick 5
ok "der fremde Stick signiert gültig"
if tools/recovery-policy.sh open pair 1 5 >/dev/null; then fail "fremder Stick öffnet"; fi
ok "ein fremder Stick öffnet trotzdem nicht: er steht in keinem der sieben Zweige"

[ "$(tools/recovery-policy.sh open pair 1 2)" = "TEST-DEK" ] || fail "nach den Negativfällen öffnet nichts mehr"
ok "nach allen Negativfällen öffnet das richtige Paar weiter"

# Three wrong PINs: the TPM locks, and the right PIN does not help any more.
printf '9999' > dist/recovery/pin/falsch.txt
chmod 600 dist/recovery/pin/falsch.txt
for versuch in 1 2 3; do
	if PIN_FILE="$(pwd)/dist/recovery/pin/falsch.txt" tools/recovery-policy.sh open pair 1 2 >/dev/null; then
		fail "falsche PIN öffnet (Versuch $versuch)"
	fi
done
if tools/recovery-policy.sh open pair 1 2 >/dev/null; then fail "TPM sperrt nach 3 Fehlversuchen nicht"; fi
ok "nach 3 falschen PINs sperrt das TPM auch die richtige PIN"
tools/recovery-policy.sh lockout-clear
[ "$(tools/recovery-policy.sh open pair 1 2)" = "TEST-DEK" ] || fail "nach lockout-clear öffnet nichts"
ok "nach Aufheben der Sperre öffnet es wieder"
```

- [ ] **Schritt 2: Laufen lassen, Fehlschlag sehen**

Ausführen: `tools/recovery-test.sh`
Erwartet: Abbruch, `replay` kennt das Skript noch nicht.

- [ ] **Schritt 3: `replay` und `half` schreiben**

In `tools/recovery-policy.sh` hinter `bump` einfügen:

```bash
# Proves a stick can sign at all and that its signature checks out against its
# own public key. Used before the foreign-stick case, which would otherwise
# pass for the wrong reason if the token were simply broken.
checkstick() {           # $1 = stick number
	printf 'TEST-STICK-PRUEFUNG' > "$w/probe.bin"
	sign_stick "$1" "$w/probe.bin" "$w/probe.sig"
	openssl dgst -sha384 -verify "$keys/stick$1.pem" -signature "$w/probe.sig" \
		"$w/probe.bin" >/dev/null
	say "Stick $1 signiert gültig"
}

# Records one honest run and plays the signature back into a fresh session. The
# new session hands out a new nonce, so the old signature signs the wrong bytes.
# Exit codes: 1 the TPM refused (the case passed), 2 the honest run already
# failed (the case proves nothing), 0 the TPM accepted the replay.
replay() {               # $1 = branch
	local branch="$1" i="${1%?}" got=""
	if ! got="$(open_branch "$branch")"; then
		say "Der ehrliche Vorlauf ist abgebrochen — der Replay-Fall beweist so nichts."
		return 2
	fi
	if [ "$got" != TEST-DEK ]; then
		say "Der ehrliche Vorlauf öffnet nicht — der Replay-Fall beweist so nichts."
		return 2
	fi
	cp "$w/stick$i.sig" "$w/replay.sig"
	primary
	counter
	new_session "$w/sess.ctx" policy
	pcr_file recovery "$w/pcr-recovery.bin"
	tpm2_policypcr -S "$w/sess.ctx" -l "$pcrs" -f "$w/pcr-recovery.bin" >/dev/null
	load_stick "$i"
	say "Spiele eine mitgeschnittene Signatur in eine neue Sitzung ein"
	if policysigned_real "$w/sess.ctx" "$w/stick$i.ctx" "$w/replay.sig"; then
		say "Das TPM hat die mitgeschnittene Signatur angenommen."
		tpm2_flushcontext "$w/sess.ctx" >/dev/null
		return 0
	fi
	tpm2_flushcontext "$w/sess.ctx" >/dev/null
	return 1
}

# Only the first stick of the pair signs - correctly. The branch stays half
# built, so the closing PolicyOR finds no match. Same exit codes as replay.
half() {                 # $1 = branch
	local branch="$1" i="${1%?}" got=""
	if ! got="$(open_branch "$branch")"; then
		say "Der ehrliche Vorlauf ist abgebrochen — der halbe Zweig beweist so nichts."
		return 2
	fi
	if [ "$got" != TEST-DEK ]; then
		say "Der ehrliche Vorlauf öffnet nicht — der halbe Zweig beweist so nichts."
		return 2
	fi
	primary
	counter
	flush
	tpm2_load -C "$handle" -u "$w/seal.pub" -r "$w/seal.priv" -c "$w/seal.ctx" >/dev/null
	new_session "$w/sess.ctx" policy
	pcr_file recovery "$w/pcr-recovery.bin"
	tpm2_policypcr -S "$w/sess.ctx" -l "$pcrs" -f "$w/pcr-recovery.bin" >/dev/null
	load_stick "$i"
	policysigned_raw "$w/sess.ctx" "$w/stick$i.ctx" "$w/tosign-$i.bin"
	sign_stick "$i" "$w/tosign-$i.bin" "$w/stick$i.sig"
	policysigned_real "$w/sess.ctx" "$w/stick$i.ctx" "$w/stick$i.sig"
	tpm2_policyauthvalue -S "$w/sess.ctx" >/dev/null
	policy_nv "$w/sess.ctx"
	tpm2_policyor -S "$w/sess.ctx" -l "sha384:$(branch_list)" >/dev/null
	flush
	if tpm2_unseal -c "$w/seal.ctx" -p "session:$w/sess.ctx+file:$pin_platform" >/dev/null; then
		say "Der halbe Zweig hat geöffnet."
		tpm2_flushcontext "$w/sess.ctx" >/dev/null
		return 0
	fi
	tpm2_flushcontext "$w/sess.ctx" >/dev/null
	return 1
}
```

Den `case` ergänzen:

```bash
replay) branch_of_pair "${2:-}" "${3:-}"; replay "$branch" ;;
half) branch_of_pair "${2:-}" "${3:-}"; half "$branch" ;;
checkstick)
	case "${2:-}" in
	1|2|3|4|5) checkstick "$2" ;;
	*) say "Stick-Nummer 1 bis 5 erwartet, nicht: ${2:-}"; exit 1 ;;
	esac ;;
```

und die Aufrufzeile am Ende auf
`Aufruf: tools/recovery-policy.sh state|seal|open|bump|replay|half|checkstick|lockout-clear`
setzen.

- [ ] **Schritt 4: Test laufen lassen**

Ausführen: `tools/recovery-test.sh`
Erwartet: die sieben `ok`-Zeilen dieser Aufgabe, darunter
`ok: Replay: eine mitgeschnittene Signatur wird in einer neuen Sitzung abgelehnt`.

- [ ] **Schritt 5: Zusichern, dass der Replay-Test kein Selbstläufer ist**

Auch das gehört in den Testlauf. Ans Ende von `tools/recovery-test.sh`:

```bash
# Two honest runs in a row have to produce two different signatures. If they
# did not, the replay case above would pass without proving anything.
[ "$(tools/recovery-policy.sh open pair 1 2)" = "TEST-DEK" ] || fail "erster Vergleichslauf öffnet nicht"
cp dist/recovery/work/stick1.sig dist/recovery/work/vergleich.sig
[ "$(tools/recovery-policy.sh open pair 1 2)" = "TEST-DEK" ] || fail "zweiter Vergleichslauf öffnet nicht"
if cmp -s dist/recovery/work/vergleich.sig dist/recovery/work/stick1.sig; then
	fail "zwei Sitzungen lassen dieselben Bytes signieren"
fi
ok "jede Sitzung lässt andere Bytes signieren"
```

Ausführen: `tools/recovery-test.sh`
Erwartet: `ok: jede Sitzung lässt andere Bytes signieren`. Schlägt der Fall fehl, hängt die
Signatur nicht an der Sitzung — dann **stoppen und den Koordinator fragen**.

- [ ] **Schritt 6: Commit**

```bash
tools/check.sh
git add tools/recovery-policy.sh tools/recovery-test.sh
git commit -m "Prove replay, half a branch, a foreign stick and the lockout all fail" \
	--trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Aufgabe 11: Der CI-Job

Die Vorgabe (Abschnitt 10) verlangt einen CI-Job, der bei jedem Commit läuft. Was dieses Paket
liefern kann, ist der Job selbst: **ein** Befehl, der die Werkzeuge prüft, die Testumgebung
herrichtet, alles durchfährt und ein Protokoll hinterlässt. Nur `tools/recovery-setup.sh install`
bleibt draußen — es braucht sudo und Netz und läuft einmal je Maschine. Wie er ausgelöst wird, entscheidet der Koordinator — das steht am Ende
dieses Plans und als offener Punkt in `CHANGELOG.md`.

**Dateien:**
- Anlegen: `tools/recovery-ci.sh`

**Schnittstellen:**
- Liefert: `tools/recovery-ci.sh` — Rückgabe 0 und `GRUEN`, wenn alle Fälle bestehen, sonst 1 und
  `ROT` mit den letzten 20 Zeilen. Protokoll unter `dist/recovery/ci/<Zeitstempel>.log`, zusätzlich
  als `dist/recovery/ci/last.log`.

- [ ] **Schritt 1: Skript schreiben**

Datei `tools/recovery-ci.sh`, ausführbar machen:

```bash
#!/usr/bin/env bash
# The CI job of work package 0: check the tools, set the test environment up,
# run every case, leave a log that says what happened. Red means no merge.
# "install" stays out: it needs sudo and the network, and a CI run must never
# reach for apt-get.
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p dist/recovery/ci
log="dist/recovery/ci/$(date +%Y%m%d-%H%M%S).log"

{
	printf 'Recovery-Testlauf %s\n' "$(date -Is)"
	printf 'Git: %s\n' "$(git rev-parse --short HEAD)"
} > "$log"

# Neither step may take the job down: the result has to reach the log and the
# summary either way. recovery-test.sh calls "prepare" itself, so the tokens
# and keys are made there.
status=0
if ! tools/recovery-tools.sh >> "$log" 2>&1; then status=1; fi
if [ "$status" -eq 0 ] && ! tools/recovery-test.sh >> "$log" 2>&1; then status=1; fi
# No symlink: the work directory has to survive being copied anywhere.
cp "$log" dist/recovery/ci/last.log

if [ "$status" -ne 0 ]; then
	printf 'ROT: der Testlauf ist fehlgeschlagen.\n' >&2
	printf 'Die letzten 20 Zeilen (vollständig in %s):\n' "$log" >&2
	tail -20 "$log" >&2
	exit 1
fi
printf 'GRUEN: alle Fälle von Paket 0 bestanden. Protokoll: %s\n' "$log" >&2
```

- [ ] **Schritt 2: Job laufen lassen**

Ausführen: `tools/recovery-ci.sh`
Erwartet: `GRUEN: alle Fälle von Paket 0 bestanden.` und ein Protokoll unter `dist/recovery/ci/`.

- [ ] **Schritt 3: Prüfen, dass Rot auch wirklich rot ist**

Ausführen:
```bash
mv dist/recovery/keys/stick2.pem dist/recovery/keys/stick2.weg
tools/recovery-ci.sh; echo "Rueckgabe: $?"
mv dist/recovery/keys/stick2.weg dist/recovery/keys/stick2.pem
tools/recovery-ci.sh; echo "Rueckgabe: $?"
```
Erwartet: erst `ROT`, die letzten 20 Zeilen und `Rueckgabe: 1`; danach wieder `GRUEN` und
`Rueckgabe: 0`. Ein CI-Job, der nie rot wird, prüft nichts.

- [ ] **Schritt 4: Commit**

```bash
tools/check.sh
git add tools/recovery-ci.sh
git commit -m "Add the CI job for work package 0" \
	--trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Aufgabe 12: Abnahmeprotokoll, Changelog und Abschluss

**Dateien:**
- Anlegen: `docs/recovery-abnahme.md`
- Ändern: `CHANGELOG.md`

**Schnittstellen:**
- Liefert: `docs/recovery-abnahme.md` — die Abnahme je Arbeitspaket und die 18 Kriterien aus
  Abschnitt 12 der Vorgabe.

**Wichtig:** Paket 0 hat **genau ein** Abnahmekriterium. Die Kriterien 1 bis 4 gehören laut
Abschnitt 11 der Vorgabe zu Paket 3 und bleiben hier **offen** — auch wenn dieser Testlauf schon
Fälle dazu fährt. Ergebnis, Datum und Prüfer werden **nach** dem echten Lauf von Hand eingetragen,
und Prüfer ist ein Mensch, nie eine Sitzung. Nichts davon wird vorausgefüllt.

- [ ] **Schritt 1: Abschlusszeile in den Test**

Ans Ende von `tools/recovery-test.sh`:

```bash
printf 'Paket 0: alle Fälle bestanden\n' >&2
```

- [ ] **Schritt 2: Ganzen Lauf von vorn**

Ausführen:
```bash
tools/recovery-swtpm.sh stop main
tools/recovery-swtpm.sh stop qemu
rm -rf dist/recovery/tpm dist/recovery/work
tools/recovery-ci.sh
```
Erwartet: `GRUEN` und im Protokoll alle `ok`-Zeilen sowie `Paket 0: alle Fälle bestanden`. Läuft
der Test nach vollständigem Wegwerfen nicht durch, ist er nicht fertig — dann nicht das Protokoll
anpassen, sondern den Fehler suchen.

- [ ] **Schritt 3: Abnahmeprotokoll anlegen**

Datei `docs/recovery-abnahme.md`:

```markdown
# Abnahme Recovery-Modus

Zwei Tabellen: oben die Abnahme je Arbeitspaket aus Abschnitt 11 von `docs/recovery-modus.md`,
unten die 18 Kriterien aus Abschnitt 12. Eine Zeile gilt erst als bestanden, wenn Ergebnis, Datum
und Prüfer eingetragen sind. **Prüfer ist immer ein Mensch.** Ein einziges offenes Kriterium
blockiert die Inbetriebnahme.

## Abnahme je Arbeitspaket

| Paket | Kriterium | Ergebnis | Datum | Prüfer | Beleg |
|---|---|---|---|---|---|
| 0 | Seal/Unseal mit PolicyOR aus 7 Zweigen auf swtpm | | | | `tools/recovery-ci.sh` |
| 1 | Testvektoren, 24 h Fuzzing ohne Absturz, Abdeckung > 90 % | offen | | | Paket 1 |
| 2 | alle Funktionen auf allen Backends, Sperre nach 3 Versuchen | offen | | | Paket 2 |
| 3 | Kriterien 1–4, 9, 10 auf swtpm und echtem TPM | offen | | | Paket 3 |
| 4 | Handbuch-Kapitel Erstellung und Ersatz laufen ohne Nachfrage durch | offen | | | Paket 4 |
| 5 | Kriterien 5, 6, 7 | offen | | | Paket 5 |
| 6 | Kriterien 1–8, 13–16 | offen | | | Paket 6 |
| 7 | Kriterien 8, 12 | offen | | | Paket 7 |
| 8 | Kriterien 4, 11, 17 | offen | | | Paket 8 |
| 9 | Kriterium 18 | offen | | | Paket 9 |
| 10 | Kriterien 14, 15 mit Zeitmessung | offen | | | Paket 10 |

So wird die Zeile für Paket 0 nach dem Lauf ausgefüllt: Ergebnis `bestanden` oder `nicht
bestanden`, Datum als `TT.MM.JJJJ`, Prüfer mit Namen, Beleg mit dem Dateinamen des Protokolls aus
`dist/recovery/ci/`.

## Die 18 Kriterien

| Nr. | Kriterium (Kurzform) | Ergebnis | Datum | Prüfer | Wird nachgewiesen in |
|---|---|---|---|---|---|
| 1 | Ein Stick allein oder ohne PIN gibt nicht frei | offen | | | Paket 3 |
| 2 | Jedes der 6 Paare gibt frei, ein gesperrter Stick in keinem | offen | | | Paket 3 |
| 3 | Mitschnitt mit altem Zählerstand gibt nicht frei (Replay) | offen | | | Paket 3 |
| 4 | Recovery-Image um 1 Byte geändert: keine Freigabe | offen | | | Paket 3 und 8 |
| 5 | Datenträger in anderer Maschine | offen | | | Paket 5 |
| 6 | Backup ohne Sticks und ohne Server-TPM | offen | | | Paket 5 |
| 7 | Fremdes OS auf dem Server | offen | | | Paket 5 |
| 8 | Admin-Datensatz direkt in der DB | offen | | | Paket 7 |
| 9 | 3 falsche PINs: Stick gesperrt, TPM 24 h gesperrt | offen | | | Paket 2 und 3 |
| 10 | TPM-Bus zeigt nur verschlüsselte Sessions | offen | | | Paket 3, nur echte Hardware |
| 11 | Kein Netz, keine Shell, kein Login | offen | | | Paket 8 |
| 12 | Audit-Eintrag beim Zeugen, Alarm in 60 s | offen | | | Paket 7 |
| 13 | Recovery-Konto verfällt nach 24 h | offen | | | Paket 6 |
| 14 | Recovery ohne Vorwissen in unter 20 min | offen | | | Paket 10 |
| 15 | Server neu aufsetzen in unter 60 min | offen | | | Paket 10 |
| 16 | Stick-Ersatz | offen | | | Paket 4 |
| 17 | Reproduzierbarer Build | offen | | | Paket 8 |
| 18 | Externer Penetrationstest | offen | | | Paket 9 |

Der Testlauf von Paket 0 fährt schon Fälle, die den Kriterien 1 bis 4 ähneln. Sie zählen hier
trotzdem **nicht** als bestanden: Sie laufen gegen den Simulator swtpm und SoftHSM2, nicht gegen
ein echtes TPM und echte Sticks, und die Vorgabe ordnet sie Paket 3 zu.
```

- [ ] **Schritt 4: Changelog-Eintrag mit den offenen Punkten**

In `CHANGELOG.md` unter `## Unveröffentlicht` einfügen:

```markdown
Testumgebung für den Recovery-Modus (Paket 0): Skripte in `tools/`, die auf der VM `ngc-recovery`
den TPM-Simulator swtpm, fünf SoftHSM2-Token als Ersatz für vier Root-Sticks und einen
Reservestick sowie QEMU mit Secure-Boot-OVMF einrichten und prüfen. Nachgewiesen ist das
Abnahmekriterium von Paket 0: Versiegeln und Öffnen eines Testgeheimnisses mit einer PolicyOR aus
sieben Zweigen (Normalstart über PCR, sechs Stick-Paare mit PolicySigned, Plattform-PIN und
NV-Zähler) gegen swtpm, dazu die Negativfälle Replay, halber Zweig, fremder Stick, alter
Zählerstand, falsche PIN mit Sperre und geänderte PCR. Ein Befehl fährt alles durch:
`tools/recovery-ci.sh`. Kein Rust in diesem Paket.

### Offen

- **Kein KVM auf `ngc-recovery`:** Der Proxmox-Host reicht keine Virtualisierung durch, QEMU läuft
  emuliert. Für Paket 0 reicht das; für die Zeitmessungen in Paket 10 nicht.
- **Auslöser des CI-Jobs:** `tools/recovery-ci.sh` läuft von Hand. Bei jedem Commit auszulösen,
  braucht einen eigenen Runner auf der VM; das lädt fremden Code und muss Thomas freigeben.
- **Prüf-Notebook ohne Netz** aus Abschnitt 11 der Vorgabe fehlt — Gerätefrage, nicht im Repo lösbar.
- **Echtes Secure Boot** mit eigener PK/KEK/db und signierten UKI kommt mit Paket 8; hier startet
  nur die Secure-Boot-Firmware.
- **EK-Zertifikat** wird nicht gegen die Hersteller-CA geprüft (`swtpm_setup --create-ek-cert`
  braucht root); gehört zu Paket 3.
- **NV-Zähler am leeren Owner-Auth:** In der Testumgebung hat die Owner-Hierarchie kein Passwort,
  der Zähler ließe sich also von jedem Prozess auf der VM erhöhen. Für den Nachweis der Policy ist
  das ohne Belang, für Paket 3 nicht.
- **Stick-PIN in der Prozessliste:** `pkcs11-tool` kann keine PIN aus einer Datei lesen. Die
  TEST-PIN steht deshalb für die Dauer des Aufrufs in `/proc/<pid>/cmdline`. Das echte Stick-Modul
  in Paket 2 benutzt `cryptoki` und hat das Problem nicht.
- **`sensitiveDataOrigin` bleibt CLEAR.** Abschnitt 5 der Vorgabe führt das Attribut für
  TPM-Objekte auf; ein von außen zugeführtes Geheimnis (`tpm2_create -i`) kann es aber nicht
  tragen, weil das Attribut gerade besagt, dass das TPM die Daten selbst erzeugt hat. Für Paket 3
  ist zu entscheiden, ob der DEK im TPM entsteht (dann gilt das Attribut) oder weiter zugeführt
  wird (dann bleibt es CLEAR und die Vorgabe wird an dieser Stelle nachgezogen).
- **Echter HSM-Stick** (Nitrokey HSM 2 für `hsm`) wird vorerst nicht beschafft (Entscheidung
  Thomas, 23.09.2026); Paket 2 entsteht für `piv` (YubiKey 5, Nitrokey 3) und `softhsm`, die
  `hsm`-Anbindung bleibt offen.
```

- [ ] **Schritt 5: Commit, Tag und Pull Request**

```bash
tools/check.sh
git add docs/recovery-abnahme.md tools/recovery-test.sh CHANGELOG.md
git commit -m "Record the acceptance sheet of work package 0" \
	--trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
git tag recovery-paket-0
git push -u origin recovery
git push origin recovery-paket-0
gh pr create --base main --head recovery \
	--title "Recovery-Modus: Testumgebung (Paket 0)" \
	--body "Abnahmekriterium von Paket 0 nachgewiesen: Seal/Unseal mit PolicyOR aus sieben Zweigen auf swtpm. Ein Befehl fährt alles durch: tools/recovery-ci.sh. Offene Punkte stehen in CHANGELOG.md.

🤖 Generated with [Claude Code](https://claude.com/claude-code)"
```

- [ ] **Schritt 6: Dem Koordinator melden**

Kurz melden: das Ergebnis von `tools/recovery-ci.sh`, den Inhalt von
`dist/recovery/werkzeuge.txt` und die Liste der offenen Entscheidungen aus dem letzten Abschnitt
dieses Plans. Die Zeile für Paket 0 im Abnahmeprotokoll füllt **Thomas** aus, nicht die Sitzung.

---

## Was dieses Paket nicht macht

- **Keine Rust-Zeile.** Die Krypto-Bibliothek ist Paket 1, das TPM-Modul Paket 3, das Stick-Modul
  Paket 2. Die Shell-Skripte hier sind Prüfstand, kein Produktcode; nichts davon läuft je auf einem
  Server.
- **Kein echtes Secure Boot.** Die Secure-Boot-Firmware startet, mehr nicht. Eigene PK/KEK/db,
  signierte UKI und dm-verity kommen mit Paket 8.
- **Keine echte Hardware.** swtpm ist kein TPM, SoftHSM2 ist kein Stick. Alles, was hier grün ist,
  wird in Paket 3 auf echtem TPM und echten Sticks erneut geprüft.
- **Keine Zeremonie, kein Einlernen, kein Stick-Ersatz.** Das sind die Pakete 4 und 6;
  `tools/recovery-test.sh` wächst mit ihnen.
- **Kein Schlüssel und kein Geheimnis von Wert.** Versiegelt wird der Text `TEST-DEK`, die Sticks
  sind Dateien, die PINs stehen sichtbar in `dist/recovery/pin/`. Alles ausdrücklich TEST und
  außerhalb von Git.

## Offene Entscheidungen für den Koordinator

Nichts davon hält die Aufgaben dieses Plans auf; alle sieben Punkte gehören zu Thomas, nicht zur
Sitzung. Sie stehen zusätzlich in `CHANGELOG.md` unter „Offen".

1. **Auslöser des CI-Jobs.** Die Vorgabe verlangt einen Lauf bei jedem Commit. `tools/recovery-ci.sh`
   gibt es; es fehlt der Auslöser. Drei Wege: ein eigener GitHub-Actions-Runner auf der VM (lädt
   fremden Code, braucht Freigabe), ein `cron`-Eintrag auf der VM, der `main` holt und den Job
   fährt, oder ein Git-Hook auf der VM. Vorschlag: `cron`, weil dabei kein fremder Code
   dazukommt.
2. **KVM auf `ngc-recovery`.** Der Proxmox-Host reicht keine Virtualisierung durch. Entweder wird
   das am Host eingeschaltet, oder es bleibt emuliert. Für Paket 0 reicht emuliert; für die
   Zeitmessungen von Kriterium 14 und 15 (unter 20 bzw. 60 Minuten) nicht.
3. **Prüf-Notebook ohne Netz.** Abschnitt 11 der Vorgabe zählt es zu Paket 0. Es ist ein Gerät, kein
   Code. Wer stellt es, und reicht bis dahin die VM?
4. **Echte Sticks.** Paket 2 verlangt die Backends `hsm` (Nitrokey HSM 2 oder YubiHSM 2) und `piv`
   (YubiKey 5, jede 5.x; Nitrokey 3). Für `piv` ist **nichts zu beschaffen**: Thomas hat drei
   YubiKey 5, an zweien ist Firmware 5.4.3 gemessen. Die frühere Zeile „ab 5.7“ war zu streng —
   P-384 in PIV gibt es seit YubiKey 4, 5.7 bringt nur RSA-3072/4096 und 25519 dazu; Einzelheiten
   in `.superpowers/sdd/recovery/root-sticks.md`. Zu beschaffen ist nur `hsm`, und das braucht
   allein die Stufe „enterprise“.
5. **Owner-Auth für den NV-Zähler.** Hier ist die Owner-Hierarchie ohne Passwort, der Zähler ließe
   sich also von jedem Prozess auf der VM erhöhen. Für Paket 3 ist zu entscheiden, wie der Zähler
   geschützt wird: Owner-Auth mit Passwort oder eine eigene Schreib-Policy auf dem NV-Index.
6. **Stick-PIN in der Prozessliste.** `pkcs11-tool` kann keine PIN aus einer Datei lesen; die
   TEST-PIN steht für die Dauer des Aufrufs in `/proc/<pid>/cmdline`. Vorschlag: für die
   Testumgebung hinnehmen (Abschnitt 13 der Vorgabe erlaubt TEST-PINs im Klartext) und in Paket 2
   über `cryptoki` richtig lösen. Wenn das nicht recht ist: bitte sagen, dann bekommt Paket 0 ein
   kleines eigenes Signierwerkzeug gegen die PKCS#11-Bibliothek.

## Reihenfolge und Abhängigkeiten

| Aufgabe | Ergebnis | Baut auf |
| --- | --- | --- |
| 1 | Pakete installiert, Werkzeuge geprüft, PIN-Dateien | — |
| 2 | `recovery-env.sh` | 1 |
| 3 | fünf Token mit fünf Schlüsseln | 1, 2 |
| 4 | swtpm in zwei Instanzen, erster Testfall | 2 |
| 5 | QEMU mit Secure-Boot-OVMF und eigener TPM-Instanz | 4 |
| 6 | Normalstart-Zweig, gemeinsame Zweig-Funktion, zwei PCR-Zustände | 4 |
| 7 | Stick-Signaturen im Zweig | 3, 6 |
| 8 | Plattform-PIN, NV-Zähler, Wörterbuchschutz | 7 |
| 9 | PolicyOR aus sieben Zweigen — **Abnahme** | 8 |
| 10 | Replay, halber Zweig, fremder Stick, Sperre | 9 |
| 11 | CI-Job | 10 |
| 12 | Abnahmeprotokoll, Changelog, Tag, Pull Request | 11 |

Aufgabe 5 hängt an keiner der Policy-Aufgaben und darf auch später kommen; alle anderen sind in
dieser Reihenfolge zu machen.
