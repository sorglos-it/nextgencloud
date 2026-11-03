# Die Setup-Auskunft und die Meldung der ungepaarten Anlage

Entwurf, 03.11.2026. Zweig `api-setup-auskunft`, Basis `origin/main` (`0471b02d`).
Rangfolge dieses Blattes: **Sicherheit > Stabilität > Geschwindigkeit.**

Dieses Blatt hat zwei Teile, und die Reihenfolge ist nicht beliebig:
**Teil A** klärt, was `GET /api/v1/setup` wem antworten darf. **Teil B** gibt der ungepaarten
Anlage eine ehrliche Meldung auf der Seite. Teil B steht hinter Teil A, weil eine ehrliche
Seite wertlos wäre, solange die Auskunft daneben etwas Gefährliches verriete. Die Messung (unten)
zeigt: sie verrät es nicht.

## Was zuerst gemessen wurde (Schritt 1, Zusammenfassung)

Gemessen am 03.11.2026, go1.27.2, ganzes `setup`-Paket 25/25 grün, Ausstieg 0. `ngc-ein` dabei
nicht angefasst, `shared/` nicht geändert.

- **Der Einmal-Code reist nie.** `GET` liefert `SetupOffer{Info, MAC}`: die `SetupInfo` (Installation,
  der eine API-Knoten mit seinen **öffentlichen** Schlüsseln und TLS-Fingerabdruck, `ReleaseKey`,
  der globale Store) und `OfferMAC(code, info)` = HMAC-SHA256 mit dem Code als Schlüssel. Der Code
  selbst steht nicht in der Antwort (`apps/server-api/classes/setup/setup.go`, `offer`).
- **Ein Fremder kommt mit der Auskunft nicht hinein.** `POST` (`take`) verlangt
  `session.VerifySetup`, und das prüft `hmac.Equal(s.MAC, SetupMAC(code, payload))`. Ohne den Code
  kein gültiger MAC.
  - Lebende Gegenprobe (`TestSetupRefusesAForeignVersionAndLocks`): fremder Admin-Schlüssel,
    eigenes v1, ohne Code → **403**, dann **429** (`E_LOCKED`), die Trust-Liste bleibt leer.
  - Positive Kontrolle (`TestSetupTakesVersionOneOnlyOnce`): mit echtem Code → **204**, Admin steht,
    danach antwortet `GET` **404**.
  - Sterbender Mutant (Code-Prüfung in `take()` entfernt, per `-overlay`, `shared/` nicht berührt):
    der fremde Versuch antwortet **204** und die negative Kontrolle wird rot. Also IST der
    code-geschlüsselte MAC der tragende Riegel, nicht nur „auch noch eine Prüfung".
- **Zwei MACs mit getrennten Namen, nicht einer** (fremde Gegenlesung, 03.11.2026,
  `shared/session/session.go`): die Auskunft trägt `OfferMAC` über das Label `"ngc/setup-offer/v1"`,
  das POST verlangt `SetupMAC` über `"ngc/setup/v1"`, und `setupHMAC` hält beide per Label
  auseinander („so a MAC made for one direction never passes for the other"). Der öffentliche MAC
  ist damit nicht einmal **wiederverwendbar**: ein Wiedereinspielen scheitert schon an der Richtung,
  bevor der Code überhaupt gebraucht wird. Zwei verschiedene Aussagen — der Mutant zeigt, dass der
  code-geschlüsselte MAC der Riegel ist; die Labeltrennung zeigt, dass der öffentliche MAC nicht als
  dieser Riegel durchgeht — und zusammen sind sie stärker als jede allein.
- **Die Reihenfolge ist Teil des Riegels** (dieselbe Gegenlesung): `VerifySetup` prüft
  `ValidSetupCode` und `hmac.Equal` als Erstes, bevor irgendetwas entschlüsselt wird. Ein Riegel
  hinter einer Entschlüsselung wäre ein anderer.
- **Der Code ist nicht zu erraten.** Alphabet 32 Zeichen (`A-H J-N P-Z 2-9`), 20 Stellen,
  `crypto/rand`, Abbildung `byte % 32` unverzerrt, weil 256 = 8·32. Macht **genau 100 Bit**. Ein
  HMAC über die öffentliche Auskunft lässt sich damit nicht zurückrechnen und gibt den Code nicht
  her.

  Diese Rechnung steht hier, damit niemand in einem Jahr den Verdacht „der MAC bricht den Code
  offline" neu hat: 2^100 Versuche sind kein Weg.

- **Gepaart sieht anders aus als ungepaart.** `GET` antwortet gepaart **404** (`Serve` prüft zuerst
  die Trust-Liste), ungepaart **200**. Die 200 ist damit ein sichtbares Zustandsmerkmal — der
  leise Befund. An zwei echten Maschinen gemessen (03.11.2026, Nachbarstrang: gepaarte kleine
  Anlage 10.2.3.210, Anker `seq 8`, gegen ngc-ein):

  | Abfrage | gepaart (.210) | ungepaart (ngc-ein) |
  |---|---|---|
  | `/` | 200, 451 Byte | 200, 451 Byte — byte-gleich |
  | `/config.json` | `anchor: {"seq": 8, …}` | kein `anchor`-Schlüssel |
  | `/api/v1/setup` | 404, 66 Byte, `E_NOTFOUND` | 200, 711 Byte, volle Auskunft |

  Also ist die 200 nicht nur aus dem Code gelesen, sondern gemessen ein Zustandsmerkmal.

**Ergebnis:** Der ernste Befund („wer die Auskunft hat, übernimmt die Anlage") ist als
Übernahmeweg widerlegt. Was bleibt, ist der leise Befund (Zustand und Identität sichtbar, geringe
Schwere) und die bereits dokumentierte Lücke um `state.checkGap`, die Schreibrecht an der Datenbank
voraussetzt — eine andere Angreiferlage, nicht Gegenstand dieses Blattes.

### Zur anchorwall: ein Richtungsfehler, berichtigt

Zuerst gemeldet als „der Zweig `feldzug-echter-server` hat die anchorwall entfernt". Nachgemessen
stimmt das nicht: `feldzug-echter-server` ist ein **Vorfahre** von `main` (836 Commits dahinter),
die Datei fehlt dort, weil sie damals noch nicht existierte — sie kam **später** in `main` dazu.
Ein alter Stand sieht wie eine Entfernung aus, wenn man ihn gegen den heutigen hält, ohne nach der
Richtung zu fragen. Prüfbar mit einem Befehl:

```
git merge-base --is-ancestor <stand> origin/main
```

Sagt er „ja", ist `<stand>` älter als `main` und eine fehlende Datei dort ist keine Entfernung.
Ein vermuteter Rückschritt an einer Sicherheitsverteidigung gehört trotzdem gemeldet, nicht selbst
geprüft-und-verschwiegen.

## Teil A — Die Auskunft

**Das `GET`-Angebot ist der nötige Erstlauf-Weg und kein Versehen.** Wer das übersieht, baut es
irgendwann zu und macht damit jede neue Installation unmöglich. Die erste Person holt über genau
dieses Angebot die öffentlichen Schlüssel und die Identität der Anlage, prüft sie mit dem Code
(der `OfferMAC` beweist ihr, dass niemand dazwischen andere Schlüssel unterschiebt), und baut
daraus die erste Trust-Liste. Ohne die Auskunft gibt es keinen Erstlauf.

Die Aufgabe ist darum **nicht** „eine Einladung schließen", sondern: beschreiben, warum die
Auskunft offen sein muss, und was sie trotzdem verrät — und ob das zu verringern ist, ohne den
Erstlauf zu brechen.

Was die Auskunft verrät: dass die Anlage **existiert und noch nicht gepaart ist**, dazu ihre
Identität (Installation, URL, öffentliche Schlüssel des einen Knotens, TLS-Fingerabdruck,
`ReleaseKey`, Store). Die Schlüssel sind **öffentliche** Schlüssel; sie stehen nach der Paarung
ohnehin in der Vertrauensliste, die kein Geheimnis ist. Geheim ist allein der Code, und der ist
nicht dabei.

### Weg A1 — offen lassen (empfohlen)

`GET` antwortet jedem **200**, solange die Anlage ungepaart ist und eine gültige Code-Datei hält.

- **Nutzen:** Der Erstlauf funktioniert von überall, auch wenn die erste Person über das Internet
  kommt. Keine neue Fehlerquelle, nichts, das später jemand versehentlich zubaut.
- **Kosten:** Der leise Befund bleibt — ein Fremder kann einer Adresse ansehen, dass dort eine
  ungepaarte NGC-Anlage steht, und ihre öffentliche Identität lesen.
- **Angreifernutzen:** gering. Er erfährt den Zustand und öffentliche Schlüssel. Hineinkommen kann
  er nicht (Code fehlt, 100 Bit). Der einzige echte Gewinn: er weiß, wo eine Anlage auf ihre
  Paarung wartet, und könnte **anderswo** nach dem Code suchen (nicht durch Rechnen) oder den
  Betreiber zur Eile drängen. Dagegen hilft Teil B, nicht eine geschlossene Auskunft.

Nach der Rangfolge ist A1 tragbar: die **Sicherheit** ist nicht berührt (keine Übernahme), die
**Stabilität** des Erstlaufs bleibt voll erhalten, und der Rest ist eine geringe Preisgabe.

### Weg A2 — Herkunftsgrenze vor das Angebot

`GET` antwortet nur, wenn die Anfrage aus einer vertrauten Herkunft kommt (lokales Netz, eine beim
Start gesetzte Origin-Grenze, oder nur hinter dem eigenen Apache-Proxy des Hosts).

- **Nutzen:** Verbirgt Zustand und Identität vor Fremden aus dem Internet.
- **Kosten:** Kann den Erstlauf **unmöglich** machen, sobald die erste Person legitim über das
  Internet kommt — das ist ein Stabilitäts- und Bedienbarkeitsschaden, der höher wiegt als die
  Preisgabe, die er verhindert. Eine Herkunft (IP, `Origin`-Kopf) ist dazu fälschbar und damit
  kein echter Riegel, sondern eine Hürde.
- **Angreifernutzen:** gering senkt die Sichtbarkeit, hebt die Übernahme-Unmöglichkeit nicht
  (die steht schon).

A2 nur, wenn der Betrieb den Erstlauf ausdrücklich aufs lokale Netz beschränkt. Als harte Grenze
für jeden Erstlauf **abgelehnt**, weil sie den eigentlichen Zweck bricht.

### Weg A3 — Auskunft kürzen (nicht gangbar)

Man könnte die `SetupInfo` auf das Nötigste kürzen und den Rest erst nach `VerifySetup` geben. Das
geht nicht: der Browser baut aus genau diesen Knoten, Schlüsseln und dem Store die erste
Trust-Liste, und der `OfferMAC` siegelt genau diese Bytes. Kürzt man sie, kann der Erstlauf die
Liste nicht bauen und der Code nicht mehr prüfen, dass die Schlüssel echt sind. **Abgelehnt.**

**Empfehlung Teil A:** A1. Die Auskunft bleibt offen, weil sie offen sein muss; der Riegel sitzt
nicht an der Sichtbarkeit, sondern am Code, und der hält. A2 bleibt als Option für einen Betrieb,
der den Erstlauf bewusst aufs lokale Netz legt.

## Teil B — Die Meldung

Heute sieht eine ungepaarte, unbenutzbare Anlage von außen wie eine gesunde aus: die Startseite
ist dieselbe, und wer sich anzumelden versucht, bekommt nur `E_UNREACHABLE`, ohne zu erfahren,
warum. Die ungepaarte Anlage soll **eine sichtbare, ehrliche Aussage** tragen — dort, wo sonst die
Anmeldung stünde.

### Der Riegel, an dem der Bau sich messen muss

Die Aussage darf **nicht** verraten, dass hier ein lebender Einrichtungscode liegt, nicht seinen
Zustand, nicht den Weg dorthin, nicht seit wann, und sie trennt nicht „nie gepaart" von „Paarung
abgebrochen" — wer das trennt, sagt einem Fremden, ob ein Versuch sich lohnt.

**Die Begründung ist nicht „sonst erfährt er es".** Gemessen (Tabelle oben) verrät die ungepaarte
Anlage einem Fremden heute schon mehr: Kennung, Knoten mit `noise`/`sign`/`tls`, Tunneladresse,
`releaseKey`, Laden mit Adresse und Schlüssel — nicht den Code, aber die ganze innere Aufstellung.
Die Begründung lautet darum: **sonst sagt es ihm die Seite auch noch, ohne dass er den Endpunkt
kennen muss.** Die Meldung ist der Weg mit der niedrigsten Schwelle, und genau darum bleibt sie
knapp. Das Bild dazu: **offene Akte, verschlossene Tür** — die Tür ist durch den sterbenden Mutanten
belegt verschlossen (keine Übernahme), die Akte liegt ohnehin offen; die Meldung soll die Akte nicht
noch einmal vorlesen.

**Und eine Berichtigung der Reihenfolge, die beim Bau auffiel:** die Seite war an einer Stelle
**gesprächiger als der Endpunkt**. Die Auskunft am Endpunkt trägt den Code nicht und sagt auch
nicht, wo er liegt; die alte Lead-Zeile der ungepaarten Seite sagte jedem „auf dem Bildschirm des
Servers steht ein Einmal-Code" — und das an der Stelle mit der niedrigsten Schwelle, ohne dass man
einen Endpunkt kennen muss. Nicht „erst der Endpunkt, dann kann die Meldung es nur schlimmer machen",
sondern umgekehrt: die Meldung verriet etwas, was der Endpunkt nicht verrät. Das ist der Satz, der
diesen Umbau rechtfertigt.

Darum sagt die Meldung dem, der davorsteht, genug, und dem Fremden nichts:

> **Diese Anlage ist noch nicht benutzbar. Der Betreiber muss die Einrichtung abschließen.**

Kein Wort von einem Code, von einem Zustand „wartet auf Paarung", von einem Weg, die Einrichtung
selbst zu beenden.

### Wie die Seite es erfährt

Die ungepaarte Lage erfährt die Seite heute schon: `shell/main.js` zeigt die Setup-Maske
(`auth/setup.js`), sobald kein Anker da ist (`anchor === null`). Genau dort, wo sonst die Anmeldung
stünde, gehört der ehrliche Satz hin.

**Offene Entscheidung für den Bau — Erstlauf gegen „Maske nicht zeigen".** Mein erster Entwurf
schrieb, die Maske werde gar nicht gezeigt, nur der Satz. Nachgemessen hält das nicht ohne Weiteres:
der Erstlauf **paart im Browser** über genau diese Maske (`tools/erstlauf-verteilt.sh`:
„first setup of an installation happens in a BROWSER — GET/POST /api/v1/setup"; der neue Admin wird
im Browser unterschrieben). Nimmt man die Maske von der offenen Seite, stirbt die
Erstlauf-Gegenprobe (Auflage 3) — dann wäre keine neue Installation mehr paarbar. Zwei Wege:

- **B1 (empfohlen):** Der ehrliche Satz ist das, was die ungepaarte Seite als Zustand zeigt; das
  Code-Feld des Betreibers bleibt als Erstlauf-Weg erreichbar. Der Satz nennt nur den Zustand, nicht
  den Code — das Feld selbst verrät keinen Code. Erstlauf unverändert.
- **B2:** Die offene Seite zeigt nur den Satz; die Paarung zieht auf einen Weg um, der nicht auf der
  offenen Seite liegt (nur aus vertrauter Herkunft/lokal). Das ist eine größere Änderung über das
  Web hinaus und braucht einen neuen, belegten Erstlauf-Weg, sonst bricht Auflage 3.

**Entschieden von der Leitsitzung (03.11.2026): B1.** B2 bräche Auflage 3 und kaufte fast nichts —
der Zustand ist über `/api/v1/setup` ohnehin für jeden lesbar, der den Endpunkt kennt; die Maske
wegzunehmen verbirgt also nichts, sondern macht nur den Erstlauf unmöglich, bis ein Ersatzweg
gebaut **und** belegt ist. Ein Gewinn, der erst mit einer späteren Lieferung entsteht, ist eine
Schuld, kein Gewinn. Gebaut wird B1: `NGC einrichten` und die heutige Lead-Zeile — die einem
Fremden heute „auf dem Bildschirm des Servers steht ein Einmal-Code" vorliest — weichen dem
Zustandssatz; das Code-Feld bleibt. „Der Betreiber", nicht „Sie": wer davorsteht, ist vielleicht
nicht der Betreiber, und der Satz soll ihm nicht sagen, dass er etwas tun kann.

### Die fünf Sprachen und `BUILT_IN`

Nach Code-Standard liegt jeder sichtbare Text als Schlüssel in `apps/web/public/assets/lang/*.json`
(de, en, fr, it, es). Ein **neuer Schlüssel** `setup.notready` trägt den Satz. Weil ein Release,
das `assets/js/` mitschickt und `assets/lang/` vergisst (oder eine volle Platte bei offener Seite),
die Sprachdateien unlesbar machen kann, muss der Satz auch ohne sie da sein: der englische Wert
kommt zusätzlich in `BUILT_IN` (`apps/web/public/assets/js/shell/lang.js`). Der Wächter dafür ist
`apps/web/tests/shell/busy.spec.js` — er hält `BUILT_IN` und `en.json` key-für-key und wert-für-wert
gleich, so kann keine Hälfte allein abweichen.

Vorschlag der fünf Werte (Schlüssel `setup.notready`):

| Sprache | Text |
|---|---|
| de | Diese Anlage ist noch nicht benutzbar. Der Betreiber muss die Einrichtung abschließen. |
| en | This installation is not ready to use yet. Its operator still has to finish the setup. |
| fr | Cette installation n'est pas encore utilisable. L'exploitant doit terminer la configuration. |
| it | Questa installazione non è ancora utilizzabile. Il gestore deve completare la configurazione. |
| es | Esta instalación todavía no se puede usar. El operador debe finalizar la configuración. |

Der englische Wert steht wortgleich in `en.json` **und** in `BUILT_IN`.

### Wie der Bau sich beweisen muss (Schritt 3, nach Zustimmung)

- **Ein Riegel, ein sterbender Mutant:** eine Abfrage von außen gegen die ungepaarte Seite zeigt,
  dass der Satz da ist und dass er weder den Code noch das Wort „Code", noch den Zustand „wartet
  auf Paarung", noch den Weg zur Einrichtung nennt. Der Mutant, der aus der ehrlichen Meldung eine
  Einladung macht (etwa „Einrichtung abschließen" verlinkt die Setup-Maske), muss den Fall rot
  machen.
- **Lebende Gegenprobe, dass der Erstlauf weiter geht:** der Betreiber holt mit dem Code das
  Angebot, baut v1, paart — die Anlage wird benutzbar. Stirbt diese Gegenprobe, ist die Behebung
  zu weit gegangen und macht jede neue Installation unmöglich.
- `BUILT_IN`-Fall grün: der Satz ist auch ohne `assets/lang/` da (`shell/busy.spec.js`).
- Der Satz kommt, wenn eine Sprache fehlt, auf Englisch zurück, nie als nackter Schlüssel.

## Reihenfolge der Umsetzung

Kleine PRs, je Riegel ein sterbender Mutant, erst nach Zustimmung der Leitsitzung. Vorschlag:

1. Teil B zuerst (Meldung): neuer Schlüssel in fünf Dateien + `BUILT_IN`, Seite zeigt den Satz,
   Wächter. Das schließt den sichtbaren Teil des leisen Befundes, ohne an der Auskunft zu rühren.
2. Teil A bleibt bei A1 — nichts zu bauen, nur dieses Blatt als Begründung, damit niemand die
   Auskunft später zubaut. A2 nur auf ausdrücklichen Wunsch für einen lokal begrenzten Betrieb.
