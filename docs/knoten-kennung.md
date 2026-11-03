# Welcher API-Knoten hat geantwortet?

Stand 03.11.2026. Gemessen im Repo auf `origin/main` (0471b02d) und im ulh-Repo. Die vier Lesungen
an der Anlage hat die Leitsitzung am 03.11.2026 gefahren; sie stehen in Abschnitt 7 mit dem
Ergebnis. **Eine davon hat einen Fund dieses Blatts widerlegt** — Abschnitt 2b sagt, welchen.

Rangfolge dieses Blatts: **Sicherheit > Stabilität > Geschwindigkeit.** Es entscheidet nichts.

## 1. Was der Befund kostet

Bei einem Fehler, der über zwei API-Knoten läuft, ist nicht feststellbar, welcher Knoten
geantwortet hat. Eine Ursachensuche an einem Anmeldefehler musste die Zuordnung über eine
Paritätsrechnung erschließen.

## 2. Die drei Messungen, nachgelesen

### a) Der Lastverteiler mit zwei Mitgliedern — bestätigt, mit einer Abweichung

`/etc/ngc/apache-apis.conf` wird **nicht von Hand gepflegt**. Die Datei entsteht aus einer Vorlage,
und zwar an **vier** Stellen, die gleich bleiben müssen:

| Stelle | Datei | Wer schreibt |
|---|---|---|
| Vorlage des Controllers | `apps/server-controller/templates/apache-apis.conf.tmpl` | `ngc-controller`, Rolle `web` |
| Beispiel als Bezug | `apps/web/templates/apache-apis.conf.example` | – (Bezugsfassung) |
| Erstinstallation | ulh `apps/cli/assets/scripts/ngc-web.sh`, `render_balancer()` | `ngc-web install/configure` |
| Testdaten | `apps/server-controller/classes/apache/testdata/apache-apis.conf` | Test |

Inhalt der Vorlage: `<Proxy "balancer://ngc-api">`, je API ein `BalancerMember` mit
`retry=5 timeout=65 connectiontimeout=2 keepalive=On upgrade=websocket`, dazu
`ProxySet lbmethod=byrequests failontimeout=On`. Deckt sich mit der Messung.

**Abweichung, am 03.11.2026 an der Anlage geklärt:** `/etc/ngc/apache-apis.conf` auf api-1 hat
`retry=5 timeout=65 keepalive=On upgrade=websocket` — **ohne** `connectiontimeout=2`. Das Abschreiben
der Leitsitzung war vollständig; die Datei an der Anlage ist älter als die Vorlage.

Und sie passt zu **keiner** der vier Stellen von heute:

| | `connectiontimeout=2` | `upgrade=websocket` am | `Retry-After` / `LogLevel` |
|---|---|---|---|
| Vorlage des Controllers (heute) | ja | `BalancerMember` | ja |
| `.example` und ulh `render_balancer()` | nein | `ProxyPass` | nein |
| **Anlage (03.11.2026, 21 Zeilen)** | **nein** | **`BalancerMember`** | **nein** |

Die fünfte Lesung (Abschnitt 7) hat das festgenagelt: an der Anlage fehlen **beide** Zeilen
`Header always set Retry-After "5" …` und `LogLevel proxy:info proxy_balancer:info`. Die Fassung
liegt damit **vor beiden** heutigen Vorlagen — nicht zwischen ihnen, wie hier zuerst stand. Die
Kopfzeile der Datei nennt ihre Herkunft selbst: geschrieben von `ngc-web` beim Aufsetzen, danach
vom Controller mit Rolle `web`.

Eigener kleiner Befund: die Anlage fährt eine Vorlage, die den Verbindungs-Zeitablauf, die
`Retry-After`-Kopfzeile und die Protokollstufe für den Lastverteiler nicht hat. **Folge für den
Entwurf in Abschnitt 9:** die dort genannte Einfügestelle „nach `LogLevel`" gibt es an der Anlage
heute gar nicht. Sie entsteht erst, wenn der Controller die neue Vorlage schreibt. Die Zeilennummern
in Abschnitt 9 sind Zeilennummern **der Vorlagen im Repo**, nicht der Datei an der Anlage.

**Eigener Fund, unabhängig von dieser Aufgabe:** `apache-apis.conf.example` behauptet im Kopf, die
Bezugsfassung zu sein, weicht aber von der Vorlage des Controllers ab — `upgrade=websocket` steht
dort am `ProxyPass` statt am `BalancerMember`. Zwei Fassungen derselben Datei, und keine Prüfung
hält sie zusammen. Eigener Punkt, eigener PR.

### b) Kein Protokollformat trägt den Namen des Arbeiters — bestätigt

Keine der vier Stellen schreibt `LogFormat` oder `CustomLog` in den Lastverteiler. Richtig, und das
ist der Befund, um den es geht.

#### Und ein Fund dieses Blatts, der widerlegt wurde

Bis zum 03.11.2026 stand hier, die Anlage protokolliere mit `combined` und damit die vollständige
Abfragezeichenkette — ein Geheimnis-im-Protokoll-Fund. **Das ist falsch.** Die Leitsitzung hat die
Dateien der Anlage gelesen:

```
/etc/apache2/sites-available/ngc-web.conf:331  LogFormat "%h %l %u %t \"%m %U %H\" %>s %O \"%{User-Agent}i\"" ngc
ngc-web.conf:352                               CustomLog …/ngc-web-80-access.log ngc
ngc-web.conf:362                               CustomLog …/ngc-web-access.log    ngc
```

Die Anlage **definiert** das Format `ngc` und benutzt es für **beide** NGC-Rechner. `%U` ist der
Pfad ohne Abfragezeichenkette, kein `Referer`, kein `Cookie` — genau die Entscheidung, die in der
Vorlage steht. `combined` steht nur in `default-ssl.conf:13` und `000-default.conf:21`, den
Debian-Vorgaben für Rechner, die nicht NGC sind.

**Wie dieser Fehlschluss zustande kam, und was daraus zu lernen ist:** gemessen war eine Abweichung
zwischen zwei Dateien im **Repo** (`apache-vhost.conf` definiert `ngc`, ulhs `render_site()` Zeile
1027 schreibt `combined`). Daraus wurde geschlossen, was an der **Anlage** steht. Das ist keine
Messung, sondern eine Kette am Code: die Anlage ist nicht aus dieser Zeile entstanden, oder die
Zeile ist jünger als die Installation. Ein Satz über eine laufende Anlage braucht eine Lesung an der
Anlage, und zwar in der Datei, die dort wirklich gilt.

Die Messung der Leitsitzung („die `LogFormat`-Definitionen sind die Debian-Vorgaben") war ihrerseits
unvollständig: gegrept waren `apache2.conf` und `/etc/ngc/*.conf`, nicht `sites-available/`. Zwei
unvollständige Messungen, die sich gegenseitig bestätigt haben — das ist die Form, in der ein
falscher Befund stabil aussieht.

**Was von dem Fund übrig bleibt, und es bleibt etwas:** ulhs `render_site()` schreibt `combined` und
definiert `ngc` nie. Für die **laufende** Anlage ist das belegt bedeutungslos. Für die **nächste**
Installation aus diesem Zweig ist es eine Falle: sie bekäme das Format, gegen das die Vorlage mit
Begründung entschieden hat. Also kein offenes Geheimnis im Log, sondern ein Mangel, der beim
nächsten Aufsetzen zuschlägt. Nur das Zweite ist belegt. Eigener Punkt, und er liegt im ulh-Zweig
`ngc-web`, nicht an der Anlage.

### c) Beide API-Knoten protokollieren keine einzelne Anfrage — stimmt mit dem Code

Im Code von `apps/server-api` gibt es keine Stelle, die eine Anfrage je Aufruf aufschreibt.
Protokolliert wird mit `log/slog`: Start, Tunnel, Runden, und in `classes/httpapi/busy.go` eine
**gebündelte** Zeile, wenn Anrufer abgewiesen wurden. Kein `os.Hostname`, kein Knotenname in einer
Antwort, kein Kopffeld außer `Cache-Control`, `Retry-After` und `Set-Cookie`.

Dass api-2 keinen Apache hat, ist im Repo nicht nachlesbar und bleibt eine Messung der Anlage. Sie
passt aber zum Aufbau: `apache-apis.conf` schreibt `ngc-web` bzw. der Controller **mit der Rolle
`web`** — nicht die API.

## 3. Was den Befund widerlegen könnte — gesucht und gefunden, aber es trägt nicht

`shared/core/results.go:158`: `core.LogEntry` hat **schon ein Feld `Node`**. Es gibt also eine
Spalte, eine Grammatik (`wire.ValidName`), eine Ansicht („Benutzer & Rechte") und einen Weg, auf
dem ein Knotenname ohne Umbau sichtbar würde.

Es trägt trotzdem nicht, aus zwei gemessenen Gründen
(`apps/server-api/classes/audit/audit.go`):

1. `Node` meint dort einen **anderen** Knoten: bei `Channel` (Zeile 225) und `Task` (Zeile 240)
   steht dort der **Controller**, an den die Aufgabe ging — nicht die API, die geantwortet hat.
2. `Login` (Zeile 189) setzt `Node` **gar nicht**. Genau der Fall, der die Ursachensuche gekostet
   hat, hat das Feld leer.

Die Auskunft gibt es also an keiner Stelle schon. Aber der nächstgelegene Weg ist kürzer als
gedacht — siehe Weg D.

## 4. Die zwei Sicherheitsfragen

### Wer darf erfahren, welcher Knoten geantwortet hat?

Das ist keine Frage des Aufwands, sondern zwei verschiedene Entscheidungen:

- **Ein Kopffeld** steht im Browser **jedes** Nutzers. Es verrät jedem, der eine Anfrage schickt,
  wie viele API-Knoten es gibt, wie sie heißen und auf welchen eine Sitzung gerade fällt. Damit kann
  ein Angreifer einen Knoten gezielt ansprechen, einen Unterschied zwischen zwei Knoten suchen
  (unterschiedliche Fassungen, ein Knoten ohne die gemeinsamen Schlüssel) und messen, wann ein
  Knoten ausfällt. Die innere Aufstellung einer Anlage ist nichts, was nach außen gehört.
- **Eine Zeile im Protokoll des Servers** verrät sie niemandem. Sie liegt in einer Datei, die nur
  `root` liest, und sie verlässt die Maschine nicht.

Nach der Rangfolge ist das Kopffeld damit erledigt, bevor über seine Kosten geredet wird.

### Was darf in einer Zeile je Anfrage stehen?

Eine Zeile je Anfrage ist auch eine Zeile je **Anmeldeversuch**. Darum, nach CLAUDE.md
(„Geheimnisse nie ins Log, auch nicht gekürzt"):

**Drin:** Zeitpunkt; Verfahren und Pfad **ohne** Abfragezeichenkette; Statuskennzahl; Größe der
Antwort; der Name des antwortenden Mitglieds (ein Rechnername des inneren Netzes); die Dauer.

**Nicht drin:** kein Anmeldename, kein Kennwort, kein Einmal-Wert, keine Marke, kein
Sitzungsplätzchen, kein Beweis einer Anmeldung, nichts davon gekürzt, kein `Referer`, keine
Abfragezeichenkette, kein freier Text. Gekürzt ist nicht entschärft: acht Zeichen einer Marke sind
acht Zeichen einer Marke.

Der Preis davon, in einem Satz: eine Anfrage, die nur in ihrer Abfragezeichenkette falsch ist, sieht
in diesem Protokoll wie jede andere auf denselben Pfad aus.

## 5. Die vier Wege

### Weg A — Rufname des Arbeiters im Protokoll des Stellvertreters

Apaches `mod_proxy_balancer` setzt je weitergegebener Anfrage die Umgebungsvariable
`BALANCER_WORKER_NAME`. Im Protokollformat lesbar als `%{BALANCER_WORKER_NAME}e`.

- **Wo eingetragen:** entweder im Rufnamen `ngc` in `apps/web/templates/apache-vhost.conf` (Rolle
  Web), oder als eigenes Protokoll in `apache-apis.conf.tmpl` (Rolle Controller). Die zweite Fassung
  ist die schärfere: `apache-apis.conf` wird in Zeile 656 der Vorlage **innerhalb** des virtuellen
  Rechners eingebunden, und `CustomLog … env=BALANCER_WORKER_NAME` schreibt eine Zeile **nur** für
  Anfragen, die der Stellvertreter weitergegeben hat. Ohne `env=` bekäme jede Anfrage des ganzen
  virtuellen Rechners eine zweite Zeile.
- **Wer liest:** `root` auf der Web-Maschine. Niemand sonst.
- **Kosten:** eine Zeile Vorlage, eine Zeile Skript, eine Zeile Testdaten. Keine Änderung an Go.
  Keine zusätzliche Last außer dem Schreiben der Zeile. Rückbau durch Zurücknehmen einer Zeile.
- **Angreifernutzen:** keiner. Nichts geht nach außen; wer die Datei lesen kann, ist schon `root`.
- **Lücke:** er greift nur, wo ein Stellvertreter steht. **Aber:** jede `/api/`-Anfrage läuft über
  **diesen einen** Stellvertreter, egal welcher Knoten antwortet — dass api-2 keinen Apache hat,
  nimmt diesem Weg darum nichts. Die echte Lücke ist eine **andere**: was die API **selbst** macht,
  ohne dass eine Anfrage von draußen kommt (Runden, Tunnel, Aktualisierungen), steht hier nie.
  Und bei mehreren Web-Maschinen liegt die Antwort je Maschine in einer eigenen Datei.
- **Stabilität:** `apachectl configtest` läuft vor jedem Neuladen (`apache.go`), ein Tippfehler
  erreicht die laufende Anlage also nicht.

### Weg B — Kopffeld, das die API mitgibt

- **Wo eingetragen:** `apps/server-api/classes/httpapi`, ein `w.Header().Set`.
- **Wer liest:** **jeder.** Auch jeder Angreifer.
- **Kosten:** wenige Zeilen Go, ein Neubau, eine Vertragsfrage (Kopffelder sind Außenverhalten).
- **Angreifernutzen:** hoch, siehe oben.
- **Nach der Rangfolge:** abgelehnt. Auch die Spielarten „nur für Angemeldete mit Adminrecht" und
  „nur bei einem Fehler" sind abgelehnt: ein Kopffeld steht im Browser und damit in jedem
  Zwischenspeicher, jedem Werkzeugkasten und jedem Fehlerbericht, den ein Nutzer weitergibt, und
  „nur bei einem Fehler" sagt dem Angreifer gerade dann, wo er hingreifen soll.

### Weg C — Zeile je Anfrage im Journal der API

- **Wo eingetragen:** `apps/server-api`, eine Zwischenschicht um den HTTP-Behandler, mit
  `slog` und dem Knotennamen aus `api.json` (`config.API.Node`, den der Code schon kennt).
- **Wer liest:** `root` auf dem jeweiligen API-Knoten, über `journalctl`.
- **Kosten:** die höchsten der vier. Neue Go-Zeilen in der Anfragestrecke, Tests, und vor allem
  eine **neue Gefahrenquelle**: jede Zeile je Anfrage ist eine Zeile je Anmeldeversuch, und jedes
  Feld darin ist eine Gelegenheit, ein Geheimnis zu verlieren. Dazu Journalplatz und
  Schreiblast auf beiden Knoten.
- **Angreifernutzen:** keiner nach außen. Nach innen: wer das Journal lesen kann, sieht mehr als
  vorher — darum die Liste in Abschnitt 4.
- **Was er zusätzlich kann:** er ist der **einzige** Weg, der auch das sichtbar macht, was die API
  ohne eine Anfrage von draußen tut, und der einzige, der auf dem Knoten selbst liegt und damit auch
  dann noch da ist, wenn die Web-Maschine weg ist.

### Weg D — den Knoten in den Prüfeintrag schreiben

`core.LogEntry.Node` gibt es schon, ebenso die Ansicht dafür. `Login` ließe sich den Namen der
antwortenden API mitgeben.

- **Wo eingetragen:** `apps/server-api/classes/audit/audit.go` — **und** `shared/core`.
- **Wer liest:** Administratoren, in der Anlage, über eine Ansicht, die es schon gibt. Nichts im
  Browser eines fremden Nutzers.
- **Kosten:** klein im Code, aber **gesperrt**: `Node` meint heute den Controller. Dieselbe Spalte
  mit zwei Bedeutungen zu füllen macht jede vorhandene Zeile unlesbar; ein zweites Feld (`api`) ist
  eine Änderung an `shared/` und damit **eine Sache des Koordinators**, nicht dieser Sitzung.
  Diese Sitzung ändert `shared/` nicht.
- **Angreifernutzen:** keiner.
- **Besonderheit:** er ist der einzige Weg, der die Antwort **genau bei dem Ereignis** festhält, das
  die Ursachensuche gekostet hat — dem fehlgeschlagenen Anmeldeversuch — und sie in der Datenbank
  ablegt, die ohnehin über alle Knoten gleich ist. Er braucht keine zweite Datei und keinen
  Protokollabgleich über zwei Maschinen.

## 6. Empfehlung nach der Rangfolge

Der frühere Punkt 1 („`combined` auf der Anlage ersetzen") ist nach 2b **weg**: an der Anlage gibt
es nichts zu ersetzen. Es bleibt ein Punkt im ulh-Zweig `ngc-web`, und er hält diese Reihenfolge
nicht auf.

1. **Weg A, in der Fassung des Controllers** (eigenes Protokoll in `apache-apis.conf.tmpl` mit
   `env=BALANCER_WORKER_NAME`): nichts nach außen, kein Go, kein Vertrag, Rückbau in einer Zeile,
   und `configtest` schützt die laufende Anlage. Er beantwortet die gestellte Frage für jede Anfrage,
   die von draußen kommt. Entwurf in Abschnitt 9.
2. **Weg D als Nachtrag**, mit einem **eigenen** Feld, nicht durch Umdeuten von `Node`. Feldform in
   Abschnitt 10; die Entscheidung und die Änderung an `shared/` liegen beim Koordinator.
3. **Weg C nur**, wenn gemessen wird, dass A und D eine Lücke lassen, die trägt (etwas, was die API
   ohne Anfrage von draußen tut). Er ist der teuerste und der einzige, der eine neue Stelle schafft,
   an der ein Geheimnis verloren gehen kann.
4. **Weg B nicht.**

Zu Thomas' Stand: das Protokollformat des Stellvertreters ist tatsächlich das Billigste und
Ungefährlichste. Der Einwand „es greift nur dort, wo ein Stellvertreter steht, und api-2 hat keinen"
trägt in dieser Form **nicht**: es gibt nur **einen** Stellvertreter, und jede `/api/`-Anfrage läuft
über ihn, gleich welcher Knoten antwortet. Was wirklich fehlt, ist etwas anderes — alles, was die
API ohne Anfrage von draußen tut.

## 7. Die Lesungen an der Anlage

Gefahren von der Leitsitzung am 03.11.2026; diese Sitzung hat keinen eigenen Zugang und nimmt
keinen. Erwartung und Ergebnis je Lesung:

Was hier **nicht** steht, ist genauso wichtig wie was dasteht: je Lesung der durchsuchte Raum. Eine
Aussage ohne ihren Suchraum kann von einer zweiten bestätigt werden, die dieselbe Lücke hat — genau
das ist bei 2b passiert.

| | Erwartung | Gemessen |
|---|---|---|
| a) `connectiontimeout=2` in den `BalancerMember`-Zeilen? | unklar | **nein** — Datei älter als die Vorlage (2a) |
| b) Rollen in `/etc/ngc/controller.json` auf api-1 | `web` | **`roles = ['web']`**, kein Feld `node` |
| c) Rufnamen in `/etc/apache2/` und der benutzte | nur Debian, `combined` | **widerlegt:** `ngc` ist definiert und wird benutzt (2b) |
| d) Apache auf api-2 | nein | **nein** — `inactive`, `not-found` |

Zu b): `api-1` ist der **Maschinenname**, nicht die Rolle. Die Maschine mit dem Stellvertreter trägt
die Rolle `web`, und das deckt sich mit dem Aufbau: `apache-apis.conf` schreibt der Controller mit
Rolle `web`.

Drei von vier bestätigen. Die dritte hat einen Fund dieses Blatts umgedreht, und das ist der
Grund, warum Lesungen an der Anlage nicht durch Lesungen im Repo ersetzbar sind.

**Lesung e), nachgetragen:** `/etc/ngc/apache-apis.conf` auf api-1 hat **21 Zeilen** und trägt
weder `Header always set Retry-After` noch `LogLevel proxy:info proxy_balancer:info` noch
`connectiontimeout`. Erwartung war „eine der beiden Fassungen"; gemessen ist eine **dritte**, die
vor beiden liegt. Siehe 2a.

**Lesung f), die Drehung des Protokolls:** `/etc/logrotate.d/apache2` dreht
`/var/log/apache2/*.log`, und das Neuladen danach ist schon eingetragen. Die Datei aus Weg A wird
also mitgedreht. Geprüft, nicht geglaubt.

## 8. ulh — eigener Punkt

Schritt 3 des Auftrags vermutete, das Muster für `apache-apis.conf` stehe in den ulh-Skripten, eine
Änderung sei also womöglich eine ulh-Änderung, und ulh erreiche beim Start jede Installation.
Gemessen am 03.11.2026 im Repo `Z:\GitHub\ulh`:

- Auf ulhs `main` gibt es **kein einziges** NGC-Skript — `git grep -l apache-apis main` findet
  nichts, und `find` nach `*ngc*` ebenso nicht.
- `ngc-web.sh` liegt nur auf den **nicht zusammengeführten** Zweigen `ngc-web` und
  `ngc-kette-main`.
- Dort erzeugt `render_balancer()` die Datei mit **fest eingetragenen** Zeilen, „byte für byte die
  Vorlage des NGC-Repos" (eigener Kommentar, Zeile 927–932).

Daraus folgt:

- **Heute** ist eine Änderung der Vorlage **keine** ulh-Änderung, die jede Installation erreicht:
  auf `main` ist nichts davon. Die Anlage ist also mit einem Skript von einem dieser Zweige
  aufgesetzt worden — welcher, ist an der Anlage zu klären und nicht hier.
- **Sobald** diese Zweige zusammengeführt werden, erreicht jede Änderung beim Start jede
  Installation, und dann muss die Vorlage an **allen vier** Stellen aus Abschnitt 2a gleich sein.
  Dass sie es heute nicht ist (`.example` weicht ab, und `render_site` schreibt `combined` statt
  `ngc`), ist der Grund, warum das vor dem Zusammenführen geklärt werden sollte.
- Ein PR, der die Vorlage ändert, braucht darum zwei Teile: einen hier (`api`/`controller`-Gebiet)
  und einen im ulh-Repo auf `ngc-web`. Beide gehören dem Koordinator bzw. der Web-Sitzung —
  `apps/web/` und `apps/server-controller/` sind nicht das Gebiet dieser Sitzung.

## 9. Entwurf zu Weg A — je Stelle die genaue Zeile

Diese Sitzung baut das nicht; die vier Stellen gehören den Sitzungen, die diese Teile führen. Hier
steht, was wohin kommt, damit die Leitsitzung es verteilen kann.

### Die Zeile

**Ein Rufname wird nicht vergeben.** `CustomLog` nimmt das Format auch direkt, und ein Rufname ist
serverweit: ihn in `apache-apis.conf` zu definieren hieße, dass zwei Dateien aus zwei Repos ihn
gemeinsam gültig halten müssen. Eine Zeile ohne Rufname hat diese Kopplung nicht.

```apache
# Which member answered. mod_proxy_balancer sets BALANCER_WORKER_NAME on every
# request it hands on, so env= keeps this log to exactly those: without it the
# whole virtual host would get a second line per request. The fields are the
# ones of the "ngc" format in apache-vhost.conf and nothing more - %U is the
# path WITHOUT the query string, and there is no Referer and no Cookie, because
# this file is also written to on every failed login ("Geheimnisse nie ins
# Log", CLAUDE.md). %D is the time it took, in microseconds.
CustomLog ${APACHE_LOG_DIR}/ngc-api-access.log "%h %l %u %t \"%m %U %H\" %>s %O %{BALANCER_WORKER_NAME}e %D" env=BALANCER_WORKER_NAME
```

`${APACHE_LOG_DIR}` ist auf Debian und Ubuntu `/var/log/apache2`; `envvars` setzt es, und die
Vorlage benutzt es in `apache-vhost.conf` schon so. Die Drehung des Protokolls ist geprüft
(Abschnitt 7, Lesung f): `/etc/logrotate.d/apache2` fasst `/var/log/apache2/*.log` und damit diese
Datei mit.

Die beiden Auflagen der Leitsitzung stehen im Kommentar, und sie stehen dort als **Begründung**,
nicht als Hinweis: warum `env=` bleiben muss (sonst eine zweite Zeile für **jede** Anfrage des
ganzen virtuellen Rechners, nicht nur für die weitergegebenen), und warum `%U` und nicht `%r`
(`%U` ist der Pfad **ohne** Abfragezeichenkette, und diese Datei wird bei jedem fehlgeschlagenen
Anmeldeversuch geschrieben). Wer die Zeile später kürzt, soll beides lesen können, bevor er es tut.

### Wohin, je Stelle

| Stelle | Datei | Einfügen | Wessen Gebiet |
|---|---|---|---|
| 1 | `apps/server-controller/templates/apache-apis.conf.tmpl` | **nach Zeile 22** (`LogLevel proxy:info …`), vor Zeile 24 (`ProxyPreserveHost Off`) | Controller |
| 2 | `apps/server-controller/classes/apache/testdata/apache-apis.conf` | **nach Zeile 23**, vor Zeile 25 — gleiche Stelle, die Testdaten müssen die Vorlage spiegeln | Controller |
| 3 | `apps/web/templates/apache-apis.conf.example` | **nach Zeile 23**, vor Zeile 25 | Web |
| 4 | ulh `apps/cli/assets/scripts/ngc-web.sh`, `render_balancer()` | als Eintrag in die `printf '%s\n'`-Liste **nach** `'ProxyPreserveHost Off' \` (Zweig `ngc-web`, Zeile 956) | Web / ulh |

Zu Stelle 4: die Zeile enthält `"` und `${APACHE_LOG_DIR}`. Im Skript steht sie in **einfachen**
Anführungszeichen, dann gibt die Shell nichts davon an. Also:

```sh
'CustomLog ${APACHE_LOG_DIR}/ngc-api-access.log "%h %l %u %t \"%m %U %H\" %>s %O %{BALANCER_WORKER_NAME}e %D" env=BALANCER_WORKER_NAME' \
```

### Was dazugehört, sonst ist es nicht fertig

- **Der Test des Controllers muss rot werden, wenn die Zeile fehlt.** `apache_test.go` vergleicht
  gegen `testdata/apache-apis.conf`; Stelle 2 ohne Stelle 1 ist also rot, und Stelle 1 ohne Stelle 2
  auch. Das ist die Prüfung, die es gibt — und sie deckt die Stellen 3 und 4 **nicht**. Ein Test,
  der `.example`, die Vorlage und `render_balancer()` gegeneinander hält, fehlt; er ist der
  eigentliche Fund aus Abschnitt 2a und gehört in einen eigenen PR.
- **Eine Drehung des Protokolls** (`logrotate`): die neue Datei wächst mit jeder `/api/`-Anfrage.
  `/etc/logrotate.d/apache2` dreht auf Debian `/var/log/apache2/*.log`, das Muster fasst die neue
  Datei also mit. Zu prüfen, nicht zu glauben: eine Lesung an der Anlage.
- **Rückbau:** eine Zeile zurücknehmen, Controller schreibt die Datei neu, `configtest`, Neuladen.

### Was an der laufenden Anlage passiert

Der Controller mit Rolle `web` schreibt `apache-apis.conf` neu, prüft mit `apachectl configtest`
und lädt Apache erst danach neu (`apps/server-controller/classes/apache/apache.go`). Ein Tippfehler
erreicht die laufende Anlage also nicht; was sie erreicht, ist ein Neuladen. **Das ist ein Eingriff
und wird vorher angesagt.** Diese Sitzung macht ihn nicht.

## 10. Feldform zu Weg D — zur Entscheidung des Koordinators

`Node` nicht umdeuten: bei `Channel` und `Task` steht dort der Controller, und eine Spalte mit zwei
Bedeutungen macht jede vorhandene Zeile unlesbar. Also ein eigenes Feld. Vorschlag:

```go
// API is the API node that answered - the node of its api.json
// (config.API.Node), not the controller Node names. Empty in every entry
// written before this field existed, and empty is a legitimate value: an entry
// is never refused because of it.
API string `json:"api,omitempty"`
```

- **Name:** `API` / `api`. Nicht `node2`, nicht `source`: der Leser der Ansicht soll am Namen sehen,
  welcher der beiden Knoten gemeint ist.
- **Grammatik:** `wire.ValidName`, dieselbe wie `Node`. Verletzung → `Dropped`, Eintrag wird
  trotzdem geschrieben; das ist die Regel dieses Pakets und sie gilt auch hier.
- **Wer füllt es:** die API aus ihrer eigenen `api.json`. Der Wert kommt **nicht** von außen, nie
  aus einem Kopffeld und nie aus einer Anfrage — sonst schreibt der Anrufer in den Prüfeintrag.
- **Zuerst bei `Login`** (`audit.go:189`), denn das ist der Fall, der gekostet hat. Danach sinnvoll
  in jedem Eintrag, den die API schreibt.
- **Nichts Geheimes:** ein Knotenname ist ein Rechnername des inneren Netzes, und die Ansicht sehen
  nur Administratoren. Kein Nutzer-Browser bekommt ihn.

### Der Zeilenvorschlag für `shared/core/results.go`

`LogEntry` hat kein `Validate` und kein `Verify` — die Prüfung der Felder liegt in `audit.keep(…)`.
Das Feld kommt darum **hinter** `Node` (Zeile 158) und **vor** `Result`, damit die beiden
Knotenfelder in der Reihe beieinanderstehen und ein Leser den Unterschied an der Stelle sieht:

```go
	Node   string `json:"node,omitempty"`
	// API is the API node that answered - the node of its api.json
	// (config.API.Node). Node above is a different machine: the controller a
	// task or a channel went to. Two fields and not one, because the same
	// column with two meanings makes every row written before unreadable.
	// Empty is a legitimate value: every entry written before this field
	// existed has it empty, and no entry is ever refused because of it.
	API    string `json:"api,omitempty"`
	Result string `json:"result"` // "ok" or a wire.Code
```

Dazu, in derselben Änderung, die Prüfung in `audit.go` neben die vorhandene (Zeile 179):

```go
	keep(&e.API, e.API == "" || wire.ValidName(e.API), Dropped)
```

Mehr ist es in `shared/` nicht. Die acht bzw. neun Stellen darunter folgen daraus.

Die Stellen, die mitziehen müssen — damit niemand die Hälfte baut:

| Stelle | Datei |
|---|---|
| Vertrag | `shared/core/results.go:149` (`LogEntry`) — **Koordinator** |
| Prüfung des Feldes | `apps/server-api/classes/audit/audit.go:179` (`keep(&e.Node, …)`) |
| Füllen | `audit.go:189` (`Login`), dann die weiteren Einstiegspunkte |
| Spalte und Einfügen | `apps/server-db/classes/sqlstore/log.go:24` (`INSERT INTO ngc_log …`) und die Abfrage daneben |
| Wanderung | `apps/server-db/classes/migrate/` — eine neue Spalte in `ngc_log` |
| Gedächtnisspeicher | `shared/dbapi/memstore/memstore.go:344` |
| Prüfsatz der Speicher | `shared/dbapi/dbtest/dbtest.go` (ab Zeile 277) |
| Ansicht | „Benutzer & Rechte" in `apps/web/` — eine Spalte mehr |
| Zuordnung Spalte→Platzhalter | `sqlstore.ColumnType` — **kommt erst mit PR 159** |

**Zur neunten Stelle, und sie ist nachgetragen, nicht selbst gemessen:** die Leitsitzung nennt
`sqlstore.ColumnType`, eine Zuordnung Spalte→Platzhalter samt zwei Prüfungen, die sie gegen die
Wanderungen hält. **Auf `origin/main` (0471b02d, am 03.11.2026 nachgefetcht) gibt es sie nicht** —
`git grep ColumnType origin/main -- apps/server-db/classes/sqlstore` findet nichts, nur
`migrate/lint_test.go` trägt den Wortteil in zwei Testnamen. Sie kommt also mit PR 159. Darum steht
hier keine Zeilennummer: wer Weg D baut, baut auf einem Stand, der PR 159 schon hat, und sucht die
Stelle dort. **Ohne Eintrag wird die neue Spalte rot oder, schlimmer, ungeprüft eingebaut.**

**Eine Frage, die der Koordinator mitentscheiden muss:** soll das Feld auch in `core.LogQuery`
filterbar werden („zeig mir alles, was api-2 geantwortet hat")? Das ist der eigentliche Nutzen bei
einer Ursachensuche, aber es ist eine zweite Vertragsänderung. Dieses Blatt empfiehlt: erst das
Feld, Filtern später, wenn gemessen ist, dass es gebraucht wird.
