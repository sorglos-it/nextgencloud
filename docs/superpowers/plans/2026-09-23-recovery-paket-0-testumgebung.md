# Recovery-Modus, Arbeitspaket 0: Testumgebung

> **Für Agenten:** PFLICHT-SUBSKILL: `superpowers:subagent-driven-development` (empfohlen) oder
> `superpowers:executing-plans`, Aufgabe für Aufgabe. Die Schritte sind Kästchen (`- [ ]`).

**Ziel:** Auf der Debian-VM `ngc-recovery` steht eine Testumgebung, in der Versiegeln und Öffnen
eines Geheimnisses mit einer TPM-Policy aus sieben Zweigen (PolicyOR) gegen swtpm und vier
simulierte Root-Sticks nachweisbar funktioniert — das Abnahmekriterium von Paket 0.

**Aufbau:** Sechs Shell-Skripte in `tools/`, die aufeinander aufbauen: prüfen, einrichten, swtpm
steuern, QEMU mit OVMF und virtuellem TPM starten, die Policy bauen und öffnen, alles testen. Alle
Laufzeitdaten (swtpm-Zustand, SoftHSM2-Token, Schlüssel, Kontexte) liegen unter `dist/recovery/`
und damit nie in Git. Keine Rust-Zeile in diesem Paket; die kommt mit Paket 1.

**Technik:** Debian 13, `tpm2-tools` gegen `swtpm` im Socket-Modus, `SoftHSM2` über `pkcs11-tool`
als Ersatz für die vier Root-Sticks, `qemu-system-x86_64` mit `OVMF`, Bash.

## Verbindliche Vorgaben

Aus `docs/recovery-modus.md` und `CLAUDE.md`, gelten für jede Aufgabe:

- Vorgabe ist `docs/recovery-modus.md`. Abschnitt 11, Paket 0 und Abnahmekriterium: „Seal/Unseal mit
  PolicyOR aus 7 Zweigen auf swtpm“. Nichts daran abschwächen; geht etwas nicht: stoppen und fragen.
- **Fail-closed:** Jeder Fehler bricht ab. Jedes Skript beginnt mit `set -euo pipefail`.
- **Keine Geheimnisse ins Log:** keine PINs, Schlüssel oder Nonces ausgeben, auch nicht gekürzt.
- Test-Schlüssel und Test-PINs dürfen im Repo stehen, wenn sie klar als TEST gekennzeichnet sind.
  Hier liegen sie unter `dist/recovery/` und damit außerhalb von Git; die PINs stehen sichtbar im
  Einrichtungsskript und heißen dort ausdrücklich TEST.
- Die Policy hat **sieben** Zweige (ein Normalstart-Zweig plus sechs Stick-Paare aus vier Sticks),
  also die Form der Stufe `enterprise`. Entwickelt wird in Stufe `test`; das Abnahmekriterium prüft
  trotzdem die große Form, weil das TPM höchstens acht Zweige kann und das früh feststehen muss.
- Ordner- und Dateinamen englisch, klein, Trenner `-`. Neue Ordner nur mit Namen aus dem
  Projektstruktur-Standard. Alles Erzeugte nach `dist/`.
- Code, Bezeichner, Kommentare und Commit-Nachrichten **englisch**; Ausgaben für Thomas und jede
  Fehlermeldung **deutsch** und so, dass sie sagt, was als Nächstes zu tun ist.
- Branch `recovery`, kleine Commits, Trailer
  `Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>`, am Ende ein Pull Request nach `main`.
- Kommentare erklären, warum etwas so ist, nicht was die Zeile tut.

## Umgebung

Die VM `ngc-recovery` (Proxmox, Debian 13, 4 Kerne mit `host`-CPU und verschachtelter
Virtualisierung, 8 GB RAM, 60 GB Platte) hat Netz für `apt` und einen Benutzer mit `sudo`. Das Repo
liegt dort geklont unter `~/nextgencloud`. Alle Befehle in diesem Plan laufen auf dieser VM, nicht
auf Thomas' Windows-Rechner.

## Dateien

| Datei | Aufgabe |
| --- | --- |
| `tools/recovery-env.sh` | prüft, ob alle Werkzeuge da sind; nennt sonst genau, was fehlt |
| `tools/recovery-setup.sh` | installiert die Pakete, legt Arbeitsverzeichnis, SoftHSM2-Token und die vier Stick-Schlüssel an |
| `tools/recovery-swtpm.sh` | startet, stoppt und setzt den TPM-Simulator zurück |
| `tools/recovery-qemu.sh` | startet QEMU mit OVMF und virtuellem TPM und beweist, dass der Gast ein TPM sieht |
| `tools/recovery-policy.sh` | baut die Policy-Zweige, versiegelt und öffnet das Geheimnis |
| `tools/recovery-test.sh` | fährt alle Fälle durch, auch die, die scheitern müssen — das ist der CI-Job |
| `docs/recovery-abnahme.md` | Abnahmeprotokoll: die 18 Kriterien mit Datum, Prüfer, Ergebnis |

Arbeitsverzeichnis `dist/recovery/` (nicht in Git): `tpm/` swtpm-Zustand, `softhsm/` Token und
Konfiguration, `keys/` öffentliche Stick-Schlüssel und TPM-Kontexte, `ovmf/` beschreibbare Kopie der
Firmware-Variablen, `work/` Sitzungen, Policy-Digests, versiegeltes Objekt.

---

### Aufgabe 1: Umgebungsprüfung

**Dateien:**
- Anlegen: `tools/recovery-env.sh`

**Schnittstellen:**
- Liefert: `tools/recovery-env.sh` — Rückgabe 0, wenn alle Werkzeuge da sind, sonst 1 mit einer
  deutschen Liste des Fehlenden. Jede weitere Datei ruft es als Erstes auf.

- [ ] **Schritt 1: Skript anlegen, das noch nichts kann**

```bash
cat > tools/recovery-env.sh <<'SH'
#!/usr/bin/env bash
# Checks that the recovery test environment has everything it needs.
set -euo pipefail
exit 1
SH
chmod +x tools/recovery-env.sh
```

- [ ] **Schritt 2: Laufen lassen, Fehlschlag sehen**

Ausführen: `tools/recovery-env.sh; echo "Rueckgabe: $?"`
Erwartet: `Rueckgabe: 1`

- [ ] **Schritt 3: Prüfung schreiben**

```bash
#!/usr/bin/env bash
# Checks that the recovery test environment has everything it needs. Every
# other script starts with this, so a missing package fails early and says so.
set -euo pipefail

need="qemu-system-x86_64 swtpm swtpm_setup softhsm2-util pkcs11-tool tpm2_startauthsession tpm2_policyor openssl xxd"
missing=""
for tool in $need; do
	command -v "$tool" >/dev/null 2>&1 || missing="$missing $tool"
done
# OVMF is data, not a program: check the firmware file itself.
[ -f /usr/share/OVMF/OVMF_CODE_4M.fd ] || missing="$missing OVMF_CODE_4M.fd"
[ -f /usr/lib/softhsm/libsofthsm2.so ] || missing="$missing libsofthsm2.so"

if [ -n "$missing" ]; then
	echo "Es fehlt:$missing"
	echo "Einrichten mit: tools/recovery-setup.sh"
	exit 1
fi
echo "Umgebung: vollständig"
```

- [ ] **Schritt 4: Laufen lassen**

Ausführen: `tools/recovery-env.sh; echo "Rueckgabe: $?"`
Erwartet auf der frischen VM: Liste des Fehlenden und `Rueckgabe: 1`. Genau das ist der
fehlschlagende Test für Aufgabe 2.

- [ ] **Schritt 5: Commit**

```bash
git add tools/recovery-env.sh
git commit -m "Check the recovery test environment"
```

---

### Aufgabe 2: Einrichtung der VM

**Dateien:**
- Anlegen: `tools/recovery-setup.sh`

**Schnittstellen:**
- Benutzt: `tools/recovery-env.sh`
- Liefert: Arbeitsverzeichnis `dist/recovery/`, SoftHSM2-Konfiguration
  `dist/recovery/softhsm/softhsm2.conf`, vier Token `TEST-STICK-1` bis `TEST-STICK-4` mit je einem
  Schlüsselpaar `stickN` (EC secp384r1, ID `0N`), öffentliche Schlüssel als
  `dist/recovery/keys/stickN.pem`. Benutzer-PIN aller Token: `12345678` (TEST), SO-PIN `87654321`
  (TEST).

- [ ] **Schritt 1: Skript anlegen**

```bash
#!/usr/bin/env bash
# Sets up the recovery test environment on Debian 13: packages, work directory,
# SoftHSM2 tokens and four EC keys that stand in for the four root sticks.
# Everything here is TEST material and lives outside git, under dist/recovery/.
set -euo pipefail
cd "$(dirname "$0")/.."
work="dist/recovery"

sudo apt-get update
sudo apt-get install -y qemu-system-x86 ovmf swtpm swtpm-tools softhsm2 opensc tpm2-tools openssl xxd

mkdir -p "$work"/{tpm,softhsm/tokens,keys,ovmf,work}
cat > "$work/softhsm/softhsm2.conf" <<CONF
directories.tokendir = $(pwd)/$work/softhsm/tokens
objectstore.backend = file
log.level = ERROR
CONF
export SOFTHSM2_CONF="$(pwd)/$work/softhsm/softhsm2.conf"
module=/usr/lib/softhsm/libsofthsm2.so

for n in 1 2 3 4; do
	label="TEST-STICK-$n"
	# Each stick is its own token: four different keys, no clones, as the
	# specification demands of the real sticks.
	softhsm2-util --show-slots | grep -q "$label" && continue
	softhsm2-util --init-token --free --label "$label" --so-pin 87654321 --pin 12345678
	pkcs11-tool --module "$module" --token-label "$label" --login --pin 12345678 \
		--keypairgen --key-type EC:secp384r1 --label "stick$n" --id "0$n"
	pkcs11-tool --module "$module" --token-label "$label" --read-object --type pubkey \
		--label "stick$n" --output-file "$work/keys/stick$n.der"
	openssl ec -pubin -inform DER -in "$work/keys/stick$n.der" -outform PEM \
		-out "$work/keys/stick$n.pem"
done

tools/recovery-env.sh
echo "Testumgebung eingerichtet: $work"
```

- [ ] **Schritt 2: Laufen lassen**

Ausführen: `tools/recovery-setup.sh`
Erwartet: läuft durch und endet mit `Umgebung: vollständig` und `Testumgebung eingerichtet`.

- [ ] **Schritt 3: Nachprüfen, dass vier verschiedene Schlüssel entstanden sind**

Ausführen:
```bash
for n in 1 2 3 4; do openssl ec -pubin -in dist/recovery/keys/stick$n.pem -noout -text | tail -3; done \
  | sort -u | wc -l
```
Erwartet: mehr als eine Zeile, also keine Klone. Sind zwei Schlüssel gleich, ist das ein Fehler in
der Einrichtung: Token löschen (`rm -rf dist/recovery/softhsm/tokens/*`) und neu anlegen.

- [ ] **Schritt 4: Erneut laufen lassen**

Ausführen: `tools/recovery-setup.sh`
Erwartet: läuft ohne Fehler durch und legt nichts doppelt an (das `grep -q` überspringt bestehende
Token).

- [ ] **Schritt 5: Commit**

```bash
git add tools/recovery-setup.sh
git commit -m "Set up the recovery test environment"
```

---

### Aufgabe 3: swtpm steuern

**Dateien:**
- Anlegen: `tools/recovery-swtpm.sh`

**Schnittstellen:**
- Liefert: `tools/recovery-swtpm.sh start|stop|reset|tcti`. `start` startet swtpm im Socket-Modus
  auf `dist/recovery/tpm/sock`, `tcti` gibt die Zeichenkette
  `swtpm:path=<absoluter Pfad>/dist/recovery/tpm/sock` aus, die `TPM2TOOLS_TCTI` braucht. `reset`
  wirft den Zustand weg und startet neu — danach ist das TPM jungfräulich.

- [ ] **Schritt 1: Test schreiben, der noch scheitert**

In `tools/recovery-test.sh` anlegen:

```bash
#!/usr/bin/env bash
# Runs every case of work package 0, including the ones that must fail.
set -euo pipefail
cd "$(dirname "$0")/.."
tools/recovery-env.sh >/dev/null

fail() { echo "FEHLGESCHLAGEN: $1"; exit 1; }

tools/recovery-swtpm.sh reset
export TPM2TOOLS_TCTI="$(tools/recovery-swtpm.sh tcti)"
tpm2_getrandom 8 >/dev/null || fail "TPM antwortet nicht"
echo "ok: TPM antwortet"
```

- [ ] **Schritt 2: Laufen lassen, Fehlschlag sehen**

Ausführen: `chmod +x tools/recovery-test.sh && tools/recovery-test.sh`
Erwartet: bricht ab, weil `tools/recovery-swtpm.sh` fehlt.

- [ ] **Schritt 3: Skript schreiben**

```bash
#!/usr/bin/env bash
# Starts, stops and resets the TPM simulator the whole package tests against.
set -euo pipefail
cd "$(dirname "$0")/.."
dir="$(pwd)/dist/recovery/tpm"
sock="$dir/sock"
pidfile="$dir/swtpm.pid"

start() {
	mkdir -p "$dir"
	[ -f "$dir/tpm2-00.permall" ] || swtpm_setup --tpm2 --tpm-state "$dir" --createek --create-ek-cert --create-platform-cert --overwrite
	if [ -f "$pidfile" ] && kill -0 "$(cat "$pidfile")" 2>/dev/null; then return 0; fi
	swtpm socket --tpm2 --tpmstate "dir=$dir" --server "type=unixio,path=$sock" \
		--ctrl "type=unixio,path=$sock.ctrl" --flags not-need-init,startup-clear \
		--daemon --pid "file=$pidfile"
	# The socket appears a moment after the daemon forks; without this wait the
	# first tpm2 command races it.
	for _ in $(seq 50); do [ -S "$sock" ] && return 0; sleep 0.1; done
	echo "swtpm ist nicht gestartet: $dir"; exit 1
}

stop() {
	[ -f "$pidfile" ] || return 0
	kill "$(cat "$pidfile")" 2>/dev/null || true
	rm -f "$pidfile"
}

case "${1:-}" in
start) start ;;
stop) stop ;;
reset) stop; rm -rf "$dir"; start ;;
tcti) echo "swtpm:path=$sock" ;;
*) echo "Aufruf: tools/recovery-swtpm.sh start|stop|reset|tcti"; exit 1 ;;
esac
```

- [ ] **Schritt 4: Test laufen lassen**

Ausführen: `tools/recovery-test.sh`
Erwartet: `ok: TPM antwortet`

- [ ] **Schritt 5: Zurücksetzen prüfen**

Ausführen:
```bash
export TPM2TOOLS_TCTI="$(tools/recovery-swtpm.sh tcti)"
tpm2_nvdefine -C o 0x1500016 -a "nt=counter|ownerwrite|ownerread|authread" && tpm2_nvincrement -C o 0x1500016
tools/recovery-swtpm.sh reset
export TPM2TOOLS_TCTI="$(tools/recovery-swtpm.sh tcti)"
tpm2_nvreadpublic | grep -c 0x1500016 || echo "Zähler ist weg, wie erwartet"
```
Erwartet: `Zähler ist weg, wie erwartet`

- [ ] **Schritt 6: Commit**

```bash
git add tools/recovery-swtpm.sh tools/recovery-test.sh
git commit -m "Drive the TPM simulator"
```

---

### Aufgabe 4: QEMU mit OVMF und virtuellem TPM

**Dateien:**
- Anlegen: `tools/recovery-qemu.sh`
- Ändern: `tools/recovery-test.sh`

**Schnittstellen:**
- Benutzt: `tools/recovery-swtpm.sh start|tcti`
- Liefert: `tools/recovery-qemu.sh check` startet QEMU mit OVMF und dem swtpm als TPM-Gerät, fragt
  über QMP `query-tpm` ab, gibt `TPM im Gast: <Modell>` aus und beendet die VM wieder. Rückgabe 0
  nur, wenn ein TPM gemeldet wird.

- [ ] **Schritt 1: Test anhängen (scheitert)**

In `tools/recovery-test.sh` vor dem Ende anhängen:

```bash
tools/recovery-qemu.sh check | grep -q "TPM im Gast" || fail "QEMU sieht kein TPM"
echo "ok: QEMU mit OVMF und TPM"
```

- [ ] **Schritt 2: Laufen lassen, Fehlschlag sehen**

Ausführen: `tools/recovery-test.sh`
Erwartet: bricht ab, weil `tools/recovery-qemu.sh` fehlt.

- [ ] **Schritt 3: Skript schreiben**

```bash
#!/usr/bin/env bash
# Proves that QEMU can boot OVMF with the simulator attached as a TPM. The
# real images come with work package 8; here only the plumbing is checked.
set -euo pipefail
cd "$(dirname "$0")/.."
work="$(pwd)/dist/recovery"
vars="$work/ovmf/OVMF_VARS_4M.fd"

[ -f "$vars" ] || cp /usr/share/OVMF/OVMF_VARS_4M.fd "$vars"
tools/recovery-swtpm.sh start
sock="$work/tpm/sock"
qmp="$work/work/qmp.sock"
rm -f "$qmp"

# No disk, no network: the firmware comes up, reports the TPM over QMP and the
# VM is shut down again. -nographic keeps it headless on the server.
qemu-system-x86_64 -machine q35,accel=kvm -m 1024 -nographic -no-reboot \
	-drive "if=pflash,format=raw,unit=0,readonly=on,file=/usr/share/OVMF/OVMF_CODE_4M.fd" \
	-drive "if=pflash,format=raw,unit=1,file=$vars" \
	-chardev "socket,id=chrtpm,path=$sock" -tpmdev emulator,id=tpm0,chardev=chrtpm \
	-device tpm-tis,tpmdev=tpm0 \
	-qmp "unix:$qmp,server,nowait" -daemonize -pidfile "$work/work/qemu.pid"

answer=$( { printf '%s\n' '{"execute":"qmp_capabilities"}' '{"execute":"query-tpm"}'; sleep 1; } \
	| socat - "UNIX-CONNECT:$qmp" || true)
kill "$(cat "$work/work/qemu.pid")" 2>/dev/null || true

model=$(printf '%s' "$answer" | grep -o '"model": *"[^"]*"' | head -1 | cut -d'"' -f4)
if [ -z "$model" ]; then
	echo "QEMU meldet kein TPM. Prüfen: läuft swtpm (tools/recovery-swtpm.sh start), ist KVM da (ls /dev/kvm)?"
	exit 1
fi
echo "TPM im Gast: $model"
```

`socat` gehört in die Paketliste von `tools/recovery-setup.sh` und in die Prüfliste von
`tools/recovery-env.sh`; beides in diesem Schritt ergänzen.

- [ ] **Schritt 4: Test laufen lassen**

Ausführen: `tools/recovery-setup.sh && tools/recovery-test.sh`
Erwartet: `TPM im Gast: tpm-tis` und `ok: QEMU mit OVMF und TPM`

- [ ] **Schritt 5: Commit**

```bash
git add tools/recovery-qemu.sh tools/recovery-setup.sh tools/recovery-env.sh tools/recovery-test.sh
git commit -m "Boot OVMF in QEMU with the simulated TPM"
```

---

### Aufgabe 5: Zweig 1 — Versiegeln und Öffnen nur über PCR

**Dateien:**
- Anlegen: `tools/recovery-policy.sh`
- Ändern: `tools/recovery-test.sh`

**Schnittstellen:**
- Benutzt: `TPM2TOOLS_TCTI` aus `tools/recovery-swtpm.sh tcti`
- Liefert: `tools/recovery-policy.sh seal` versiegelt das Testgeheimnis unter einer Policy und legt
  `dist/recovery/work/seal.pub` und `seal.priv` an; `tools/recovery-policy.sh open pcr` öffnet es
  über den Normalstart-Zweig und gibt das Geheimnis auf die Standardausgabe. Das Geheimnis ist der
  Text `TEST-DEK`, nie ein echter Schlüssel.

- [ ] **Schritt 1: Test anhängen (scheitert)**

```bash
tools/recovery-policy.sh seal
[ "$(tools/recovery-policy.sh open pcr)" = "TEST-DEK" ] || fail "Zweig 1 öffnet nicht"
echo "ok: Zweig 1 (Normalstart über PCR)"
```

- [ ] **Schritt 2: Laufen lassen, Fehlschlag sehen**

Ausführen: `tools/recovery-test.sh`
Erwartet: bricht ab, weil `tools/recovery-policy.sh` fehlt.

- [ ] **Schritt 3: Skript mit dem ersten Zweig schreiben**

```bash
#!/usr/bin/env bash
# Builds the sealing policy and opens the sealed secret again. The policy has
# the shape the specification demands: one branch for the normal boot, six for
# the pairs of four sticks.
set -euo pipefail
cd "$(dirname "$0")/.."
work="$(pwd)/dist/recovery/work"
keys="$(pwd)/dist/recovery/keys"
mkdir -p "$work"
export TPM2TOOLS_TCTI="${TPM2TOOLS_TCTI:-$(tools/recovery-swtpm.sh tcti)}"
pcrs="sha384:0,1,2,3,4,5,6,7,11"

primary() {
	[ -f "$work/primary.ctx" ] || tpm2_createprimary -C o -g sha384 -G ecc384 -c "$work/primary.ctx" >/dev/null
}

# Digest of the normal-boot branch: current PCR values, nothing else.
digest_pcr() {
	tpm2_startauthsession -S "$work/trial.ctx" >/dev/null
	tpm2_policypcr -S "$work/trial.ctx" -l "$pcrs" -L "$work/branch-pcr.digest" >/dev/null
	tpm2_flushcontext "$work/trial.ctx" >/dev/null
}

seal() {
	primary
	digest_pcr
	printf 'TEST-DEK' > "$work/secret.bin"
	tpm2_create -C "$work/primary.ctx" -g sha384 -u "$work/seal.pub" -r "$work/seal.priv" \
		-i "$work/secret.bin" -L "$work/branch-pcr.digest" \
		-a "fixedtpm|fixedparent|adminwithpolicy|noda" >/dev/null
	shred -u "$work/secret.bin"
	echo "Versiegelt: $work/seal.pub" >&2
}

open_pcr() {
	primary
	tpm2_load -C "$work/primary.ctx" -u "$work/seal.pub" -r "$work/seal.priv" -c "$work/seal.ctx" >/dev/null
	tpm2_startauthsession --policy-session -S "$work/sess.ctx" >/dev/null
	tpm2_policypcr -S "$work/sess.ctx" -l "$pcrs" >/dev/null
	tpm2_unseal -c "$work/seal.ctx" -p "session:$work/sess.ctx"
	tpm2_flushcontext "$work/sess.ctx" >/dev/null
}

case "${1:-}" in
seal) seal ;;
open) case "${2:-}" in pcr) open_pcr ;; *) echo "Aufruf: tools/recovery-policy.sh open pcr"; exit 1 ;; esac ;;
*) echo "Aufruf: tools/recovery-policy.sh seal|open <zweig>"; exit 1 ;;
esac
```

- [ ] **Schritt 4: Test laufen lassen**

Ausführen: `tools/recovery-test.sh`
Erwartet: `ok: Zweig 1 (Normalstart über PCR)`

- [ ] **Schritt 5: Negativfall anhängen — veränderter Boot-Pfad sperrt den Zweig**

```bash
tpm2_pcrextend 11:sha384=$(head -c 48 /dev/zero | xxd -p -c 48) >/dev/null
if tools/recovery-policy.sh open pcr >/dev/null 2>&1; then fail "Zweig 1 öffnet trotz geänderter PCR"; fi
echo "ok: geänderte PCR sperrt Zweig 1 (Kriterium 4)"
tools/recovery-swtpm.sh reset
export TPM2TOOLS_TCTI="$(tools/recovery-swtpm.sh tcti)"
rm -f dist/recovery/work/*.ctx dist/recovery/work/seal.*
tools/recovery-policy.sh seal
```

Ausführen: `tools/recovery-test.sh`
Erwartet: beide `ok`-Zeilen.

- [ ] **Schritt 6: Commit**

```bash
git add tools/recovery-policy.sh tools/recovery-test.sh
git commit -m "Seal against the PCR branch"
```

---

### Aufgabe 6: Zweig mit Stick-Signatur

**Dateien:**
- Ändern: `tools/recovery-policy.sh`, `tools/recovery-test.sh`

**Schnittstellen:**
- Liefert: `tools/recovery-policy.sh open pair <i> <j>` öffnet über den Zweig des Stick-Paars
  (i, j aus 1..4, i < j). Neue Hilfsfunktionen im Skript: `load_stick <n>` lädt
  `dist/recovery/keys/stickN.pem` als externen Schlüssel ins TPM
  (`$work/stickN.ctx`), `sign_nonce <n> <datei>` lässt SoftHSM2 die TPM-Nonce signieren.

Wichtig für den Umsetzer: In einer **Trial-Session** prüft das TPM die Signatur nicht (TPM-2.0-Spec,
TPM2_PolicySigned), beim Bauen der Digests genügt deshalb eine Platzhalter-Signatur. Beim Öffnen ist
die Session echt, dort muss die Signatur stimmen und über die TPM-Nonce laufen (`--nonce` /
`--ticket`-Optionen von `tpm2_policysigned` gegen `man tpm2_policysigned` auf der VM prüfen; der
Test entscheidet).

- [ ] **Schritt 1: Test anhängen (scheitert)**

```bash
[ "$(tools/recovery-policy.sh open pair 1 2)" = "TEST-DEK" ] || fail "Stick-Paar 1+2 öffnet nicht"
echo "ok: Stick-Paar 1+2"
```

- [ ] **Schritt 2: Laufen lassen, Fehlschlag sehen**

Ausführen: `tools/recovery-test.sh`
Erwartet: Abbruch mit `Aufruf: tools/recovery-policy.sh open pcr`.

- [ ] **Schritt 3: Signieren über SoftHSM2 einbauen**

```bash
module=/usr/lib/softhsm/libsofthsm2.so
export SOFTHSM2_CONF="$(pwd)/dist/recovery/softhsm/softhsm2.conf"

load_stick() {
	[ -f "$work/stick$1.ctx" ] || \
		tpm2_loadexternal -C o -G ecc -u "$keys/stick$1.pem" -c "$work/stick$1.ctx" >/dev/null
}

# The stick signs the nonce the TPM generated itself; that is what makes a
# recorded run worthless (replay).
sign_nonce() {
	pkcs11-tool --module "$module" --token-label "TEST-STICK-$1" --login --pin 12345678 \
		--sign --mechanism ECDSA-SHA384 --signature-format openssl \
		--input-file "$2" --output-file "$3"
}
```

- [ ] **Schritt 4: Zweig-Digest und Öffnen ergänzen**

```bash
# One digest per pair: PCR of the recovery image plus both stick signatures.
digest_pair() {
	tpm2_startauthsession -S "$work/trial.ctx" >/dev/null
	tpm2_policypcr -S "$work/trial.ctx" -l "$pcrs" >/dev/null
	for n in "$1" "$2"; do
		load_stick "$n"
		: > "$work/dummy.sig"
		tpm2_policysigned -S "$work/trial.ctx" -c "$work/stick$n.ctx" -g sha384 \
			-s "$work/dummy.sig" -f ecdsa >/dev/null
	done
	tpm2_policyor -S "$work/trial.ctx" -l "sha384:$work/branch-pcr.digest" >/dev/null || true
	tpm2_getpolicydigest -S "$work/trial.ctx" -o "$work/branch-$1$2.digest" >/dev/null
	tpm2_flushcontext "$work/trial.ctx" >/dev/null
}

open_pair() {
	primary
	tpm2_load -C "$work/primary.ctx" -u "$work/seal.pub" -r "$work/seal.priv" -c "$work/seal.ctx" >/dev/null
	tpm2_startauthsession --policy-session -S "$work/sess.ctx" >/dev/null
	tpm2_policypcr -S "$work/sess.ctx" -l "$pcrs" >/dev/null
	for n in "$1" "$2"; do
		load_stick "$n"
		tpm2_policynoncetpm -S "$work/sess.ctx" -o "$work/nonce.bin" >/dev/null 2>&1 || \
			tpm2_getrandom 32 -o "$work/nonce.bin" >/dev/null
		sign_nonce "$n" "$work/nonce.bin" "$work/stick$n.sig"
		tpm2_policysigned -S "$work/sess.ctx" -c "$work/stick$n.ctx" -g sha384 \
			-s "$work/stick$n.sig" -f ecdsa >/dev/null
	done
	tpm2_unseal -c "$work/seal.ctx" -p "session:$work/sess.ctx"
	tpm2_flushcontext "$work/sess.ctx" >/dev/null
}
```

Der genaue Weg, an die TPM-Nonce zu kommen, hängt von der Version der tpm2-tools ab. Maßgeblich ist:
Die Signatur muss über eine Zahl gehen, die das TPM in dieser Sitzung erzeugt hat. Geht das mit der
vorhandenen Version nicht, **stoppen und fragen** — nicht durch eine selbst gewählte Zahl ersetzen,
das wäre genau die Lücke, die Kriterium 3 (Replay) findet.

- [ ] **Schritt 5: `seal` auf den Paar-Zweig umstellen und Test laufen lassen**

`seal` versiegelt vorerst gegen `branch-12.digest`, damit der Zweig einzeln prüfbar ist.
Ausführen: `tools/recovery-test.sh`
Erwartet: `ok: Stick-Paar 1+2`

- [ ] **Schritt 6: Negativfall — ein Stick allein reicht nicht**

```bash
if tools/recovery-policy.sh open pair 1 1 >/dev/null 2>&1; then fail "ein Stick allein öffnet"; fi
echo "ok: ein Stick allein öffnet nicht (Kriterium 1)"
```

Ausführen: `tools/recovery-test.sh`
Erwartet: beide `ok`-Zeilen.

- [ ] **Schritt 7: Commit**

```bash
git add tools/recovery-policy.sh tools/recovery-test.sh
git commit -m "Open the sealed secret with two stick signatures"
```

---

### Aufgabe 7: Plattform-PIN und Zähler

**Dateien:**
- Ändern: `tools/recovery-policy.sh`, `tools/recovery-test.sh`

**Schnittstellen:**
- Liefert: Jeder Paar-Zweig enthält zusätzlich `tpm2_policyauthvalue` (Plattform-PIN, TEST-Wert
  `1234`) und `tpm2_policynv` gegen den Zähler `0x1500016`. Neuer Unterbefehl
  `tools/recovery-policy.sh bump` erhöht den Zähler und versiegelt mit `c+1` neu.

- [ ] **Schritt 1: Test anhängen (scheitert)**

```bash
tools/recovery-policy.sh bump
[ "$(tools/recovery-policy.sh open pair 1 2)" = "TEST-DEK" ] || fail "nach Zählerstand +1 öffnet nichts"
echo "ok: Zähler +1 und neu versiegelt"
```

- [ ] **Schritt 2: Laufen lassen, Fehlschlag sehen**

Ausführen: `tools/recovery-test.sh`
Erwartet: Abbruch, `bump` kennt das Skript noch nicht.

- [ ] **Schritt 3: Zähler anlegen, PIN und PolicyNV in den Zweig aufnehmen**

```bash
nv=0x1500016
counter_file="$work/counter.bin"

counter() {
	tpm2_nvreadpublic | grep -q "$nv" || \
		tpm2_nvdefine -C o "$nv" -a "nt=counter|ownerwrite|ownerread|authread" >/dev/null
	tpm2_nvincrement -C o "$nv" >/dev/null 2>&1 || true   # a fresh counter must be written once
	tpm2_nvread -C o "$nv" -s 8 -o "$counter_file" >/dev/null
}
```

In `digest_pair` und `open_pair` nach den Stick-Signaturen ergänzen:

```bash
	tpm2_policyauthvalue -S "$session" >/dev/null
	tpm2_policynv -C o "$nv" -S "$session" -i "$counter_file" eq >/dev/null
```

`seal` bekommt `-p 1234` (TEST-PIN), `tpm2_unseal` entsprechend
`-p "session:$work/sess.ctx+1234"`.

- [ ] **Schritt 4: `bump` schreiben**

```bash
bump() {
	tpm2_nvincrement -C o "$nv" >/dev/null
	tpm2_nvread -C o "$nv" -s 8 -o "$counter_file" >/dev/null
	rm -f "$work/seal.pub" "$work/seal.priv" "$work"/branch-*.digest
	seal
	echo "Zähler erhöht, neu versiegelt" >&2
}
```

- [ ] **Schritt 5: Test laufen lassen**

Ausführen: `tools/recovery-test.sh`
Erwartet: `ok: Zähler +1 und neu versiegelt`

- [ ] **Schritt 6: Negativfälle anhängen — alter Zählerstand und falsche PIN**

```bash
cp dist/recovery/work/counter.bin dist/recovery/work/counter.old
tools/recovery-policy.sh bump
cp dist/recovery/work/counter.old dist/recovery/work/counter.bin
if tools/recovery-policy.sh open pair 1 2 >/dev/null 2>&1; then fail "alter Zählerstand öffnet"; fi
echo "ok: alter Zählerstand öffnet nicht (Kriterium 3)"
tpm2_nvread -C o 0x1500016 -s 8 -o dist/recovery/work/counter.bin

if PIN=9999 tools/recovery-policy.sh open pair 1 2 >/dev/null 2>&1; then fail "falsche PIN öffnet"; fi
echo "ok: falsche Plattform-PIN öffnet nicht (Kriterium 1)"
```

Dafür liest das Skript die PIN aus `${PIN:-1234}`. Die PIN darf nirgends ausgegeben werden.

- [ ] **Schritt 7: Commit**

```bash
git add tools/recovery-policy.sh tools/recovery-test.sh
git commit -m "Bind the policy to the platform PIN and the counter"
```

---

### Aufgabe 8: PolicyOR aus sieben Zweigen — das Abnahmekriterium

**Dateien:**
- Ändern: `tools/recovery-policy.sh`, `tools/recovery-test.sh`

**Schnittstellen:**
- Liefert: `seal` versiegelt gegen `tpm2_policyor` über sieben Digests: `branch-pcr.digest` und
  `branch-12`, `branch-13`, `branch-14`, `branch-23`, `branch-24`, `branch-34`. `open pcr` und
  `open pair i j` schließen ihren Ablauf jeweils mit demselben `tpm2_policyor` ab.

- [ ] **Schritt 1: Test anhängen (scheitert)**

```bash
for pair in "1 2" "1 3" "1 4" "2 3" "2 4" "3 4"; do
	[ "$(tools/recovery-policy.sh open pair $pair)" = "TEST-DEK" ] || fail "Paar $pair öffnet nicht"
done
echo "ok: alle sechs Stick-Paare öffnen (Kriterium 2)"
[ "$(tools/recovery-policy.sh open pcr)" = "TEST-DEK" ] || fail "Normalstart öffnet nicht"
echo "ok: sieben Zweige in einer Policy (Abnahme Paket 0)"
```

- [ ] **Schritt 2: Laufen lassen, Fehlschlag sehen**

Ausführen: `tools/recovery-test.sh`
Erwartet: scheitert beim ersten Paar, das nicht der versiegelte Zweig ist.

- [ ] **Schritt 3: PolicyOR bauen**

```bash
branches="pcr 12 13 14 23 24 34"

policy_or_digest() {
	for b in $branches; do [ "$b" = pcr ] || digest_pair "${b:0:1}" "${b:1:1}"; done
	digest_pcr
	list=""
	for b in $branches; do list="$list,$work/branch-$b.digest"; done
	tpm2_startauthsession -S "$work/trial.ctx" >/dev/null
	tpm2_policyor -S "$work/trial.ctx" -l "sha384:${list#,}" -L "$work/policy.digest" >/dev/null
	tpm2_flushcontext "$work/trial.ctx" >/dev/null
}
```

`seal` benutzt `-L "$work/policy.digest"`. Am Ende von `open_pcr` und `open_pair` steht jeweils:

```bash
	tpm2_policyor -S "$work/sess.ctx" -l "sha384:${list#,}" >/dev/null
```

- [ ] **Schritt 4: Test laufen lassen**

Ausführen: `tools/recovery-test.sh`
Erwartet: `ok: alle sechs Stick-Paare öffnen (Kriterium 2)` und
`ok: sieben Zweige in einer Policy (Abnahme Paket 0)`

- [ ] **Schritt 5: Negativfall — fremder Stick**

```bash
openssl ecparam -name secp384r1 -genkey -noout -out dist/recovery/work/fremd.key
openssl ec -in dist/recovery/work/fremd.key -pubout -out dist/recovery/keys/stick9.pem
if tools/recovery-policy.sh open pair 1 9 >/dev/null 2>&1; then fail "fremder Stick öffnet"; fi
rm -f dist/recovery/keys/stick9.pem dist/recovery/work/fremd.key
echo "ok: fremder Stick öffnet nicht (Kriterium 2)"
```

- [ ] **Schritt 6: Commit**

```bash
git add tools/recovery-policy.sh tools/recovery-test.sh
git commit -m "Seal against a PolicyOR of seven branches"
```

---

### Aufgabe 9: Abnahmeprotokoll und Abschluss

**Dateien:**
- Anlegen: `docs/recovery-abnahme.md`
- Ändern: `tools/recovery-test.sh`

**Schnittstellen:**
- Liefert: `tools/recovery-test.sh` endet mit `Paket 0: ok` und Rückgabe 0; `docs/recovery-abnahme.md`
  hält alle 18 Kriterien mit Ergebnis, Datum und Prüfer fest.

- [ ] **Schritt 1: Abschlusszeile in den Test**

```bash
echo "Paket 0: ok"
```

- [ ] **Schritt 2: Ganzen Lauf von vorn**

Ausführen:
```bash
tools/recovery-swtpm.sh reset
rm -rf dist/recovery/work && mkdir -p dist/recovery/work
tools/recovery-test.sh
```
Erwartet: alle `ok`-Zeilen und `Paket 0: ok`. Läuft der Test nach vollständigem Zurücksetzen nicht
durch, ist er nicht fertig.

- [ ] **Schritt 3: Abnahmeprotokoll schreiben**

```markdown
# Abnahme Recovery-Modus

Die 18 Kriterien stehen in `docs/recovery-modus.md`, Abschnitt 12. Ein Kriterium gilt erst als
bestanden, wenn es hier mit Datum, Prüfer und Ergebnis steht. Ein einziges offenes Kriterium
blockiert die Inbetriebnahme.

| Nr. | Kriterium (Kurzform) | Ergebnis | Datum | Prüfer | Beleg |
|---|---|---|---|---|---|
| 1 | Ein Stick allein oder ohne PIN gibt nicht frei | vorläufig bestanden gegen swtpm und SoftHSM2 | 2026-09-23 | Sitzung Recovery | `tools/recovery-test.sh` |
| 2 | Jedes der 6 Paare gibt frei, fremder Stick nicht | vorläufig bestanden gegen swtpm | 2026-09-23 | Sitzung Recovery | `tools/recovery-test.sh` |
| 3 | Alter Zählerstand gibt nicht frei (Replay) | vorläufig bestanden gegen swtpm | 2026-09-23 | Sitzung Recovery | `tools/recovery-test.sh` |
| 4 | Geändertes Image sperrt über die PCR | vorläufig bestanden gegen swtpm | 2026-09-23 | Sitzung Recovery | `tools/recovery-test.sh` |
| 5 | Datenträger in anderer Maschine | offen | | | Paket 5 |
| 6 | Backup ohne Sticks und TPM | offen | | | Paket 5 |
| 7 | Fremdes OS auf dem Server | offen | | | Paket 5 |
| 8 | Admin-Datensatz direkt in der DB | offen | | | Paket 7 |
| 9 | Sperren nach 3 Fehlversuchen | offen | | | Paket 2 und 3 |
| 10 | TPM-Bus zeigt nur verschlüsselte Sessions | offen | | | Paket 3, nur auf echter Hardware |
| 11 | Kein Netz, keine Shell, kein Login | offen | | | Paket 8 |
| 12 | Audit-Eintrag beim Zeugen, Alarm in 60 s | offen | | | Paket 7 |
| 13 | Recovery-Konto verfällt nach 24 h | offen | | | Paket 6 |
| 14 | Recovery ohne Vorwissen in unter 20 min | offen | | | Paket 10 |
| 15 | Server neu aufsetzen in unter 60 min | offen | | | Paket 10 |
| 16 | Stick-Ersatz | offen | | | Paket 4 |
| 17 | Reproduzierbarer Build | offen | | | Paket 8 |
| 18 | Externer Penetrationstest | offen | | | Paket 9 |

„Vorläufig bestanden“ heißt: gegen den Simulator swtpm und SoftHSM2 nachgewiesen. Mit echtem TPM
und echten Sticks wird jedes dieser Kriterien in Paket 3 erneut geprüft und hier ergänzt.
```

- [ ] **Schritt 4: Commit und Pull Request**

```bash
git add docs/recovery-abnahme.md tools/recovery-test.sh
git commit -m "Record the acceptance results of work package 0"
git push -u origin recovery
gh pr create --base main --head recovery --title "Recovery-Modus: Testumgebung (Paket 0)"
```

- [ ] **Schritt 5: Dem Koordinator melden**

Kurz melden: Ergebnis von `tools/recovery-test.sh`, die Version der tpm2-tools auf der VM, und die
beiden offenen Entscheidungen aus diesem Paket — (1) wie der CI-Job bei jedem Commit ausgelöst wird
(GitHub Actions braucht einen eigenen Runner auf der VM, das lädt Code herunter und muss Thomas
freigeben), (2) ob die vier Test-Sticks später durch echte Nitrokey HSM 2 ersetzt werden, weil
Paket 2 beide Backends verlangt.

---

## Was dieses Paket nicht macht

- Keine Rust-Zeile: die Krypto-Bibliothek ist Paket 1, das TPM-Modul Paket 3.
- Kein echtes Secure Boot: OVMF startet, die eigenen Schlüssel und die signierten UKI kommen mit
  Paket 8.
- Keine Zeremonie, kein Einlernen, kein Stick-Ersatz: das sind die Pakete 4 und 6. Der CI-Job fährt
  sie erst durch, wenn es sie gibt; `tools/recovery-test.sh` wächst mit.
