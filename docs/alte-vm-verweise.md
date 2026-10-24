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

> **Nachtrag vom 24.10.2026, nachmittags: entschieden und gemacht.** Thomas hat zugestimmt:
> neue Maschine aufsetzen, die alte einmal zum Bergen anwerfen. Beides ist passiert. Was
> dabei herauskam, steht in Abschnitt 6 — darunter **die Antwort auf die offene Frage, was
> die alte Maschine umgeworfen hat**. Die Abschnitte 4 und 5 bleiben stehen, wie sie
> vorgelegt wurden; sie waren die Grundlage der Entscheidung.

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

---

## 6. Was danach geschah: geborgen, abgeschaltet, neu aufgesetzt

**Alles in diesem Abschnitt ist am 24.10.2026 gemessen**, nicht geplant.

### Die alte Maschine: einmal an, geborgen, sauber aus

Vor dem Start ein Schnappschuss (`vor-bergung-20261024`), damit ein Start, der das
Dateisystem beschädigt, die vier Dinge nicht mitnimmt. Danach `qm start 208`: Port 22 war
nach **16 Sekunden** offen, die Anlage lief wieder auf `0.2.2`.

Geborgen sind die kleinen, die nirgends sonst liegen:

| Was | Wohin |
|---|---|
| `make-store-tls.sh` | `tools/pruefladen-tls.sh` |
| `install-store.sh` | `tools/pruefladen-stellen.sh` |
| `inspect.sh` | `tools/haertung-nachsehen.sh` |
| `catalog.json`, `store-key.pem`, `manifest.json`, die zwei Oberflächendateien von vor Punkt 73 | außerhalb des Repos abgelegt |

**Die geheimen Schlüssel der alten Test-CA sind absichtlich nicht geborgen.** Das Skript baut
die CA selbst neu — nachgewiesen, indem es auf der neuen Maschine genau das getan hat. Einen
geheimen Schlüssel zu kopieren, den man nicht braucht, ist ein Risiko ohne Gegenwert.

**Nicht geborgen, mit Grund:** `/srv/ngc-store/terminal-1.0.0.ngc` (103 MB) und die zwei
Pakete in `~/uhr-bau/` (190 MB). Das sind gebaute Pakete, die `ngc-store pack` aus dem Repo
wieder herstellt. Der Katalog daneben ist geborgen und ist das, was Beweiskraft hat —
`/srv/ngc-store/catalog.json` ist bytegleich mit dem in `ngcapp/store/` (beide
`5464c828…dbf1`), es gab also nur eine Fassung.

Danach `qm shutdown 208` — diesmal sauber. **Der Schnappschuss bleibt liegen.** Die Maschine
ist aus, nicht gelöscht; das Löschen entscheidet Thomas, und nichts drängt.

### Die offene Frage ist beantwortet: es war kein Absturz

Punkt 74 schrieb auf: „Was sie umgeworfen hat, ist **nicht gemessen**, und zwei statische
Dateien einer Oberfläche sind keine plausible Ursache." Das Zweite war richtig. Das Erste
lässt sich jetzt beantworten, und zwar nicht aus der VM, sondern aus dem **Aufgabenbuch des
Wirts**:

```
UPID:…:qmstop:208:root@pam:   ->  24.10.2026 12:37:27   OK
```

Die letzte Zeile im Journal des vorigen Laufs ist **12:36:19**. Eine Minute später hat ein
`qmstop` als `root@pam` die Maschine **hart angehalten**. Kein Absturz, kein Kernel, kein
NGC-Fehler: sie wurde vom Wirt aus ausgeschaltet. Am selben Tag gab es noch zwei weitere
solche Eingriffe (`qmstop` 01:11:14, `qmshutdown` 04:41:40).

**Warum das wichtig ist:** wer „die Anlage ist umgefallen" als Befund über NGC aufschreibt,
schreibt hier etwas Falsches auf. Wer VM 208 angehalten hat und warum, ist damit **nicht**
gemessen — nur, dass es ein Befehl war und kein Fehler.

**Nebenbefund aus demselben Journal:** bis zuletzt kam **alle 63 Sekunden** ein
`TLS handshake error … remote error: tls: bad certificate` von `10.111.111.111`. Regelmäßig,
also etwas, das fragt und nicht aufgibt. Was dort fragt, ist nicht gemessen.

### Zwei Befunde aus `app-weg.md` haben sich verschoben

Beide standen dort als „nicht gemessen". Jetzt sind sie gemessen — jenes Papier ist
eingefroren, darum steht es hier:

1. **Das Release bringt `ngc-store` mit.** Befund 4 fragte, ob das Release es nicht
   mitliefert oder die Einzel-VM von Hand bestückt wurde. Aus `ngc-api-amd64.tar` des
   Release **v0.2.2** sind genau `ngc-api` und `ngc-store` herausgefallen, und das
   herausgefallene `ngc-store` meldet `0.2.2`. **Das Release liefert es.** Also war die alte
   Maschine von Hand bestückt — ein Beleg mehr für den Handbau.
2. **`/usr/lib/ngc/ngc-store` war auf der alten Maschine da**, mit Datum 19.10. Befund 4
   hatte „heute ist es nicht da" gemessen, und das war damals richtig. Daneben lagen zehn
   Programme wie `ngc-api.vor-0.2.2`, `ngc-api.rechtekreis`,
   `ngc-controller.vor-terminalkanal`. Nichts davon kommt aus einem Release.

### Noch mehr, das nur dort liegt: 22 Messskripte

Der Befund aus Abschnitt 4 ist größer als vier Dinge. In `/home/berry` liegen **22 eigene
Skripte** (zusammen 2,0 MB) in acht Ordnern: `task5fix3` (7), `task5fix1`, `task6fix1` (2),
`smoke` (2), `relbuild`, `ulhrepo` (2), `ngcapp` (3) und vier direkt im Heimatverzeichnis
(`diag.sh`, `probe.sh`, `mutate.sh`, `mut2.sh`). Das sind Messskripte früherer Sitzungen zu
einzelnen Vertragspunkten.

**Nicht geborgen, und das ist eine Entscheidung, keine Nachlässigkeit.** Drei von ihnen
hatten allgemeinen Wert und sind jetzt im Repo. Die anderen 19 gehören zu je einer Messung,
die schon aufgeschrieben ist; sie unbesehen ins Repo zu schütten würde `tools/` mit Dingen
füllen, die niemand mehr aufruft. **Sie liegen auf dem Datenträger und im Schnappschuss.**
Wer einen davon braucht, startet VM 208 noch einmal. **Zu entscheiden (Thomas):** ob vor dem
Löschen der Platte jemand diese 19 durchsieht.

### Die neue Maschine: `ngc-ein`, und sie ist nachbaubar

Angelegt mit `tools/einzelmaschine.sh anlegen` — **ein Skript, kein Handbau**, das war der
ganze Grund für eine neue statt der alten:

| | |
|---|---|
| VMID / Name | 307, `ngc-ein` |
| Adresse | 10.2.3.216, Name `ngc.test` |
| System | Debian 13 (trixie), Kernel 6.12.113+deb13-cloud-amd64 |
| Größe | 2 Kerne, 3922 MiB, 32 GB + 16 GB |
| NGC | `ngc-api` **0.2.2**, `ngc-controller` **0.2.2**, Oberfläche **0.2.1** |
| Datenbank | SQLite, `/var/lib/ngc/api/ngc.db` — der kleine Fall |
| Laden | Prüfladen auf derselben Maschine, `https://store.ngc.test` |

Der Weg war: Pakete, `ulh` geholt, Release v0.2.2 nach `/var/lib/ngc/staged`,
**`tools/pruefladen-tls.sh` und `tools/pruefladen-stellen.sh`** für den Laden, dann
`ngc-all.sh install`, dann `ngc-web.sh install`. Beide Installationen endeten mit
**Ausstiegscode 0**.

**Der Laden musste vor der Installation stehen**, und das ist kein Umweg: ohne eigenen Laden
bricht `ngc-all.sh` mit einer eigenen Meldung ab, weil der Schlüssel des offiziellen Ladens
ein Platzhalter ist (Punkte 56, 57). Genau dafür sind die geborgenen Skripte da. Damit ist
auch gemessen, dass sie **auf einer frischen Maschine** tun, was sie sollen — und dort fiel
eine Lücke auf, die auf der alten nie auffallen konnte: `mod_ssl` war dort schon von NGCs Web
eingeschaltet, auf einer frischen Debian nicht. Das Skript schaltet es jetzt selbst ein.

### Der kleine Fall, durchgemessen

Von außen gemessen, über den **Namen** und nicht über die Adresse (`curl --resolve`, damit
kein `hosts`-Eintrag nötig ist):

| Was | Ergebnis |
|---|---|
| `https://ngc.test/` | **`http=200`, 451 Byte**, `<title>NGC</title>` |
| `/config.json` | `rp.id` = **`ngc.test`**, `apis` = `https://ngc.test:7443` |
| `GET /api/v1/setup` über 443 (Apache) | **200** |
| `GET /api/v1/setup` auf 7443 (API selbst) | **200** |
| Zertifikat | `subject=CN=ngc.test`, `SAN: DNS:ngc.test` — **keine IP-Adresse** |
| Dienst `ngc-api` | `active`, **`NRestarts=0`** |
| `grep -c REPLACE-THIS-LINE /etc/ngc/api.json` | **0** (Punkt 57) |

**`451 Byte` ist genau die Zahl, die im README für den 16.10.2026 steht** — damals auf der
alten Maschine. Dieselbe Seite, dieselbe Größe, auf einer Maschine, die aus einem Skript und
einem Release entstanden ist. Damit ist die Messung des kleinen Falls nicht nur wieder
möglich, sie ist **nachbaubar**, und das war sie bei der alten Maschine nie.

Auch die Behauptung aus Schritt 0 der Anleitung ist jetzt an einer **zweiten** Anlage
gemessen: im Zertifikat steht ein Name und keine Adresse. Über die IP kann es nicht passen.

### Was auf der neuen Maschine noch NICHT gemessen ist

Ehrlich benannt, denn hier hört es auf:

- **Die Ersteinrichtung im Browser.** Der Einmal-Code steht auf dem Schirm, und den ersten
  Verwalter legt ein **Mensch** an. Kein Agent tippt den Code.
- **Darum auch nichts dahinter:** Controller koppeln, Systemrechte, eine App einrichten, und
  der ganze Weg der Punkte 71 und 72. Der Controller ist **nicht gekoppelt**, also läuft
  keine App.
- **Die zweite Oberflächenfassung zu Punkt 73** ist hier nicht eingespielt; die neue Maschine
  läuft auf `origin/main`.
- **Die 16-GB-Platte ist leer.** `fscrypt` ist auf ihr nicht gemessen.
- Der Prüfladen hat einen Katalog, aber **keine App darin** (Folge 1, leer). Für eine
  Auslieferung müsste ein Paket hinein.

### Was Thomas jetzt tun kann, wenn er will

Der Einmal-Code und der Fingerabdruck stehen auf der Maschine (`root`), nicht in diesem
Papier und in keinem Log. Wer `https://ngc.test` im Browser öffnen will, braucht den Namen
auf seinem eigenen Rechner — **Schritt 0 der Anleitung**, mit `10.2.3.216   ngc.test`. Dann
ist der kleine Fall ab der Ersteinrichtung wieder begehbar.
