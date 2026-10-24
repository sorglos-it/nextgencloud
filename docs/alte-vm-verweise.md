# Die Einzel-VM ist aus — was im Repo noch auf sie zeigt

**Stand 24.10.2026.** Die erste wirklich laufende NGC-Anlage lag auf **einer** Maschine
(`ssh ngc`, 10.2.3.208, Name `ngc.test`, NGC `0.2.0`/`0.2.2`). Diese Maschine ist **aus**.
Seit Tagen zeigen Dokumente, Aufträge und Skripte auf sie. Wer ihnen heute folgt, misst ins
Leere — und schreibt dann „nicht erreichbar" als Befund über NGC auf, obwohl der Befund über
eine ausgeschaltete Maschine lautet.

Dieses Papier tut drei Dinge: es zählt die Stellen, es trennt Messung von Anweisung, und es
legt Thomas die Frage vor, die dahinter steht.

## 1. Selbst nachgemessen, 24.10.2026

**Die alte Maschine:**

| Was | Ergebnis |
|---|---|
| `ssh ngc` | `connect to host 10.2.3.208 port 22: Connection timed out` |
| TCP 10.2.3.208:22 | zu |
| VM 208 auf dem Proxmox-Wirt | `stopped` |

Sie ist nicht langsam und nicht gestört. Sie läuft nicht.

**Was stattdessen läuft** — Rolle am laufenden Dienst abgefragt, nicht aus dem Namen
geschlossen, denn **die Namen lügen**:

| Adresse | Name | läuft wirklich |
|---|---|---|
| 10.2.3.210 | api-1 | `ngc-api` **und** `ngc-controller` |
| 10.2.3.211 | api-2 | nur `ngc-api` |
| 10.2.3.212 | db-1 | nur `ngc-controller` |
| 10.2.3.213 | svc-1 | `ngc-controller` + podman |
| 10.2.3.214 | svc-2 | `ngc-controller` + podman |
| 10.2.3.215 | store | kein NGC, nur nginx |

`https://ngc.mein.lan` zeigt auf api-1. Zugang: Benutzer `root`, Schlüssel
`~/Documents/ssh/keys/id_ed25519_ngc`.

Wer eine Rolle braucht, fragt sie ab. `db-1` trägt keine Datenbank, und `store` trägt kein NGC.

## 2. Wie viele Stellen es sind

**64 Zeilen in 9 Dateien** sprechen von der alten Maschine (`10.2.3.208`, `ssh ngc`,
„Einzel-VM", `ngc.test` als Anlagenname).

| Datei | Zeilen | Darf ich daran? |
|---|---|---|
| `docs/vertraege-v1.2.md` | 30 | nur hinten anhängen |
| `docs/app-weg.md` | 17 | **eingefroren**, andere arbeiten daran |
| `docs/thomas-entscheidet.md` | 4 | **eingefroren** |
| `docs/messungen-m1-m3.md` | 1 | **eingefroren** |
| `docs/installation-kleines-system.md` | 7 | ja |
| `docs/zweiter-benutzer-schritte.md` | 2 | ja |
| `docs/erstlauf.md` | 1 | ja |
| `README.md` | 1 | ja |
| `tools/proxmox-sechs.sh` | 1 | ja — aber nicht nötig, siehe unten |

**Nicht mitgezählt, und mit Absicht nicht angefasst:**

- **Rund 250 Vorkommen von `ngc.test` in `shared/`, `apps/` und den Prüfdaten.** Das ist
  **nicht** die alte Maschine. Das ist der für Prüfzwecke reservierte Namensraum `.test`
  (RFC 2606), den der Code als Festwert in Prüfungen benutzt. Hier ein echter Name
  einzusetzen wäre ein Fehler: Prüfungen dürfen keine Namen auflösen, die es wirklich gibt.
  `shared/` ist ohnehin eingefroren.
- **40 Vorkommen in `docs/superpowers/plans/*`.** Datierte Pläne vom September 2026. Ein Plan
  ist ein Papier von damals; wer ihn liest, liest ihn als solchen.
- **`docs/STAND.md:381` und `docs/feldzug-echter-server.md:121`** sprechen von `ssh ngc-recovery`
  — eine **andere** Maschine, die mit dieser Sache nichts zu tun hat.
- **`docs/proxmox-sechs.md:58`** nennt den Schlüsselpfad `id_ed25519_ngc`. Der Schlüssel gilt
  weiter; mit ihm habe ich oben gemessen.
- **`tools/proxmox-sechs.sh:101`** schreibt `(208 = 10.2.3.208)`. Das erklärt die Regel des
  Wirts „letztes Oktett = VMID" an einem Beispiel. Es ist keine Anweisung, dorthin zu gehen,
  und die Regel stimmt weiter. **Bleibt, wie es ist** — eine Änderung würde eine richtige
  Erklärung schlechter machen.

## 3. Je Stelle entschieden

Drei Arten, drei verschiedene Behandlungen. Pauschal ersetzen wäre an zwei von drei Stellen
falsch.

### Messungen bleiben stehen

Eine Messung war richtig, als sie gemacht wurde. Dass die Maschine heute aus ist, macht sie
nicht falsch. **Eine Messung umzuschreiben, weil die Maschine weg ist, wäre
Geschichtsfälschung.** Sie bekommt nur dazu: wo sie entstand, und dass dieser Ort heute aus ist.

- `README.md` — die Tabelle „What runs today, and what measured it" bleibt Zeile für Zeile
  stehen. Darunter steht jetzt ein Satz, auf **welcher** Maschine das gemessen wurde und dass
  sie aus ist.
- `docs/erstlauf.md` — der Grund vom 23.10.2026, eine Wegwerf-Instanz zu nehmen („die eine
  eingerichtete Maschine darf nicht kaputtgehen"), bleibt. Er war richtig. Als Nachtrag steht
  dabei, dass er heute nicht mehr trägt: es gibt keine eingerichtete Einzel-Maschine mehr,
  die man schonen müsste.
- `docs/vertraege-v1.2.md` — **30 Zeilen, keine angefasst.** Punkt 64, 71, 72, 73 und die
  Punkte 56 bis 59 sind an dieser Maschine gemessen. Stattdessen hängt **Punkt 75** hinten an
  und sagt es einmal für alle.
- Die eingefrorenen Dateien (`app-weg.md`, `messungen-m1-m3.md`, `thomas-entscheidet.md`)
  enthalten fast nur Messungen. Punkt 75 deckt sie mit ab; wer an ihnen arbeitet, findet es
  dort.

### Anweisungen zeigen ins Leere und mussten sich ändern

- `docs/zweiter-benutzer-schritte.md` — die Schritte **galten** für die Einzel-VM
  (Vergangenheit). Oben steht jetzt, dass die Maschine aus ist, mit der Messung dazu, und der
  ausdrückliche Satz: wer sie heute geht, kommt nirgends an, und „nicht erreichbar" ist dann
  kein Befund über NGC.
- `docs/installation-kleines-system.md` — der Kasten oben sagt, dass der Messort aus ist, dass
  die Messungen gelten und dass die verteilte Anlage den kleinen Fall **nicht** ersetzen kann.
  In der Tabelle steht eine Zeile „Läuft heute? nein".
- **Die Zeile zum Abtippen war die gefährlichste Stelle.** In Schritt 0 stand
  `10.2.3.208   ngc.test` als `hosts`-Zeile. Das sieht aus wie etwas, das man übernimmt, und
  führt zu einer toten Adresse. Jetzt steht dort zuerst
  `<IP-Adresse deiner Anlage>   <dein Name>`, und die alte Zeile darunter, ausdrücklich als
  Muster von damals gekennzeichnet.

### `ngc.test` in der Anleitung: bleibt Beispielname

**Entscheidung: Beispiel, und von jetzt an ausdrücklich so benannt.** Begründung, in dieser
Reihenfolge:

1. **`.test` ist für genau das reserviert (RFC 2606).** Der Name kann nie mit einer echten
   Domain zusammenstoßen. Ein Beispielname, der versehentlich jemandem gehört, ist ein Fehler,
   den man nicht wieder einfängt.
2. **Der ganze Prüfsatz benutzt `ngc.test`.** Setzt die Anleitung einen echten Namen ein,
   läuft sie dem Code auseinander, und der Leser findet in den Prüfdaten eine andere
   Schreibweise als in der Anleitung.
3. **Ein echter Name wäre `ngc.mein.lan`, also Thomas' Heimnetz.** Das hilft keinem Leser, der
   NGC zum ersten Mal aufsetzt — und die Anleitung ist genau für den geschrieben.
4. Die Anleitung hatte den **umgekehrten** Fehler schon einmal: sie sagte `<host>` und nicht,
   welche Anlage sie beschreibt, und Leser trugen den falschen Namen ein (das steht in ihrem
   eigenen Abschnitt „Was gefehlt hat"). Die Abhilfe ist nicht, einen echten Namen
   festzunageln, sondern zu sagen, dass es ein Beispiel ist und man seinen eigenen einsetzt.

**Eine Ausnahme, und sie ist wichtig:** wo die Anleitung von einer **Messung** spricht
(`/config.json` gab `rp.id` so aus), ist `ngc.test` kein Beispiel, sondern der Name, unter dem
wirklich gemessen wurde. Diese Stellen stehen in der Vergangenheitsform, sind als Messung
gekennzeichnet und bleiben inhaltlich, wie sie waren.

## 4. Befund: etwas, das das Projekt braucht, liegt nicht im Repo

Auf der alten Maschine lag Dinge, die **nirgends sonst** existieren. Ich habe nichts
herübergeholt — das wäre eigenmächtig, und die Maschine müsste dafür erst wieder anlaufen.
Benannt gehört es trotzdem:

1. **Die zwei Skripte des Prüfladens, in `/home/berry/ngcapp/`.** Eines baut eine eigene CA und
   ein Zertifikat für `store.ngc.test`, eines legt die CA in den System-Vertrauensspeicher,
   schreibt den Namen in `/etc/hosts`, kopiert nach `/srv/ngc-store` und legt einen
   Apache-vhost auf 443 an. **Keines von beiden ist im Repo** (`docs/app-weg.md`, Befund 3).
   Ohne sie baut das jeder neu aus dem Kopf — oder gar nicht. Der Vorschlag, sie als
   `tools/pruefladen-tls.sh` und `tools/pruefladen-stellen.sh` zu holen, steht dort schon.
   **Jetzt liegen sie auf einem Datenträger, an den niemand kommt, solange die VM aus ist.**
2. **Der Inhalt des Ladens unter `/srv/ngc-store`** samt dem Katalog in Folge 1, unterschrieben
   mit dem Schlüssel `WM0yymuJtd9UxaUONGIXow`. Der geheime Teil dieses Schlüssels war schon
   vorher nicht zu finden (`app-weg.md`, Befund 2) — diesem Laden kann also ohnehin niemand
   eine App mehr hinzufügen. Das ist **kein** Grund, ihn zu verlieren: er ist der einzige
   Beleg dafür, wie ein echt ausgelieferter Katalog aussah.
3. **Ein von Hand übersetztes `ngc-store` in `~/uhr-bau/`.** Auf der Maschine lag in
   `/usr/lib/ngc/` nur `ngc-api` und `ngc-controller`; `ngc-store` fehlte und wurde für den Lauf
   selbst gebaut (`app-weg.md`, Befund 4). **Ungemessen blieb, ob das Release `ngc-store`
   wirklich nicht mitbringt** oder ob die Maschine von Hand bestückt war. Diese Messung ist
   jetzt an dieser Maschine nicht mehr möglich — sie ist aber am Release nachholbar und sollte
   nachgeholt werden.
4. **`/root/zweiter-benutzer-backup-20261024/`** — die Sicherung der zwei Oberflächendateien
   (`users.js`, `shell.css`) von vor der Änderung zu Punkt 73. Die geänderten Fassungen liegen
   im Repo; der Stand **davor**, gegen den gemessen wurde, liegt nur dort.

**Was all vier gemeinsam haben:** die erste wirklich laufende Anlage war ein **Handbau**, über
Monate gewachsen, und ihr Zustand ist mehrfach von Hand verändert worden. Deshalb ist sie nicht
nachbaubar — und deshalb ist das hier kein Verlust an Dateien, sondern ein Befund über die
Arbeitsweise: **was nur auf einer Maschine liegt, ist nicht gesichert.**

## 5. Für Thomas: braucht das Projekt die Einzel-Maschine noch?

Die Frage gehört beantwortet, nicht umgangen. **Entscheiden darf sie Thomas.**

### Ja, der kleine Fall braucht eine Maschine

Drei Gründe, nach der Rangfolge (Stabilität zuerst):

1. **Der kleine Fall ist ein ausdrücklich unterstützter Fall, kein Nebenweg.** Er hat eine
   eigene Anleitung (`installation-kleines-system.md`) und eigene Vertragspunkte (47: eine
   Datenbankdatei und ein Schlüssel je Programm). Er ist der Fall, den die meisten Nutzer
   fahren werden — ein Rechner, alles drauf.
2. **Die verteilte Anlage kann ihn nicht ersetzen.** Sie unterscheidet sich genau dort, wo der
   kleine Fall seine eigenen Fehler hat: SQLite statt eigener Datenbankmaschine, ein Skript
   (`ngc-all`) statt vier, ein Name statt mehrerer. Was der kleine Fall bricht, bricht die
   verteilte Anlage nicht — und umgekehrt.
3. **Er hat die ersten sechs Hürden ans Licht gebracht** (Punkt 71, „Sechs Wände auf einem
   Weg"). Eine Umgebung, die nachweislich Fehler findet, schaltet man nicht ohne Ersatz ab.
   Ohne Maschine ist der kleine Fall ab heute **ungemessen** — und die Punkte 56 bis 59, 64,
   71 und 73 sind nicht nachprüfbar.

### Mein Vorschlag: eine **neue** Maschine, und die alte vorher **einmal** anwerfen

**Nicht** die alte dauerhaft zurückholen. Begründung:

- **Die alte ist ein Handbau und darum nicht nachbaubar.** Ihr Zustand ist über Monate von
  Hand gewachsen, der Prüfladen liegt in einem Heimatverzeichnis, `ngc-store` war von Hand
  hineingelegt, der Ladenschlüssel ist weg. Wer sie zurückholt, holt eine Maschine zurück,
  die niemand ein zweites Mal aufbauen kann. **Stabilität heißt hier: nachbaubar.** Eine
  Messung auf einem Handbau sagt nicht, was ein Nutzer erlebt.
- **Eine neue, aus Release und Repo nach der Anleitung aufgesetzte Maschine prüft zwei Dinge
  auf einmal:** den kleinen Fall **und** die Anleitung selbst. Genau dieser doppelte Lauf hat
  beim letzten Mal die sechs Wände gefunden.
- **Aber die alte darf nicht gelöscht werden, bevor Abschnitt 4 geborgen ist.** Vier Dinge
  liegen nur auf ihrem Datenträger. Darum: **einmal starten, die vier Dinge holen, ins Repo
  bringen, was ins Repo gehört — dann abschalten oder löschen.**

**Zu entscheiden (Thomas), drei Fragen:**

| # | Frage | Mein Vorschlag |
|---|---|---|
| a | Soll der kleine Fall weiter geprüft werden? | **Ja.** Ohne Maschine ist er ab heute ungemessen. |
| b | Alte zurück oder neue dazu? | **Neue dazu**, aus Release und Anleitung aufgesetzt — nachbaubar, und sie prüft die Anleitung mit. |
| c | Was mit der alten? | **Einmal starten**, die vier Dinge aus Abschnitt 4 bergen, dann weg. Nicht vorher löschen. |

Zu (c): das Bergen ist **nicht** eigenmächtig gemacht worden und sollte es auch nicht werden —
es braucht eine laufende Maschine und eine Entscheidung darüber, was davon ins Repo gehört
(der Prüfladen: ja; ein Katalog mit einem verlorenen Schlüssel: eher als Beleg, nicht als
Werkzeug).
