# Welcher API-Knoten hat geantwortet?

Stand 03.11.2026. Gemessen im Repo auf `origin/main` (0471b02d) und im ulh-Repo; **nicht** an der
Anlage — die drei Messungen der Leitsitzung vom 30./31.10.2026 sind hier nachgelesen, soweit das
Repo sie trägt, und die Lücken stehen unten unter „Was noch an der Anlage zu lesen ist".

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

**Abweichung:** die Leitsitzung nennt `retry=5 timeout=65 keepalive=On upgrade=websocket` — ohne
`connectiontimeout=2`. Entweder wurde es beim Abschreiben weggelassen, oder die Datei an der Anlage
stammt aus einer älteren Fassung. Das ist nachzulesen (siehe Abschnitt 7).

**Eigener Fund, unabhängig von dieser Aufgabe:** `apache-apis.conf.example` behauptet im Kopf, die
Bezugsfassung zu sein, weicht aber von der Vorlage des Controllers ab — `upgrade=websocket` steht
dort am `ProxyPass` statt am `BalancerMember`. Zwei Fassungen derselben Datei, und keine Prüfung
hält sie zusammen. Eigener Punkt, eigener PR.

### b) Kein Protokollformat trägt den Namen des Arbeiters — bestätigt, und die Ursache ist gefunden

Keine der vier Stellen schreibt `LogFormat` oder `CustomLog` in den Lastverteiler. Richtig.

Die Messung sagt weiter, die `LogFormat`-Definitionen seien die Debian-Vorgaben. Das Repo
widerspricht dem auf dem **einen** Weg und bestätigt es auf dem **anderen**:

- `apps/web/templates/apache-vhost.conf:316` definiert ein **eigenes** Format mit dem Rufnamen
  `ngc` und benutzt es in beiden virtuellen Rechnern (Zeilen 337 und 347). Es lässt die
  Abfragezeichenkette, den Verweis (`Referer`) und das Kennwortplätzchen (`Cookie`) absichtlich weg
  — begründet mit „Geheimnisse nie ins Log" aus der CLAUDE.md.
- ulh `ngc-web.sh`, `render_site()`, Zeile 1027, schreibt dagegen
  `CustomLog ${APACHE_LOG_DIR}/ngc-web-access.log combined` — die **Debian-Vorgabe**. Der Rufname
  `ngc` wird dort nie definiert.

Also: die Anlage ist mit ulh aufgesetzt, und darum stehen dort nur die Debian-Formate. Die Messung
der Leitsitzung ist richtig, und der Grund liegt nicht an der Anlage, sondern in der Abweichung
zwischen Skript und Vorlage.

**Das ist ein Sicherheitsfund, kein Nebenbefund:** `combined` protokolliert die vollständige
Abfragezeichenkette und den Verweis. Genau das, wogegen die Vorlage mit Begründung entschieden hat.
Ein Einmal-Code oder eine Rücksetz-Marke in einer Adresse landet damit im Zugriffsprotokoll. Eigener
Punkt, eigener PR, und er ist dringender als die Knotenkennung selbst.

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

1. **Zuerst, und unabhängig von der Knotenfrage: `combined` auf der Anlage ersetzen.** Das ist ein
   Geheimnis-im-Protokoll-Fund, und Sicherheit steht oben. Erst danach lohnt es, dem Format ein Feld
   hinzuzufügen — sonst wird ein Feld in ein Format eingebaut, das selbst ein Fund ist.
2. **Weg A, in der Fassung des Controllers** (eigenes Protokoll in `apache-apis.conf.tmpl` mit
   `env=BALANCER_WORKER_NAME`): nichts nach außen, kein Go, kein Vertrag, Rückbau in einer Zeile,
   und `configtest` schützt die laufende Anlage. Er beantwortet die gestellte Frage für jede Anfrage,
   die von draußen kommt.
3. **Weg D als Nachtrag**, wenn der Koordinator ein eigenes Feld vergibt: er setzt die Antwort genau
   an das Ereignis, das gekostet hat.
4. **Weg C nur**, wenn gemessen wird, dass A und D eine Lücke lassen, die trägt (etwas, was die API
   ohne Anfrage von draußen tut). Er ist der teuerste und der einzige, der eine neue Stelle schafft,
   an der ein Geheimnis verloren gehen kann.
5. **Weg B nicht.**

Zu Thomas' Stand: das Protokollformat des Stellvertreters ist tatsächlich das Billigste und
Ungefährlichste. Der Einwand „es greift nur dort, wo ein Stellvertreter steht, und api-2 hat keinen"
trägt in dieser Form **nicht**: es gibt nur **einen** Stellvertreter, und jede `/api/`-Anfrage läuft
über ihn, gleich welcher Knoten antwortet. Was wirklich fehlt, ist etwas anderes — alles, was die
API ohne Anfrage von draußen tut.

## 7. Was noch an der Anlage zu lesen ist

Diese Sitzung hat keinen eigenen Zugang und nimmt keinen. Zu lesen, je Frage eine Datei:

1. `/etc/ngc/apache-apis.conf` auf der Maschine, die den Stellvertreter trägt: steht in den beiden
   `BalancerMember`-Zeilen `connectiontimeout=2`? (Wenn nein, ist die Datei älter als die Vorlage.)
2. Welche Maschine ist das, und welche Rollen stehen in ihrer `/etc/ngc/controller.json`
   (`roles`)? Die Messung nennt api-1 — nach dem Aufbau wäre das die Rolle `web`.
3. Auf **derselben** Maschine: `grep -rn 'LogFormat\|CustomLog' /etc/apache2/` — welche Rufnamen
   gibt es, und welchen benutzt der virtuelle Rechner, der `/etc/ngc/apache-apis.conf` einbindet?
   Erwartung aus dem Repo: nur die Debian-Formate, benutzt wird `combined`. Das ist die
   Gegenprobe zu Abschnitt 2b, und sie entscheidet, ob Punkt 1 der Empfehlung an der Anlage gilt.
4. Hat api-2 einen Apache (`systemctl status apache2`)? Erwartung: nein.

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
