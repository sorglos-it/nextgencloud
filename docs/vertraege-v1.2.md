# Verträge V1.2 — Sammelliste für den nächsten Schnitt

Stand 24.09.2026. Gepflegt vom Koordinator. `shared/` ist eingefroren auf
`vertraege-v1.1`; alles hier ist aufgelaufen, WEIL es nicht geändert werden
durfte. Diese Liste ist der Auftrag für den Schnitt V1.2 — nichts davon darf
still unter den Tisch fallen.

Herkunft steht je Punkt dabei: Strang und Ledger-Zeile.

---

## Inhaltsverzeichnis (Stand 18.10.2026)

**Achtung, zwei Nummernreihen in einer Datei.** Die Nummern 19 bis 35 sind
**doppelt vergeben**:

- **Reihe A** – die Sammelliste ganz oben, Punkte **1-35** (plus **33b**), nach
  Vertragspaket geordnet.
- **Reihe B** – die Abschnitte **„## Punkt 19"** bis **„## Punkt 62"** darunter,
  nach Datum geordnet.

Jeder Verweis im Text ab „Punkt 19: Die Release-Signatur" meint **Reihe B**,
sofern nicht „dieser Liste" oder „des Vertrags" dabeisteht – dann Reihe A.
**Darum die Überschrift zitieren, nicht die Nummer** (so steht es auch in
`STAND.md`).

Lücken gibt es in keiner der beiden Reihen.

### Stand in Zahlen

| Stand | Reihe A | Reihe B | zusammen |
|---|---|---|---|
| gilt | 35 | 34 | 69 |
| überholt / erledigt durch einen späteren Punkt | 0 | 6 | 6 |
| offen, wartet auf Thomas | 1 | 5 | 6 |
| **Einträge** | **36** | **45** | **81** |

Dazu zwei Abschnitte ohne Nummer (U1, U2) – beide gelten. Zusammen **83**.

- **Überholt:** 36, 38, 47 (teilweise), 50, 56 (teilweise), 60.
- **Offen, wartet auf Thomas:** Reihe A 10 · Reihe B 26, 27, 39, 41, 43.
- **Gilt, aber in einem Teil später berichtigt:** 28, 30, 32, 44, 49, 52, 53.
- **Berichtigt sich selbst im eigenen Text:** 24, 35, 40, 43, U2.

### Reihe A – Sammelliste für den Schnitt V1.2

| Nr. | Überschrift | Stand | Art |
|---|---|---|---|
| 1 | Argon2-Vorgabewerte auf m=19456, t=2, p=1 senken | gilt | Entscheidung |
| 2 | `proof_kdf.json` braucht neue Vektoren | gilt | Messung |
| 3 | `validateKDF` engere Grenzen geben | gilt | Sicherheit |
| 4 | `BlobNewest` oder ein `limit` für `BlobFrom` | gilt | Sicherheit |
| 5 | Zeilengrenze für `AppList` und `BlobFrom` | gilt | Sicherheit |
| 6 | `NewestSeq` ohne Vollscan, Anker vorrücken | gilt – Bedingung für 33b | Sicherheit |
| 7 | Atomarer Zähler für die Anmeldesperre | gilt – **vor** dem zweiten API-Server | Sicherheit |
| 8 | `memstore` hat keine Längenschranken | gilt | Messung |
| 9 | `dbtest` prüft `Seq` nach `BlobGet` nicht | gilt | Messung |
| 10 | Unauthentifiziertes GET-Angebot ist Klartext | **offen, wartet auf Thomas** – Entscheidung in **Punkt 43 (Reihe B)** | Sicherheit |
| 11 | Der Anker gehört außerhalb der Datenbank | gilt | Sicherheit |
| 12 | Kommentar über WebKit in `wire.go` berichtigen | gilt | Messung |
| 13 | Ausgeschriebenes `"b":null` kostet den Tunnel | gilt | Entscheidung |
| 14 | `shared/README.md:229` sagt „Wert" statt „Container" | gilt | Entscheidung |
| 15 | Gemeinsamer Helfer für Dateirechte | gilt | Sicherheit |
| 16 | RAW-Pfad hebt die Grenze einer Liste auf | gilt | Entscheidung |
| 17 | `dbtest` pinnt `Updated` und `Seq` nur halb | gilt | Messung |
| 18 | `dbapi` hat keine verlässliche Zeitquelle | gilt – sichere Seite gewählt | Entscheidung |
| 19 | `UserByName` verrät den Namen über die Zeit | gilt | Sicherheit |
| 20 | Leere DATA-Frames kosten keine Freigabe | gilt | Sicherheit |
| 21 | Der Nonce der Anmeldung soll sich selbst tragen | gilt | Sicherheit |
| 22 | `dbtest` nagelt `UserGet(ctx, "")` nicht fest | gilt | Messung |
| 23 | `dbtest` kann einen falschen Store nicht erkennen | gilt – **eine Hälfte zu** | Messung |
| 24 | Sieben Antworten laufen zwischen den Speichern auseinander | gilt | Messung |
| 25 | Einmalwert hängt nicht nachweislich an `(kind, token)` | gilt | Sicherheit |
| 26 | Streu-Frame: der Code ist strenger als der Vertrag | gilt | Entscheidung |
| 27 | Frames nach dem CLOSE der Gegenseite – Vertrag schweigt | gilt | Entscheidung |
| 28 | Der Vertrag widerspricht sich bei `OSRelease` | gilt | Messung |
| 29 | `AppState` braucht ein fünftes Wort für „weiß niemand" | gilt | Entscheidung |
| 30 | `PasswordToo` sucht den Benutzer ein zweites Mal | gilt | Sicherheit |
| 31 | Eine gültige Anfrage verrät die Anmeldeart | gilt | Sicherheit |
| 32 | Sind die `params`-Bytes kanonisch? | gilt | Entscheidung |
| 33 | Führendes U+FEFF wird verschieden behandelt | gilt – bewusst zurückgestellt | Messung |
| 33b | Wer die Datenbank schreibt, nimmt jede Rücknahme zurück | gilt – **der kritische Punkt** | Sicherheit |
| 34 | `Hello.TrustSeq` ist eine Fenstermarke, keine Quittung | gilt | Entscheidung |
| 35 | Zwei Vertragssätze behaupten eine Prüfung, die es nicht gibt | gilt | Messung |

**Ohne Nummer, zwischen Reihe A und Reihe B:**

| Kürzel | Überschrift | Stand | Art |
|---|---|---|---|
| U1 | `shared/dbapi` – die obere Grenze einer Protokollseite (500) | gilt – Gabelung benannt, nicht geschlossen | Messung |
| U2 | `shared/trust` – Speicher-Obergrenze flattert unter `-race` | gilt – Störung des Messwerkzeugs, kein Loch | Messung |

### Reihe B – Punkte nach Datum

| Nr. | Überschrift | Stand | Art |
|---|---|---|---|
| 19 | Release-Signatur muss einen DIGEST decken | gilt – **vor** dem ersten Release | Sicherheit |
| 20 | Die Lizenz ist eine nicht aufgerufene Funktion | gilt | Entscheidung |
| 21 | Eine Aktion darf `use` heißen | gilt – örtlicher Riegel gebaut | Sicherheit |
| 22 | Diagnoseschalter ohne Weg in die Konfiguration | gilt | Entscheidung |
| 23 | `store.ValidPath` ist POSIX-sicher, nicht Windows-sicher | gilt – → v1.3 **P11** | Umgebung |
| 24 | Ein Abbild ohne Referenz kann niemand entfernen | gilt – enthält eine **Selbstberichtigung**; → v1.3 **P5** | Messung |
| 25 | Adressprüfung des Herunterladens nicht aufrufbar | gilt, **verschärft durch Punkt 45**; → v1.3 **P1** | Sicherheit |
| 26 | Einmal-Code wird vor der MAC-Prüfung verbraucht | **offen, wartet auf Thomas** | Sicherheit |
| 27 | Zwei Server mit demselben Namen verdrängen einander | **offen, wartet auf Thomas** | Entscheidung |
| 28 | Controller startet Migrationen als Programm | Entscheidung gilt – **zwei Begründungen berichtigt** durch „Punkt 29, Nachtrag" | Entscheidung |
| 29 | Jeder Rahmen des Terminals trägt seine Länge | gilt – Zahl nachgereicht im Nachtrag | Entscheidung |
| 29n | **Punkt 29, Nachtrag** – Zahl der App-Kanäle (128) | gilt – **berichtigt Punkt 28** | Entscheidung |
| 30 | Niemand hält die hochgeladene Datei | gilt – die Meldung „erledigt" war **falsch**; am Store-Weg gemessen in **Punkt 45** | Messung |
| 31 | Nichts hält die Rechte des Katalogeintrags gegen das Manifest | gilt – → v1.3 **P12** | Sicherheit |
| 32 | Controller ohne Migrationssperre startet nicht | Entscheidung gilt – offene Hälfte **gemessen in Punkt 44 und 48** | Entscheidung |
| 33 | Rollenprüfung für NGC-Teile auf der falschen Seite | gilt | Sicherheit |
| 34 | `ngc-api` kann heute keine Datenbank öffnen | gilt | Messung |
| 35 | Weitergeleiteter Kanal hat keine Stillzeitgrenze | gilt, BLOCKED – → v1.3 **P8**; enthält eine **Selbstberichtigung** zu `-race` | Messung |
| 36 | Zwei Härtungsschalter fehlen | **überholt durch Punkt 51** – beide gebaut | Sicherheit |
| 37 | Der Plan hat denselben Fehler zweimal vorgegeben | gilt | Messung |
| 38 | Ein Fehler in einem Kanal tötet den ganzen Server | **überholt durch Punkt 55** – gebaut | Entscheidung |
| 39 | Abbild der Terminal-App hängt an einem Tag | **offen, wartet auf Thomas** | Sicherheit |
| 40 | Das Terminal echot | gilt – enthält eine **Selbstberichtigung** | Sicherheit |
| 41 | Terminal kann kein Passwort und kein `vi` | **offen, wartet auf Thomas** | Entscheidung |
| 42 | `ngc.open(...)`-Parameter sind wirkungslos | gilt – **ENTSCHIEDEN** am 12.10.2026 | Entscheidung |
| 43 | Vertrauensanker verriegelt den Weg nach einem Totalverlust | **offen, wartet auf Thomas** – gehört zu **Reihe A, Punkt 10** | Sicherheit |
| 44 | API und Controller haben zum ersten Mal gesprochen | gilt – **berichtigt** die Meldung „Punkt 32 ist gefallen" | Messung |
| 45 | Ein echter Store – Kettenprüfung war überall aus | gilt – **berichtigt rückwirkend jede frühere Store-Messung**; Nebenbefund 1 erledigt durch **Punkt 46** | Messung |
| 46 | Der Rückdreh-Riegel hat jetzt einen Ausgang | gilt – gebaut | Sicherheit |
| 47 | Der kleine Fall – eine Datenbankdatei je Programm | **teilweise überholt**: die Lücke ist **zu seit Punkt 59**; offen bleibt die Entscheidung über die fünf Skripte | Umgebung |
| 48 | Zwei Programme auf einer SQLite-Datei – die Zahl ist vier | gilt – zwei Posten gebaut in **Punkt 49** | Messung |
| 49 | Die Frist gehört zum Zweck | gilt – **berichtigt durch Punkt 54** (P9 ist ein Bruch) | Messung |
| 50 | Der Benutzer hört, sein Passwort sei falsch | **behoben durch Punkt 52 und 53**, am Benutzer gesehen in **Punkt 58** | Messung |
| 51 | Prozessdecke, `nosuid`/`nodev` | gilt – **offen: `noexec` auf `/data`** (Thomas); → v1.3 **P13** | Sicherheit |
| 52 | Die Lüge über das Passwort ist weg | gilt – **berichtigt durch Punkt 54**; offener Posten gebaut in **Punkt 53** | Sicherheit |
| 53 | Der Fehler überlebt `Live` | gilt – **berichtigt durch Punkt 54** | Messung |
| 54 | „Ergänzung, kein Bruch" war falsch | gilt – **berichtigt Punkt 49, 52 und 53**; ordnet die v1.3-Reihe neu | Messung |
| 55 | Punkt 38 ist gebaut – dreizehn Fangstellen | gilt – **erledigt Punkt 38** | Sicherheit |
| 56 | Das kleine System läuft auf echtem Debian | **teilweise überholt** durch **Punkt 57, 59, 61**; offen bleibt der Store-Schlüssel | Umgebung |
| 57 | NGC hat einen Release-Schlüssel | gilt – **offen: Hardware-Stick, Store-Schlüssel** (Thomas) | Umgebung |
| 58 | Die Einrichtungsseite im echten Browser | gilt – die Frage darin ist in **Punkt 59** beantwortet | Umgebung |
| 59 | Angemeldet. Der Schreibtisch steht im Browser | gilt – **schließt die Lücke aus Punkt 47**; die Aussperrung aus Punkt 60 ist in **Punkt 61** behoben | Messung |
| 60 | Der Controller lässt sich nicht koppeln | **ÜBERHOLT durch Punkt 61 – in der Kernaussage falsch** | Messung |
| 61 | Gekoppelt – Punkt 60 war falsch | gilt – **berichtigt Punkt 60** | Messung |
| 62 | Die Anlage entsteht allein aus den Skripten | gilt – bestätigt **Punkt 61** an frischer Anlage | Umgebung |

### Die Widersprüche auf einen Blick

| Berichtigt / überholt | durch | worin |
|---|---|---|
| Punkt 60 | **Punkt 61** | „den Knopf gibt es nicht" – es gibt ihn; gezählt wurde über den falschen Zweig |
| Punkt 49, 52, 53 | **Punkt 54** | P9 (`E_BUSY`) sei „Ergänzung, kein Bruch" – ein alter Leser wirft den ganzen Rahmen weg |
| Punkt 28 (zwei Begründungen) | **Punkt 29, Nachtrag** | eigener SQL-Treiber nötig / `shared/` ändert sich nicht – beides gemessen falsch |
| Punkt 36 | **Punkt 51** | beide Härtungsschalter sind gebaut und gemessen |
| Punkt 38 | **Punkt 55** | je Kanal gefangen, dreizehn Fangstellen |
| Punkt 47 (die Lücke) | **Punkt 59** | ohne `ngc-web` könne sich niemand anmelden – geht jetzt |
| Punkt 50 | **Punkt 52 + 53** | die Falschauskunft über das Passwort ist weg |
| Punkt 30 („erledigt" aus Aufgabe 24) | **Punkt 30 selbst**, 12.10.2026 | die Meldung war falsch, der Punkt gilt unverändert |
| Punkt 24 („nimmt es mit dem Benutzer mit") | **Punkt 24 selbst** | gemessen falsch – das Abbild liegt im Datenverzeichnis |
| Punkt 35 („`-race` nicht messbar") | **Punkt 35, Nachtrag** | der Übersetzer liegt unter `C:\esp\tools\llvm-mingw` |
| Punkt 40 („der Plan hat es falsch gesagt") | **Punkt 40 selbst** | der Plan sagt zum Echo gar nichts |
| Punkt 43 (Antwort B) | **Punkt 43 selbst**, Mutant C | B hebt den Schutz ganz auf und ist keine Antwort |
| Punkt 32 („Annahme gefallen") | **Punkt 44** | zu scharf gemeldet – die offene Hälfte ist nur jetzt gemessen |
| alle Store-Messungen vor dem 13.10. | **Punkt 45** | der Prüfstand hatte die TLS-Kettenprüfung aus |
| Punkt 45 (Rückdreh ohne Ausgang) | **Punkt 46** | es gibt jetzt einen Weg zurück |
| Punkt 56 (fehlender Release-Schlüssel) | **Punkt 57** | Schlüsselpaar da, Riegel gebaut |
| Punkt 56/57 (Anmeldung ungemessen) | **Punkt 59** | im Browser bis zum Schreibtisch |

**Die offenen Entscheidungen für Thomas** stehen gesammelt am Ende dieser Datei.

---

## shared/proof — Passwortschlüssel

1. **Vorgabewerte auf m=19456, t=2, p=1 senken.** Entscheidung von Thomas am
   24.09.2026 („ein Viertel der Arbeit"). Gemessen im Web-Strang: mit den
   Vertragswerten (m=65536, t=3, p=1) braucht die Ableitung 983–1159 ms auf
   chromium und 1906–2171 ms auf webkit, mit den neuen Werten 227–234 ms bzw.
   435–463 ms — 4,3–4,4× schneller. Auf einem Telefon 1,3–2,3 s statt 5–10 s.
   m=19456 ist das Vertragsminimum und weiter OWASP-konform.
   *Kein Zeitdruck:* `deriveSeed` verlangt die Werte ausdrücklich und hat keinen
   Rückfall; die Vorgabe wirkt erst dort, wo ein neues Passwort angelegt wird,
   und das ist noch nicht gebaut. (web, Zeile 38 + 40)

2. **`proof_kdf.json` braucht neue Vektoren.** Heute kommt `p>1` überhaupt nicht
   vor, und weder m noch t sind variiert. Nach dem Schnitt auf m=19456/t=2 läge
   **kein einziger Vektor mehr auf den Vorgabewerten**. Als Zwischendecke prüft
   ein Fall die kopierte Bibliothek gegen den veröffentlichten RFC-9106-Vektor
   (5.3) mit p=4 — beide Engines aufs Byte gleich. Das ersetzt die Vektoren
   nicht. (web, Zeile 42)

3. **`validateKDF` engere Grenzen geben.** Heute bis 1 GiB Speicher und t=10.
   Gemessen: mit der 120-s-Decke im Web läuft m=262144/t=10 auf einem fünfmal
   langsameren Telefon (rund 157 s) in die Frist — der Anwender bekommt einen
   Fehler statt endlosem Warten, aber die eigentliche Abhilfe sind Grenzen, die
   solche Werte gar nicht erst annehmen. (web, Zeile 44)

## shared/dbapi — Datenbankvertrag

4. **`BlobNewest` (höchste vorhandene Nummer) oder ein `limit` für `BlobFrom`.**
   Ohne das greift der Loch-Riegel nur bis 8 gelöschte Fassungen am Stück, und
   ein Abschneiden am oberen Ende bleibt unsichtbar, solange der Anker in der
   Datenbank liegt. (api, Zeile 21)

5. **`AppList` und `BlobFrom` haben keine Zeilengrenze** — auch `memstore` nicht.
   Mit `MaxData` = 80 MiB füllen wenige große Blobs einer Art den Speicher. Die
   Grenze gehört in den Vertrag, nicht in den einzelnen Store. (db, Zeile 39)

6. *(Stand: gilt – **33b** macht daraus die Bedingung dafür, dass der Entzug
   eines Schlüssels überhaupt hält.)* **`state.NewestSeq` ohne Vollscan**, und ein Weg, **den Anker vorzurücken** —
   sonst steht die Fassungsliste bei `MaxVersions` = 4096 endgültig. Der Hebel
   liegt in `dbapi`, nicht im Controller. (controller, Zeile 19)

7. **Ein atomarer Zähler für die Anmeldesperre.** Das Zählen der Fehlversuche ist
   heute prozesslokal (Mutex). Bei einem zweiten API-Server auf derselben
   Datenbank zählt die Sperre wieder zu wenig. **Muss geklärt sein, BEVOR der
   zweite API kommt.** (api, Zeile 37)

8. **`memstore` hat gar keine Längenschranken, `dbtest` prüft zu lange
   Schlüssel nirgends.** Gemessen: `sqlstore.BlobPut` mit einer 256-Byte-ID
   gibt `ErrInvalid`, `memstore` nimmt sie an. Die Spaltung zwischen den
   Speichern fällt deshalb nicht auf. (db, Aufgabe 4, Fund 10)

9. **`dbapi/dbtest` prüft `Seq` nach `BlobGet` nicht.** Deshalb gilt eine Zeile im
   API nur dann als abgelaufen, wenn BEIDE Zeiten (Seq und Updated) es sagen,
   mit eigenem Test gegen einen Speicher ohne Seq. Eine Zeile in `dbtest` gehört
   in den Schnitt. (api, Zeile 49)

17. **`dbtest` pinnt `Updated` nur über `BlobGet` und `Seq` nur über
    `BlobFrom`.** Ein vertragstreuer Speicher darf `Updated` aus `BlobFrom`
    weglassen — gemessen: damit nimmt `lock.Purge` eine Zeile weg, die
    `lock.Check` noch zählt. Für Zeilen, die das Paket selbst schreibt, bleibt
    es dicht; der Vertragstest hält aber weniger, als der Code zusagt.
    (api, Aufgabe 5, Fixrunde 4)

23. *(Stand: gilt – eine Hälfte ist seit 24.09.2026 zu, siehe unten. Verwandt:
    Reihe A, Punkt 35.)* **`dbtest` kann einen falschen Store nicht von einem richtigen
    unterscheiden — gemessen.** Ein absichtlich **zweischrittiges** `OnceUse`
    (lesen, dann bedingungslos schreiben) besteht `once`, `locks`,
    `concurrent once` und `concurrent lock` **alle**, dazu einen eigenen
    Wettlauf mit 256 × 16 Goroutinen. Zwei Gründe: `concurrent once` lässt
    **`OnceCreate`** um die Wette laufen, **nie `OnceUse`** — die Atomarität des
    *Verbrauchens* misst der Vertragstest **überhaupt nicht**. Und SQLite rettet
    den falschen Store: der zweite Leser bekommt beim Hochstufen `BUSY`, die
    Transaktion wird verworfen und wiederholt, der zweite Durchgang sieht
    `used = 1`. **Auf MariaDB/InnoDB wäre derselbe Code falsch** — ein `SELECT`
    in REPEATABLE READ sperrt nichts, beide sähen `used = 0`, beide gewännen.
    **Das ist der wichtigste Punkt dieser Liste:** solange `dbtest` das nicht
    misst, ist „atomar" für jeden künftigen Store eine Zusage ohne Prüfung.
    **Stand 24.09.2026: die eine Hälfte ist geschlossen.** Gemessen:
    zwei Schritte **ohne** Transaktion — die Form, die man tatsächlich falsch
    schreibt — fängt eine Probe im Paket sofort und stabil, 12 von 12 Läufen
    rot, und sie fällt sogar bei einem Verbindungspool von **eins**, weil die
    Zweischrittigkeit selbst die Bedingung ist. Offen bleibt nur noch die Form
    mit zwei Schritten **in** einer Transaktion: dort rettet SQLites
    WAL-Schnappschuss plus `write()`-Retry den falschen Store, und erst
    MariaDB im Lauf zeigt es. Dazu: `dbtest.Run` hängt bis heute **nur an
    `memstore`** — `concurrent once` fasst `sqlstore` überhaupt nicht an.
    (db, Aufgabe 5, Fixrunde 1)

24. **Sieben Antworten laufen zwischen `sqlstore` und `memstore` auseinander**,
    alle einer Klasse: **zu lange Eingaben**. `sqlstore` gibt `ErrInvalid` bzw.
    `ErrNotFound`, `memstore` gibt `nil` bzw. `(true, nil)`. `dbtest` prüft
    zu lange Eingaben nirgends, deshalb fällt es nicht auf. (db, Aufgabe 5)

25. **`dbtest` nagelt nicht fest, dass ein Einmalwert an `(kind, token)` hängt.**
    Gemessen: `onceRead` **und** `onceState` dürfen ihr `kind` verlieren
    (`WHERE ? IS NOT NULL AND token = ?`) und das ganze Paket bleibt grün,
    obwohl der Primärschlüssel beides umfasst — zwei Zeilen dürfen denselben
    Token unter verschiedenen Arten tragen, und nichts legt das je an. Ein
    Store, der die Arten verwechselt, kommt durch den Vertragstest.
    (db, Aufgabe 5, Fixrunde 1)

## shared/session — Einrichtung

10. *(Stand: **offen, wartet auf Thomas**. Die Entscheidung dazu ist
   ausgearbeitet in **Punkt 43 der Überschriftenreihe** – dort sind Antwort A,
   Antwort B und ein dritter Weg gemessen; B ist als keine Antwort belegt.)*
   **Das unauthentifizierte GET-Angebot ist Klartext**: Installations-ID,
   Knotenschlüssel, TLS-Pin, Release-Schlüssel, Store-URL und -Schlüssel. Das ist
   eingefrorene Vertragsgestaltung, kein Fehler im Code — beim Schnitt bewusst
   entscheiden, ob es so bleibt.
   *Bewusst so und kein Fund:* Eine zurückgespielte Datenbank ohne Vertrauens­liste
   öffnet die Einrichtung mit neuem Code wieder — das ist der Weg zurück nach
   einem Totalverlust. (api, Zeile 39)

11. **Der Anker für den API gehört außerhalb der Datenbank.** Solange er darin
    liegt, bleibt ein Abschneiden am oberen Ende unsichtbar (siehe Punkt 4).
    (api, Zeile 21)

18. **`dbapi` hat keine verlässliche Zeitquelle.** `Seq` und `Updated` kommen
    beide vom Aufrufer. Deshalb kann die Anmeldesperre gegen verstellte Uhren nur
    in **eine** Richtung schützen: aus einer Lesung ist „Zeile aus der Zukunft"
    nicht von „Uhr geht nach" zu unterscheiden. Thomas hat am 24.09.2026 die
    sichere Seite gewählt — die Sperre hält, und der Preis ist ein Eimer, der so
    lange zu bleibt, wie eine vorgehende Uhr vorging. **Sauber lösbar nur mit
    einer monotonen Quelle im Vertrag.** (api, Aufgabe 5, nach fünf Fixrunden)

34. **`Hello.TrustSeq` sagt nicht, was sein Name verspricht.** Der eingefrorene
    Vertrag beschreibt das Feld an `tunnel.go:132` nur im Kommentar; Code gibt es
    auf keiner Seite. Der Controller füllt es jetzt, und zwar richtig für seinen
    Zweck: Er nennt die Fassung, **ab der er die Kette braucht** — nach dem
    Pairing ist das Anker minus eins. Ein frisch gepaarter Controller meldet also
    `1`, **ohne je eine Fassung geprüft zu haben**.

    Wer das Feld in einer Oberfläche als „geprüft bis" anzeigt, zeigt eine
    Fassung, die nie geprüft wurde. Beim Schnitt muss im Vertrag stehen, was die
    Zahl ist: **eine Fenstermarke, keine Quittung.** Und die Gegenpflicht
    gehört dazu — die API muss die Kette ab `TrustSeq + 1` liefern, sonst bleibt
    ein gepaarter Controller ausgesperrt. (controller, Aufgabe 10)

## shared/wire — Nachrichten

12. **`shared/wire/wire.go` nimmt an, der Browser hinke immer hinterher. Das
    stimmt nicht.** WebKit ist bei 44 Codepunkten großzügiger als Go: in
    Unicode 17.0 unvergeben, WebKits Tabellen kennen sie. Die Richtung ist die
    sichere (der Server weist ab, was der Browser durchlässt), und kein
    Steuer-, Format- oder Leerzeichen ist darunter — **der Kommentar im Vertrag
    gehört berichtigt**, nicht die Prüfung. (web, Zeile 22)

13. **Ein ausgeschriebenes `"b":null` in einer Steuernachricht weist
    `wire.ParseControl` ab und kostet den Tunnel.** Erzeuger müssen das Feld
    weglassen. Gehört in den Auftrag der Aufgabe, die Steuernachrichten erzeugt —
    und als Satz in den Vertrag. (controller → api)

19. **`trust.List.UserByName` verrät den Namen über die Zeit.** Gemessen auf
    10 000 Benutzern: ein Name **vorn** in der Liste kostet 0 µs, ein Name, den
    es **nicht gibt**, 18 µs — der frühe Ausstieg folgt der Stelle des Namens.
    Der API schreibt sich deshalb sieben eigene Zeilen ohne frühen Ausstieg
    (20,6 gegen 20,6 µs). **Der Vertrag sollte es selbst richtig machen**, sonst
    tut es jeder Aufrufer einzeln — oder vergisst es.
    Dazu: der Kommentar an dieser Funktion behauptet, `session.Challenge`
    benutze sie. Tut sie nicht. (api, Aufgabe 6)

20. **Leere DATA-Frames und EOF kosten keine Freigabe.** Damit kann eine
    Gegenstelle die Warteschlange eines Kanals **umsonst** verlängern. Der
    Controller behilft sich, indem er die Zahl wartender Frames auf das
    begrenzt, was die gegebene Freigabe hätte bezahlen können — das ist eine
    erfundene Regel für eine Vertragslücke und gehört in den Vertrag.
    (controller, Aufgabe 6)

21. **Der Nonce der Anmeldung sollte sich selbst tragen.** Heute schreibt
    `login/start` für einen **bekannten** Namen eine Zeile in die
    Einmalwerte-Tabelle und für einen unbekannten nicht — gemessen 2,20 µs
    Unterschied (9,2 %). Symmetrisch schreiben wäre eine **unbegrenzte,
    unauthentifizierte** Schreiberlaubnis auf eine Tabelle ohne Obergrenze,
    deren `OncePurge` nirgends verdrahtet ist. Sauber: Nonce = Zufall + MAC über
    Benutzer-ID und Ablauf mit `ChallengeKey`; `login/finish` prüft den MAC
    **ohne** Datenbank und legt die Zeile erst dort an, wo jeder Versuch schon
    einen `lock.Fail` kostet. Braucht Platz im Feld → `shared/session`.
    (api, Aufgabe 6)

22. **`dbtest` nagelt `UserGet(ctx, "")` nicht fest.** Geprüft wird nur ein
    Name, den es nicht gibt — nie die **leere** ID. `memstore` gibt dort
    `ErrNotFound`, `UserPut` gibt für eine leere ID aber `ErrInvalid`. Baut ein
    zweiter Store das spiegelbildlich, antwortet die Anmeldung für jeden
    unbekannten Namen anders als für jeden bekannten. Eine Zeile in `dbtest`.
    (api, Aufgabe 6)

26. **Der Vertrag beendet die Leitung für ein Streu-Frame, das eine Nummer zu
    viel machen würde** (`shared/wire/wire.go:45-47`). Seit der Controller für
    **jedes** CLOSE die Nummer zurückgibt, kann ein CLOSE auf eine freie
    Nummer gar keine Nummer mehr zu viel machen — der Code ist damit strenger
    als der Vertrag, den er an dieser Stelle zitiert. Eins von beidem muss
    nachziehen. (controller, Aufgabe 6, Fixrunde 1)

27. **Der Vertrag sagt nicht, was mit Frames nach dem CLOSE der Gegenseite
    passiert.** Der Controller hat entschieden, DATA nach dem CLOSE der API
    anzunehmen und auszuliefern, ohne mit `E_PROTOCOL` zu antworten — und die
    Entscheidung haengt daran, dass man die Klammer in „neither open nor
    closing (never opened, or free again)“ als **abschliessend** liest. Wer sie
    als Beispiel liest, kommt zum Gegenteil. Ein Satz im Vertrag erspart der
    naechsten Umsetzung dieselbe Auslegung. (controller, Aufgabe 6, Fixrunde 2)

28. **Der Vertrag widerspricht sich bei `OSRelease` selbst — gemessen.**
    `wire.ValidName` lässt keinen Punkt zu und weist damit `ubuntu-24.04` ab —
    **genau das Beispiel, das der Vertrag für dieses Feld selbst nennt.** Der
    Controller hat sich deshalb eine eigene Regel genommen und an
    `tunnel.ParseHello` festgenagelt, statt eine zweite Kopie zu pflegen. Eine
    der beiden Stellen muss nachziehen. (controller, Aufgabe 7)

29. **`tunnel.AppState` braucht ein fünftes Wort für „weiss niemand“.** Heute
    hat der Vertrag vier Zustände, und `ParseStatus` beendet die Leitung über
    jeden fünften. Eine Maschine, die nicht antwortet, lässt sich damit gar
    nicht berichten: der Controller lässt die Zeile deshalb weg statt `failed`
    zu schreiben, denn `failed` wäre sonst als einziges der vier Wörter
    doppeldeutig — und ausgerechnet das, auf das der Admin reagieren soll. Der
    Preis: eine unerreichbare App ist nicht von einer zu unterscheiden, die der
    256er-Deckel verschluckt hat. Das fünfte Wort darf erst gesendet werden,
    wenn `speak()` sagt, dass die Gegenstelle mindestens V1.2 spricht — die
    Fassungslogik dafür steht seit Aufgabe 7. (controller, Aufgabe 7)

30. **`trust.List.PasswordToo` sucht den Benutzer ein zweites Mal — gemessen.**
    `trust.go:405-406` nimmt **nicht** den Benutzer, den `KeyOwner` gerade
    zurückgegeben hat, sondern sucht ihn über die ID neu; `session.go:832` ruft
    es genau so. `KeyOwner` läuft linear, `User` binär — eine Liste mit zwei
    Benutzern derselben ID lässt die beiden also auf **verschiedene** Zeilen
    zeigen, **ohne jeden Getter**. `validateList` weist doppelte IDs ab, wer die
    Liste aber von Hand baut, umgeht das. Ein `PasswordToo`, das den gefundenen
    Benutzer nimmt, löscht die zweite Suche und damit die Frage. (web, Aufgabe 8)

31. **Was eine *gültige* Anfrage kostet, verrät die Anmeldeart.** Der API-Strang
    hat vier Runden darauf verwendet, den Preis einer **abgelehnten** Anfrage
    von ihrem Inhalt zu lösen. Bei angenommener Anfrage bleibt er am Schlüssel
    hängen: `proof.Verify` verzweigt nach Schlüsselart — ein `ed25519.Verify`
    beim Passwortschlüssel, die ganze WebAuthn-Assertion beim Passkey
    (`shared/proof/proof.go:234-245`); dazu läuft `KeyOwner` linear über
    `l.Users` × `u.Keys` (`shared/trust/trust.go:390-399`) und `PasswordToo`
    über die Rollen (`:405-416`). Wer die Laufzeit von außen misst, ohne den
    Inhalt zu sehen, unterscheidet damit systematisch, **wie** sich jemand
    anmeldet — eine Aussage über den Benutzer, die dieser Pfad nicht macht.
    Gegen Netzrauschen heute klein (zehner µs), aber der Vertrag sagt dazu
    nichts, und Punkt 30 räumt nur die Hälfte der Ursache weg. (api, Aufgabe 8)

32. **Der Vertrag sagt nicht, ob die `params`-Bytes kanonisch sind — und die
    beiden Seiten entscheiden verschieden.** Gemessen: Gos `json.Marshal`
    **verdichtet** eine `json.RawMessage`, `params "  {}"` wird zu
    `"params":{}`; die Shell schreibt die Bytes wörtlich und liefert
    `"params":  {}`. Beide lesen beides zurück, und für die Shell ist ihr
    Verhalten sogar das einzig richtige, weil sie die **unterschriebenen** Bytes
    exakt nachbauen muss. Genau deshalb muss der Vertrag sagen, welche Form
    unterschrieben wird — sonst hängt die Gültigkeit einer Signatur an der
    Implementierung, die sie erzeugt hat. (web, Aufgabe 9)

33. **Ein führendes U+FEFF in den `params` wird auf den beiden Seiten
    verschieden behandelt — gemessen.** `TextDecoder` verschluckt es: es ist der
    **einzige** von 1 112 064 Code Points, der nicht zurückkommt. Die Shell
    unterschreibt `params` mit BOM also **ohne** ihn, und `params`, die nur ein
    BOM sind, gelten ihr als **gar keine** `params`. Go weist beides ab
    (`shared/wire.ValidParams`). Die Shell macht aus einer Eingabe, die Go
    zurückweist, still eine gültige — und zwar genau in den Bytes, die
    unterschrieben werden.

    Nicht gefährlich, und deshalb zurückgestellt: Prüfung und Unterschrift
    sehen **dieselben** Bytes, es driftet nichts auseinander. Der Preis ist,
    dass ein Aufrufer `params` unterschrieben bekommt, die er so nicht
    übergeben hat. Beide naheliegenden Abhilfen scheiden in der Aufgabe selbst
    aus — eine zweite Lesung der Aufrufer-Bytes ist genau das, was die
    `x.length`-Familie verbietet, und der Decoder liegt in `b64.js`. Die
    Richtung, damit sie niemand neu herleitet:
    `new TextDecoder('utf-8', {ignoreBOM: true})` erhält das BOM, dann weisen
    beide Seiten dasselbe ab. Gehört zum selben Schnitt wie Punkt 32.
    (web, Aufgabe 9)

35. *(Stand: gilt. Verwandt: Reihe A, Punkt 23 und der Abschnitt U1 unten.)*
    **Zwei Sätze des Vertrags behaupten eine Prüfung, die es nicht gibt.**
    `shared/dbapi/dbtest/dbtest.go:2-3` sagt „Every implementation in
    apps/server-db/ has to pass Run, against MariaDB and SQLite", und
    `shared/dbapi/dbapi.go:3` sagt „dbtest checks every implementation".

    Gemessen: **`dbtest.Run` hat genau eine Aufrufstelle im ganzen Baum** —
    `shared/dbapi/memstore/memstore_test.go:12`, gegen `memstore.New()`. Keine
    Datei unter `apps/` importiert `dbtest`; keine Build-Tags, keine
    Umgebungsschalter. **Der Store, der später produktiv läuft, wird vom
    Vertragslauf nicht geprüft.**

    Die Messungen der Umsetzer waren echt — sie haben `dbtest.Run` jede Runde von
    Hand gegen `sqlstore` verdrahtet und gefahren. Nur fährt es im Repo niemand,
    also fängt `go test ./...` einen Vertragsbruch dort **nicht**.

    Beim Schnitt gehört beides entschieden: der Satz auf das zurücknehmen, was
    gilt — **und** `sqlstore` an den Vertragslauf anschließen, denn sonst prüft
    die Zusage dauerhaft den falschen Store. Die drei Kommentare in `sqlstore`
    sagen inzwischen dazu, dass ihre Messung von Hand kam. (db, Aufgabe 6,
    BLOCKED gemeldet)

## shared/README.md und Querschnitt

14. **`shared/README.md:229` sagt „Wert", wo „Container" stehen muss.**

15. **Ein gemeinsamer Helfer für Dateirechte.** Heute legt `OpenDB` keine fest
    (der Schutz kommt aus dem Verzeichnis), und jeder Strang löst es selbst.
    (db, Zeile 12)

16. **Die Falle im Vertragsentwurf benennen:** Ein RAW-Pfad über einer Liste hebt
    deren Grenze auf — so macht es Go. Das gehört als Satz in den Vertrag, nicht
    nur in den Kopf des nächsten Umsetzers. (web, Zeile 18)

## Der eine kritische Punkt

33b. *(Stand: gilt – **der kritische Punkt dieser Reihe**; hängt an Punkt 6
    dieser Reihe. Der Weg zurück nach einem Totalverlust ist Punkt 43 der
    Überschriftenreihe.)*
    **Wer die Datenbank schreiben kann, nimmt jede Rücknahme zurück — und kann die
    Anlage übernehmen. Gebaut, nicht vermutet.**

    Die Vertrauensliste liegt als Folge von Fassungen in der Datenbank. Wer Zeilen
    löschen oder schreiben kann, dreht sie zurück, und jeder in den verlorenen
    Fassungen **entzogene Schlüssel nimmt wieder an**. Gemessen an einer Sitzung,
    nicht an einer Fehlermeldung.

    Eine Prüfung dagegen gab es (`state.checkGap`), sie sah aber nur Löcher bis zu
    acht Fassungen Breite. Die API-Aufgabe 8 hat sie auf **jede** Breite erweitert
    — und dann hat die Prüfung zwei Formen gefunden, die je **eine Anweisung mehr**
    kosten und wieder durchgehen:

    - die Kette **abschneiden** statt ein Loch hineinzuschneiden (oben nichts, also
      ist die Kette zu Ende),
    - den `Seq` der obersten Zeile **senken**. `Seq` ist kein Merkmal des Speichers,
      sondern ein Feld, das der Schreiber setzt; `memstore` legt es wörtlich ab und
      besteht `dbtest` trotzdem vollständig.

    Dazu, derselbe Angreifer: Zeile 1 löschen, und ein **neu gestarteter Knoten
    öffnet die Erstinstallations-Tür wieder** (gemessen: alter Knoten 404, neuer
    Knoten 200 mit frischem Einrichtungsangebot).

    **Der Satz, um den es geht:** gegen einen Schreiber der Datenbank kann keine
    Prüfung halten, die **in** der Datenbank liegt. Nur ein Anker außerhalb kann es.
    Den gibt es als Typ (`trust.Anchor`) und als Datei (`config.LoadAnchor`,
    `trust-anchor.json`) — er wird heute nur nicht vorgerückt. **Das ist Punkt 6
    dieser Liste**, und damit ist Punkt 6 nicht eine Sparmaßnahme, sondern die
    Bedingung dafür, dass der Entzug eines Schlüssels überhaupt hält.

    Bis dahin bleibt es offen, und die Kommentare im Code sagen es so.
    (api, Aufgabe 8)

## U1 · shared/dbapi — die obere Grenze einer Protokollseite

> **Stand: gilt.** Die Gabelung der zwei Speicher ist **benannt und bezahlt,
> nicht geschlossen**. Art: Messung. Verwandt: Reihe A, Punkt 23 und 35.

**Gemessen am 01.10.2026 vom db-Strang (Aufgabe 7), gemeldet als BLOCKED statt
`shared/` anzufassen.** Der Vertrag zieht die Grenze einer Protokollseite bei
500 — aber nur an der API-Grenze (`core.LogQuery.Validate`). Darunter hält sie
niemand, und ein NGC-interner Aufrufer überquert diese Grenze nie.

`apps/server-db` weist jetzt `Limit > 500` mit `dbapi.ErrInvalid` ab (kein
Kappen: eine Abfrage, die 1000 verlangt und 500 bekäme, sähe für den Aufrufer
aus wie „es gibt nur 500"). **Damit ist dieser Store strenger als der Vertrag
ihn beschreibt**, und die zwei Speicher gabeln sich an dieser Grenze. Drei
Zeilen schließen das:

1. **`shared/dbapi/memstore/memstore.go:368`** — `q.Limit > 500` in die Bedingung
   von `LogQuery`. **Die Zahl, kein Verweis auf `sqlstore`**: `shared/` darf nicht
   nach `apps/` zeigen.
2. **`shared/dbapi/dbapi.go:185–187`** — der Satz zählt die `ErrInvalid`-Fälle von
   `LogQuery` auf und **nennt die obere Grenze nicht**. Solange das so ist,
   beschreibt der Vertrag etwas Schwächeres, als die Speicher tun.
3. **`shared/dbapi/dbtest/dbtest.go`, `testLog`** — die Zeile
   `"limit above the page bound": {Limit: 501}`. **Das ist die wichtigste der
   drei**, denn sie löst „zwei Speicher dürfen sich nicht gabeln"
   **strukturell** statt je Speicher nachgebaut. Heute stellt keine Gruppe von
   `dbtest.Run` ein `Limit` über 500, also hält der Vertragssatz die Gabelung
   nicht — er könnte sie nicht einmal sehen.

**Punkt 2 und 3 müssen ZUSAMMEN kommen.** Kommt die `dbtest`-Zeile ohne den Satz
in `dbapi.go`, erzwingt die Vertragsgruppe eine Regel, die der Vertrag nicht
schreibt — und der nächste Speicher scheitert an etwas, das nirgends steht.
(Gemessen und benannt vom Prüfer der Aufgabe 7 am 01.10.2026.)

**Und die Gabelung ist größer als diese eine Grenze.** Gemessen gabeln sich die
zwei Speicher heute an **neun** Stellen allein aus Aufgabe 7: sieben
Längengrenzen in `LogAdd`, `okKey(q.User)`, und `Limit > 500`. Dazu eine
**zehnte**, die niemand nennt und niemand hält: die **leere Seite** ist in
`sqlstore` `nil`, in `memstore` nicht-`nil` — über `json.Marshal` wird daraus
`null` gegen `[]` in `ngc.users/log`. Der Art nach vorbestehend (`UserList`,
`AppList`, `PendingList` genauso), aber sie gehört in diesen Punkt, weil sie
dieselbe Ursache hat: **`dbtest.Run` hält nur, was es fragt.**

**Bis 1 bis 3 da sind, ist die Gabelung bezahlt und benannt**, nicht geschlossen.
Das war der ausdrückliche Preis dafür, `shared/` nicht anzufassen.

Nebenbei an derselben Stelle gefunden, und es gehört zur Form dieses Punktes:
gehalten war nur „einer mehr als die Grenze". `>` zu `>=` zu machen blieb
**grün** — die **größte vom Vertrag erlaubte** Seite wäre abgewiesen worden, ohne
dass ein Test fällt. Wer Punkt 1 baut, stellt **beide** Zahlen: `500` geht durch,
`501` nicht, und dem Vertrag werden dieselben zwei gestellt.

## U2 · shared/trust — eine Speicher-Obergrenze, die unter -race FLATTERT

> **Stand: gilt.** Eine Störung des **Messwerkzeugs**, kein Loch im Code.
> Der Abschnitt berichtigt sich selbst: die erste Fassung sagte „nie grün",
> gemessen ist „kann grün sein, ohne jede Reparatur". Art: Messung.

**Berichtigt am 01.10.2026, nachdem ein Strang meine erste Fassung widerlegt hat.**
Ich hatte geschrieben, der Fall könne unter `-race` **nie** grün sein. Gemessen
ist es schlimmer: er **kann** grün sein, und zwar ohne jede Reparatur.

`shared/trust/trust_test.go:653` hält `trust.Open` gegen ein Budget von `64<<10`
= 65536 zugeteilten Bytes. Zwei Läufe von `go test -race -count=1 ./...` auf
**demselben** Baum:

| Lauf | Ergebnis |
|---|---|
| 1. | **exit 0**, `shared/trust` **ok** (10,625 s), 0 `DATA RACE` |
| 2. | **exit 1**, `FAIL`: `Open: 65736 bytes allocated`, 0 `DATA RACE` |
| Paket allein | FAIL 3 von 3 |
| nur der Fall | FAIL 5 von 5, mit **65640** *und* **65736** Byte |

Drei verschiedene Zahlen über alle Messungen: **65640, 65736, 65928** — gegen ein
festes Budget von 65536. Ohne `-race` jedes Mal grün.

**Die Ursache liegt im Messverfahren, nicht an `-race` allein.** `allocated`
(`trust_test.go:580`) nimmt die Differenz von `runtime.MemStats.TotalAlloc` —
einen **prozessweiten** Zähler. Er zählt alles, was während `f()` irgendwo im
Prozess zuteilt, nicht nur `f()`. Der Renn-Sucher teilt im Hintergrund zu, also
landet Fremdes im gemessenen Fenster. **Warum es flattert, ist nicht gemessen
und wird hier nicht behauptet**; zwei naheliegende Erklärungen sind geprüft und
verworfen (das Paket hat kein `t.Parallel()` und kein `go func` in seinen Tests).

**Was zu tun ist — und das Budget anzuheben bleibt falsch.** Ein Zaun, der für
ein Werkzeug-Artefakt gelockert wird, ist dauerhaft lockerer, und eine neue
Schwelle würde nur die drei Zahlen von heute abdecken. Zwei Wege, der erste ist
der kleinere:

1. Die beiden Zuteilungs-Zusicherungen (`Advance` in Zeile 646, `Open` in Zeile
   653) **überspringen, wenn der Renn-Sucher läuft**, mit dem Grund daneben. Go
   hat dafür keine Abfrage in der Standardbibliothek; idiomatisch ist ein
   Dateipaar im Testpaket:

   ```go
   //go:build race
   package trust_test
   const raceEnabled = true
   ```

   ```go
   //go:build !race
   package trust_test
   const raceEnabled = false
   ```

2. Oder das Verfahren austauschen: `testing.AllocsPerRun` misst wiederholt und
   isoliert besser als eine Differenz auf einem prozessweiten Zähler. Das ist die
   grüßere Änderung und die ehrlichere — der Zaun würde dann auch ohne `-race`
   verlässlicher.

**Und eine Warnung an den, der die Reparatur prüft:** verlass dich **nicht** auf
„einmal grün". Einmal grün ist **ohne** Reparatur gemessen worden. Die tragende
Aussage eines `-race`-Laufs ist `grep -c 'DATA RACE'` = 0, nicht der
Ausstiegscode — in beiden Läufen oben war sie erfüllt.

**Zur Einordnung, damit die Dringlichkeit stimmt:** über alle drei Server-Pläne
gemessen **0 DATA RACE**, 60 Pakete grün. Und die Prüfung hat Zähne: mit
entferntem Schreibschloss meldet sie `WARNING: DATA RACE` in
`noisep.(*Transport).Seal()` (`noisep.go:117`) über `link.go:225`, zwei
Goroutinen im selben `Seal`, exit 1. Dieser Punkt ist also eine Störung des
Messwerkzeugs, kein Loch im Code.

## Nicht vergessen

- Die Zahlen aus Punkt 1 und 3 sind **gemessen**, nicht geschätzt. Wer sie in den
  Vertrag schreibt, schreibt die Messung dazu.
- Punkt 7 ist der einzige mit einer harten Reihenfolge: vor dem zweiten
  API-Server.

## Punkt 19: Die Release-Signatur muss einen DIGEST decken, nicht die Nachricht

> **Stand: gilt.** Art: Sicherheit. Reihenfolge: **vor** dem ersten echten Release.

**Herkunft:** die Schlüssel-Zeremonie am 02./03.10.2026, Thomas' YubiKey
(5.4.3, OpenPGP 3.4), Seriennummer 18241912. Alles unten ist **gemessen**.

Heute signiert `shared/sign` die **rohe Nachricht**:

```go
// shared/sign/sign.go:120
Sig: ed25519.Sign(priv, Message(purpose, payload))
// und :132
!ed25519.Verify(pub, Message(purpose, env.Payload), env.Sig)
```

`Message` ist `"ngc/" + purpose + 0x00 + payload` und damit **beliebig lang**
(für ein Release gemessen 193 Byte).

**Der Schlüssel liegt auf der Karte und kann das nicht signieren.** Zwei
unabhängige Messungen:

1. **Über gpg:** die Hülle einer OpenPGP-Signatur trägt zwar einen rohen
   öffentlichen Schlüssel (32 Byte) und eine rohe Signatur (64 Byte) — die
   richtigen Formen für unseren Vertrag. Aber sie deckt den **OpenPGP-Digest**,
   nicht die Nachricht. Gemessen mit `crypto/ed25519`:
   `Verify(pub, nachricht, sig)` = **false**, `Verify(pub, digest, sig)` =
   **true**.
2. **Unter gpg hindurch, direkt an die Karte** (`SCD SETDATA` + `SCD PKSIGN`):
   die Karte signiert **32 Byte** und **64 Byte** roh und anstandslos — aber
   30 Byte lehnt scdaemon mit `ERR Invalid value` ab. Es gehen also nur Längen,
   die einem bekannten Hash entsprechen. Unsere 193 Byte gehen nie.

**Also, für V1.2:** `sign.Sign`/`sign.Verify` signieren
`sha512(Message(purpose, payload))` statt `Message(purpose, payload)`.

- Der Wert in der Hülle bleibt gleich lang (64 Byte Ed25519), `len(e.Sig) !=
  ed25519.SignatureSize` in Zeile 87 bleibt unverändert richtig.
- Sicherheitlich ist es derselbe Satz: Ed25519 hasht intern ohnehin; eine
  Vorhashung mit SHA-512 ist der übliche Weg für Karten (vgl. Ed25519ph) und
  verliert nichts, solange der Hash kollisionsresistent ist. **Der Preis ist
  nicht Sicherheit, sondern ein Satz mehr Vertrag:** was signiert wird, ist ab
  dann der Digest, und das muss dastehen, damit niemand später die Nachricht
  einsetzt und sich wundert.
- **Betroffen sind alle signierten Dokumente**, nicht nur das Release — der
  Zweck (`purpose`) unterscheidet sie, der Weg ist derselbe.

**Die zwei Alternativen und warum sie verworfen sind:**

- **Direkt per PC/SC-APDU an die Karte**, unter scdaemon hindurch: machbar,
  aber es braucht ein eigenes Werkzeug, und es muss sich mit scdaemon um die
  Karte streiten. Mehr Code für dieselbe Wirkung.
- **Den Schlüssel als Datei halten:** dann signiert alles sofort, aber der
  Schlüssel verlässt die Hardware. Das ist genau die Eigenschaft, für die der
  YubiKey da ist.

**Reihenfolge:** dieser Punkt muss **vor** dem ersten echten Release stehen.
Der Packer ist gebaut (Branch `release-packer`), er packt reproduzierbar, und
der Signierer wartet nur auf diesen einen Vertragssatz.

## Punkt 20: Die Lizenz war nie eine Vertragsfrage — sie ist eine nicht aufgerufene Funktion

> **Stand: gilt.** Art: Entscheidung. `shared/` ändert sich dadurch nicht.

**Entschieden am 03.10.2026. Zwei Stränge haben am selben Tag dieselbe Lücke
gemeldet** — der API-Strang in Aufgabe 13 („Lizenz ist nirgends geprüft",
gemessen: `grep -rn "CodeLicense\|VerifyLicense\|BlobLicense" apps/` ohne Tests
findet nichts) und der Controller-Strang in Aufgabe 14 (die Stelle steht dort
nur als Kommentar, mit Verweis auf Aufgabe 17). Beide haben die Frage als
Vertragsfrage an mich gegeben. **Sie ist keine.**

Gemessen in `shared/` — es ist alles da, und zwar absichtlich zweigeteilt:

| Was | Wo | Für wen |
|---|---|---|
| `verifyLicense` **plus Schlüsselprüfung** | `shared/store/catalog.go:381` | nur die **API** — „refuses everything a controller cannot do" |
| `VerifyLicenseFor(env, st, installation, app)` | `catalog.go:404` | der **Controller**, „as a controller can, without the key" |
| `LicenseOf(l, now)` → `none/valid/grace/expired` | `shared/core/results.go:342` | beide, für `InstalledApp` und `LicenseResult` |
| `AppUnlicensed` | `results.go:24` | die Antwort nach außen |
| `BlobLicense` (die **neueste** Lizenz) | `shared/dbapi/dbapi.go:20` | der Controller hält sie |
| Aktion `license`, `MaxLicenseIn` = 16 KiB | `shared/core/core.go:59/75/159/170` | `ngc.store/api/license`, `ngc.store/controller/license` |

**Also ändert sich an `shared/` nichts.** Der Vertrag ist vollständig, der
Einfrierstand bleibt, wie er ist. Was fehlt, sind **Aufrufe** — und wo sie
hingehören, steht im Entwurf 4.4, Schritte 2 bis 4, und nicht in meinem Kopf:

1. **Die API prüft die Lizenz samt Schlüssel.** Nur sie kann den
   App-Schlüssel mit dem privaten Schlüssel der Installation entschlüsseln;
   läuft ein Abo aus, **gibt sie den Schlüssel nicht mehr heraus.** Das ist die
   Stelle, an der eine abgelaufene Lizenz wirklich greift.
2. **Der Controller prüft Unterschrift, Installation und App selbst**, mit
   `VerifyLicenseFor` — den Schlüssel der Installation prüft er **nicht**, er
   kann es nicht. Nach der Übergangsfrist startet er den Dienst nicht mehr.
3. **Die Rechteprüfung je Kanal trägt die Lizenz als FÜNFTES Stück** neben
   Benutzer, Rolle, App und Aktion (Entwurf Zeile 582). Die Grenze „pro
   Benutzer" (Zeile 332) ist dabei eine **Rechte**-Frage: nur so viele
   Benutzer bekommen „benutzen", wie die Lizenz erlaubt.

**Was heute wirklich passiert, und das ist der Satz, den beide Stränge
richtig aufgeschrieben haben: eine lizenzpflichtige App öffnet für jeden mit
dem Recht.** Das ist kein Loch im Vertrag, sondern eine unfertige Baustelle —
und sie ist jetzt benannt, statt in zwei Berichten zu liegen.

**Für die Aufträge heißt das:**
- Die Rechte-Aufgabe des API-Strangs bleibt, wie sie ist. Die Lizenz kommt
  nicht nachträglich hineingeflickt, sondern mit der Aufgabe, die den Aufrufer
  baut — dort gehört auch der Fall hin, der eine **abgelaufene** Lizenz gegen
  ein vorhandenes Recht stellt.
- Der Controller bekommt seinen Aufruf in Aufgabe 17, wie es dort schon steht.
  Der Kommentar an der Stelle darf nicht sagen, dass geprüft wird — er muss
  sagen, dass **noch nicht** geprüft wird, und auf diesen Punkt zeigen.
- **Niemand baut eine eigene Lizenzprüfung.** Es gibt genau zwei Eingänge, und
  der Unterschied zwischen ihnen ist die Sicherheitsaussage: wer als
  Controller den Schlüssel prüfen könnte, hätte ihn.

## Punkt 21: Eine Aktion darf `use` heißen — und dann ist das Recht dasselbe

> **Stand: gilt.** Art: Sicherheit. Örtlicher Riegel gebaut, der Vertragssatz fehlt.

**Gemessen am 03.10.2026** von der Prüfung der API-Aufgabe 13, und es ist der
erste Fund dieser Sitzung, der eine Änderung in `shared/` **wirklich**
braucht.

Ein Recht hat die Form `<app>/<aktion>`, und `<app>/use` ist das Recht, die
App überhaupt zu sehen. `core.ValidRight` (`shared/core/core.go:319`) erlaubt
als Aktion aber **jeden** gültigen Namen:

```go
ok && wire.ValidAppID(app) && !wire.CoreApp(app) && (action == RightUse || wire.ValidName(action))
```

Nennt ein **Manifest** seine Aktion `use`, fällt das Aktionsrecht mit dem
Sichtbarkeitsrecht zusammen. Gemessen: mit **nur** dem Sichtbarkeitsrecht
antwortet die Prüfung `code=""` — **der Kanal öffnet.**

**Warum das in `shared/` gehört:** `use` ist dort ein **reservierter** Name,
und wer reserviert, muss es auch durchsetzen. Heute reserviert `core`
den Namen und `store` lässt ihn als Manifest-Aktion zu — zwei Hälften
desselben Vertrags, die sich widersprechen. Jede Umsetzung, die das örtlich
abfängt, baut die Prüfung ein zweites Mal.

**Bis zur nächsten Vertragsfassung:** eine Zeile in `rights/check.go` fängt es
ab, mit einem Kommentar, der auf `core.ValidRight` zeigt und sagt, dass sie
weg muss, sobald der Vertrag es hält. Entschieden für den örtlichen Riegel,
weil der Einfrierstand hält und die Lücke heute offen ist; der Preis ist die
doppelte Prüfung.

**Und ein zweiter Satz hängt daran:** die Lizenzprüfung (Punkt 20) darf
**nicht an `<app>/use` hängen**. Es ist dieselbe Zeile, und wer sie dort
aufhängt, macht aus einer Namenskollision eine Lizenzumgehung.

## Punkt 22: Ein Diagnoseschalter ohne Weg in die Konfiguration ist keiner

> **Stand: gilt.** Art: Entscheidung. Braucht ein Feld in `shared/config`.

**Gemessen am 03.10.2026** in der Controller-Aufgabe 14, als Folge einer
Entscheidung, die ich selbst getroffen habe.

Der stderr einer App ging bei **jedem** echten Aufruf ungekürzt ins Protokoll,
und `NGC_PARAMS` ist Benutzereingabe — das verstößt gegen „Geheimnisse nie ins
Log". Entschieden wurde die sichere Fassung: ins Protokoll geht nur, **dass**
es stderr gab und **wie viele Bytes**; den Text bekommt nur, wer ihn
ausdrücklich einschaltet, und **aus ist der Standard**.

Umgesetzt ist das über `CallInput.LogStderr` / `Deps.LogStderr`. Und hier
endet es, denn **`shared/config` ist eingefroren**: es gibt kein Feld, über das
ein Mensch den Schalter umlegen könnte. „Einschalten" heißt damit heute **neu
bauen** — für einen Operator, der um drei Uhr nachts wissen will, warum eine
App nicht startet, ist das keine Antwort.

**Der Schalter bleibt trotzdem so, wie er ist**, und der Grund ist die
Reihenfolge: die unsichere Fassung durfte nicht stehen bleiben, nur weil die
schöne Fassung einen Vertrag braucht. Ein Schalter ohne Weg nach draußen ist
ein halbes Werkzeug; ein Protokoll, das Benutzereingaben mitschreibt, ist ein
Fehler.

**Für die nächste Vertragsfassung:** ein Feld in `shared/config`, das diesen
Schalter trägt — und zwar so, dass sein Standard **aus** ist und ein
eingeschaltetes Protokoll als solches erkennbar bleibt. Wer es baut, prüft
zugleich, ob es weitere Schalter dieser Art gibt, die heute nur im Quelltext
stehen; **gemessen ist bisher nur dieser eine.**

## Punkt 23: `store.ValidPath` ist POSIX-sicher, nicht Windows-sicher — und das ist jetzt gemessen

> **Stand: gilt.** Art: Umgebung. Der fehlende Vertragssatz liegt in der
> v1.3-Reihe als **P11**.

**Gemeldet und belegt am 10.10.2026** aus Aufgabe 15 des Controllers, zweimal
unabhängig (Prüfer und Umsetzer).

`store.ValidPath` weist zurück, was auf einem POSIX-Dateisystem gefährlich ist:
absolute Pfade, `..`, Links, Sparse-Dateien. Was es **nicht** kennt, ist ein
Dateisystem, das **Groß- und Kleinschreibung faltet**. Zwei Namen, die sich nur
darin unterscheiden, sind für `Walk` **zwei** Einträge und für Windows **einer**.

**Nach `CLAUDE.md` ist das kein Produktmangel** — „Windows wird nicht
umgesetzt". Neu und messbar ist die Folge für die **Entwicklung**:

| Dateisystem | Was mit zwei Namen passiert, die sich nur in der Schreibweise unterscheiden |
|---|---|
| faltend (Windows, hier) | die zweite Datei scheitert an `O_EXCL` → **`install.ErrDisk`** |
| case-sensitiv (Linux, Ziel) | beide werden **angenommen**, beide liegen da |

Damit verhält sich dieselbe Eingabe auf Entwicklungs- und Zielplattform
**verschieden**, und der Unterschied ist kein Zufall, sondern eine Lücke in der
Zusage. Der Fall, der das prüft, **rät die Plattform nicht**: er **misst das
Dateisystem** und verlangt auf jeder Seite das Richtige. So läuft er auf beiden
Zielen und sagt auf beiden etwas.

**Was offen bleibt und in die nächste Vertragsfassung gehört:** entweder
`store.ValidPath` lehnt zwei Namen ab, die sich nur in der Schreibweise
unterscheiden (dann ist ein Paket auf jedem Dateisystem gleich), oder der
Vertrag sagt ausdrücklich, dass er das **nicht** zusagt. Beides ist besser als
heute, wo es niemand sagt. **`shared/` ist eingefroren, also ist das hier
notiert und nicht gebaut.**

## Punkt 24: Ein Abbild ohne Referenz kann niemand entfernen

> **Stand: gilt.** Art: Messung. **Enthält eine Selbstberichtigung:** der Satz
> „ein Deinstallieren nimmt es mit dem Benutzer mit" ist gemessen **falsch**.
> Das Referenzfeld liegt in der v1.3-Reihe als **P5**.

**Gemessen am 10.10.2026** in Aufgabe 16 des Controllers, und der Strang hat die
Lücke benannt statt sie mit einer Notlösung zu überdecken.

Verliert eine App bei einem Update ihre Dienste, wird ihr Container entfernt —
**ihr Abbild bleibt liegen.** `Engine.RemoveImage` braucht eine **Referenz**,
und die kommt aus dem tar des Pakets, mit dem das Abbild kam. Ein Update ohne
Dienste bringt kein solches tar, und **niemand schreibt die Referenz auf**:
`dbapi.App` hat kein Feld dafür.

Der naheliegende Ausweg wäre, die Liste aus der Ausgabe von podman zu lesen und
„alles zu entfernen, was drin ist". **Das tut dieses Paket nirgends**, und es
wäre die falsche Richtung: eine Liste, die der Container-Dienst ausgibt, ist
keine Buchführung, sondern eine Momentaufnahme.

**Was es heute kostet, vollständig – und hier stand zuerst etwas Falsches:**
der Satz „ein Deinstallieren nimmt es mit dem Benutzer mit" ist **gemessen
falsch**. Gemessen am 10.10.2026:

| gemessen | Stelle |
|---|---|
| der Benutzer bekommt das **Datenverzeichnis der App** als Zuhause (`--home-dir DataDirOf(app)`) | `user.go:188` |
| podman läuft mit `HOME=u.home`, der Bildspeicher liegt also **in den Daten der App** | `podman.go:438` |
| `userdel` läuft **ohne `-r`** – das Zuhause bleibt stehen | `user.go:258` |
| `DataSize` läuft über `DataDirOf(app)` und **zählt die Abbild-Schichten als Benutzerdaten mit** | `data.go:227` |

Daraus folgt zweierlei, und beides ist schlimmer als „verlorener Platz":

- Das Abbild liegt **im Datenverzeichnis**, nicht irgendwo beim Benutzer. Mit
  `KeepData` beim Deinstallieren bleibt es **für immer** liegen, und keine
  Buchführung weiß mehr davon.
- Die Zahl, die ein Verwalter **vor** dem Löschen liest, ist um
  Größenordnungen daneben: ein Abbild von einem Gigabyte erscheint ihm als ein
  Gigabyte **eigene Daten**. Er entscheidet über das Löschen anhand einer Zahl,
  die etwas anderes zählt, als ihr Name sagt.

Richtig bleibt nur, dass der Zugang nicht offen steht: der Bildspeicher gehört
dem Benutzer der App und ist nur für sie erreichbar.

**Die richtige Lösung ist ein Referenzfeld in `dbapi.App`** — und das ist eine
Änderung im eingefrorenen `shared/`. Darum steht sie hier und ist nicht gebaut.
Wer sie baut, prüft gleich mit, ob dasselbe Feld auch dem Deinstallieren hilft,
statt sich dort auf den Benutzer zu verlassen.

## Punkt 25: Die Adressprüfung des Herunterladens ist von außen nicht aufrufbar

> **Stand: gilt — und seit **Punkt 45** schärfer:** am echten Katalog prüft
> **niemand** Pfad und Query einer absoluten Adresse. Art: Sicherheit.
> In der v1.3-Reihe als **P1**.

**Gemessen am 10.10.2026** in Aufgabe 19 der API; der Strang hat BLOCKED gemeldet
statt sich eine zweite Zeichengrammatik zu bauen. Das war richtig.

Die Grammatik einer unterschriebenen Download-Adresse ist **da und richtig**:
`store.validHTTPSURL` (`shared/store/catalog.go:208`) erlaubt eine Query — Stores
unterschreiben Download-Links — und verbietet das Fragment, Steuerzeichen,
Leerzeichen und `" ' < > \ #`; den Rechnernamen beurteilt sie über
`trust.ValidHost`/`ValidHostPort`, also überall gleich. Sie ist aber
**klein geschrieben** und läuft nur innerhalb von `VerifyCatalog`.

Daraus folgt für jeden Abholer außerhalb von `shared/`: er kann die Adresse, die
er gleich anfragt, **nicht selbst gegen dieselbe Grammatik prüfen**. Es bleiben
zwei Wege, und beide sind schlechter:

- **Darauf bauen, dass `VerifyCatalog` schon geprüft hat.** So macht es
  `appstore.Download` heute (Schema, Länge 512, Rechnername). Das ist keine
  falsche Prüfung, aber eine **Naht ohne Riegel**: wer die Adresse später
  irgendwo anders herbekommt, merkt nichts.
- **Eine eigene Grammatik schreiben.** Dann gibt es zwei, sie laufen
  auseinander, und keiner weiß, welche gilt. `trust.ValidStoreURL` taugt dafür
  nicht: sie verbietet die Query, die ein Store gerade braucht.

**Die richtige Lösung ist ein ausgeführtes `store.ValidDownloadURL`** — dieselbe
Funktion, groß geschrieben, mit eigenen Fällen. Das ist eine Änderung im
eingefrorenen `shared/`, darum steht sie hier und ist nicht gebaut. Wer sie baut,
lässt `validHTTPSURL` nicht daneben stehen, sondern macht die große Fassung zur
einzigen.

## Punkt 26: Der Einmal-Code wird vor der MAC-Prüfung verbraucht

> **Stand: offen, wartet auf Thomas.** Art: Sicherheit. Ungemessen ist, wie oft
> ein Code neu ausgegeben wird. In der v1.3-Reihe als **N1** (kein Vertrag).

**Gemessen am 12.10.2026** in Aufgabe 18 der API und dort als Bedenken gemeldet.
`join.Accept` verbraucht den Einmal-Wert (`OnceUse`), **bevor** es die MAC prüft —
übernommen aus `controllers.Pair`, mit Begründung im Kommentar.

**Was das bedeutet:** wer die Adresse kennt und eine Anfrage mit **falscher** MAC
schickt, verbrennt den wartenden Beitrittscode. Der Angreifer kommt nicht herein,
aber der Admin muss einen neuen Code ausgeben — und beim nächsten Versuch wieder.
Das ist kein Einbruch, sondern ein billiges Zumachen von außen.

Die andere Reihenfolge hat den umgekehrten Preis: wer erst die MAC prüft, lässt
jemanden beliebig oft gegen denselben Code rechnen. Welche Seite schwerer wiegt,
hängt daran, wie lang ein Code gilt und wie oft er neu ausgegeben wird — und das
ist **nicht gemessen**.

Festgehalten, nicht geändert: die Reihenfolge steht an **zwei** Stellen
(`join.Accept` und `controllers.Pair`), und wer sie anfasst, muss beide anfassen,
sonst verhalten sich Beitritt und Paarung verschieden. Wer das entscheidet,
entscheidet es für beide, mit einer Messung, wie oft ein Code neu ausgegeben wird.

## Punkt 27: Zwei Server mit demselben Namen verdrängen einander

> **Stand: offen, wartet auf Thomas.** Art: Entscheidung. Festgehalten, nicht
> gebaut. In der v1.3-Reihe als **N2** (kein Vertrag).

**Gemessen am 12.10.2026** in Aufgabe 18 der API. Eine zweite Beitrittsanfrage für
denselben Knotennamen **überschreibt** die erste (`BlobPut` nach `req.Node`). Der
Admin sieht dann nur die letzte und weiß nicht, dass es eine erste gab.

Im Entwurf ist das nicht geregelt. Zwei Lesarten, beide vertretbar: die zweite
Anfrage ist derselbe Server, der es noch einmal versucht (dann ist Überschreiben
richtig), oder zwei verschiedene Server wollen denselben Namen (dann gehört die
zweite abgewiesen und gemeldet). Unterscheiden lassen sie sich am öffentlichen
Schlüssel — der liegt in der Anfrage.

Festgehalten, nicht gebaut. Wer es baut, entscheidet am Schlüssel, nicht am Namen.

## Punkt 28: Der Controller startet Migrationen als Programm, nicht selbst

> ⚠ **Die Entscheidung gilt — zwei ihrer Begründungen sind gemessen falsch.**
> Die Berichtigung steht unten im Abschnitt **„Punkt 29, Nachtrag → Berichtigung
> vom 12.10.2026"**: der Controller trägt den SQL-Treiber längst, und `shared/`
> ist sehr wohl betroffen. **Die echte Blockade ist die Konfiguration, nicht das
> Entpacken.** Art: Entscheidung. In der v1.3-Reihe als **P7**.

**Entschieden am 12.10.2026 vom Koordinator**, nachdem Aufgabe 21 des Controllers
deswegen BLOCKED gemeldet hat. Der Entwurf führte die Frage selbst als offen
(Entscheidungsprotokoll, Zeile 1049, 23.09.2026: „wie startet der Controller die
Migrationen eines Updates? … wird mit der nächsten Vertragsänderung entschieden").

**Gemessen, worauf die Entscheidung ruht:**
- Die Migrationen sind **keine Dateien zum Entpacken**. Sie liegen als `go:embed`
  **im Programm** (`apps/server-db/sql/embed.go`). Der Controller kann nicht
  auspacken, was in einem Binary steckt — Schritt 4 des Plans beschreibt einen Weg,
  den es nicht gibt.
- Sperre, Ledger und Linter sitzen schon in `apps/server-db/classes/migraterun`,
  und dessen Paketkopf sagt es selbst: „That is the whole of what `ngc-api migrate`
  does — one controller with role db starts it".
- `ngc-api migrate` **gibt es noch nicht**: `apps/server-api/main.go` kennt nur
  `-version` und `-config` und ruft `migraterun` nirgends auf.

**Die Entscheidung: `ngc-api migrate` als Programm.** Der Controller startet es wie
jedes andere Programm; er bekommt **keinen** eigenen Läufer.

Der Grund ist nicht Bequemlichkeit, sondern dass die Gegenrichtung **zwei Wahrheiten
über dasselbe Schema** bedeutet: ein zweiter Läufer im Controller braucht einen
eigenen SQL-Treiber, eine eigene Zeile in `tools/modules.go` — und dann migrieren
zwei Programme eine Datenbank. Das ist genau die Klasse, die im db-Paket als Befund
**W-1** gemessen wurde: zwei Zeilen, die sich gegenseitig stützen und zusammen
nichts halten.

**Daraus folgt, was zu bauen ist:** der Unterbefehl `migrate` in
`apps/server-api/main.go`. Das ist **Aufgabe 24 des API-Plans** („`main.go`
verdrahten"), keine neue Aufgabe. `shared/` ändert sich dadurch **nicht**.
Bis der Unterbefehl steht, hat der Controller kein `migrate.go` — ein Gerüst wäre
schlimmer als nichts, es sieht aus wie eine Zusage.

## Punkt 29: Jeder Rahmen des Terminals trägt seine Länge

> **Stand: gilt.** Art: Entscheidung. Die hier offen gelassene Zahl („wie viele
> Terminals gleichzeitig") ist im Abschnitt **„Punkt 29, Nachtrag"** auf **128**
> App-Kanäle gesetzt.

**Gebaut und gemessen am 12.10.2026** in Aufgabe 23 des Controllers, als **Abweichung**
vom Plan, und **mit der Sitzung Webinterface abzustimmen**, weil das Gegenstück dort sitzt.

Das Protokoll des virtuellen Terminals ist: **1 Byte Art, 2 Byte Länge, Nutzlast.**

Der Grund ist gemessen, nicht gewählt: `serve.shellInput` reicht die inneren Nachrichten
als **einen Bytestrom** weiter. Damit ist die Nachrichtengrenze weg — und ein Tastenrahmen
ohne Länge hat kein Ende. Ein Parser, der das Ende raten muss, lässt sich aus dem Takt
bringen: wer die Tasten schickt, bestimmt, wo der nächste Rahmen anfängt. `FuzzParseFrame`
hat 13 365 806 Läufe ohne Fund, und der Fuzzer prüft das **Ergebnis**, nicht nur, dass
nichts abstürzt.

Zwei Zahlen daneben sind **Entscheidungen**, im Code so benannt: Tasten höchstens 16 KiB,
Fenster 1 bis 2000 Spalten/Zeilen.

Dazu eine dritte Entscheidung, wo der Plan keine Zahl nannte: **Leerlaufgrenze 30 Minuten.**
Stream-Aktionen haben laut Vertrag keine Zeitgrenze — und diese Shell läuft als root.
Beide Seiten der Grenze sind mit einer Handuhr festgenagelt.

**Was noch keine Zahl hat:** wie viele Terminals gleichzeitig. Gezählt, nicht gemessen:
je Kanal 65 497 + 16 387 Byte Puffer, ein PTY-Paar, vier Goroutinen, ein Containerprozess
— bei 1024 Kanälen etwa 80 MiB. Eine Obergrenze setzt dieses Paket nicht; sie gehört in
`serve` und in den Vertrag.

## Punkt 30: Niemand hält die hochgeladene Datei zwischen Hochladen und Aktivieren

> **Stand: gilt unverändert.** Art: Messung. ⚠ Die Meldung der Aufgabe 24, dieser
> Punkt sei **erledigt**, ist gemessen **falsch** (siehe Unterabschnitt). Am
> Store-Weg nachgemessen in **Punkt 45**: das Store-Paket trägt, der
> **Hochlade-Weg nicht**.

**Gefunden am 12.10.2026** von Aufgabe 22 der API, beim Bauen, nicht beim Lesen.

`ngc.store/upload` behält das Paket **nicht** — es behält nur die ui-tar. `shared/` hat
keinen Platz für ein Paket. `Activate` bekommt darum `ActivateRequest.File`/`Size`: die
Datei gehört dem **Aufrufer**.

**Wo sie zwischen `upload` und `activate` liegt, ist nicht entschieden und nicht gebaut.**
Das gehört zur Verdrahtung (Aufgabe 24 des API-Plans) und ist keine Änderung in `shared/`.

Zwei Dinge, die bei dieser Entscheidung gemessen gehören und es nicht sind:
- **Mehrere API-Server teilen die Datenbank, nicht die Platte.** Ein Zwischenlager auf der
  Platte des einen Servers ist für den anderen nicht da.
- **Was nach einem Absturz im Zwischenlager liegen bleibt.** Aufgabe 22 legt das Paket für
  das Senden in ein Zwischenlager (`Stores.Spool`) statt es zu strömen — ein Strom liest
  sich einmal, dann wäre vor dem Senden **keine** Prüfung mehr möglich, und in den
  Arbeitsspeicher darf ein 4-GiB-Paket nicht. Jeder Fall dort prüft, dass nichts liegen
  bleibt; für das Zwischenlager **zwischen zwei Aufrufen** gibt es diese Prüfung noch nicht.

### Bestätigt am 12.10.2026, und eine Behauptung dazu war falsch

Aufgabe 24 der API hatte gemeldet, dieser Punkt sei erledigt: die Shell schicke die Datei auf dem
Aktivierungskanal mit, womit sich beide Messpunkte auflösten. **Das ist gemessen falsch**, und der
Satz stand so in `server.go`, bis ein weiterer Strang ihn gegen den Code gelesen hat:
`core.Action.MaxIn` und `core.ShellOpens` sagen das Gegenteil, und `appstore.Upload` behält nur
die ui-tar.

**Also gilt dieser Punkt unverändert — und die Folge ist jetzt gemessen statt vermutet:** eine
Aktivierung aus `core.LocalStore` endet mit **`E_UNREACHABLE`**. Eine **hochgeladene App lässt
sich heute nicht aktivieren**, und zwar nicht aus Versehen, sondern weil niemand das Paket
zwischen den zwei Aufrufen hält.

Festgenagelt ist es mit einem Fall, der einen **anderen** Fehlercode als Kontrolle erwartet —
damit die Zeile rot wird, sobald jemand das Halten baut, statt still weiter zu behaupten, es gehe
nicht.

**Nicht still gefüllt, und das war richtig:** wo die Datei liegt, ist keine Entscheidung eines
Handlers. Die drei Fragen dahinter (Platte, Absturz, zweiter Server) sind oben benannt und bleiben
offen.

## Punkt 31: Nichts hält die Rechte des Katalogeintrags gegen das Manifest

> **Stand: gilt.** Art: Sicherheit. Heute keine Lücke, solange niemand die
> Katalogkopie als Grundlage nimmt. In der v1.3-Reihe als **P12**.

**Gemessen am 12.10.2026** vom Strang, der das App-Einrichten beim Update gebaut hat — und
zwar nicht als Nebenbefund, sondern als **Grund, einen vorgeschlagenen Weg zu verwerfen**.

`store.Package.Needs` (die Rechte im **Katalogeintrag**) wird an genau einer Stelle angefasst:
`shared/store/catalog.go:260`, und dort nur durch `validNeeds` — also auf **Form** geprüft.
**Nichts** hält diesen Eintrag gegen `store.Manifest.Needs`, die Rechte im **Paket**: weder
`VerifyCatalog`, noch `store.Walk`, noch `install.Accept`.

Gebaut wird der Container aber aus dem **Manifest**.

Daraus folgt der Satz, der zählt: **eine Rechteprüfung gegen die Katalogkopie wäre eine Wand
vor einer offenen Tür.** Verlangt das Manifest `root` und sagt der Eintrag nichts, wird `root`
durchgewinkt — und die Prüfung, die davor steht, meldet nichts, weil sie die falsche Kopie
liest.

**Darum wurde der Weg verworfen**, bei dem `state.Staged` die Rechte aus dem Katalogeintrag
mitträgt. Gebaut ist statt dessen: der Aufrufer liest die Rechte, und `install.Update` glaubt
seiner Lesung **nicht** — es vergleicht sie gegen das Manifest (`sameSet`, sonst `ErrVerify`)
und baut die Zeile aus den **Manifest**-Rechten, nie aus den übergebenen. Ein Wert, eine
Quelle.

Das ist dieselbe Klasse wie der Befund **W-1** des Datenbankpakets: zwei Zeilen, die sich
gegenseitig stützen und zusammen nichts halten. Hier ist sie vor dem Bauen erkannt worden,
nicht danach.

**Was offen bleibt:** ob `VerifyCatalog` die beiden Rechtelisten künftig selbst
gegeneinanderhalten soll. Das wäre eine Änderung in eingefrorenem `shared/` und gehört in die
Reihe des Vorschlags für v1.3 — mit dem Hinweis, dass es **heute keine Lücke ist**, solange
niemand die Katalogkopie als Grundlage einer Rechteentscheidung nimmt. Wer das tut, muss es
zuerst hier lesen.

## Punkt 32: Ein Controller, der die Migrationssperre nicht bekommt, startet nicht — und soll es nicht

> **Stand: die Entscheidung gilt.** Art: Entscheidung. Die hier als ungemessen
> benannte Hälfte ist inzwischen **gemessen** — **Punkt 44** (die Sperre hält zwei
> Programme nicht auseinander) und **Punkt 48** (zwei Schreiber sind sicher, nur
> langsam; die Zahl ist vier).

**Gemessen am 12.10.2026** in Aufgabe 24 des Controllers, mit einem **echten zweiten Prozess**
(`os.Args[0]` neu ausgeführt), nicht mit einer Attrappe:

- Nacheinander (Neustart): der Controller kommt hoch, die Zeilen sind da.
- Sperre von einem anderen Eigentümer gehalten: **abgewiesen, er kommt nicht hoch.**
- Acht Öffnungen gleichzeitig, **30 Runden**: 20× alle acht, 4× sieben, 2× sechs, 2× fünf,
  2× vier. Jede, die hochkam, war **sofort lesbar**. In **10 von 30** Runden traf es also
  mindestens einen von acht.

Die Sperre läuft nach **900 Sekunden** von selbst ab, und ihre Meldung sagt das auch.

**Die offene Frage war:** soll `state.Open` auf die Sperre warten, oder soll der Dienst
neu gestartet werden?

**Entschieden (Koordinator): `state.Open` wartet nicht.** Es wird abgewiesen, der Controller
startet nicht, und der Neustart ist Sache des Dienstes (`Restart=on-failure` mit Abstand).
Drei Gründe, in dieser Reihenfolge:

1. **Ein Controller, der beim Start hängt, ist schlimmer als einer, der nicht startet.** Wer
   wartet, sieht einen Dienst, der „läuft" und nichts tut, und die Wartezeit wäre bis zu
   900 Sekunden. Ein Fehlstart mit einer Zeile, die den Grund nennt, ist für den Admin die
   ehrlichere Lage — und die Meldung nennt die 900 Sekunden.
2. **Der häufige Fall ist ein überlappender Neustart**, nicht zwei Server. Dafür ist ein
   Neustart mit Abstand genau das richtige Werkzeug, und es ist eines, das der Admin schon
   kennt und abschalten kann.
3. **Warten im Programm wäre eine zweite Wiederholungslogik** neben der des Dienstes. Zwei
   Stellen, die dasselbe tun, ist die Klasse, die hier schon einmal Geld gekostet hat.

**Was daraus zu bauen ist, und von wem:** die Dienstdatei braucht den Neustart mit Abstand.
Das gehört zu **Aufgabe 25 des Controller-Plans** (ulh-Skript und Abnahme), nicht in den
Programmcode. Wer sie schreibt, nimmt den Abstand **größer als die Sperrzeit nicht** — ein
Neustart alle paar Sekunden ist richtig, denn die Sperre fällt, sobald der andere Lauf fertig
ist, und nicht erst nach 900 Sekunden.

**Diese Hälfte war die wichtigere, und sie ist seit dem 13.10.2026 gemessen (Punkt 44):** die
Sperre gilt **nur für den Augenblick der Migration**, nicht für die Lebenszeit des Stores — und
**sie hält zwei Programme nicht auseinander.** Gemessen am laufenden Programm: ein zweiter
`ngc-api` auf derselben Datei kommt **durch** `dbopen.Open` und stirbt erst am Port
(`bind: address already in use`); zwei auf verschiedenen Ports würden die Datei beide öffnen. Und
`ngc-api migrate` läuft, **während** eine API dieselbe Datei offen hält (`0 Migrationen
eingespielt.`, Code 0). Die Entscheidung oben bleibt davon unberührt — sie betrifft den **Start**
—, aber wer zwei Programme auf eine Datei lässt, kann sich auf die Sperre **nicht** verlassen; die
Antwort des Datenbankpakets darauf ist `busyTimeout` von 10 Sekunden plus Wiederholung, und **die
ist weiter ungemessen.**

## Punkt 29, Nachtrag: die Zahl der App-Kanäle — und eine Berichtigung zu Punkt 28

> **Stand: gilt.** Art: Entscheidung. Setzt die in **Punkt 29** offen gelassene
> Zahl auf **128** App-Kanäle je Server. ⚠ **Der Unterabschnitt darunter
> berichtigt Punkt 28** in zwei Begründungen; die Entscheidung von Punkt 28
> bleibt.

Aufgabe 24 des Controllers hat die Grenze gesetzt, die Punkt 29 verlangt hat:
**`serve.DefaultMaxApps = 128`** App-Kanäle je Server, gebucht **vor** dem E2E-Handschlag,
Abweisung mit `E_LIMIT`, mit Zählern `AppChannels()` und `AppRefusals()`.

Die Rechnung dahinter, als Entscheidung benannt: je Kanal höchstens 256 KiB DATA, 64 KiB
stderr, bis 65 514 Byte Nutzlast, ein Rohrpaar und ein Containerprozess (beim Terminal
zusätzlich ein PTY-Paar) — also etwa **50 MiB und 128 Prozesse** im Höchstfall.

Es ist ausdrücklich **nicht** 1024 (`MaxTunnelChannels` gilt je Tunnel) und **nicht** 64
(`MaxSessionChannels` ist Sache der API). Ebenfalls benannt und nicht gemessen: 60 s, 1 h,
24 h und 10 s Gnadenfrist.

**Keine Grenze hat die Zahl der Tunnel** — sie ist die Zahl der APIs in `controller.json`
und wird von `config` begrenzt.

### Berichtigung vom 12.10.2026: die Entscheidung steht, zwei meiner Begründungen waren falsch

Ein Messauftrag hat Punkt 28 nachgeprüft. **Die Entscheidung bleibt** — `ngc-api migrate` als
Programm, kein eigener Läufer im Controller. **Zwei Sätze meiner Begründung sind gemessen
falsch**, und sie standen hier so, als wären sie Tatsachen:

**Falsch war: „ein zweiter Läufer im Controller braucht einen eigenen SQL-Treiber".**
`apps/server-controller/classes/state/open.go` importiert `apps/server-db/classes/sqlite`,
`sqlite/store.go` ruft `migrate.Apply`, und `migrate` importiert `apps/server-db/sql`. Der
Controller **trägt** den SQLite-Treiber, den XTS-VFS, das Paket `migrate` und jede eingebettete
`.sql` schon heute. Technisch fehlte für einen eigenen Läufer nur noch **eine** der fünf Zeilen
in `tools/modules.go` und etwa zwanzig Zeilen Code.

**Falsch war auch: „`shared/` ändert sich dadurch nicht".** `ngc-api migrate` geht über
`config.LoadAPI` → `(*API).Validate` und verlangt eine **vollständige** `api.json`: Knoten-ID,
Tunnel-Adresse, Listen-Adresse, zwei Zertifikatspfade, vier Schlüssel zu je 32 Byte, Release- und
Store-Schlüssel, Store-Adresse, zwei Laufzeiten. Ein reiner Datenbankknoten hat das nicht — er hat
`controller.json` mit einem Feld `Migrate`. **Es gibt keinen Weg, `ngc-api migrate` nur eine
`config.DB` zu geben.**

**Das ist die echte Blockade von Aufgabe 21** — nicht das Entpacken, wie ich geschrieben hatte.

**Warum die Entscheidung trotzdem bleibt:** der Grund ist nicht der Treiber, sondern dass
`migrate.List()` in zwei Programmen **zwei Übersetzungen desselben `go:embed`** wäre. Zwei
Programme, die eine Datenbank migrieren, sind die Form von W-1. Das gilt unverändert.

**Drei gemessene Lücken auf dem beschlossenen Weg**, die vor dem Bauen zu schließen sind:
1. Die bereitgelegte Datei ist **`0600`** (`update/stage.go`, ausdrücklich so gewollt) — nicht
   ausführbar.
2. Die Konfiguration oben: ohne einen Weg, nur eine `config.DB` zu übergeben, geht es nicht.
3. **`config.Controller.Migrate` wird von keinem Programm gelesen** — nur von `Validate` und
   `LogValue`. Ein totes Vertragsfeld.

Und ein Risiko, das benannt gehört: die Prüfsumme gegen das unterschriebene Release sitzt in
`install.go` (`sameFile`), **nicht** im Bereitlegen. Wer die bereitgelegte Datei startet, führt
sie **vor** dieser Prüfung aus.

**Zwei Dinge, die der Weg (b) umdrehen würde** und die darum gegen ihn sprechen, beide gemessen:
`migrate.Apply` **wartet nicht** — der zweite Läufer bekommt sofort einen Fehler, also startet der
zweite API-Server nicht (dieselbe Form, die Punkt 32 mit 10 von 30 Runden gemessen hat). Und
`shared/config` gibt die `ngc_migrate`-Zugangsdaten **nur** der Rolle `db`; bei (b) bräuchte sie
jeder API-Server.

**Ein Riegel, der dabei gefunden wurde und den ungemessenen Punkt der db-Abschlussprüfung
schließt:** dass die **alte** API während einer Wanderung weiterlaufen darf, ist kein Versprechen,
sondern gebaut — `migrate/lint.go` verbietet dreizehn Wörter (darunter DROP, RENAME, TRUNCATE),
erlaubt vier Formen, und später hinzugefügte Spalten **müssen** nullable sein. `migrate.List()`
prüft das **zur Laufzeit** bei jedem `Apply`. Auf der SQL-Ebene ist der Punkt damit zu; was der
Linter nicht deckt, ist die **Bedeutung** einer neuen Spalte.

## Punkt 33: Die Rollenprüfung für NGC-Teile sitzt auf der falschen Seite der Leitung

> **Stand: gilt.** Art: Sicherheit. Zu bauen in `apps/`, ohne Vertragsänderung.

**Gemessen am 12.10.2026** und von mir selbst nachgeprüft: in
`apps/server-controller/classes/update/install.go` kommt das Wort `Roles` **kein einziges Mal**
vor, und `update.Deps` hat kein solches Feld.

Daraus folgt: **ein `install ngc-api` auf einem Datenbankknoten würde dort nach `/usr/lib/ngc`
kopieren und `systemctl restart ngc-api` fahren** — auf einer Maschine, die kein API-Server ist.

Dass das nicht passiert, hängt **allein** an `rollout.go` auf der API-Seite, die nur dem richtigen
Knoten etwas schickt. Das ist eine Wand auf der falschen Seite der Leitung: wer dem Controller
etwas anderes schickt, wird nicht abgewiesen, sondern bedient.

Der Controller soll nicht darauf bauen, dass die Gegenseite richtig fragt. **Die Prüfung gehört an
den Einbau**, in `apps/`, ohne Änderung in `shared/` — und sie braucht einen Fall, der zeigt, dass
ein Teil für eine fremde Rolle abgewiesen wird, mit einer Zwischenprüfung, dass derselbe Weg für
die eigene Rolle durchgeht.

## Punkt 34: `ngc-api` kann heute überhaupt keine Datenbank öffnen

> **Stand: gilt.** Art: Messung. Der schärfste Fall von „gebaut ist nicht gelaufen".

**Gemessen am 12.10.2026**, von mir nachgeprüft: `classes/server/server.go` ruft `dbopen.Open`,
aber **kein Produktionscode ruft `dbopen.Register` oder `RegisterMigrator`** — nur Tests tun das
(`git grep "dbopen.Register" api -- apps/` ohne Testdateien: **kein Treffer**).

Das heißt: `ngc-api` ist verdrahtet, gebaut, gemessen — und könnte auf einem Server **nicht
starten**, weil niemand eine Umsetzung anmeldet. Das ist der schärfste Fall von „gebaut ist nicht
gelaufen", den dieses Projekt bisher hat, und er ist kein Fehler eines Strangs: jede Aufgabe hat
ihren Teil richtig gebaut, und die Anmeldung war niemandes Auftrag.

Daraus folgt auch, dass `apps/server-db` **ohnehin** in `ngc-api` hinein muss, damit es die API
sein kann — und damit kostet `RegisterMigrator` danach keine zusätzliche Abhängigkeit mehr. Eine
Formanpassung braucht es: `migraterun.Run(ctx, cfg, owner, now) (int, error)` gegen
`dbopen.Migrator func(ctx, cfg, io.Writer) error`.

## Punkt 35: Ein weitergeleiteter Kanal hat keine Stillzeitgrenze

> **Stand: gilt, BLOCKED.** Art: Messung. In der v1.3-Reihe als **P8** — und **P8**
> hängt seit **Punkt 54** an **P10**, weil ein neuer Schließgrund ein **Bruch**
> ist, keine Ergänzung. **Enthält eine Selbstberichtigung:** der Satz „`-race` ist
> auf dieser Maschine nicht messbar" war falsch.

**Gemessen am 12.10.2026**, nachdem die Zusage aus Aufgabe 17 („je Kanal eine Goroutine, Zahl
gleichzeitiger nicht gemessen") nachgezählt wurde.

**Was in Ordnung ist, und zwar gezählt statt behauptet:** bei 128 gleichzeitig mit Fehler
beendeten Kanälen steht die Goroutinenzahl **vorher 2, Spitze 130, nachher 2** — über 30 Läufe
identisch. Also genau **eine** Goroutine je drainendem Kanal, **und sie fällt wieder: kein Leck.**
Die Frist wird genau eingehalten (höchste Überschreitung über 60 Messungen: 0,67 ms), und bei
**abgebrochenem Kontext endet das Warten sofort**, nicht erst nach Ablauf. Der Mutant, der
`defer cancel()` entfernt, stirbt genau am Leck: „peak 258, after 130 … 128 did not come back".

**Was nicht in Ordnung ist:** ein weitergeleiteter Kanal hat **keine Grenze für seine Stillzeit.**
Keine Frist in `hop.Next`/`sh.Next`, der einzige Kontext ist der des Aufrufers, und `link.Link`
sagt selbst, nach dem ersten Bild begrenze nichts die Stille. `controllers.IdleTimeout` hängt am
**Tunnel**, nicht am Kanal; im `browser`-Paket prüft nichts `Expires` oder `SessionLifetime`.

**Wer wie lange hält:** ein **angemeldeter** Benutzer, 64 Kanäle je Sitzung, 1024 je Tunnel,
**so lange der Browser den Socket offen hält** — ohne Zeitgrenze, und die Zahl der Sitzungen je
Benutzer ist nicht begrenzt. Gerechnet: je Kanal 2 Goroutinen à 8 KiB plus `DefaultWindow`
256 KiB, also je Tunnel **etwa 256 MiB und 2048 Goroutinen**, zeitlich unbegrenzt gehalten.
Zum Vergleich: Punkt 29 begrenzt die App-Kanäle des Controllers auf 128 mit etwa 50 MiB.

**Vorschlag, nicht gebaut: 30 Minuten Stillzeit je weitergeleitetem Kanal, dann `CLOSE
E_TIMEOUT`.** Die Zahl ist nicht gewählt, sondern abgeleitet — der Controller beendet sein
Terminal nach `terminal.IdleLimit = 30 Minuten`; kürzer schlösse Kanäle, die er noch für lebend
hält. Sie unterscheidet sich darum bewusst von den 10, 25 und 50 Sekunden an anderen Stellen.

**Warum es BLOCKED ist:** der Abschnitt „Relay" in `shared/wire/wire.go` nennt keine Stillzeit,
und die Shell müsste **dieselbe** Zahl kennen — sonst schließt der eine, was der andere für offen
hält. Das ist eine Vertragsänderung und gehört in die Reihe des Vorschlags für v1.3.

**Eine Grenze für die Anzahl wurde ausdrücklich NICHT gebaut**, und das ist die zweite Hälfte der
Messung: sie ist schon da und greift (`hop.Open` → `ErrLimit` → `E_LIMIT`, mit vorhandenem Fall).
Eine zweite, kleinere Zahl in `relay` wären zwei Stellen für dieselbe Sache — genau der Grund,
aus dem Punkt 32 das Warten im Programm abgelehnt hat.

**Nachtrag 12.10.2026:** der Satz „`-race` ist auf dieser Maschine nicht messbar" stand hier und
war **falsch** — der Übersetzer liegt unter `C:\esp\tools\llvm-mingw\bin\gcc.exe`, und
`-race` läuft (`relay` und `link`: 0 DATA RACE). Die Nebenläufigkeit dieses Pakets ist damit
**nachmessbar und nachzumessen**; gezählt wurden bisher nur Goroutinen, nicht Zugriffsfolgen.

## Punkt 36: Zwei Härtungsschalter fehlen — Entscheidung offen — ERLEDIGT durch Punkt 51

> ⚠ **Überholt durch Punkt 51.** Beide Schalter sind dort gebaut und gemessen:
> `--pids-limit 512` (die Zahl ist gemessen, nicht geraten) und `nosuid,nodev` auf
> beiden Einhängungen. **Neu offen aus Punkt 51: `noexec` auf `/data`** — diesen
> Schalter nennt Punkt 36 gar nicht. Art: Sicherheit.
> Der Text unten bleibt als Messung von damals stehen.

**Gefunden am 12.10.2026**, als die Härtung des App-Containers endlich messbar gemacht wurde.
Die Vorgeschichte gehört dazu, weil sie erklärt, wie die Lücke so lange bleiben konnte:

**Die Härtung war von keinem einzigen Fall gehalten.** Die Abschlussprüfung hat drei Mutanten
gefahren und alle drei **lebten** bei grünem Satz: `--cap-drop ALL` und
`--security-opt no-new-privileges` entfernt, `--network none` und `--read-only` entfernt, und jedes
`0o700` auf `0o777` gesetzt. Das ist die **einzige Wand zwischen einer App und dem Server**.

Die Ursache war eine stille Abschaltung: die Attrappe für podman ist ein `/bin/sh`-Skript und
überspringt sich außerhalb von Linux — damit fielen genau die Fälle weg, die podmans
Argumentliste lesen. Behoben, ohne Linux: die Argumentliste wird gebaut und verglichen, ohne
podman zu starten. **Jetzt tötet jeder der neun Schalter einen Mutanten**, darunter zwei, die
vorher niemand als ungemessen bemerkt hatte (`--user 0:0` und das Hold-Volume als `:ro`).

**Dabei sind zwei Schalter aufgefallen, die ganz fehlen** — gelesen, nicht gemessen, weil es ohne
podman nicht zu zeigen ist:

1. **Keine Grenze für die Prozesszahl (`--pids-limit`).** Eine Fork-Bombe in einer App ist nur
   durch podmans Vorgabe von 2048 begrenzt — also durch eine Entscheidung, die **nicht im Repo
   steht** und sich mit einer podman-Fassung ändern kann.
2. **Keine Einhängeoptionen auf `/data` (`nosuid`, `nodev`).** `no-new-privileges` fängt den
   suid-Weg ab, aber eine zweite Lage fehlt, und sie kostet nichts.

**Nicht eingebaut, und das war richtig:** beides sind Sicherheitsentscheidungen, und eine Zahl für
`--pids-limit` zu setzen heißt, eine Obergrenze für jede App festzulegen. Das gehört Thomas.

**Was dabei offen bleibt und offen bleiben muss:** ob podman die Schalter **einhält**, prüft hier
nichts. Gemessen ist, dass wir sie **schicken** — und das ist die Hälfte, die wir messen können.
Die andere Hälfte braucht einen Linux-Server mit podman und einen echten Ausbruchsversuch; der
entsprechende Fall überspringt sich laut.

## Punkt 37: Der Plan hat denselben Fehler zweimal vorgegeben

> **Stand: gilt.** Art: Messung. Eine Lehre über Vorlagen, kein offener Posten.

**Festgehalten am 12.10.2026**, nachdem der kritische Befund K-1 der API behoben war.

Zweimal hat ein Strang einen **Entwurfs-Codeblock des Plans** gelesen, ihn gegen seinen eigenen
Kommentar gehalten und festgestellt, dass der Block das Gegenteil dessen tut, was darüber steht —
und zwar beide Male **dieselbe** Sache: er sammelt die ganze Eingabe im Arbeitsspeicher.

- **Aufgabe 20** fand es in `Upload`: der Block las mit `io.ReadAll` bis zu 4 GiB, während sein
  Kommentar sagte, die Bytes gingen „nur im Puffer des Aufrufers" zur Platte. Gemessen: 128 MiB
  Eingabe hielten 268 MB. Der Strang hat den Block **nicht übernommen** und einen Durchlauf
  gebaut — 0 MiB gehalten bei 32, 128 und 512 MiB.
- **Die Abschlussprüfung** fand dasselbe eine Schicht höher, im Dispatcher: `ReadInput` sammelte
  in einem `bytes.Buffer` bis `MaxIn`. Gemessen über den echten Kanal: 64 MiB Eingabe hielten
  **191 MiB**, linear mit der Eingabe. Behoben, jetzt **3 MiB** — und `ReadInput` ist entfernt,
  nicht als zweite Fassung stehen gelassen.

**Die Lehre ist nicht „der Plan hat einen Fehler", sondern wie er sich fortpflanzt.** Aufgabe 20
hat ihren Teil richtig gelöst und gemessen; der Durchlauf endete nur **vor** dem Dispatcher, wo
jemand anders schon alles eingesammelt hatte. Zwei richtige Lösungen hintereinander ergeben keine
richtige Kette, solange niemand die Kette als ganze misst.

**Darum stehen beide Plan-Stellen jetzt berichtigt da** (Zeile 6988 und der `ReadInput`-Block
darunter), mit der Zahl und mit dem Satz, dass wer den Block abschreibt, den Befund wieder
einbaut. Ein Plan ist in diesem Projekt kein Vorschlag, sondern eine Vorlage — und eine Vorlage
mit einem falschen Kommentar erzeugt den Fehler so oft, wie jemand sie liest.

**Was daraus für jede weitere Aufgabe folgt:** einen Entwurfs-Codeblock des Plans **gegen seinen
eigenen Kommentar lesen**, bevor man ihn übernimmt. Das hat heute zweimal einen Befund gefunden,
den sonst erst ein echter Server gefunden hätte — beim zweiten Mal einen, der den Prozess
umbringt.

## Punkt 38: Ein Fehler in einem Kanal tötet den ganzen Server — Entscheidung offen — ENTSCHIEDEN, siehe Punkt 55

> ⚠ **Überholt durch Punkt 55 — die Entscheidung ist getroffen und gebaut.**
> Gewählt wurde der Vorschlag „je Kanal fangen": dreizehn Fangstellen, `E_INTERNAL`
> am Kanal, Zähler und Minutenbericht gegen das Verschlucken. Art: Entscheidung.
> Der Text unten bleibt als Abwägung von damals stehen.

**Gemessen am 12.10.2026** in Aufgabe 30 der API, gefunden von einem **Fuzz-Samen**, nicht von
einem Mutanten: `coreapp/testdata/fuzz/FuzzOpenParams/4b950b6123cdb67b`. **Der Samen muss im Repo
bleiben.**

Der Einzelfall: `ngc.users/pair-code` und `join-code` lasen `Deps.Controllers`/`Deps.Join` ohne
Prüfung. Ein Shell-Kanal auf `join-code` stürzt mit `nil pointer dereference` ab. **Behoben** im
Handler, Antwort `E_NOTFOUND` — und im laufenden Programm war er nicht erreichbar, weil
`server.Build` beide Felder setzt. `Deps` sagt die Regel sogar selbst („may be nil → register NO
handler"); `registerUsers` hielt sie nicht.

**Der eigentliche Befund steckt dahinter, und den habe ich selbst nachgemessen:**
`browser.(*Server).channel` startet die Kanalarbeit **in einer eigenen Goroutine ohne `recover`**.
Und `recover()` kommt im **gesamten Produktionscode** von `apps/server-api`,
`apps/server-controller` und `shared` **nicht ein einziges Mal** vor — nur in zwei Testdateien
(`git grep -c "recover()"`).

**Daraus folgt: jeder Programmfehler in irgendeinem Handler tötet den ganzen `ngc-api`** — mit
**jeder** Sitzung jedes Benutzers, nicht nur der eigenen. Ein einzelner Kanal nimmt den Server mit.

**Das ist eine Entscheidung, keine Behebung, und sie gehört Thomas.** Beide Seiten sind
vertretbar und beide haben einen Preis:

- **Nichts fangen (wie heute).** Das ist die übliche Haltung in Go: ein Programmfehler ist ein
  Programmfehler, und ein Dienst, der weiterläuft und falsch antwortet, ist schlimmer als einer,
  der neu startet. Der Preis: ein Fehler in einer Nebensächlichkeit wirft alle Benutzer heraus,
  und auf einem Server ohne Neustarthilfe bleibt er unten.
- **Je Kanal fangen.** Der Kanal endet mit `E_INTERNAL`, die anderen Sitzungen merken nichts.
  Der Preis: ein gefangener Fehler, der **nur** gefangen und nicht sichtbar protokolliert wird,
  ist ein Fehler, den niemand mehr findet. Wer das baut, muss ihn so protokollieren, dass er
  auffällt — sonst tauscht man einen Absturz gegen einen stillen Defekt.

**Mein Vorschlag:** je Kanal fangen, mit einer Protokollzeile in der Schärfe eines Absturzes
(Stapel mit), und der Kanal endet `E_INTERNAL`. Begründung: dieser Server trägt **alle** Benutzer
einer Installation, und `Restart=on-failure` ist hier nicht die Antwort — ein Neustart wirft jede
offene Sitzung mit heraus. **Gemessen ist das nicht**, es ist eine Abwägung.

Dazu gehört, was dieselbe Aufgabe über den Weg dorthin gemessen hat: die Regel „ein `nil`-Feld
heißt: keinen Handler anmelden" steht im Code, und sie wurde **an einer Stelle nicht befolgt**.
Ein Riegel, der im Kommentar steht und nicht im Code, ist kein Riegel.

## Punkt 39: Das Abbild der Terminal-App hängt an einem Tag, nicht an einem Digest

> **Stand: offen, wartet auf Thomas.** Art: Sicherheit. Zwei Fragen in einer:
> Digest festnageln ja/nein — und **wer hebt ihn an**, wenn Debian einen
> Sicherheitsfix veröffentlicht.

**Gefunden am 12.10.2026**, als das Kommando gebaut wurde, das dieses Abbild überhaupt erzeugt
(vorher antwortete `ngc-controller image` immer „noch nicht gebaut" — ein Dienst ohne Erzeuger
für sein Abbild läuft nie).

Der `Containerfile` der Terminal-App sagt `FROM docker.io/library/debian:13-slim`. **Das ist ein
Tag, kein Digest.** Daraus folgt zweierlei, und das Zweite ist das Ernste:

1. **Zwei Bauten desselben Commits sind zwei verschiedene Abbilder.** Reproduzierbar ist dieser
   Teil nicht, während alles andere in diesem Projekt ausdrücklich darauf gebaut ist (die Pakete
   sind bitgleich, weil die Zeit im tar 0 ist — eigens gemessen).
2. **Wer den Tag beherrscht, beherrscht, was im Container der Terminal-App läuft** — und dieser
   Container hat laut seinem Manifest `needs: ["root"]`. Das ist die Lieferkette der schärfsten
   App im System.

**Nicht erfunden, und das war richtig:** ein Digest lässt sich ohne podman und ohne Netz nicht
messen, und ein abgeschriebener Digest wäre eine Zahl ohne Messung. Statt dessen steht jetzt ein
Fall da, der **rot wird, sobald jemand doch festnagelt**, mit der Aufforderung, ihn dann
wegzunehmen — eine benannte Lücke mit Wächter statt einer stillen.

`--pull=always` im Bau-Aufruf nagelt **nichts** fest; es verschiebt nur von „Zwischenspeicher
dieses Rechners" auf „was die Registry heute sagt". Der Kommentar sagt das so.

**Zu entscheiden:** Digest festnageln — ja oder nein. Und wenn ja: **wer hebt ihn an**, wenn
Debian einen Sicherheitsfix veröffentlicht? Ein festgenagelter Digest, den niemand pflegt, ist
nach einem halben Jahr schlechter als ein Tag. Diese Frage gehört zum Release-Weg, nicht zum
Controller.

**Dazu eine Entscheidung, die dabei getroffen und begründet wurde:** das Abbild baut der **Admin
von Hand auf einem Baurechner**, nicht der Controller beim ersten Terminal. Drei Gründe, alle am
Code: `install.Activate` **lädt** Abbilder aus dem unterschriebenen Paket und baut nie — ein Bau
auf dem Server wäre ein zweiter Weg in den App-Speicher; ein Bau beim ersten Start wäre Wartezeit
und eine Registry-Abhängigkeit genau dann, wenn jemand auf eine Shell wartet; und ein auf dem
Server gebautes Abbild hat **niemand unterschrieben**.

## Punkt 40: Das Terminal echot — und die Oberfläche hätte Passwörter hingeschrieben

> **Stand: gilt, behoben.** Art: Sicherheit. **Enthält eine Selbstberichtigung:**
> nicht der Plan hat es falsch gesagt — der Plan sagt zum Echo **gar nichts**.

**Gemessen am 12.10.2026** vom Webpaket, beim Bauen der Gegenseite des Terminal-Protokolls
(Punkt 29). Es ist der Fund des Tages mit der direktesten Folge für einen Benutzer.

**Der Dienst echot.** `pty_linux.go` startet `/bin/sh -i` auf einem PTY, und ein PTY hat eine
Zeilendisziplin — das Getippte kommt zurück. In `apps/web/assets/apps/terminal/app.js:213` stand
aber der Satz, der Dienst echoe **nicht**, „it is no terminal line discipline", und die Oberfläche
hat sich darauf verlassen: sie schrieb die getippte Zeile **selbst** hin.

**Zwei Folgen, und die zweite ist die ernste:**
1. Jede Zeile hätte **doppelt** dagestanden.
2. **Ein Programm, das nach einem Passwort fragt, schaltet das Echo ab** — der Dienst hätte also
   nichts zurückgeschickt, und die Oberfläche hätte das Passwort **trotzdem hingeschrieben.**

Das lokale Echo ist entfernt, der Zaun ist ein Zähler (ein getippter Befehl muss **genau einmal**
dastehen), und der Satz ist mit `e86a1a6` weg.

**Und die Lücke ist eine andere, als sie zuerst aussah.** Ich hatte hier geschrieben, der Plan habe
es falsch gesagt, und wollte den Plan berichtigen. Nachgezählt (`git grep -n -E "does not echo|line
discipline" e5f1fcc`) gibt es **genau eine** Stelle im ganzen Baum, und das ist die obige Zeile in
`app.js` — **im Plan kommt keines der beiden Wörter vor.** Der Plan hat zum Echo also **überhaupt
nichts** gesagt, auch nichts Richtiges, und der Umsetzer der Aufgabe 25 musste es **raten**.

Das ist der schärfere Befund: ein Plan, der eine Eigenschaft nicht nennt, erzeugt eine Vermutung,
und eine Vermutung im Kommentar liest der Nächste als Tatsache. **Darum steht hier, was gilt** —
der Dienst echot, weil er ein PTY benutzt — und nicht, wo ein falscher Satz stand.

## Punkt 41: Das Terminal kann kein Passwort und kein `vi` — Entscheidung offen

> **Stand: offen, wartet auf Thomas.** Art: Entscheidung. Keine Vertragsänderung —
> das Format kann beides, es fehlt die Entscheidung über die Oberfläche.

Dieselbe Runde, und es ist die Frage dahinter: die Oberfläche aus Plan 1 ist ein **Zeilenfeld**
(`input type=text`). Sie schickt ganze Zeilen und zeigt alles Getippte.

Damit geht **nicht**: ein Passwort eingeben, ohne es im Feld zu sehen · `Strg+C` · Pfeiltasten ·
Tab-Ergänzung · und **jedes** Programm, das einzelne Tasten liest (`vi`, `top`, `less`).

**Das Format kann all das** — ein Tastenrahmen trägt beliebige Bytes (Punkt 29). Es fehlt allein
die **Oberflächenentscheidung**:

- **Rohtasten.** Ein echtes Terminal: jede Taste geht sofort hin, die Anzeige kommt nur vom
  Dienst. Dann ist eine Passwortabfrage **von selbst still**, weil der Dienst das Echo abschaltet
  und die Oberfläche nichts von sich aus zeigt. Preis: die Oberfläche muss eine Terminalanzeige
  sein, kein Textfeld — mehr Arbeit, und Tasten müssen zuverlässig ankommen.
- **Zeilenfeld behalten** und in der Doku sagen, dass Passwortabfragen **sichtbar** sind. Preis:
  ein Admin tippt irgendwann ein `sudo`-Passwort in ein Feld, das es anzeigt, und niemand liest
  vorher die Doku.

**Mein Vorschlag: Rohtasten.** Nicht wegen `vi`, sondern wegen des Passworts: die Variante, die
das Geheimnis **durch ihre Bauart** nicht zeigt, ist der Variante überlegen, die sich auf einen
Satz in der Doku verlässt. Und das Format kann es schon, es ist keine Vertragsänderung.
**Gemessen ist das nicht** — es ist eine Abwägung über Arbeit gegen Sicherheit, und die
Entscheidung gehört Thomas.

## Punkt 42: `ngc.open("terminal","shell",{cols,rows})` ist wirkungslos — ENTSCHIEDEN

> **Stand: gilt — ENTSCHIEDEN am 12.10.2026** (Parameter bleiben Beiwerk).
> Art: Entscheidung. Kein offener Posten mehr.

Ebenfalls am 12.10.2026 gemessen: `terminal.go` **liest keine Parameter**, und `shellEnv` baut die
Umgebung aus nichts. Die Fenstergröße wirkt **nur** über den Rahmen des Protokolls.

Im Plan steht der Aufruf aber mit Parametern, und zwar in
`docs/superpowers/plans/2026-09-23-ngc-plan-1-web.md:6750`: „Der Kanal ist ein Datenstrom
(`ngc.open("terminal", "shell", {cols, rows})`)" — **so, wie es dasteht, liest der nächste
Umsetzer sie als den Weg.** Entweder die Parameter werden gelesen, oder es gehört dazugeschrieben, dass sie
Beiwerk sind. Beides ist in `apps/` zu machen, keine Vertragsänderung.

**ENTSCHIEDEN am 12.10.2026 (Zweig `controller-p42`): die Parameter bleiben Beiwerk, sie werden
NICHT gelesen.** Die Messung, die das trägt, steht auf der Gegenseite: die Webseite sendet die
Größe als **ersten** Rahmen nach dem Öffnen und danach bei jeder Änderung
(`apps/web/assets/apps/terminal/app.js`, `tellSize`, aufgerufen in der `then` des `open` und im
`resize`-Horcher; ihr Fall „the first thing the terminal sends is the window size, byte for
byte" hält die drei Kopfbytes und die zwei Zahlen). Der Rahmen ist also keine späte Korrektur
einer Größe, die schon in den Parametern stand — er ist das erste Wort des Kanals. Die
Parameter zusätzlich zu lesen hätte **einen Wert auf zwei Wegen** herein gebracht (die Form des
Befundes W-1) und die Klemmung 1..`MaxDim` an zwei Stellen gelegt.

Was dafür geschrieben wurde: der Block „THE PARAMETERS OF ngc.open ARE DECORATION" im Kopf von
`apps/server-controller/classes/terminal/terminal.go` (mit dem **benannten Preis**: bis der erste
Rahmen da ist, hat das Pseudoterminal die Größe, die der Kern ihm gab), ein Riegel
`TestShellEnvCarriesNothing`, der `shellEnv` gegen eine mit allen Dienstvariablen vergiftete
Umgebung hält — so hat auch `NGC_PARAMS` keine Hintertür —, und die Berichtigung im Plan an
Zeile 6750.
## Punkt 43: Der Vertrauensanker verriegelt den dokumentierten Weg nach einem Totalverlust

> **Stand: offen, wartet auf Thomas.** Art: Sicherheit. **Dieser Punkt ist die
> Entscheidung zu Punkt 10 der Reihe A** (Sammelliste oben), nicht zu Punkt 10
> dieser Reihe — die gibt es nicht. **Antwort B ist gemessen keine Antwort**
> (Mutant C); empfohlen ist der **dritte Weg** (Mutant D). Gebaut ist nichts.

**Gelesen am 12.10.2026** beim Einhängen des Ankers, **aus dem Code**, nicht gemessen — und der
Zustand liegt **schon auf `main`**, vor jeder Änderung dieser Runde.

**Was der Vertrag sagt** (V1.2, Punkt 10): eine zurückgespielte Datenbank ohne Vertrauensliste
**öffnet die Einrichtung wieder**. Das ist der dokumentierte Weg zurück nach einem Totalverlust.

**Was wirklich passiert, sobald eine `trust-anchor.json` mit Nummer > 1 liegt** — und die liegt
dort nach **jeder** Kopplung:

1. Die Einrichtung öffnet noch, wie versprochen.
2. Fassung **1** wird unterschrieben und gespeichert.
3. Der nächste `Refresh` liest **ab der angehefteten Nummer**, findet dort nichts, und bleibt bei
   **`ErrNoTrust`**.

**Der Weg zurück ist also offen und endet in einer Wand.** Für den Admin heißt das: die
Einrichtung nimmt ihn an, und danach läuft nichts.

**Das ist nicht von der Änderung dieser Runde erzeugt** — das „nur vorwärts" des Vorrückens
erzeugt es nicht und behebt es nicht. Es ist eine **Entscheidung über den Recovery-Weg**, und sie
hat genau zwei Antworten:

- **Der Recovery-Modus räumt die Datei weg.** Dann ist der Anker weg, sobald jemand bewusst
  wiederherstellt — und der Schutz, den er gibt (niemand dreht die Liste zurück), ist genau in
  dem Augenblick weg, in dem ein Angreifer eine Wiederherstellung vortäuschen könnte.
- **Die Einrichtung räumt sie weg**, wenn sie eine Datenbank ohne Vertrauensliste findet. Dann
  entscheidet der Zustand der Datenbank über den Anker — und wer die Datenbank leeren kann, hebt
  damit den Anker.

**Beide Antworten geben etwas her.** Darum gehört die Entscheidung zu Punkt 10 des Vertrags und
nicht in ein Paket, und darum ist sie hier benannt statt gebaut.

**Was der Anker leistet, in einem Satz, damit die Abwägung klar ist:** er schützt gegen einen, der
die **Datenbank** schreiben kann, aber **keinen Admin-Schlüssel** hat — er soll die Vertrauensliste
nicht auf einen älteren Stand zurückdrehen und entzogene Schlüssel nicht wieder gelten lassen
können.

**Und was er nicht kann, gemessen:** die Kette **oben abschneiden**. Das ist die eine Form, die
`checkGap` selbst als nicht fangbar benennt, und sie ist jetzt mit zwei Zwischenprüfungen
festgenagelt: der Stift steht nach Fassung 3 auf 3, ein kalter API öffnet darunter die ganze Kette
ohne Fehler — und mit abgeschnittener Zeile 3 kommt `ErrNoTrust`. Die Kontrolle zeigt, was ohne
Anker passiert wäre: Fassung 2 wird **stillschweigend wieder in Kraft gesetzt**.

### Gemessen am 12.10.2026 — und die Lage ist schärfer, als sie gelesen war

Zwei Fälle über den **echten** Handler und das echte `state`, mit Zwischenprüfung (derselbe Weg
**ohne** Anker geht durch: Einrichtung 200, Fassung 1 = 204, kalter Knoten öffnet sie, Tür danach
404 — der Fall misst also den Anker und keine frühere Wand). Mutant A (`from = anchor.Seq` →
`from = 1`) **stirbt**, Kontrolle **lebt**, `-race` 0 mit Mutant A als Gegenprobe.

**Die Wand kommt — und zwei Dinge daran waren nicht gelesen:**
1. **Der aufnehmende Knoten sieht sie nicht.** `AddVersion` setzt `s.cur`, also läuft die
   Einrichtung scheinbar durch. Die Wand kommt beim **Neustart** oder auf dem **zweiten Knoten**.
2. **Sie schließt sich hinter dem Admin.** Die Einrichtung öffnet danach wieder, aber eine zweite
   Fassung 1 wird abgewiesen, weil Zeile 1 schon liegt — der Weg zurück ist **verbaut**.

**Was der Recovery-Modus heute mit der Datei tut: nichts.** `git grep -i "trust-anchor"` über die
Skripte des Recovery-Pakets: **kein Treffer**; `docs/recovery-modus.md` nennt die Datei
**nirgends**. Im Produktionscode gibt es **zwei Schreiber** (`node.pair`, `state.advanceAnchor`)
und **keinen Löscher**.

**Der Sachverhalt, der in meiner Abwägung fehlte — es gibt zwei Treiber:**
- **MariaDB:** die Datenbank liegt auf einem **anderen** Rechner. Dort ist der Anker das Einzige,
  was hält.
- **SQLite:** die Datenbank ist eine **Datei derselben Platte**. Wer sie schreibt, löscht den
  Anker.

**Der Anker schützt damit messbar nur die MariaDB-Installation.** Das ändert die ganze Abwägung:
bei SQLite ist der Schutz, über den hier gestritten wird, schon heute nicht vorhanden.

**Antwort B (die Einrichtung räumt die Datei weg) hebt den Schutz GANZ auf**, nicht teilweise —
Mutant C gemessen: alle Zeilen weg ergibt heute `ErrNoTrust`, unter B eine **Übernahme** (Fassung 1
mit fremdem Admin in Kraft); nur Zeilen 2 und 3 weg ergibt heute die Wand, unter B einen
**gelungenen Rollback**. Der Grund ist einfach: **jeder** Rollback erzeugt genau das `ErrNoTrust`,
das B zum Auslöser macht. **B ist damit keine Antwort.**

**Antwort A (der Recovery-Modus räumt sie weg) kostet nichts Neues:** gemessen gegen `main`, ohne
jede Codeänderung, genügt ein `os.Remove` — `anchorFor` deutet jeden Dateifehler als „kein Anker".
A vergrößert die Angriffsfläche also nicht. Ob ein Angreifer eine Wiederherstellung **vortäuschen**
kann, hängt allein daran, ob das Wegräumen das **Stick-Quorum** verlangt (dann reicht
Schreibzugriff nicht) oder als Datei auf dem Produktivsystem gebaut wird (dann reicht er).
**Diese Hälfte ist nicht messbar, weil es noch keinen Recovery-Code gibt.**

### Ein dritter Weg, gemessen (Mutant D) — und er hält beides

**Der Stift wird nur von einer *abgeschlossenen* Einrichtung geräumt**: in `take`, **nach** der MAC
des lokalen Einmal-Codes, der Unterschrift und `AddVersion` — nicht beim Anblick einer leeren
Datenbank.

- **Weg zurück: offen.** Ein kalter Knoten läuft danach auf Fassung 1.
- **Rollback-Angriff: weiter verriegelt.** Liegt Zeile 1, kommt `ErrOldVersion`; liegt sie nicht,
  fehlt dem Angreifer der **lokale** Einmal-Code.
- **Preis:** gegen einen, der die Datenbank schreibt **und** die lokale Platte liest, hält er
  nichts — **aber genau der hat den Anker schon heute**, mit einer Dateilöschung. Es kostet also
  nichts, was nicht ohnehin verloren ist, und legt den Anker **nicht** in die Hand des reinen
  Datenbank-Schreibers.
- **Vor einem Bau zu klären:** `EnsureCode` legt nach einem Rollback einen **frischen** Einmal-Code
  aus; und der Stift wird ohne Admin-Schlüssel der alten Liste geräumt — unvermeidlich nach einem
  Totalverlust, und genau dort könnte Antwort A mit den Root-Sticks mehr leisten.

**Mein Vorschlag: der dritte Weg**, weil er der einzige ist, der den dokumentierten Weg zurück
offen hält, ohne den Anker dem Angreifer zu geben, gegen den er gebaut ist. **Gebaut ist er
nicht** — er gehört zu Punkt 10 des Vertrags.

## Punkt 44: API und Controller haben zum ersten Mal miteinander gesprochen

> **Stand: gilt.** Art: Messung. **Berichtigt eine Meldung:** „Punkt 32 ist
> gefallen" war zu scharf — Punkt 32 steht, nur seine offene Hälfte ist jetzt
> gemessen.

**Gemessen am 13.10.2026**, unter Linux in WSL, mit beiden Programmen für linux/amd64 gebaut. Das
war auf **beiden** Abschlussprüfungslisten der größte Posten — „kein echter Controller, kein
echter Tunnel" (API: Punkt 5 von 23, Controller: Punkt 13 von 22). Alles war gegen Attrappen
gemessen.

**Der Beleg ist derselbe Prüfwert auf beiden Seiten:**

```
Negativ-Kontrolle (API aus):   versions=0
nach 10 s mit laufender API:   versions=1 seq=1 hash=IEoZ6APgO_ay-0kPa7P2A985bgExpVQi1PmpIibxfuM
API-Seite derselben Version:   seq=1 hash=IEoZ6APgO_ay-0kPa7P2A985bgExpVQi1PmpIibxfuM
```

Die Zeile steht im **verschlüsselten SQLite des Controllers**, und nur `chain.Keeper.Apply`
schreibt sie. **Durch den Kanal ging die Vertrauensliste.** Der Weg, ganz echter Code: TLS 1.3 →
WebSocket → Noise_KK → beide Hallos → numerierter Kanal → `serve.trust`. Eine zweite Fassung lief
ebenso durch. **Abgekürzt ist nur die Shell** (Fassung 1 kommt nicht durch `setup.Handler`, der
Kopplungscode nicht aus `ngc.users/pair-code`) — das ist offen benannt.

**Der Statusweg ist mit einem Mutanten direkt belegt:** ohne ihn endet der Tunnel nach 75
Sekunden, mit `case wire.CtlStatus: return false` stirbt er **genau auf der Sekunde des
Status-Ticks** und wird neu aufgebaut. Also ging ein echter `CtlStatus` raus und wurde angenommen.

**Nichts ist weggestorben** — kein `panic`, kein Stapel, in sieben Läufen, auch nicht unter dem
Mutanten, der einen stehenden Tunnel abbrach. Beide Prozesse jedes Mal **0** auf SIGTERM. Das ist
die erste Messung, die Punkt 38 (kein `recover` im Produktionscode) **nicht** zum Tragen bringt —
sie entlastet ihn nicht, sie hat nur nichts gefunden.

### Was dabei gefallen ist

**Punkt 32 ist nicht widerlegt — seine offene Hälfte ist jetzt gemessen.** Der Strang hat es als
„Annahme gefallen" gemeldet, und das ist zu scharf: Punkt 32 sagt selbst, die Sperre gelte **nur
für den Augenblick der Migration**, nicht für die Lebenszeit, und nennt genau das als
**ungemessen**. Jetzt ist es gemessen: ein zweiter `ngc-api` auf derselben Datei kommt **durch**
`dbopen.Open` und stirbt erst am Port (`bind: address already in use`). **Zwei APIs auf
verschiedenen Ports würden die Datei beide öffnen.** Und: **`ngc-api migrate` läuft, während eine
API dieselbe Datei offen hält** (`0 Migrationen eingespielt.`, Code 0).

Damit steht die Entscheidung von Punkt 32 unverändert — wer die Sperre beim **Start** nicht
bekommt, startet nicht —, aber der Satz „wer zwei Controller auf eine Datei lässt, muss das zuerst
messen" ist jetzt beantwortet: **die Sperre hält sie nicht auseinander.**

**Weiter gefallen, je am laufenden Programm:**
- **`Hello.TrustSeq` ist 0, nicht 1**, solange nur der Anker steht — darum öffnet die API schon
  beim **ersten** Hallo einen Kanal. Stand im Kommentar, war nie gelaufen.
- **Die provisorische `controller.json` des ulh-Skripts hält am Programm**: `pair` kommt bis zur
  Code-Frage, ein falscher Code gibt Code 2 — also hinter `LoadController`, und `/etc/ngc` bleibt
  unberührt.
- **Die `/tunnel`-Pfade:** `POST /tunnel/pair` → 400 `E_PROTOCOL`, `GET` darauf ebenso, `GET
  /tunnel` ohne Schlüssel → 404.
- **W-3 ist am Draht bestätigt:** ausgehandelt wird **TLS_AES_128_GCM_SHA256**. Der Chiffren-Riegel
  wirkt auf dieser Strecke **nicht** — genau wie der Kommentar es seit der Entscheidung sagt. TLS
  1.2 wird abgewiesen.

### Zwei Dinge, die von außen nicht zu sehen sind

1. **Vom Schreibtisch gibt es keinen Weg zu einem gelisteten Controller.**
   `shared/session/session.go:909` vergleicht die Knoten, und `setup.Handler` kennt nur den
   API-Knoten — ein Controller kann also **nie** in Fassung 1 stehen, sondern nur über Fassung 2,
   und die geht allein über eine Shell. Die Vorrichtung dieser Messung geht darum an
   `state.AddVersion` vorbei, und sie sagt das. `shared/` ist eingefroren; das gehört in die Reihe
   für v1.3.
2. **Es gibt keinen zweiten Zeugen für die Kopplung.** `controllers.Pair` schreibt keine Zeile, und
   `node.pair` gibt **jeden** Fehlschlag als dieselbe Zeile mit Code 1 aus. „API aus" und „Code
   abgelehnt" sind von außen **nicht zu trennen** — für einen Admin, der ein Gerät einrichtet, ist
   das der Unterschied zwischen „noch mal versuchen" und „Netz prüfen".

**Nebenbefund, Ursache ungemessen:** nach einem abgebrochenen Tunnel scheiterte der **erste**
Neuversuch des Controllers mit „the API cannot be reached", obwohl die API lief; der zweite kam 14
Sekunden später durch.

## Punkt 45: Ein echter Store — und alle früheren Store-Messungen hatten die Kettenprüfung aus

> **Stand: gilt.** Art: Messung. ⚠ **Berichtigt rückwirkend jede Store-Messung vor
> dem 13.10.2026**: der Prüfstand setzte den Transport des Testservers, die
> TLS-Kette wurde nie beurteilt. Der Nebenbefund „Rückdreh-Falle ohne Ausgang" ist
> **erledigt durch Punkt 46**. Verschärft **Punkt 25**. Der Widerspruch zur
> **vierstelligen Hausnummerierung** ist weiter offen und wartet auf Thomas.

**Gemessen am 13.10.2026.** „Kein echter Store" war Punkt 18 von 23 auf der Abschlussprüfungsliste
der API. Gebaut wurde einer **nur mit dem Werkzeug** (`pack`, `encrypt`, `catalog`, `release`),
und `check -store` sagt **`store ok`**.

**Die API hat zwei Pakete wirklich geholt**, über eigene CA und `store.ngc.test`, mit dem Client,
den `appstore.New` selbst baut: `hits=1 took=169ms seq=1`, angekommen 3072 Byte mit dem Kopf
`manifest.json\0…` und 3112 Byte mit `NGCENC1\n`. Dazu eine Release-Datei gegen den
**Release**-Schlüssel.

**Punkt 39 hält, und zwar als Objektkennung:** zweimal gepackt, zwei Sekunden auseinander,
`git hash-object` dreimal derselbe Wert — nicht nur dieselbe Länge.

**Punkt 30 ist auf dem Store-Weg gemessen und geht:** 3072 Byte an den Controller, unterschriebener
Katalog im Kopf, App-Zeile danach `active=true`, Zwischenlager **leer**. Derselbe Aufruf mit
`store: local` → **`E_UNREACHABLE`**. Also: **Store-Paket trägt, Hochlade-Weg nicht.**

**Und der 60-Sekunden-Riegel ist zum ersten Mal gefahren**, nicht nur gesetzt: ein tröpfelnder
Server wird nach `1m0s` abgebrochen.

### Der Fund, der rückwirkend zählt

**Jede bisherige Store-Messung lief mit abgeschalteter Kettenprüfung.**
`apps/server-api/classes/appstore/fixture_test.go:212` setzt
`fx.St.Client.Transport = fx.HTTP.Client().Transport` — damit trägt der Prüfstand den Transport des
Testservers, und die TLS-Kette wird nie beurteilt. **Alles, was über „die API holt von einem Store"
gemessen wurde, galt also für einen Weg ohne diesen Riegel.**

Gegengemessen, ohne Überlagerung: **die API verlangt die Kette wirklich** — eine fremde Wurzel
ergibt `hits=0`, der Handschlag fällt vorher, und der Grund lautet „Der Store hat nicht oder nicht
brauchbar geantwortet."

Der Riegel ist also da. Was fehlte, war eine Messung, die ihn **nicht** umgeht.

### Punkt 25 am echten Katalog: gilt unverändert, und schärfer

Relative Pfade fängt `store.ValidPath` im Vertrag — `../..`, Query, `#`, Leerzeichen kommen nicht
durch. Eine **absolute** https-Adresse wird aber nur über Schema, Länge, `#` und **Rechnernamen**
geprüft:

- `https://store.ngc.test:8443/…?token=x` ging durch — **und die Anfrage ging raus.**
- `https://127.0.0.1:1/x.ngc` kam ebenfalls durch `PackageURL` — **ein anderer Rechner als der
  Store.**

**Pfad und Query prüft niemand.** Dass ein Paket auf einem anderen Rechner liegen darf, ist
vertretbar (ein Store kann eine Auslieferung benutzen) — dass **niemand** den Pfad ansieht, ist der
Punkt. Die Lösung bleibt ein ausgeführtes `store.ValidDownloadURL`;
`shared/store/catalog.go:208` ist eingefroren, also **BLOCKED** und in der v1.3-Reihe.

### Zwei Befunde darüber hinaus

1. **Wer den Store-Schlüssel hat, sperrt den echten Store dauerhaft aus.** Nach einem
   untergeschobenen Katalog mit Folge 57 ist der heile Store mit Folge 1 **für immer** ein
   Rückdreh. **Wie ein Admin das zurücksetzt, ist nicht gemessen** — und solange es keinen Weg
   gibt, ist der Rückdreh-Riegel auch eine Falle.
2. **`ngc-store` schrieb `store-key.pem` nicht, die sein eigenes `check -store` verlangt.** Alle
   Unterbefehle grün, der fertige Store **unprüfbar**. Behoben, mit einem Fall, der nur über `Run`
   geht und zusätzlich prüft, dass kein Saatwert ins Store-Verzeichnis gerät; Mutant tot, Kontrolle
   lebt.

**Dazu ein Widerspruch zur Hausnummerierung:** `release -version 0.0.1.0` fällt durch, weil
`store.ValidVersion` **dreistellig** ist — die Hausnummerierung dieses Nutzers ist vierstellig.
Das gehört vor dem ersten Release entschieden, nicht danach.

## Punkt 46: Der Rückdreh-Riegel war eine Falle ohne Ausgang — jetzt gibt es einen

> **Stand: gilt, gebaut.** Art: Sicherheit. Erledigt den Nebenbefund 1 aus
> **Punkt 45**. ⚠ Die Fälle erben die abgeschaltete Kettenprüfung aus Punkt 45.

**Gemessen am 13.10.2026.** Punkt 45 hatte es als Nebenbefund genannt: wer eine hohe Folgenummer
unterschiebt, sperrt den echten Store dauerhaft aus. Nachgemessen ist es schlimmer, und zwar in
drei Richtungen.

**Erstens: die Falle schnappt wirklich zu.** Stand 1 (Zwischenprüfung: geht durch) → Folge 57 →
der heile Store mit Folge 1 gibt `store.ErrRollback`, und die gespeicherte Folge bleibt **57**,
auch nach weiteren Versuchen. Die getrennte Gegenmessung zeigt, dass Folge 1 gegen Stand 1 **kein**
Rückdreh ist — der Fall fällt also an der 57 und an nichts anderem.

**Zweitens: es braucht gar keinen Angreifer.** Mit dem **eigenen** Schlüssel des Stores gemessen:
Stand 7, **einmal** Folge 57 (ein Testlauf, ein zurückgespielter Stand), und danach sind 8, 9, 10
und 56 alle `ErrRollback`. Erst 57 geht wieder durch. **Ein einziger Refresh im falschen Moment**,
und der echte Store muss 49 Kataloge überzählen, um sich selbst wieder ausliefern zu dürfen. Der
Code sieht eine Zahl, nicht einen Angreifer.

**Drittens: es gab keinen Weg zurück.** Mit `grep` belegt: die Folgenummer steht in der
Blob-Tabelle unter der Store-Kennung, und `BlobDelete` wird **nie** darauf gerufen — vier Stellen
lesen und schreiben, keine löscht. In `core.All()` gibt es **keine** Aktion, die etwas zurücksetzt.
**Und „Store entfernen und neu anlegen trug nicht"** — das war meine Vermutung, und sie ist
gemessen falsch: die Zeile blieb bei 57, auch mit **neuem Store-Schlüssel**. Übrig blieben eine
**neue Store-Kennung** (dann nennen die installierten Apps weiter die alte) oder **Handarbeit an
der Datenbank**.

**Gebaut, in `apps/`:** ein Store, der nicht mehr in der Vertrauensliste steht, **verliert seinen
Katalog**. Der Weg zurück ist damit: entfernen, einmal auffrischen, wieder eintragen — mit genau
dem Recht, das einen Store **hinzufügt**. Es kostet nichts Erreichbares, denn so eine Zeile ist ab
der Entfernung ohnehin unlesbar; sie hielt nur noch den Riegel.

**Der Preis, benannt:** wer die Vertrauensliste schreiben darf, hebt den Riegel für eine Kennung
auf. Das ist vertretbar — derselbe darf dem Store auch einen anderen Schlüssel und eine andere
Adresse geben. Einen alten Katalog an einem **gelisteten** Store vorbeizuschmuggeln kann er nicht.

**Verworfen:** „der Admin senkt die Zahl beliebig". Das hebt den Riegel ganz auf und bräuchte eine
**neue Aktion** in `shared/core/core.go:126–168` — eingefroren.

**Belegt ist es als rot-vorher/grün-nachher an der Zusicherung selbst:** der alte Fall
(„die Zeile bleibt stehen") war auf `309dc2a` **grün** und ist nach dem Bau **rot**, unverändert.
Dazu zwei Mutanten tot, eine Kontrolle lebt, und die `-race`-Null ist mit einer Wegwerf-Mutation
belegt, die im **selben** Lauf rot wird.

**Ungemessen, und es gehört dazu:** diese Fälle erben die Zeile aus dem Prüfstand, die die
**TLS-Kette abschaltet** (Punkt 45) — der Strang hat das in den Kopf der Testdatei geschrieben,
statt es zu übergehen. Und die Zeile mit Zeit und Grund bleibt beim Entfernen stehen; das ist
bewusst so, aber ungemessen.

## Punkt 47: Der kleine Fall — eine Datenbankdatei je Programm, und eine gemessene Lücke — Lücke zu seit Punkt 59

> ⚠ **Teilweise überholt.** Die **gemessene Lücke** („ohne `ngc-web` kann sich
> niemand anmelden, also auch keinen Kopplungscode holen") ist **zu seit Punkt 59**
> und an einer frischen Anlage bestätigt in **Punkt 62**. **Offen bleibt die
> Entscheidung:** die fünf `ngc-*`-Skripte nach `ulh`-`main` zusammenführen —
> ein Push dort erreicht sofort alle Nutzer. Art: Umgebung.
> Dazu **Punkt 48**: „ein Schreiber je Datei" ist eine Entscheidung über
> **Einfachheit**, nicht über Sicherheit.

**Gemessen am 13.10.2026.** Für ein kleines System (eine Maschine) ist der Treiber
`config.DriverSQLite`: **eine Datei**, verschlüsselt mit AES-256-XTS über eine eigene VFS,
Schlüssel 64 Byte in `config.DB.Key`. MariaDB/Galera (`config.DriverMariaDB`, Konten `ngc` und
`ngc_migrate`, TLS mit CA) ist erst der Fall „mehrere Maschinen".

**Die Falle dabei:** bei SQLite wandert **jeder** `Open`-Aufrufer selbst die Migration durch;
`ngc_lock` deckt nur den Augenblick der Migration. Zwei Programme auf **einer** Datei sind damit
zwei Leser desselben `go:embed` — Befund W-1.

**Gebaut, in `ulh` (Zweig `ngc-all`, `4faabb7`):** `ngc-all.sh` (1580 Zeilen, 82 neue Prüffälle,
fünf Sprachen) richtet ein kleines System ein mit **je einer Datei und je einem Schlüssel pro
Programm** — `/var/lib/ngc/api/ngc.db`, `/var/lib/ngc/controller/state.db`. `check_two_files`
bricht **vor** dem ersten Schreiben ab, falls die Pfade je gleich wären. Die eine Stelle, die der
Vertrag doch teilt (Rolle `db`: `controller.json`s `migrate` trägt Datei und Schlüssel der API,
siehe `shared/ulh.md`), ist mit `After=network-online.target ngc-api.service` und
`Restart=on-failure`/`RestartSec=5` auf beiden Einheiten abgefedert — nicht aufgehoben.

**Die gemessene Lücke, und sie ist die wichtigste:** Apache, PHP, Zertifikat und `web.json` sind
`ngc-web`s Arbeit. Je Zweig gezählt (committeter Stand, nicht Arbeitsbaum): `origin/main` hat
**null** `ngc-*`-Skripte. `ngc-api.sh`/`ngc-controller.sh` liegen auf `ngc-skripte` und `ngc-all`,
`ngc-db.sh` auf `ngc-db`, `ngc-web.sh` auf `ngc-web` und `ngc-web-kopfzeilen`. **Kein einziges ist
veröffentlicht.** Folge: ohne `ngc-web` kann sich niemand anmelden, also auch keinen
Kopplungscode holen — **der kleine Fall ist erst mit `ngc-web` durchgängig**, und durchgängig
geprüft ist er erst auf echtem Debian (Feldplan).

**Zu entscheiden (Nutzer):** die fünf Skripte nach `ulh`-`main` zusammenführen. Ein Push dorthin
erreicht per Auto-Update **sofort alle Nutzer** — deshalb liegt das nicht bei mir.

## Punkt 48: Zwei Programme auf einer SQLite-Datei — sicher, aber mit einer Zahl: vier

> **Stand: gilt.** Art: Messung. Zwei Posten der Liste „hingelegt, nicht gebaut"
> sind in **Punkt 49** gebaut. Beantwortet die offene Hälfte von **Punkt 32**.

**Gemessen am 13.10.2026**, die letzte offene Hälfte von Punkt 32. Alles mit **zwei echten
Prozessen** über `sqlite.OpenDB` → Pool → `sqlstore` → `LogAdd`, keine Attrappe, auf **C:** und
auf **Z:** (NAS).

**Gleichzeitig schreiben trägt.** Je Prozess 1/4/16/64/128/192 Fäden: 0 Fehler in jeder Lage,
`integrity_check=ok`, Zeilenzahl je Prozess exakt. Die Dateisperre hält über Prozessgrenzen,
auch auf dem NAS. Mehr Last ist nur Warteschlange: der Durchsatz bleibt ~180/s je Prozess,
ob 4 oder 192 Fäden. Bei 384 gleichzeitigen Schreibern reißt die 10-s-Frist (11,44 s) und es
gibt **trotzdem 0 Fehler** — die Wiederholung fängt es.

**Die Frist ist 10 s × 8 = 80 s.** `busy_timeout=10000` (`sqlite.go:27`) mal `maxTries=8`
(`sqlstore/store.go:17`): gegen eine 95-s-Sperre wartet der Verlierer **80,6 s** (C: und Z:
gleich), dann Fehler — und zwar `sqlite3: database is locked`, durch `httpapi.Fault` als
**`E_UNREACHABLE`** (503). **Von „Datenbank weg" nicht zu unterscheiden.** `fault.go` sagt diesen
Tag selbst voraus.

**Die Grenze ist vier, nicht „soundso viele Benutzer".** Der Pool hält 4 Verbindungen
(`sqlite.go:52`), und ein wartender Schreiber hält seine: blockierte Schreiber gegen die Kosten
des ersten **Lesens** — 2 → 0,008 s, 3 → 0,007 s, **4 → 9,513 s**. Vier wartende Schreibvorgänge
legen jede Abfrage des Prozesses lahm, auch jedes Lesen.

**Die Migration hat keine Wiederholung.** Mit echter Wanderung (`ALTER TABLE`) gegen 4 Schreiber
bei 328/s: `migrate-ok 1 in 0,029 s`. Gegen eine 95-s-Sperre: `migrate-error after 10,014s` —
**eine** Frist, **keine** Wiederholung, weil `bootstrap`/`take` über `db.ExecContext` direkt gehen.

**Behoben, weil die Meldung offensichtlich falsch war:** `busyOr` in `migrate.go` nennt jetzt den
Schritt und bei einem Wiederholungsfehler die Ursache („ein anderes Programm schreibt"),
ausdrücklich nicht den Satz der gehaltenen Migrationssperre. Rot vorher an der Zusicherung selbst,
grün nachher; 50 Fälle unter `-v`, zwei Mutanten tot, eine Kontrolle lebt, `-race` null mit
Wegwerf-Mutation belegt.

**Was folgt — und was nicht:** Zwei Schreiber auf einer Datei sind **sicher, nur langsam**. Eine
Regel „ein Schreiber je Datei" (so macht es `ngc-all.sh`, Punkt 47) ist damit eine Entscheidung
über **Einfachheit**, nicht über Sicherheit. Hingelegt, nicht gebaut: ein Schreibkanal je Prozess
statt vier Verbindungen; die Frist an den Zweck binden (kurz auf Benutzerwegen, lang beim Start —
heute hat **kein** Store-Aufruf ein `context`-Zeitlimit); „busy" von `E_UNREACHABLE` trennen, mit
`Retry-After`; und ob `migrate.Apply` eine Wiederholung bekommt (mit `maxTries=8` wären das 80 s
Hängen beim Start — genau die Abwägung von Punkt 32).

**Ungemessen:** zwei echte `ngc-api` über die ganze HTTP-Strecke; was der Benutzer im Browser bei
`E_UNREACHABLE` liest; dasselbe auf einem echten Pi; drei Pools gleichzeitig (Sicherung) unter
dieser Last.

## Punkt 49: Die Frist gehört zum Zweck — und „beschäftigt" ist nicht „weg"

> **Stand: gilt, gebaut.** Art: Messung. ⚠ **Berichtigt durch Punkt 54:** der hier
> als BLOCKED genannte Code `E_BUSY` (**P9**) ist **kein** „Ergänzung, kein Bruch" —
> ein Leser, der ihn nicht kennt, wirft den ganzen Rahmen weg. **P9 hängt seit
> Punkt 54 an P10.**

**Gemessen und gebaut am 13.10.2026**, zwei Posten von Punkt 48s Liste „hingelegt, nicht gebaut".

**Vorher belegt, nicht behauptet:** `context.WithTimeout`/`WithDeadline` kommt in `apps/server-api`
(ohne Tests) **neunmal** vor — Tunnel, Join, Link, Relay, Server — und **kein einziges Mal** um
einen Store-Aufruf. 16 Stellen geben `r.Context()` ungebremst in den Store. Die 80,6 s aus
Punkt 48 galten also auf jedem Benutzerweg gleich.

**Gebaut, alles in `apps/`:**
- `apps/server-api/classes/dbwait` (neu): **User = 5 s**, **Start = 90 s**, jede Frist mit ihrem
  **Grund** (`context.WithTimeoutCause`) — so überlebt der Grund die Frist. 90 s, weil eine volle
  Wiederholungskette 80,6 s braucht und ein **hängender** Start schlimmer ist als keiner (Punkt 32).
- `sqlstore` gibt an 11 Rückgabestellen die **benannte Ursache** zurück statt des nackten
  `context.DeadlineExceeded` — nötig, weil `database/sql` den Kontextfehler selbst liefert und
  dabei keine Zeile von uns passiert.
- `httpapi.FailFault`: „beschäftigt" bekommt **`Retry-After: 5`** und eine Protokollzeile
  `reason=db-busy`, „weg" bekommt `reason=db-unreachable` und keinen Header. Nur Grund und Zahl im
  Protokoll, nichts aus der Anfrage.
- Die **fünf** Wege vor dem WebSocket (`setup`, `login/start`, `login/finish`, `logout`, `trust`)
  bekommen die User-Frist; **`socket` nicht** — er lebt so lange wie die Sitzung.

**BLOCKED, und es ist der Kern:** ein eigener Code `E_BUSY` liegt in
**`shared/wire/wire.go:450–462`** — eingefroren. Genau das sagt `fault.go` selbst voraus. Also
bleibt der Code `E_UNREACHABLE` und nur Header plus Protokollgrund unterscheiden. In der v1.3-Reihe
als **P9**. Ebenso `shared/dbapi/dbapi.go:109–111` (ein vierter Wächter wäre die saubere Naht
gewesen; über die Kontext-Ursache umgangen) und `apps/server-api/classes/state/state.go:29–32`
(liegt in `apps/`, aber seine richtige Antwort ist derselbe fehlende Code — unverändert gelassen).

**Zahlen:** `httpapi`+`sqlstore` unter `-v` von **68 auf 73 bestanden**, 0 rot; `dbwait` 3 bestanden.
Rot-vorher an der Zusicherung selbst zweimal belegt (`cause_test.go:57` „der Grund des Aufrufers ist
weg"; `busy_test.go:68` `Retry-After = ""`). Vier Mutanten tot, Kontrollmutation lebt
(`sqlstore` exit 0, 151 bestanden). `-race` 0 über drei Pakete, belegt mit einer Wegwerf-Mutation,
die im selben Lauf **2 DATA RACE** zeigt.

**Nebenbefund aus der eigenen Messung:** der erste `FailFault` gab für jeden anderen Code nur den
Code zurück, **ohne zu antworten** — drei Fälle in `login` haben es gefangen (`from=3: 200` statt
404). Behoben, Fall dazu. Ein Fall, der nicht nur die neue Zeile prüft, findet den Rest.

**Ungemessen, und es gehört dazu:** `login/start`, `login/finish`, `logout` antworten weiter ohne
`Retry-After` (`login/live.go:313` gibt nur einen Code nach oben, `setup/setup.go:94` macht aus
jedem `E_UNREACHABLE` bewusst `E_INTERNAL`). Ohne User-Frist bleiben `join/join.go:87`,
`join/keys.go:46`, `controllers/pair.go:59` (laufen nicht über `httpapi.Routes.Mux`). Die
Hintergrundrunden (`minute`/`hour`) haben keine Frist. **Der echte 80-s-Fall mit zwei Prozessen ist
nach der Änderung nicht nachgefahren** — belegt ist die Kette im Prüfstand, nicht am laufenden Paar.
Und ein Klient, dessen Rumpf länger als 5 s tröpfelt, hört künftig „beschäftigt" statt `E_PROTOCOL`
bei 15 s; im Code benannt, nicht gemessen.

## Punkt 50: Zwei echte APIs über HTTP — der Benutzer hört, sein Passwort sei falsch — behoben durch Punkt 52 und 53

> ⚠ **Der Befund ist behoben — durch Punkt 52** (die Unsymmetrie weg, „besetzt" wird
> beantwortet) **und Punkt 53** (der Fehler überlebt `Live`). Der Wortlaut, den der
> Benutzer liest, ist in **Punkt 58** im echten Browser **gesehen**, nicht mehr nur
> abgelesen. Die P9-Einordnung ist durch **Punkt 54** berichtigt. Art: Messung.

**Gemessen am 13.10.2026**, zwei echte `ngc-api` für linux/amd64 auf **einer** SQLite-Datei,
Erst-Einrichtung echt über HTTP (GET+POST `/api/v1/setup`, Einmal-Code, `VerifySetup` grün).

**Zuerst der Ort, weil er die Zahlen bindet:** auf **Windows ist diese Messung unmöglich**.
`config.DB.Validate` verlangt das Pfad-Präfix `/` (`shared/config/config.go:572`),
`sqlite.validPath` verlangt `filepath.IsAbs` (`apps/server-db/classes/sqlite/sqlite.go:201`) — kein
Windows-Pfad erfüllt beides. Alle Zahlen gelten für **WSL/ext4**, nicht für C: und nicht für Z:.

**Der zweite Start ist stumm.** Beide „listening" in < 0,25 s, beide antworten `GET /setup` mit
**derselben** Installations-ID (`9FEQ…`) — Beleg für die geteilte Datei. 8 gleichzeitige Anfragen
auf frischer Datei: **8 × 200**, 19–45 ms, eine ID. Das `ErrBusy` von `EnsureInstallation` kam
**nicht** zum Tragen. Es gibt also keine Warnung, dass zwei APIs auf einer Datei liegen.

### Der Befund, und er ist der schlimmste bisher

Vier wartende Schreiber (`POST /login/start` mit **bekanntem** Namen) gegen eine Fremdsperre:
**200 OK nach 80,64 / 90,20 / 80,66 / 90,42 s** — mit gültigem Challenge-Körper. Der Schreibfehler
wird **verschluckt** (so entworfen, `login/start.go`), und im Protokoll steht nur
„a login challenge could not be kept".

**Der Benutzer sieht also keinen Fehler.** Er sieht eine Anmeldung, die 81–90 s braucht und danach
**„Anmeldung fehlgeschlagen."** sagt — **von einem falschen Passwort nicht zu unterscheiden**. Der
Zustand „die Datenbank ist gerade besetzt" erreicht ihn als **Falschauskunft über sein Passwort**.

**Punkt 49 verkürzt das, behebt es nicht:** die neue User-Frist von 5 s trifft `login/start`, also
wartet er künftig 5 s statt 81 — und hört dann dasselbe Falsche. Das Verschlucken in
`login/start.go` ist der eigentliche Posten.

**Lesen wird nicht lahmgelegt**, anders als im Prüfstand von Punkt 48: fünfte Anfrage nur lesend
**7,00 s / 200**, `GET /setup` **18 ms** — die Vertrauensliste kommt aus dem Zwischenspeicher.

**`E_UNREACHABLE` am Draht gibt es wirklich:** Datei unter der laufenden API weggenommen →
**503 in 50 ms**, Körper exakt `{"status":"error","code":"E_UNREACHABLE","message":"Dienst nicht
erreichbar."}`. Von 8 gleichen Anfragen 2 × 503, 6 × 200 — offene Verbindungen trugen weiter.

### Der Wortlaut, den der Benutzer liest

„**Dienst nicht erreichbar. Bitte später erneut versuchen.**" — Anmeldemaske
(`apps/web/public/assets/js/auth/login.js:336`) und Einrichtungsmaske (`auth/setup.js:64`), Grundtext
aus der eigenen Tabelle der Oberfläche (`contracts/wire.js:505`), gleichlautend mit
`shared/wire/wire.go:474`. **Die Oberfläche nimmt nie den `message` der Antwort** — ein genauerer
Satz vom Server käme beim Benutzer nicht an. Dieser Code liegt nur auf Zweig `web`; auf `main` hat
`apps/web/` zwei Dateien. **Gelesen, nicht gerendert.**

### Drei Pools

Unter echter HTTP-Last (16 Schleifen, 25 s, beide APIs): **13 728 Anfragen, 13 728 × 200**,
langsamste 245 ms, Sicherung als dritter Pool `backup-ok in 0,058 s` — **kein Engpass**. Erst mit
Fremdsperre: `backup-error after 10,005 s` — **eine** Frist, **keine** Wiederholung, wie
`migrate.Apply`.

**Behoben:** jeder andere Fehlschlag in `sqlite/backup.go` nennt seinen Schritt auf Deutsch; der
**einzige, den eine Sicherung unter Last wirklich trifft**, kam als nacktes englisches
`sqlite3: database is locked`. Jetzt nennt `busyOrStep` Schritt und Ursache. Rot vorher an der
Zusicherung selbst (`backup_test.go:781/784/787`), grün nachher; zwei Mutanten tot, eine Kontrolle
lebt; Paket 55 Fälle, 48 bestanden, 7 übersprungen (Symlink, Windows, laut), 0 rot.

**Offen:** `fault.go:94` bildet `state.ErrBusy` auf **`E_INTERNAL` (500)** ab, und der Kommentar
dort beruft sich darauf, dass **eine** API auf einer Datei liege. **Zwei liegen jetzt nachweislich
darauf.** Gehört zu P9. Ungemessen bleiben: echtes Rendern im Browser, ein echter Pi, und ob vier
blockierte Schreiber einen **angemeldeten** Weg (WebSocket, Kanäle) lahmlegen — die Messung reicht
nur bis `login/start`.

## Punkt 51: Die Prozessdecke und `nosuid`/`nodev` — und drei Fälle, die blind geblieben wären

> **Stand: gilt, gebaut.** Art: Sicherheit. **Erledigt Punkt 36.**
> **Offen und wartet auf Thomas: `noexec` auf `/data`.** Eine App-eigene
> Prozessgrenze wäre ein Manifestfeld → v1.3 **P13**.

**Gemessen und gebaut am 13.10.2026**, die zwei offenen Posten aus Punkt 36.

**Die Kommandozeile ist jetzt vollständig belegt**, Zeile für Zeile aus
`apps/server-controller/classes/runtime/podman.go`: `--network none` (242), `--cpus` (246),
`--memory` (247), `--read-only` (250), `--cap-drop ALL` (257), `no-new-privileges` (259), zwei
`--volume` (260, 263), `--user 0:0` (266, und 398 beim `exec`). Eine Behauptung über neun Schalter
ist damit eine Liste mit Fundstellen.

**Einhängungen: genau zwei.** `<DataDir>:/data:rw` — die einzige, deren Inhalt die App selbst
schreibt, und damit die, bei der eine Datei mit gesetztem suid-Bit (Upload, Rücksicherung) im
Container geachtet würde. Und `<Hold>:/ngc/ngc-hold:ro`, der Controller selbst. **Beide tragen
jetzt `nosuid,nodev`** — auch die `:ro`, weil eine Einhängeoption kein Urteil über den Schreiber
der Datei ist und nichts kostet. Nicht benannt werden podmans eigene `/proc`, `/sys`, `/dev`;
`nodev` auf `/dev` machte den Container unbrauchbar.

**`--pids-limit 512`, und die Zahl ist gemessen, nicht geraten:** ein Go-Programm in der Form des
hold-Prozesses (1000 Goroutinen, `GOMAXPROCS` 64) hält **24 OS-Threads** — der cgroup-Zähler zählt
Threads, nicht Prozesse. 512 ist Faktor 21 darüber und ein Viertel von podmans eigener Vorgabe
(2048), steht aber **im Repo**. Eine Fork-Bombe ist unbegrenzt: jede endliche Zahl stoppt sie, die
Frage ist nur, ob eine normale App hineinpasst. Der Fall prüft nicht nur die Zahl, sondern auch
`> 0` (0 und −1 heißen bei podman „unbegrenzt") und `>= 128` gegen den gemessenen Bedarf.

**Der Nebenbefund, der der eigentliche Fund ist:** drei bestehende Fälle lasen eine Volume-Angabe
als **einen String** und suchten mit `HasSuffix(":rw")`. Nach dem Anhängen von `,nosuid,nodev`
hätten sie **gar kein beschreibbares Volume mehr gefunden und wären grün geblieben**. Jetzt wird
die Angabe zerlegt (`volumesOf`/`optsOf`). Eine Zusicherung, die am Ende eines Strings hängt, ist
genau so lange wahr, bis jemand etwas anhängt.

**Zahlen:** `runtime` 143 → **145 Fälle**, 0 rot; ganzer Controller-Satz **989 Fälle, 67
übersprungen, 0 rot, dreimal von drei**; `tools/check.sh` ohne Pipe Code 0 mit `check: ok`;
`-race` 0 DATA RACE, belegt mit einer Wegwerf-Mutation, die im selben Lauf 2 Fälle rot macht.
Sechs Mutanten tot (Schalter weg, `0`, `64`, `nosuid` weg, `nodev` weg, hold ohne Optionen), eine
Kontrolle lebt.

**Nicht gemessen, und es ist dieselbe Grenze wie immer:** gemessen ist, dass wir die Schalter
**schicken** — nicht, dass podman sie einhält. Das gilt für alle elf. Offen bleiben: ob
`--pids-limit 512` eine echte Fork-Bombe stoppt und ob der `pids`-Controller rootless überhaupt
delegiert ist (ohne Delegation **warnt** podman statt zu scheitern); ob diese podman-Fassung
`nosuid`/`nodev` in der Optionsliste annimmt (ein Tippfehler würde erst auf einem Linux-Server
abgewiesen); und ob eine suid-Datei unter `/data` danach wirklich wirkungslos ist. Die 23
podman-Fälle (`fakePodman` ist ein `/bin/sh`-Skript) **überspringen sich auf Windows laut** —
`TestCreateBuildsTheArgumentList` muss auf Linux nachgemessen werden.

**Zu entscheiden (Nutzer):** `noexec` auf `/data`. Punkt 36 nennt es nicht, und es ist eine
Verhaltensänderung für jede App, die unter `/data` Skripte hält. Dazu vorgemerkt, kein Blocker
heute: eine App-eigene Prozessgrenze wäre ein Manifestfeld,
`shared/store/manifest.go:50–51` — eingefroren.

## Punkt 52: Die Lüge über das Passwort ist weg — über die Unsymmetrie, nicht über die Meldung

> **Stand: gilt, gebaut.** Art: Sicherheit. Behebt **Punkt 50**. Der hier offen
> gebliebene Posten (`login/live.go:313`) ist in **Punkt 53** gebaut.
> ⚠ **Berichtigt durch Punkt 54:** `E_BUSY` (**P9**) ist ein Bruch, keine Ergänzung.

**Gemessen und gebaut am 13.10.2026**, der Posten aus Punkt 50.

**Rot vorher an der Zusicherung selbst:** `known: 200, want 503` **mit gültigem
Challenge-Körper**, `Retry-After ""`, im Protokoll nur „a login challenge could not be kept".
Beide Namen 200. Die verschluckende Stelle war der `OnceCreate`-Aufruf **nur `if known`**.

**Der Weg dorthin ist der eigentliche Gedanke:** die Verschluckung existierte **nur** wegen der
Unsymmetrie — der Schreibvorgang war der eine Aufruf, den ein bekannter Name macht und ein
unbekannter nicht. Also zuerst die Unsymmetrie weg, dann wird die Antwort möglich: **jeder Name
hält jetzt einen Nonce.** Der Scheinname unter einer frischen `secrets.ID()` — gleiche Form wie
eine echte Benutzer-ID, ID von niemandem. `finish.go` prüft `nonceUser != cert.User` **nach**
`session.VerifyClient` gegen die Vertrauensliste, der Scheinnonce kann also niemanden anmelden.
Eine **feste** Scheinkennung wurde verworfen: sie wäre ein Namensorakel für jeden mit
Datenbank-Lesezugriff.

**Nur „besetzt" wird beantwortet** (503, `Retry-After: 5`, `reason=db-busy`), und die Begründung
ist die Grenze selbst: besetzt ist ein Zustand der **Installation** — ein anderes Programm hält die
Datei, 80,6 s aus Punkt 48 — und kann darum nicht für bekannt/unbekannt verschieden ausfallen.
**Alles andere bleibt verschluckt** (volle Platte, Lesereplik, Kontingent): das hängt am **Inhalt**
der Zeile, und die unterscheidet sich zwischen den Namen um genau einen Wert. Ein Speicher, der die
echte ID nimmt und die Schein-ID ablehnt, gäbe das Orakel **umgekehrt** zurück. Die sicherere
Variante ist hier die sparsamere Antwort.

**Das Orakel ist mit Zahlen ausgeschlossen, nicht mit einem Argument:**
`TestStartMakesTheSameDatabaseCallsForEveryName` ist von `len(known) == len(unknown)+1` auf
**`slices.Equal`** verschärft — die Aufruflisten sind **identisch**. Bei besetzter Datenbank für
beide Namen: 503, **gleiche Kopfzeilen** (`maps.EqualFunc`) und **gleicher Körper**, Protokoll ohne
Name. Die Zeitgleichheit — der eigentliche Riegel — allein nachgefahren: **0 rot von 10**, und
**0 rot von 10 mit `GOMAXPROCS=1`**.

**Zahlen:** `login` unter `-v` **114 → 116 bestanden**, 0 rot; `./apps/server-api/...` 22 Pakete ok;
`tools/check.sh` ohne Rohr `check: ok`, Code 0. Drei Mutanten tot (Besetzt-Riegel aus; Schreiben nur
für bekannte Namen; kein Scheinname), eine Kontrolle lebt. `-race` 0 über drei Pakete, belegt mit
einer Wegwerf-Mutation (Schloss aus `warnThrottle.take`, 8 gleichzeitige `Start`) → **2 DATA RACE**.

**Ein Zwischenfall, der fast als Messung durchgegangen wäre:** der erste Race-Mutant lief mit
`-run TestShare` — das traf **keinen** Fall und meldete „0 DATA RACE". Seitdem prüft das Messskript
die `=== RUN`-Zahl und bricht bei 0 ab. Als Regel in `messen.md`.

**`login/live.go:313` ist für diese Änderung nicht im Weg** (`Start` ruft `Live` nicht auf, 0
Treffer) — **steht aber anderswo im Weg**, und das ist belegt: `Live` gibt `(*Live, wire.Code,
bool)` zurück, der Fehler ist an Zeile 313 weg; seine beiden Aufrufer `login/trust.go:33` und
`browser/browser.go:121` machen nur `httpapi.Fail(w, code)` und können darum **kein**
`Retry-After` setzen. Die Signatur zu drehen fasst `browser/` an — offen.

**BLOCKED:** `shared/wire/wire.go:450–462` (`E_BUSY`, P9) — ein Klient, der nur den **Code** liest,
kann „besetzt" weiter nicht von „weg" trennen. Dazu `shared/dbapi/dbapi.go:109–111`.

**Ungemessen:** der echte 80-s-Fall mit zwei Prozessen über HTTP **nach** der Änderung; was der
Benutzer im Browser bei 503 + `Retry-After` liest; die zusätzliche Schreiblast durch Nonce-Zeilen
für unbekannte Namen (Ablauf über `session.ChallengeLifetime` + `OncePurge`) — benannt, nicht
gemessen. `login/finish` und `logout` antworten weiter ohne `Retry-After`.

## Punkt 53: Der Fehler überlebt `Live` — und er ging an drei Stellen verloren, nicht an einer

> **Stand: gilt, gebaut.** Art: Messung. Schließt den offenen Posten aus **Punkt 52**.
> ⚠ **Berichtigt durch Punkt 54:** `E_BUSY` (**P9**) ist ein Bruch, keine Ergänzung.

**Gemessen und gebaut am 13.10.2026**, der offene Posten aus Punkt 52.

**Die Fundstelle war größer als gedacht:** `login/live.go:212` gab `(*Live, wire.Code, bool)`
zurück, und der Fehler fiel an **drei** Stellen heraus — `sessionRow`/`SessionGet`, `Refresh`
(die in Punkt 52 genannte Zeile 313) und `SessionTouch`. Die Aufrufer `login/trust.go:33` und
`browser/browser.go:121` antworteten darum `503` mit `Retry-After: ""` und einem Protokoll **ohne
Grund**. Rot vorher an genau diesen Zusicherungen, sechsmal, über Trust, Logout, Finish, Start und
Socket.

**Gebaut:** `httpapi.FailCoded(w, code, err)` — **der Code entscheidet die Antwort, der Fehler nur
Header und Protokollzeile**. Ausdrücklich **nicht** `FailFault` direkt: `Fault` bildet
`dbapi.ErrNotFound` auf `E_INTERNAL` ab, `sessionFault` auf `E_LOGIN` — ein Neuberechnen hätte das
**still gekippt**. Genau dafür gibt es einen Mutanten (M5, 5 Fälle tot). `Live` und `sessionRow`
geben den Fehler neben dem Code zurück; mitgenommen wurden `logout` (2 Stellen), `finish` (alle
fünf `E_UNREACHABLE`) und in `start` die **zwei Lesevorgänge** vor der Namenssuche — Punkt 52 hatte
dort nur den Schreibvorgang.

**Zahlen:** `=== RUN` **206 in beiden Läufen** (identische Fallzahl, also vergleichbar) — vorher
**5 rot**, nachher **0**; 8 neue Fälle. 22 Pakete ok. `-race` 206 Fälle, 0 DATA RACE, belegt mit
einer Wegwerf-Mutation, die im selben Lauf 2 zeigt. Sechs Mutanten tot, Kontrolle lebt (121 Fälle).

**Kein neues Orakel, mit Zahlen belegt:** vier Fälle vergleichen Status, **jede Kopfzeile mit
jedem Wert** und Körper — echtes gegen erfundenes Cookie (Trust und Logout), zwei Token am Socket,
ein Schlüssel aus der Vertrauensliste gegen einen fremden (die einzige der fünf finish-Stellen, die
ihre Zeile danach wählt), bekannter gegen unbekannten Namen. **Diese vier sind vorher und nachher
grün** — sie sind der Riegel, nicht der Beleg der Änderung. Sie können halten, weil besetzt ein
Zustand der **Installation** ist: der Lesevorgang scheitert **vor** jeder Zeile.

**BLOCKED:** unverändert `shared/wire/wire.go:450–462` (`E_BUSY`, P9), dazu
`shared/dbapi/dbapi.go:109–111` und in der Folge `apps/server-api/classes/state/state.go:29–32`.

**Ein vierter Zeitflattrer, und er erklärt sich selbst:** `TestFinishTakesTheSameTimeForAKeyNobody
Holds` war in einem von zwei `check.sh`-Läufen rot (174,57 s), **0 Treffer** auf die
Laufzeit-Trennlinie — nach den Regeln also ein echter Befund. In der Ausgabe steht aber der
**Null-Kontrollzweig des Falls selbst**: `286,087µs` gegen `584,965µs`, „this machine cannot measure
a factor of two today". Allein 0 rot in 3 Läufen. **Nachgemessen und freigemessen:** 1 rot von 10 an der Änderung
allein, 0 von 10 am Grundstand `bb52ccf` — das trennt nach `messen.md` nichts. Getrennt hat erst
der **gepaarte** Lauf: zehn verschachtelte Paare, gleicher Maschinenzustand je Paar, **0/10 gegen
0/10**. Die Änderung ist damit frei. **Ungemessen bleibt** nur, ob derselbe Fall unter der vollen
Baumlast von `check.sh` am Grundstand genauso rot wird — dort wurde er nur allein gemessen.

**Offen mit Zeile:** `join/join.go:120` und `controllers/pair.go:93` antworten weiter mit nacktem
`httpapi.Fail` (laufen nicht über `httpapi.Routes.Mux`, haben auch keine Frist);
`setup/setup.go:95–98` macht aus jedem `E_UNREACHABLE` bewusst `E_INTERNAL`; die Hintergrundrunden
haben weder Frist noch Grund. Und was der Benutzer bei `503 + Retry-After` **liest**, ist weiter
ungemessen — die Oberfläche nimmt den `message` der Antwort nie (`contracts/wire.js:505`).

## Punkt 54: „Ergänzung, kein Bruch" war falsch — ein neuer Code wirft den ganzen Rahmen weg — berichtigt Punkt 49, 52 und 53

> **Stand: gilt.** Art: Messung. ⚠ **Dieser Punkt berichtigt Punkt 49, 52 und 53**
> in dem, was sie über **P9** gesagt haben, und ebenso **P8** aus Punkt 35.
> Folge für die v1.3-Reihe: **P10** ist neu und das Tor, **P4** ist erledigt und
> gestrichen, **P10–P13** sind dazugekommen — und die **alte** Reihe hatte
> `P6`, `P7`, `P8` **doppelt** vergeben (jetzt **N1–N3**).

**Gemessen am 15.10.2026**, und es berichtigt, was die Punkte 49, 52 und 53 über P9 gesagt haben.

Die v1.3-Reihe nannte einen neuen Schließgrund (P8) und einen neuen Fehlercode (P9) jeweils eine
**Ergänzung**. Eine Sonde gegen den HEAD-Stand von `shared/wire` zeigt das Gegenteil:

```
ParseClose(code=E_BUSY)        -> code=""              err=wire: malformed CLOSE payload
ParseClose(code=E_UNREACHABLE) -> code="E_UNREACHABLE" err=<nil>
```

Die Zeile dahinter ist `shared/wire/wire.go:528` in `Close.Validate`: ein unbekannter Code fällt
in `ErrClose`. Ein Leser, der `E_BUSY` nicht kennt, sieht also **nicht** „einen Schluss ohne
Grund" — er sieht einen **kaputten Rahmen** und wirft ihn weg. `shared/README.md:199–200` macht
daraus die Regel für alles: „unbekannte Felder werden abgelehnt."

**Und es gibt keine Protokollfassung.** Das einzige `Version`-Feld (`shared/tunnel/tunnel.go:151`)
ist die Fassung einer **App**. Zwei Seiten können sich nicht darauf verständigen, was sie beide
verstehen — während API, Controller und Oberfläche **einzeln** aktualisiert werden (`trust.AutoAPI`,
`AutoController`, `AutoWeb`, `shared/tunnel/tunnel.go:164`). Eine neue API mit `E_BUSY` an einem
alten Controller heißt: abgewiesener Rahmen.

**Folge für die Reihe:** daraus wurde **P10**, und P8 wie P9 hängen jetzt daran — erst die
Verträglichkeit, dann der neue Code. Die Reihe ist dabei neu geordnet (die alte hatte `P6`, `P7`,
`P8` **doppelt**), P4 ist **erledigt und gestrichen**, und vier Posten sind dazugekommen
(P10–P13). Die Vorlage steht in `docs/vertraege-v1-3-vorschlag.md`: je Punkt der fertige
Änderungssatz, der Preis, was ohne ihn für immer fehlt, und eine Empfehlung — mit JA oder NEIN zu
beantworten.

**Die Lehre, und sie gilt über dieses Projekt hinaus:** ob eine Änderung ein Bruch ist, entscheidet
nicht die Form („wir fügen nur etwas hinzu"), sondern der **Leser der alten Fassung**. Das muss man
messen, nicht argumentieren. Steht in `messen.md`.

## Punkt 55: Punkt 38 ist gebaut — dreizehn Fangstellen, und keine ist wie die andere

> **Stand: gilt, gebaut.** Art: Sicherheit. **Erledigt Punkt 38** — die dort offene
> Entscheidung ist damit getroffen.

**Gebaut am 16.10.2026**, API und Controller, und auf `main` nachgeprüft: `go build` 0,
`go test ./apps/...` **43 Pakete ok, 0 rot, Code 0**.

**Die Abwesenheit war belegt, nicht behauptet:** `recover()` kam in `apps/server-api`,
`apps/server-controller`, `apps/server-db`, `apps/web`, `shared`, `tools` und `cmd` **nirgends**
vor — die Liste der Orte, nicht eine Zahl. Folge: eine einzige Fehlstelle in einem Handler nahm
`ngc-api` ganz mit, samt aller 128 App-Kanäle jeder Verbindung und der Controller-Tunnel.

**Gefangen wird ein FEHLER, nie eine Absage.** Ein Kanal, der einen Code antwortet, antwortet ihn
weiter auf dem gewöhnlichen Weg. Neu ist `apps/server-api/classes/guard`.

**Dreizehn Fangstellen, nach dem unterschieden, was danach noch möglich ist** — das ist der
Gedanke, der den Punkt löst:
- **Im Kanal** (`browser.channel`, `coreapp.open`, `relay.toController`, `relay.sendStream`): der
  Kanal endet mit `E_INTERNAL`, der Browser hört also, dass etwas kaputt ist, statt auf eine
  Antwort zu warten, die nie kommt.
- Drei davon schreiben `E_INTERNAL` zusätzlich in den **Prüfeintrag** — der Mangel steht dann in
  der Datenbank, nicht nur in einer Datei, die jemand wegdreht. `browser.channel` kann das nicht:
  der Aufruf, der den Eintrag geschrieben hätte, ist genau der, der vorher gestorben ist.
- **In einer Runde oder einem Haken ohne Kanal** (`server.round`, `server.rollout`, drei
  Rückhalte in `Server.serve`, `controllers.hello`, `coreapp.pushTrust`, `link.closeBounded`):
  dort folgt **nichts** außer Zeile und Zähler. Kanal 0 in der Zeile sagt genau das.

**Gegen das Verschlucken (Punkt 50) gesichert:** `Panics()` ist die Zahl, `Report()` bringt sie
einmal je Minute aus der Hausrunde dorthin, wo jemand liest — und schweigt, solange die Zahl nicht
wächst. Ein Mangel, der einmal pro Stunde feuert, ist damit eine **Zahl und kein Gefühl**, und er
übersteht ein weggedrehtes Protokoll.

**Was NICHT in der Zeile steht, und es ist gemessen, nicht gemeint**
(`TestTheLineCarriesNoValueOfThePanic`): der **Wert** der Panik — nur ihr Go-**Typ**. Ein
Panikwert wird von dem geschrieben, der gestorben ist, und trägt, was der gerade in der Hand
hatte: eine Kennung, ein Passwort, einen Namen, eine Datenbankzeile. Für den Stapelabzug gilt
dasselbe, denn Go druckt dort die Argumentwörter jedes Rahmens. Stattdessen steht die **eine
Stelle, die zählt**: die Zeile, die gestorben ist, als die letzten zwei Teile ihres Pfades.
Aus Anfrage, Sitzung, Benutzer oder Parametern wird hier **gar nichts** gelesen — der Aufruf
übergibt eine Kanalnummer und einen festen Ortsnamen, einen dritten Weg hinein gibt es nicht.

## Punkt 56: Das kleine System läuft — auf echtem Debian, mit drei Befunden auf dem Weg — Punkte 1, 3 und 4 zu seit Punkt 57/59/61

> ⚠ **Teilweise überholt.** Offener Punkt 1 (Release-Schlüssel) ist **zu seit
> Punkt 57**, ebenso der fehlende Riegel aus Befund 1. Punkt 3 (Anmeldung im
> Browser) ist **zu seit Punkt 59**, Punkt 4 (Kopplung) **zu seit Punkt 61**.
> **Offen bleibt der Store-Schlüssel** und die fünfte Schutzeinstellung aus
> Befund 3. Art: Umgebung.
> ⚠ **Ein Satz dieses Punktes ist gemessen falsch:** „Damit ist auch **P3**
> beantwortet". Gemessen am 18.10.2026 ist die Form, die hier steht (Namen im
> Wurzelverzeichnis), **nicht** die, die der Controller beim Update auspackt
> (Namen unter `ui/`) — `store.WalkUI` weist das Wurzel-tar mit `ErrPackage`
> ab. P3 ist damit nicht beantwortet, sondern **dringender**. Einzelheiten im
> Nachtrag zu P3 in [`vertraege-v1-3-vorschlag.md`](vertraege-v1-3-vorschlag.md).

**Gemessen am 16.10.2026** auf **Debian 13 (trixie), Apache 2.4.69, PHP 8.4, podman 5.4.2**,
systemd läuft (WSL2-Instanz, also ein echtes Debian-Userland; was nur dort gilt, steht unten).

**Das Ergebnis zuerst:** `curl -sk https://ngc.test/` gibt **`http=200`, 451 Byte**, und es ist
NGCs eigene Seite (`<title>NGC</title>`). `GET /api/v1/setup` **durch Apache** liefert die
unterschriebene Installationsauskunft — dieselbe wie direkt an Port 7443. Die Strecke Browser →
Apache → Balancer → API **trägt**.

**Dafür musste es zuerst ein Release geben, und es gibt jetzt eins:** `ngc-store release` über
drei Teile, je mit `ngc-store check` geprüft — **`release ok`** für `ngc-web all`,
`ngc-api amd64` und `ngc-controller amd64`. Damit ist auch **P3 beantwortet**, gemessen am Code
der Skripte: `ngc-web-all.tar` trägt im Wurzelverzeichnis `public/index.php` und
`templates/apache-vhost.conf` (83 Einträge, keine Verknüpfungen); `ngc-api-amd64.tar` trägt
**genau zwei** gewöhnliche Dateien, `ngc-api` und `ngc-store`, ohne Ordner;
`ngc-controller-amd64.tar` genau eine. Jede andere Form weist das Skript ab.

### Befund 1: der Store-Platzhalter landet in `api.json`, und die API lehnt ihn ab

`ngc-all.sh` hat einen Riegel für den **Release**-Schlüssel (`*REPLACE-THIS-LINE*` → Abbruch mit
klarem Satz), aber **keinen für den Store-Schlüssel**. Folge, wörtlich aus dem Protokoll:

```
ERROR die Konfiguration lässt sich nicht lesen file=/etc/ngc/api.json error="config: invalid configuration"
```

In der Datei stand `"storeKey": "REPLACE-THIS-LINE-WITH-THE-NGC-STORE-KEY"`. Dazu zwei Posten,
die zusammengehören: **das Skript meldete `RC=0` und „The API is running"**, während der Dienst in
einer Neustartschleife lag (`restart counter is at 8`) — „gebaut ist nicht gelaufen", hier als
„eingerichtet ist nicht gestartet". Und die Fehlermeldung der API nennt **kein Feld**; welches
es war, kostete einen Blick in die Datei. Mit einem gültigen Schlüssel in der Antwort: Dienst
`active`, Port 7443 belegt, Zertifikat da, Fingerabdruck und Einrichtungscode gedruckt.

### Befund 2: die Balancer-Zeile des Vertrags weist Apache ab

```
AH00526: Syntax error on line 19 of /etc/ngc/apache-apis.conf:
ProxyPass unknown Balancer parameter
```

`upgrade=websocket` stand am **`ProxyPass`** eines `balancer://`-Ziels — dort liest Apache jedes
`schlüssel=wert` als **Balancer**-Parameter. Es ist aber ein **Worker**-Parameter und gehört an
den `BalancerMember`. Verschoben sagt derselbe `apachectl configtest` **`Syntax OK`**.
Die Planzeile daneben („`upgrade=websocket` braucht Apache 2.4.47; Debian 13 bringt es mit") ist
**richtig in der Fassung und falsch im Ort** — genau die Sorte Satz, die niemand nachprüft.

Behoben an allen Stellen: `shared/ulh.md` (die eine gewährte Ausnahme der Einfrierung),
`apps/server-controller/templates/apache-apis.conf.tmpl`, dessen Prüfdaten, und `ngc-web.sh` in
ulh. **Belegt als rot-vorher/grün-nachher am laufenden Apache** — und mit einem neuen Fall
`TestWorkerParametersStayOnTheBalancerMember`, der genau das fängt, was die byteweise Vorlage
**nicht** fangen kann: mit der Mutation (Parameter zurück an `ProxyPass`, in Vorlage **und**
Prüfdatei) **besteht** `TestRenderMatchesTheTemplate` weiter — es vergleicht sich mit sich selbst —
und der neue Fall **fällt**. Kontrollmutation (nur eine lokale Variable umbenannt) **lebt**.

### Befund 3: eine Schutzeinstellung weniger, als NGC mitbringt

`⚠ NGC brings 5 protective settings for the files of the interface, and this version of ngc-web
knows 4 of them.` Das Skript sagt es laut und macht weiter — richtig so, aber die fünfte ist
**nicht gemessen**. Welche es ist, gehört geklärt, bevor jemand sie für vorhanden hält.

### Zwei Umgebungsfallen, die als Programmfehler aussahen

- **`/tmp` in WSL ist ein 32-GB-tmpfs und war zu 100 % voll** (zwei ulh-Prüfläufe, 13 und 19 GB).
  apt zerlegt seine Signaturen dort und meldete dann
  `Splitting up ... into data and signature failed` und `Write error (28: No space left on device)` —
  also **OpenPGP-Fehler als Erscheinung einer vollen Platte**. `TMPDIR` auf die ext4-Platte gelegt:
  alles läuft.
- **`TMPDIR` auf `/mnt/d` geht nicht:** dpkg verlangt Verknüpfungen,
  `Symlinking ... failed - DPkg::Go (1: Operation not permitted)`. Es muss ein Linux-Dateisystem sein.

### Was offen ist — und einer davon ist deine Entscheidung

1. **Der echte Release-Schlüssel von NGC fehlt**, und das ist der Riegel, an dem jede Installation
   heute hält: „This script has no real release key of NGC yet." Gemessen wurde mit einem
   **Testschlüssel**, der nirgends eingecheckt ist. Der echte Schlüssel gehört laut Entwurf 4.8 auf
   einen Hardware-Stick; sein **öffentlicher** Teil muss in `ngc-all.sh`, `ngc-api.sh`,
   `ngc-controller.sh`, `ngc-web.sh` und `ngc-db.sh` an die Stelle von
   `REPLACE-THIS-LINE-WITH-THE-NGC-RELEASE-KEY`. **Ohne diesen Schritt kann niemand NGC
   installieren** — das ist kein Programmfehler, sondern ein fehlendes Schlüsselpaar.
2. Dasselbe für den **Store-Schlüssel** (`REPLACE-THIS-LINE-WITH-THE-NGC-STORE-KEY`), plus der
   Riegel aus Befund 1.
3. **Die Anmeldung im Browser ist nicht gemessen.** Die Seite kommt, der Einrichtungscode ist
   gedruckt, aber den ersten Verwalter anzulegen braucht einen Passschlüssel — und `ngc.test` mit
   selbst unterschriebenem Zertifikat ist dafür der nächste Messpunkt, nicht dieser.
4. Der **Controller ist nicht gekoppelt** („no app can run"), weil das über die angemeldete
   Oberfläche läuft. Die Kette endet hier also nicht an der Technik, sondern an Punkt 3.

### Nachtrag vom 23.10.2026: PR 71 „ein Schreibkanal je Prozess" — entscheidungsreif, zwei Wege, ein Vorschlag

> **Zur Nummer:** PR 71 trägt den Titel „Point 56", seine Commits sagen „Point 49", und
> gemessen wurde die Sache in **Punkt 48**. Der Nachtrag steht hier, weil der PR diese
> Nummer trägt; **der Punkt, den er fortsetzt, ist Punkt 48/49.** Wer dort liest, findet
> die Vormessung.

**Was der Umbau ändert.** Heute (main) hat eine SQLite-Datei **einen** Pool mit **vier**
Verbindungen; Lesen und Schreiben teilen ihn, und ein wartender Schreiber **hält** seine
Verbindung. Der PR macht daraus **zwei** Pools auf derselben Datei: einen Schreibgriff mit
**einer** Verbindung und einen Lesegriff mit **vier** (`sqlite.OpenRW`, `sqlstore.NewSplit`).
Dazu eine Schranke, wie lange ein Schreiber **vor** dem Schreibgriff wartet (`takeTurn`),
mit einem neuen Fehler `ErrTurn`. **Der Titel führt in die Irre:** die Vier ist nicht weg,
sie ist auf die **Lesevorgänge** gewandert.

**Umfang:** 12 Dateien, +857/−69, **alle unter `apps/server-db/`**.
**`shared/` ist unberührt** — gegen die heutige Spitze geprüft, nicht angenommen. **Nicht BLOCKIERT.**

#### Gemessen am 23.10.2026 — beide Seiten, derselbe Prüfstand

Debian 13 (WSL2), go1.27.2, ext4, 128 Kerne. **Ein** Messprogramm, das auf **beiden** Bäumen
unverändert übersetzt (es benutzt nur `Open`, `OpenDB` und `dbapi.Store` — auf beiden Seiten
gleich), je gegen main `3a9a3e2` und gegen den PR auf main nachgezogen. Die Schreibsperre hält
ein **echter zweiter Prozess**, denn die SQLite-Sperre ist eine Sperre auf der Datei.
Jede Messung prüft ihre eigene Voraussetzung und bricht ab, statt eine Zahl zu nennen:
alle Schreiber müssen in der Luft sein, die Sperre muss beim Messpunkt noch halten,
der Lesevorgang muss wirklich Daten zurückgegeben haben.

**1. Was die heutige Grenze von vier kostet.** Wartezeit des **ersten Lesens** des Prozesses,
hinter K blockierten Schreibern desselben Prozesses:

| blockierte Schreiber | main (ein Pool von 4) | PR 71 (geteilt) |
|---|---|---|
| 2 | 0,001 s | 0,001 s |
| **4** | **9,490 s** | **0,001 s** |
| 8 | 9,491 s | 0,001 s |

Befehl: `NGC_AB=1 go test -count=1 -v -run '^TestABReadBehindBlockedWriters$' ./apps/server-db/classes/sqlite/`

Das ist die Zahl aus Punkt 48, heute unabhängig nachgemessen: **bei vier kippt es**, und zwar
für **jede** Abfrage des Prozesses, auch jedes Lesen. Der Aufrufer bekommt `E_UNREACHABLE` (503)
— von „Datenbank weg" nicht zu unterscheiden.

**2. Was ein einziger Kanal kostet — Durchsatz.** 16 Fäden, 10 s, Datei sonst unbenutzt,
je **drei** Läufe:

| | Lauf 1 | Lauf 2 | Lauf 3 | Fehler |
|---|---|---|---|---|
| main | 3045,4/s | 3009,1/s | 3008,7/s | 0 |
| PR 71 | 3251,2/s | 3291,1/s | 3225,3/s | 0 |

Befehl: `NGC_AB=1 go test -count=1 -v -run '^TestABWriteThroughput$' ./apps/server-db/classes/sqlite/`

**+7 bis +8 %**, die Bänder überschneiden sich nicht, 3 von 3 reproduziert. Der ein Kanal
kostet hier **keinen** Durchsatz, er bringt welchen. **Achtung, umgebungsabhängig:** der PR
nennt für Windows 404→625/s (C:) und 241→530/s (Z:, NAS), also +55 bis +120 %. Auf diesem
Rechner sind es +7–8 %. **Die Richtung ist auf beiden Ständen dieselbe, die Höhe nicht.**

**3. Was passiert, wenn der eine Kanal hängt.** Ein fremder Prozess hält die Sperre die ganze
Zeit und gibt sie nie zurück; drei Schreiber ohne eigene Frist:

| | main | PR 71 |
|---|---|---|
| Schreiber 0 | 80,614 s · `sqlite3: database is locked` | 80,000 s · `sqlstore: no turn at the write handle came free` |
| Schreiber 1 | 80,600 s · `database is locked` | 80,000 s · `no turn at the write handle came free` |
| Schreiber 2 | 80,695 s · `database is locked` | 80,537 s · `database is locked` |
| **gesamt** | **80,696 s** | **80,537 s** |
| Lesen währenddessen | 0,001 s | 0,001 s |

Befehl: `NGC_AB=1 go test -count=1 -v -timeout 25m -run '^TestABWriteChannelStuck$' ./apps/server-db/classes/sqlite/`

**Die Schranke hält:** die Warterei wächst **nicht** mit der Zahl der Schreiber, der ganze
Vorgang bleibt bei **einer** Kette (80,6 s), genau wie main. Das Lesen war hier auf **beiden**
Seiten schnell — bei nur drei Schreibern hatte auch main noch eine vierte Verbindung frei;
der Unterschied steht in Messung 1, nicht hier.

**4. Wo der neue Fehler landet.** `Fault(sqlstore.ErrTurn, FromRows)` = **`E_UNREACHABLE`**
(503, „komm wieder"). Gemessen, nicht gelesen. Das ist die richtige Antwort — eine volle
Warteschlange geht von selbst vorbei — und sie braucht **keine** Änderung in `shared/`
und **keine** in `fault.go`. Der Text ist dabei **besser** als heute: er nennt die
Warteschlange dieses Prozesses statt „Dienst nicht erreichbar".

#### Der rote `-race`-Lauf: die Ursache liegt NICHT im Umbau

Der rote Lauf vom 18.10.2026 fiel an
`TestAPingLostToOurOwnWriteDoesNotKillTheTunnel` in
`apps/server-controller/classes/link` — ein Paket, das dieser PR **nicht anfasst**
(alle 12 Dateien liegen unter `apps/server-db/`). Die drei `mariadb`-Namen im Protokoll
stehen in der Liste der **übersprungenen** Fälle, nicht der gefallenen.

Gegenprobe auf main, **ohne jeden PR-Code** (10 Läufe je Stand, `-race`, `-count=1`):

| Stand | Ergebnis |
|---|---|
| main `b853f6e` (= main am 18.10., der Stand, gegen den CI merzte) | **6 von 10 FAIL** |
| main heute (`3a9a3e2`) | 10 von 10 bestanden |
| PR 71 auf main heute nachgezogen | 10 von 10 bestanden |

Befehl: `go test -race -count=1 -run 'TestAPingLostToOurOwnWriteDoesNotKillTheTunnel' ./apps/server-controller/classes/link/`

Also: **ein Zeitfehler im Prüffall auf main, schon vorher da**, von main selbst am
20./21.10.2026 behoben (`e723f0d` „give the tunnel ping timing test a null control",
`ec27cc5` „the null control had a red branch it could never justify"). Dass der
`push`-Lauf derselben Kennung grün und der `pull_request`-Lauf rot war, passt dazu:
`pull_request` prüft den **Merge** mit main.

**Ganzer Satz auf dem nachgezogenen Stand, heute:** `3412 Fälle bestanden, 0 gefallen,
0 DATA RACE` über 73 Pakete (ohne `shared/trust`, wie die CI).
*Ein* Fall (`TestRealArchiveIsClosed`) fiel zunächst, weil meine Messkopie ohne `.git`
lief — der Fall sagt das selbst laut; mit git bestanden, einzeln nachgemessen.

#### Nicht gemessen, abgewogen

- **Speicher der Pools auf dem Höhepunkt einer Sicherung.** Es sind jetzt **vier** Pools statt
  drei. Der PR nennt das selbst als offen. `maxMemory` ist eine **Decke, keine Reservierung**,
  und der Lesegriff bindet keinen Blob — aber eine Zahl dafür gibt es nicht, und auf einem Pi
  ist das der teuerste Posten.
- **Zwei echte `ngc-api` über die ganze HTTP-Strecke.** Unverändert offen aus Punkt 48.
- **Auf einem echten Pi und auf dem NAS (Z:).** Meine Zahlen sind ext4 auf 128 Kernen.
- **Was der Benutzer im Browser bei `E_UNREACHABLE` liest.** Offen aus Punkt 48/49.
- **Die 190–240 s, die der Umbau vor der Schranke selbst verursacht hat.** Steht so im
  Kommentar des PR (gemessen 16.10.2026); ich habe sie **nicht** nachgemessen, sondern nur,
  dass die Schranke heute hält (Messung 3).

#### Zwei Wege, je mit Preis

**Weg A — zusammenführen (den geteilten Griff nehmen).**
*Preis:* ein Pool mehr je Store, und was die vier auf dem Höhepunkt einer Sicherung halten,
ist **nicht gemessen**; ein neuer Fehler `ErrTurn`, den kein Aufrufer beim Namen kennt
(er landet richtig auf 503, aber ein Leser muss ihn lernen); der Schreibweg hat jetzt eine
eigene Warteschlange, deren Schranke beim Öffnen von der Datenbank abgelesen wird;
+857 Zeilen im empfindlichsten Weg des Programms.
*Gewinn:* die Klippe bei vier ist weg (0,001 s statt 9,490 s, gemessen); +7–8 % Schreibdurchsatz
hier, mehr auf Windows; ein hängender Schreiber legt nicht mehr jedes Lesen des Prozesses still;
der Fehlertext nennt die Ursache statt „Dienst weg".

**Weg B — liegen lassen (bei vier bleiben).**
*Preis:* vier wartende Schreiber legen **jede** Abfrage des Prozesses lahm, Lesen eingeschlossen
— **9,49 s, heute auf main nachgemessen**. Das ist nicht „langsam", das ist für den Aufrufer
nicht von einer toten Datenbank zu unterscheiden. Vier wird erreicht, sobald ein zweiter
schreibender Prozess da ist: Sicherung, zweites API, `ngc-all.sh`. **Die Grenze ist vier,
nicht „soundso viele Benutzer".**
*Gewinn:* nichts Neues zu lernen, kein zweiter Pool, kein neuer Fehler; main bleibt, wie
Punkt 48/49 es gemessen hat.

#### Vorschlag: Weg A, zusammenführen

Die Messung, die entscheidet, ist Messung 1. **9,49 s bei vier blockierten Schreibern** ist
keine Durchsatzzahl, sondern ein Stillstand des ganzen Prozesses, den der Aufrufer nicht von
einer toten Datenbank unterscheiden kann — und vier ist erreicht, sobald eine Sicherung und
zwei Benutzerschreibvorgänge zusammenkommen. Genau das nimmt der PR weg (0,001 s, mit
**demselben** Prüfstand auf **beiden** Seiten gemessen). Er nimmt dafür nichts: der Durchsatz
steigt, er fällt nicht. Er rührt keinen eingefrorenen Vertrag an. Und sein einziges echt neues
Risiko — die unbegrenzte Warteschlange, die er zuerst selbst erzeugt hat — hat er selbst
gemessen und beschränkt; dass die Schranke hält, steht in Messung 3.

Offen bleibt der Speicher auf dem Höhepunkt einer Sicherung. Das ist eine Frage an einen Pi,
nicht an diesen Merge: es ist eine Decke und keine Reservierung, und es ist messbar, sobald
ein Controller auf dem Tisch liegt. Dagegen steht ein Stillstand, der **heute in main** ist.

**Zustand des PR:** auf main (`3a9a3e2`) nachgezogen, konfliktfrei, `shared/` unberührt,
ganzer Satz grün, `-race` grün (0 DATA RACE). **Nicht zusammengeführt** — ein Ja genügt.

## Punkt 57: Der Riegel ist auf — NGC hat einen Release-Schlüssel, und nichts wird mehr ersetzt

> **Stand: gilt.** Art: Umgebung. Das hier noch Ungemessene (Anmeldung im Browser,
> Kopplung) ist in **Punkt 59** und **Punkt 61** gemessen. **Offen und wartet auf
> Thomas:** der geheime Schlüssel gehört auf einen **Hardware-Stick**, und der
> **Store-Schlüssel** ist weiter ein Platzhalter.

**Gemessen am 17.10.2026** auf demselben Debian 13 wie Punkt 56, diesmal **ohne jede Ersetzung**:
der öffentliche Schlüssel steht in den Skripten, der geheime unterschreibt.

**Das Schlüsselpaar.** Erzeugt in einem Linux-Dateisystem, damit die Rechte echt sind:
der geheime Teil liegt als `/root/ngc-keys/release.key` mit **`-rw-------`**, nur für root, und ist
**in keinem Repo**. Der öffentliche Teil steht in `ngc-all.sh`, `ngc-api.sh`,
`ngc-controller.sh` und `ngc-web.sh` an der Stelle des Platzhalters — `ngc-db.sh` brauchte keinen,
es holt keine Programmdateien. **Kennung `pN6Z6a9NR0aG6aEJIJ5EHg`**, und die Zahl, die zählt:
`grep -l REPLACE-THIS-LINE-WITH-THE-NGC-RELEASE-KEY *.sh` = **0**.

**Der Durchlauf, nichts ersetzt:** Release neu unterschrieben (Folge 3), dreimal **`release ok`**;
`ngc-api` **`active`** mit **`NRestarts=0`**, Port 7443 belegt; Apache **`active`**; die Seite
**`http=200`**; `GET /api/v1/setup` **durch Apache** `http=200`; und
`grep -c REPLACE-THIS-LINE /etc/ngc/api.json` = **0**. Die vier Syntaxprüfungen der geänderten
Skripte: alle `ok`.

**Der fehlende Riegel aus Punkt 56 ist gebaut** — und er ist als **rot-vorher** belegt, nämlich am
Fall, der ihn auslöst: leere Store-Antworten brechen jetzt ab mit „NGC has no official app store
yet …", **Ausstiegscode 1**, bevor etwas geschrieben wird. Vorher lief derselbe Aufruf mit
Code 0 durch, schrieb den Platzhalter nach `/etc/ngc/api.json` und meldete „The API is running",
während der Dienst in einer Neustartschleife lag.

**Was das für dich heißt:** ein kleines System lässt sich jetzt **aufsetzen und aufrufen**. Zwei
Dinge bleiben, und keines davon ist Programmierarbeit:
1. **Der geheime Schlüssel gehört auf einen Hardware-Stick** (Entwurf 4.8) und von dieser Maschine
   weg. Solange er in `/root/ngc-keys` liegt, unterschreibt ihn jeder, der dort root ist.
2. **Der Store-Schlüssel ist weiter ein Platzhalter.** Jede Installation muss darum heute ihren
   **eigenen** Store nennen — der Riegel sagt das jetzt sauber, statt es durchzulassen.

**Weiter ungemessen, unverändert aus Punkt 56:** die Anmeldung im Browser (erster Verwalter per
Passschlüssel), und damit die Kopplung des Controllers, die über die angemeldete Oberfläche läuft.

## Punkt 58: Die Einrichtungsseite im echten Browser — und der Satz aus Punkt 50, diesmal gesehen

> **Stand: gilt.** Art: Umgebung. Die hier gestellte Frage („`ngc.test` eintragen
> oder gleich mit echtem Namen messen") ist in **Punkt 59** praktisch beantwortet:
> der Browser löst selbst auf, die `hosts`-Datei blieb unberührt. Was ein Nutzer
> mit `letsencrypt` erlebt, ist weiter **ungemessen**.

**Gemessen am 17.10.2026** in einem echten Browser gegen die Installation aus Punkt 57.

**Die Seite kommt, und sie ist richtig:** „**NGC einrichten** — Diese Anlage ist neu. Auf dem
Bildschirm des Servers steht ein Einmal-Code. Bitte tippen Sie ihn hier ein." Dazu ein Feld und
`Weiter`. Die Oberfläche liest ihre Lage aus `/config.json`: API `https://ngc.test:7443`,
`rp.id` `ngc.test`.

**Zwei Riegel der Oberfläche sind dabei gefahren, nicht nur gelesen:**
- Über **`http://` an einer anderen Adresse** antwortet sie **„NGC braucht eine sichere Verbindung.
  Bitte die Seite über https:// aufrufen."** und zeigt nichts weiter. Das ist die sicherere
  Variante, und sie greift.
- Den **Testserver der Oberfläche** (`PHP_SAPI === 'cli-server'`) lässt sie an `localhost` zu —
  dort ist `window.isSecureContext` **true**, also ein sicherer Ursprung nach Browserregel.

**Und der Satz aus Punkt 50 ist jetzt gesehen, nicht abgelesen.** Einmal-Code eingetippt, `Weiter`
geklickt, nach vier Sekunden steht auf dem Schirm:

> **Dienst nicht erreichbar. Bitte später erneut versuchen.**

Dieselben Worte, die Punkt 50 aus `login.js:336` und `setup.js:64` **vorhergesagt** hatte. Der
Grund hier ist die Umgebung, nicht NGC: `fetch('https://ngc.test:7443/api/v1/setup')` aus dem
Browser gibt `TypeError: Failed to fetch`, weil `ngc.test` auf dem Windows-Rechner nicht auflöst.

**Damit ist P9 am Benutzer belegt:** dieser Satz ist derselbe, den eine **besetzte Datenbank**
erzeugt (Punkte 49/52/53 geben dort 503 mit `Retry-After: 5`, aber denselben Code
`E_UNREACHABLE`). Der Benutzer kann „Dienst weg", „Name löst nicht auf" und „Datenbank gerade
besetzt" **nicht unterscheiden** — und `Retry-After` liest die Oberfläche nicht. Das ist kein
Lesefund mehr, sondern eine gesehene Erscheinung.

**Wo es hier aufhört, und warum es kein Mangel von NGC ist:** den ersten Verwalter anzulegen
braucht einen Browser, der die API **erreicht und ihr Zertifikat annimmt**. Auf dieser Maschine
fehlt beides, und beide Abhilfen sind Eingriffe am System, die ich nicht von mir aus mache:
`ngc.test` in die Windows-`hosts` eintragen und das selbst unterschriebene Zertifikat einmal
annehmen. Der Browser der App bietet kein Weiterklicken an (`chrome-error://`), Playwright
verlangt hier ein Chrome, das nicht installiert ist.

**Der erste Verwalter braucht übrigens kein Passschlüssel-Gerät:** `apps/web/.../auth/setup.js`
leitet den Schlüssel aus einem **Passwort** ab (`proveWithPassword`, `KIND_PASSWORD`,
`ALG_EDDSA`, frischer Salt). Ein Passschlüssel ist die Kür, nicht die Pflicht — der Weg ist also
auch ohne Hardware offen.

**Zu entscheiden (du):** entweder `ngc.test` auf dem Windows-Rechner eintragen und das Zertifikat
einmal annehmen, dann gehe ich die Anmeldung zu Ende — oder gleich auf einer Maschine mit echtem
Namen und `letsencrypt`-Zertifikat messen, was ohnehin der Fall ist, den ein Nutzer erlebt.

## Punkt 59: Angemeldet. Der Schreibtisch steht im Browser

> **Stand: gilt.** Art: Messung. **Schließt die gemessene Lücke aus Punkt 47.**
> ⚠ Einschränkung: „durchgängig" galt nur für diesen einen Seitenaufbau —
> **Punkt 60** zeigt die Aussperrung danach, **Punkt 61** erklärt und behebt sie
> (ohne Kopplung kein Anker, ohne Anker hält die Oberfläche jede Anlage für neu).

**Gemessen am 17.10.2026**, echte HTTPS-Strecke gegen die Installation aus Punkt 57, in einem
echten Browser (Chromium über Playwright). **Kein Eingriff am Windows-System:** der Browser löst
`ngc.test` selbst auf (`--host-resolver-rules`) und nimmt das selbst unterschriebene Zertifikat
**für diesen Lauf** an (`ignoreHTTPSErrors`). Die `hosts`-Datei des Rechners blieb unberührt.

**Der ganze Weg, Schritt für Schritt gesehen:**
1. `https://ngc.test/` → „**NGC einrichten** … Auf dem Bildschirm des Servers steht ein
   Einmal-Code." `fetch('/api/v1/setup')` aus der Seite: **http 200**.
2. Code eingetippt, `Weiter` → **Schritt 2**: „Bitte vergleichen: Steht hier dasselbe wie auf dem
   Bildschirm des Servers?" mit **Kennung der Anlage**, **Name und Adresse des API-Servers**
   (`wss://ngc.test:7443/tunnel`) und dem **Fingerabdruck** — genau den Werten, die `ngc-all.sh`
   auf dem Server gedruckt hat. Felder: ein Name, zweimal Passwort, ein Haken für den App-Store.
3. Name und Passwort eingetragen, `Einrichten` geklickt → **der Schreibtisch**: Startleiste mit
   `Start`, Uhr `03:02 17.10.2026`, die Kacheln **App-Store** und **Benutzer & Rechte**, und der
   Hinweis **„Umstellen auf Passkeys empfohlen"**.
4. Die Sitzung steht in **einem** Keks: `ngc_session`, **`httpOnly`** und **`secure`** — gesehen,
   nicht aus dem Quelltext gelesen.

**Damit ist der kleine Fall durchgängig:** ein Rechner, ein Release, `ngc-all` und `ngc-web`, und
ein Benutzer kommt im Browser bis zum Schreibtisch. Punkt 47 hatte als Lücke genannt, dass sich
ohne `ngc-web` niemand anmelden kann — **diese Lücke ist zu.**

**Und ein Befund auf dem Weg, der keiner von NGC ist, aber einer bleibt:** mitten in der Messung
gab `/api/` plötzlich **500 Proxy Error**, und `curl` kam auf `http=000`. Ursache: **WSL hatte
`/etc/hosts` neu geschrieben** und `ngc.test` daraus entfernt, während Apache und `web.json`
weiter auf diesen Namen zeigten. Das ist die Falle aus `STAND.md`, hier zum ersten Mal mit ihrer
**Erscheinung** belegt: ein Proxy-Fehler der Oberfläche, der wie ein Fehler von NGC aussieht. Nach
dem Wiedereintragen: Seite 200, `/api/v1/setup` 200, `NRestarts=0`.

**Was jetzt noch offen ist:**
- **Der Controller ist nicht gekoppelt** („no app can run"). Der Weg dahin steht jetzt offen, denn
  der Kopplungscode entsteht in der angemeldeten Oberfläche — das ist der nächste Messpunkt.
- **Passkeys** sind nicht gemessen; die Oberfläche empfiehlt sie selbst. Der erste Verwalter läuft
  mit Passwort, und das ist vertragsgemäß.
- Die Messung lief mit **selbst unterschriebenem** Zertifikat und einem Browser, der es für diesen
  Lauf annimmt. Was ein Nutzer mit `letsencrypt` erlebt, ist damit **nicht** gemessen.

## Punkt 60: Der Controller lässt sich nicht koppeln — der Knopf dafür gibt es nicht — ÜBERHOLT durch Punkt 61

> ⚠ **ÜBERHOLT DURCH PUNKT 61 — in der Kernaussage falsch.**
> **Den Knopf gibt es:** „Kopplungscode zeigen" in *Benutzer & Rechte* unter dem
> Reiter **Server** (`apps/web/.../apps/users.js:600`). Die „null Treffer in
> `apps/web`" kamen aus einem Arbeitsbaum auf `main`, wo der Ordner zwei Dateien
> hat — der Oberflächencode liegt auf dem Zweig `web`.
> **Der geschlossene Kreis stimmte in der Erscheinung, nicht in der Ursache:**
> die Kopplung scheiterte an einer fehlenden Abfrage `?secret=…`, nicht an einem
> fehlenden Knopf. Art: Messung.
> Der Text unten bleibt als Messung von damals stehen — wer nur ihn liest, landet
> auf dem alten Stand.

**Gemessen am 17.10.2026** an der laufenden Installation aus Punkt 59. Der Auftrag war, den
Controller zu koppeln. Er lässt sich nicht koppeln, und zwar nicht aus einem Fehler, sondern
weil ein Stück fehlt.

**Die Anweisung der Skripte** (`ngc-all.sh`, `ngc-web.sh`, wörtlich): „Sign in to the web
interface, **make a pairing code for this machine there**, and call 'Pair the controller' with it."

**Die Oberfläche kann das nicht.** Gezählt im Quelltext: `apps/web` enthält **keinen einzigen**
Treffer auf `pair`, `Kopplung` oder `koppeln`. Im laufenden Startmenü stehen genau zwei Apps —
**App-Store** und **Benutzer & Rechte** — dazu Aussehen und Fensterbefehle. Kein Eintrag, der
einen Kopplungscode erzeugt.

**Die API kann es sehr wohl:** `shared/core/core.go:48` führt `ActPairCode = "pair-code"`,
`apps/server-api/classes/coreapp/users.go:255` beantwortet `ngc.users/pair-code`, und selbst der
Klient kennt die Aktion schon — `apps/web/public/assets/js/contracts/core.js:58`
(`ACT_PAIR_CODE`) und `:573` prüft die Antwort auf das Präfix `ngc-pair:`. **Es fehlt nur die
Stelle, die sie aufruft und den Code zeigt.** `apps/web/CLAUDE.md:101` schreibt sogar vor, wie er
gezeigt werden soll.

### Und daraus wird ein geschlossener Kreis

1. Ohne gekoppelten Controller gibt es **kein `/etc/ngc/trust-anchor.json`** — gemessen: die Datei
   fehlt, und die API meldet im Minutentakt `trust list could not be read … no trust list yet`.
2. Ohne diese Datei zeigt die Oberfläche **jedem** Browser die Einrichtungsseite. Gemessen mit
   einem zweiten Browser: **null API-Aufrufe**, leerer `localStorage`, die Entscheidung fällt
   serverseitig in `index.php`/`Config.php` aus der fehlenden Datei. Mit gespeicherter, gültiger
   Sitzung ist es **dasselbe** — der Keks hilft nicht.
3. Die Einrichtung ist aber schon gelaufen: `/api/v1/setup` gibt **404**, der Einmal-Code ist
   verbraucht. Wer einen Code eintippt, bekommt — richtig und hilfreich —
   **„Diese Anlage ist noch nicht fertig eingerichtet. Bitte den Controller des Webservers
   koppeln."**
4. Koppeln kann man nur mit einem Code aus der angemeldeten Oberfläche. **Die erreicht man nur in
   dem einen Seitenaufbau direkt nach der Einrichtung** — danach nie wieder.

**Folge, nüchtern:** wer nach dem Einrichten den Browser schließt oder neu lädt, **kommt nicht
mehr hinein**, und keine App kann je laufen. Ich bin selbst hineingelaufen: die Installation aus
Punkt 59 war danach nur noch über ein Zurücksetzen der Testdatenbank zu erreichen.

**Was fehlt, ist klein und benannt:** ein Weg in der Oberfläche, der `ngc.users/pair-code` ruft
und den Code zeigt. Alles darunter ist da — Vertrag, API, Prüfung der Antwort im Klienten.
Ein zweiter Weg wäre ein Unterbefehl, der einen Kopplungscode ohne Oberfläche ausgibt; der
Controller hat heute nur `pair`, das einen **fertigen** Code erwartet (`ngc-controller --help`).

**Ungemessen, weil davon abhängig:** alles hinter der Kopplung — Apps installieren, Terminal,
Container, der Weg über den Tunnel aus Punkt 44 im echten Betrieb.

## Punkt 61: Gekoppelt — und Punkt 60 war in seiner Kernaussage falsch

> **Stand: gilt.** Art: Messung. **Berichtigt Punkt 60.** An einer frischen Anlage
> bestätigt in **Punkt 62**. Noch offen: ob mit gekoppeltem Controller wirklich
> **Apps laufen**.

**Gemessen am 17.10.2026** auf Debian 13. **Zuerst die Berichtigung, weil sie mir gehört:**

> Punkt 60 sagt „der Knopf dafür gibt es nicht". **Das ist falsch.** Der Knopf heißt
> **„Kopplungscode zeigen"** und steht in *Benutzer & Rechte* unter dem Reiter **Server**
> (`apps/web/public/assets/js/apps/users.js:600`, `showPairCode` ab 613). Er ruft
> `core.ACT_PAIR_CODE`, prüft den Code mit `tunnel.checkCode` gegen die **vom Klienten geprüfte**
> Fassung und zeigt ihn erst dann — genau wie `apps/web/CLAUDE.md:101` es vorschreibt.
>
> Mein „null Treffer in `apps/web`" kam aus einem Arbeitsbaum auf `main`, wo `apps/web` nur zwei
> Dateien hat. Der Oberflächencode liegt auf dem Zweig `web`. **Ein Zähler über den falschen Baum
> ist kein Befund, sondern eine Behauptung** — die Regel dazu steht seit heute in `messen.md`.

### Der echte Grund, warum keine Kopplung zustande kam

`apps/server-controller/classes/pairing/pairing.go` schickte die Anfrage an `tunnel.PathPair`
**ohne jede Abfrage**. Die API verbraucht aber den Einmalwert, den die **Abfrage benennt**:
`?secret=<base64url des SHA-256 des Kopplungsgeheimnisses>` (`controllers/pair.go:49`), und ohne
sie antwortet sie `E_PROTOCOL`. **Also konnte eine Kopplung nie gelingen** — und `ngc-controller`
sagte dazu seine eine feste Zeile („Ist der Kopplungscode noch gültig und die API erreichbar?"),
die beide Möglichkeiten nennt und keine ausschließt.

**Der Beweis lag im eigenen Haus:** der Beitrittsweg der API baut dieselbe Abfrage aus demselben
Hash (`join/run.go:177`, `secretHash`). Eine Hälfte war gebaut, die andere nicht.

**Und der Prüfstand sah nie hin:** `pairing_test.go` las Pfad, Verfahren und Rumpf — die **Adresse
nie**. Eine Anfrage ohne Abfrage galt ihm als gut. Jetzt vergleicht er die Abfrage mit dem Hash des
Geheimnisses: der Mutant, der sie entfernt, **stirbt** (`secret of the query = ""`), eine
Kontrollmutation **lebt**, 14 Controller-Pakete grün.

**Gemessen am laufenden System:** Kopplung `RC=0`, `/etc/ngc/trust-anchor.json` **entsteht**,
`ngc-controller.service` läuft, und der Tunnel steht (`tunnel ended … seconds=122` beim Neustart).

### Zweiter Befund: der Anker war zu streng geschützt und nahm die Oberfläche mit

`ngc-all.sh` und `ngc-controller.sh` schrieben den Anker **`0600 root:root`**. `index.php` liest
ihn aber und gibt Nummer und Hash **jedem Browser** in `config.json` mit — und Apache läuft als
`www-data`. Folge: **nach einer gelungenen Kopplung antwortete die Oberfläche auf jeden Aufruf
500.** Gemessen: `0600` → 500 und `Permission denied` für `www-data`, **`0644` → 200** und die
Seite. Was den Anker schützt, ist die **Unterschrift der Vertrauensliste**, nicht der Dateimodus.
Behoben in beiden Skripten.

### Was damit zu ist

Ein zweiter Browser sieht jetzt **„Bei NGC anmelden"** statt der Einrichtungsseite. Die Aussperrung
aus Punkt 60 ist damit erklärt und behoben: sie war keine Eigenart der Oberfläche, sondern die
Folge der nie gelungenen Kopplung — ohne Kopplung kein Anker, ohne Anker hält die Oberfläche jede
Anlage für neu.

**Noch offen:** ob mit gekoppeltem Controller nun wirklich **Apps laufen** (Container, Terminal) —
das ist der nächste Messpunkt und bisher nur insoweit belegt, als der Tunnel steht.

## Punkt 62: Die Probe aufs Exempel — die Anlage entsteht allein aus den Skripten, mit vier Befunden

> **Stand: gilt.** Art: Umgebung. Bestätigt **Punkt 61** und **Punkt 59** an einer
> frisch angelegten Anlage, und **Punkt 47**s Lücke als geschlossen.
> Befund 2 (kein Weg für einen Testschlüssel außer `sed` in vier Skripten) ist
> **blockierend** und gehört zu den offenen Entscheidungen unten.

**Gemessen am 17.10.2026** in einer **frisch angelegten** Debian-13-Umgebung, nicht in der
gewachsenen Testanlage: WSL-Instanz `ngcfrisch` (`wsl --install Debian --name ngcfrisch`,
Debian 13 trixie, systemd läuft), und darin ein **eigener Container** aus `debian:trixie` mit
systemd und eigenem Netz (10.88.0.3). Vorher gemessen, dass dort **nichts** von NGC liegt:
`/etc/ngc`, `/var/lib/ngc`, `/usr/lib/ngc` fehlen alle drei, kein Port belegt, von den Werkzeugen
nur `openssl` da.

**Warum ein Container und nicht die neue WSL-Instanz allein, und es ist ein Befund über die
Umgebung:** **alle WSL-Instanzen dieses Rechners teilen einen Netzwerk-Namensraum.** Gemessen:
`hostname -I` gibt in der alten und in der neuen Instanz dieselbe Adresse (172.19.199.47), und
`ngc-api` der neuen Instanz starb 15-mal mit
`listen tcp :7443: bind: address already in use`, obwohl in ihr **kein** `ngc-api`-Prozess lief
(`ps` leer, `MainPID=0`). Es war die alte Anlage. Dieselbe Ursache hat auch den Fingerabdruck
verdorben: `ngc-web` brach richtig ab mit „The fingerprint of https://ngc.test:7443 does not
match" — es hatte die **fremde** API gemessen. **Zwei NGC-Anlagen können auf einem WSL-Rechner
nicht nebeneinander laufen**, weil der Port der Vertrag ist (`config.APIPort`, in `ngc-all.sh:139`
als Konstante). Der Container hat sein eigenes Netz und darum keine dieser Erscheinungen.

### Das Ergebnis: die Anlage entsteht aus den Skripten

Alles aus `ulh`-Zweig `ngc-kette` (`223baac`), die Programme aus `nextgencloud`-`origin/main`
(`8624a62`, `CGO_ENABLED=0 GOOS=linux GOARCH=amd64`), das Release mit einem **Testschlüssel**
unterschrieben (Kennung `QPGNzMY0a-wyp9ij7d-yGQ` — ein Testschlüssel, kein Schlüssel von NGC),
dreimal `release ok`.

| Schritt | Ergebnis |
|---|---|
| `ngc-all.sh install` | **RC=0**, `ngc-api.service` **active**, **NRestarts=0**, Port 7443 belegt |
| `ngc-web.sh install` | **RC=0**, Apache **active**, Seite **200** |
| Einrichtung im Browser | Einmal-Code → Schritt 2 mit Kennung, Adresse, Fingerabdruck → **Schreibtisch**; `ngc_session` **httpOnly** und **secure** |
| Kopplungscode holen | *Benutzer & Rechte* → Reiter **Server** → **„Kopplungscode zeigen"** → `ngc-pair:…` |
| `ngc-all.sh configure` mit dem Code | **RC=0**, `controller.json` und `trust-anchor.json` geschrieben, `ngc-controller.service` **active**, **NRestarts=0** |

**Die vier Zahlen, in einem Lauf gemessen (19:15:16 UTC):** Seite `http=200`,
`/api/v1/setup` durch Apache `http=404` (richtig: der Einmal-Code ist nach der Einrichtung
verbraucht — **vor** der Einrichtung war dieselbe Adresse `200`), `ngc-controller.service`
**active**, `/etc/ngc/trust-anchor.json` Modus **644**.

### Befund 1: `apps/web` liegt nicht auf `main` — aus `origin/main` allein gibt es kein Release

Gezählt je Zweig, nicht im Arbeitsbaum: `git ls-tree -r --name-only origin/main -- apps/web`
ergibt **2** Dateien (`CLAUDE.md`, `VERSION`), `origin/web` **128**. Und
`git merge-base --is-ancestor origin/web origin/main` sagt **nein**. Die Oberfläche, die
`ngc-web-all.tar` trägt, ist also auf dem Hauptzweig **nicht vorhanden**; ich habe sie aus
`origin/web` (`e86a1a6`) genommen und sage das bei jeder Zahl dazu. **Wer nur `main` klont, kann
das Release nicht bauen** — und ein Nutzer, der `ngc-web` ausführt, bekommt kein Archiv.

### Befund 2 (blockierend): für einen Testschlüssel gibt es keinen Weg außer der Dateiänderung

Der öffentliche Release-Schlüssel steht **fest im Skript**: `ngc-all.sh:168–170`, ebenso
`ngc-web.sh:105–107`, `ngc-api.sh` und `ngc-controller.sh`. Es ist eine nackte Zuweisung ohne
Rückfall auf die Umgebung — anders als die Adresse daneben
(`NGC_RELEASE_BASE="${NGC_RELEASE_BASE:-…}"`, `ngc-all.sh:161`). **Eine Installation mit einem
eigenen Schlüssel ist darum nur mit einem `sed` in vier Skripten zu machen**, und genau das war
der einzige Handgriff dieses Durchlaufs. Er ist nach der Messung **zurückgenommen**, und der
Testschlüssel ist in **kein** Repo gekommen.

**Was fehlt:** ein Weg, den Schlüssel von außen zu setzen, der im Betrieb nicht aufgeht. Zwei
Formen sind denkbar und beide klein: ein `NGC_RELEASE_KEY_FILE`, das nur gelesen wird, wenn es
gesetzt **und** die Datei root gehört; oder ein ausdrückliches `ngc-all.sh test-key <Datei>`, das
sagt, dass es eine Probe ist. Ohne das eine oder andere kann **niemand** die Kette prüfen, ohne
die Skripte zu verbiegen — und wer sie verbiegt, prüft nicht mehr die Skripte.

### Befund 3: `/var/lib/ngc/staged` muss der Betreiber selbst anlegen

`fetch_release` nimmt den Ordner, sobald `release.json` darin liegt, und die Fehlermeldung nennt
ihn auch („put release.json and the files … into `${NGC_STAGE}` by hand"). **Nur legt kein Skript
ihn an**, solange `install_release` noch nicht gelaufen ist: `mkdir -p /var/lib/ngc/staged` war
mein zweiter Griff, und ohne Internet ist dieser Weg laut Skript „the only way that works".
`ngc-all.sh:132` nennt den Pfad. **Was fehlt:** die Meldung sollte den `mkdir` mitnennen, oder
`install_release` legt den Ordner vor dem ersten Versuch an.

### Befund 4: `ReadWritePaths` nennt `/etc/ngc` nicht — bestätigt an einer frischen Anlage

`ngc-all.sh:1322` und `ngc-api.sh:1065` schreiben
`ReadWritePaths=${NGC_DATA} ${NGC_TLS}` — also `/var/lib/ngc` und `/etc/ngc/tls`, **nicht**
`/etc/ngc`. In der Einheit der frischen Anlage steht es wörtlich so. Die API muss aber
`/etc/ngc/trust-anchor.json` hochziehen, wenn die Vertrauensliste wächst; eine Schwestersitzung
hat am 17.10.2026 an der alten Anlage genau dort
`the trust anchor outside the database could not be moved up` gemessen und es mit einem
systemd-Zusatz (`ReadWritePaths=/etc/ngc`) behoben. **Hier ist belegt, dass der Mangel in der
frischen Anlage ebenso entsteht** — die Erscheinung selbst ist in diesem Durchlauf nicht
abgewartet (der Anker stand bei `seq: 1`, die Minutenwarnung
`trust list could not be read … no trust list yet` hörte nach dem Aufnehmen des Servers auf).

### Was ungemessen bleibt

- **Ob Apps laufen** (Container, Terminal) — unverändert offen aus Punkt 61.
- **Das Aufnehmen des Servers in der Oberfläche:** der Knopf **„Aufnehmen"** ist geklickt und die
  Minutenwarnung der API hörte danach auf, aber die Liste „Wartende Server" zeigte den Eintrag
  weiter, und der Anker blieb bei `seq: 1`. **Ob das ein Ausbleiben der Auffrischung oder ein
  halber Vorgang ist, ist nicht gemessen.**
- **Ein Nutzer mit `letsencrypt`**: gemessen wurde mit selbst unterschriebenem Zertifikat und
  einem Browser, der es für diesen Lauf annimmt (`ignoreHTTPSErrors`,
  `--host-resolver-rules`) — die Hosts-Datei des Rechners blieb unberührt.
- **Der echte Release-Schlüssel von NGC**: alle Zahlen hier gelten für einen **Testschlüssel**.
- **Die Zahlen gelten für diesen Ort:** Debian 13 in einem podman-Container in einer
  WSL2-Instanz, ext4, `TMPDIR` auf `/var/tmp`. Nicht für C:, nicht für Z:, nicht für Blech.


---

## Offene Entscheidungen für Thomas

> **Überholt, Stand 23.10.2026.** Dieser Abschnitt ist eine Momentaufnahme vom 18.10.2026 und
> kennt die Punkte 63–71 nicht; mehreres darin ist inzwischen gebaut (vierstellige Nummern,
> Systemrechte beim Aufnehmen, Katalogabruf beim Start). **Das gültige Blatt ist
> [`thomas-entscheidet.md`](thomas-entscheidet.md)** — dort steht jede offene Entscheidung aus
> dieser Datei, aus dem v1.3-Vorschlag und aus den offenen PRs, am Quelltext nachgesehen und nach
> dem geordnet, was am meisten blockiert.

Alles, was in dieser Datei auf eine Entscheidung wartet – an einer Stelle.
Je Zeile: die Frage, der Punkt, und was passiert, wenn nicht entschieden wird.

### Blockierend – ohne diese Entscheidung geht etwas gar nicht

| Frage | Punkt | Wenn nicht entschieden wird |
|---|---|---|
| **Hardware-Stick für den Release-Schlüssel** – mit welchem Werkzeug unterschreibt er? | **57** (Nr. 1), Entwurf 4.8 | Der geheime Schlüssel bleibt als Datei in `/root/ngc-keys`. **Jeder, der auf dieser Maschine root ist, unterschreibt NGC-Releases.** |
| **Der offizielle Store fehlt** – eigener Store-Schlüssel oder NGC-Store? | **56** (Nr. 2), **57** (Nr. 2) | `REPLACE-THIS-LINE-WITH-THE-NGC-STORE-KEY` bleibt stehen. Jede Installation muss ihren **eigenen** Store nennen; es gibt keine Apps von NGC. |
| **Vierstellige Hausnummerierung gegen dreistellige `store.ValidVersion`** | **45** | `ngc-store release -version 0.0.1.0` fällt durch. **Vor** dem ersten Release zu klären, nicht danach. |
| **Die fünf `ngc-*`-Skripte nach `ulh`-`main` zusammenführen** | **47** | Kein Nutzer kann NGC installieren – auf `origin/main` liegt **kein** `ngc-*`-Skript. Ein Push dorthin erreicht per Auto-Update **sofort alle Nutzer**, darum liegt es nicht beim Agenten. |
| **Ein Weg, den Release-Schlüssel von außen zu setzen** (`NGC_RELEASE_KEY_FILE` oder `ngc-all.sh test-key <Datei>`) | **62**, Befund 2 | Niemand kann die Kette mit einem eigenen Schlüssel prüfen, ohne die vier Skripte mit `sed` zu verbiegen – und wer sie verbiegt, prüft nicht mehr die Skripte. |

### Die v1.3-Reihe – je Punkt JA oder NEIN

Die Vorlage steht in [`vertraege-v1-3-vorschlag.md`](vertraege-v1-3-vorschlag.md):
je Punkt der fertige Änderungssatz, der Preis, was ohne ihn für immer fehlt, und
eine Empfehlung.

| Frage | Punkt hier | Wenn nicht entschieden wird |
|---|---|---|
| **P10** – darf ein Vertrag von `shared/` überhaupt etwas dazubekommen? | **54** | **Jede** künftige Ergänzung bleibt für immer ein Bruch. P8 und P9 kosten dann das Dreifache. P10 ist das Tor. |
| **P8** – eigener Schließgrund für die Leerlaufgrenze | **35** | Ein Leerlauf bleibt von einem Absturz nicht zu unterscheiden. Nur **nach** P10. |
| **P9** – eigener Code `E_BUSY` für „besetzt" | **49, 50, 52, 53, 58** | Der Benutzer kann „Dienst weg", „Name löst nicht auf" und „Datenbank besetzt" **nicht unterscheiden** – er liest immer „Dienst nicht erreichbar." Nur **nach** P10. |
| **P1** – `store.ValidDownloadURL` ausführen | **25, 45** | Pfad und Query einer Download-Adresse prüft dauerhaft **niemand**. |
| **P2** – `Unattended` verbietet den ersten Einbau | – | Ein erster Einbau ohne Aufsicht geht gar nicht. |
| **P3** – Format einer `ngc-web`-Release-Datei: zieht das Release-Werkzeug auf `ui/` nach, oder der Controller auf ein Wurzel-tar? | **56** | ⚠ Punkt 56 sagt „P3 ist beantwortet" – **das ist seit dem 18.10.2026 als falsch gemessen.** Die Skripte bauen Namen im Wurzelverzeichnis, der Controller packt nur Namen unter `ui/` aus; `store.WalkUI` weist das Wurzel-tar mit `ErrPackage` ab. **Ein `ngc-web`-Update über den Controller kann heute nicht gehen** – es fällt am ersten Kopfblock. |
| **P5** – Referenzfeld in `dbapi.App` | **24** | Ein Abbild ohne Referenz bleibt für immer im Datenverzeichnis liegen, und `DataSize` zählt es als Benutzerdaten. |
| **P6** – ein Kopplungscode gilt 24 Stunden | – | Die Zahl bleibt ungesetzt. Gehört zusammen mit **N1** entschieden (Punkt 26). |
| **P7** – `ngc-api migrate` braucht eine ganze `api.json` | **28** | Ein reiner Datenbankknoten kann `ngc-api migrate` nicht starten. |
| **P11** – `ValidPath` sagt nichts über die Schreibweise | **23** | Dieselbe Eingabe verhält sich auf Entwicklungs- und Zielplattform verschieden, und niemand sagt, ob das zugesagt ist. |
| **P12** – Rechte im Katalog gegen das Manifest halten | **31** | Heute keine Lücke – aber wer die Katalogkopie je als Grundlage nimmt, baut eine Wand vor eine offene Tür. |
| **P13** – Prozessdeckel je App im Manifest | **51** | Jede App teilt sich den einen Deckel von 512. Bedarf ist nicht gemessen. |

### Sicherheit und Oberfläche

| Frage | Punkt | Wenn nicht entschieden wird |
|---|---|---|
| **`noexec` auf `/data`** | **51** (Punkt 36 nennt es nicht) | Eine App darf unter `/data` abgelegte Skripte ausführen. Es ist eine **Verhaltensänderung** für jede App, die das heute tut – darum keine Agentensache. |
| **Vertrauensanker nach einem Totalverlust** – Antwort A, Antwort B oder der dritte Weg? | **43**, dazu **Reihe A, Punkt 10** | Der dokumentierte Weg zurück ist offen **und endet in einer Wand**: die Einrichtung nimmt den Admin an, danach läuft nichts. **Antwort B ist gemessen keine Antwort**; empfohlen ist der dritte Weg (Mutant D). |
| **Digest statt Tag für das Terminal-Abbild** – und **wer hebt ihn an**? | **39** | Zwei Bauten desselben Commits sind zwei Abbilder, und wer den Tag `debian:13-slim` beherrscht, beherrscht den Container mit `needs: ["root"]`. Ein festgenagelter Digest, den niemand pflegt, ist nach einem halben Jahr schlechter als ein Tag. |
| **Terminal: Rohtasten oder Zeilenfeld?** | **41** | Beim Zeilenfeld tippt ein Admin irgendwann sein `sudo`-Passwort in ein Feld, das es **anzeigt**. Empfohlen: Rohtasten – das Format kann es schon, es ist keine Vertragsänderung. |
| **Einmal-Code vor oder nach der MAC-Prüfung verbrauchen?** | **26** | Wer die Adresse kennt, verbrennt mit einer falschen MAC jeden wartenden Beitrittscode. Die Gegenrichtung lässt beliebig oft rechnen. Ungemessen: wie oft ein Code neu ausgegeben wird. Gilt für **beide** Stellen (`join.Accept`, `controllers.Pair`). |
| **Zweite Beitrittsanfrage für denselben Namen: überschreiben oder abweisen?** | **27** | Die zweite überschreibt die erste, und der Admin erfährt nichts davon. Unterscheiden ließen sie sich am öffentlichen Schlüssel. |

### Messorte

| Frage | Punkt | Wenn nicht entschieden wird |
|---|---|---|
| **Weiter mit `ngc.test` und selbst unterschriebenem Zertifikat, oder auf einer Maschine mit echtem Namen und `letsencrypt`?** | **58, 59, 62** | Was ein Nutzer mit `letsencrypt` erlebt, bleibt ungemessen – alle Zahlen gelten für einen Browser, der das Zertifikat für den Lauf annimmt. |


## Punkt 63: Die Oberfläche ist auf `main` — und was dabei an Land kam, wog schwerer als der Umzug

**Zusammengeführt am 18.10.2026.** `apps/web` hatte auf `main` **zwei** Dateien; jetzt sind es
**136**. Bau grün, 44 Pakete, 0 rot. Damit liegt zum ersten Mal der Schreibtisch, der seit
Punkt 59 nachweislich läuft, auch dort, wo gemessen wird.

**Der eigentliche Fund ist nicht der Umzug, sondern was außerhalb lag.** Die Befundliste
(`docs/befunde-17-18-10-2026.md`, aus den Commit-Botschaften der zehn Messsitzungen
zusammengetragen, deren Berichte verloren sind) nennt vier Sicherheitsbefunde, die **nicht auf
`main` waren**:

- **„Mit Passkey anmelden" meldete mit dem PASSWORT aus dem Feld an.** Der Authentifikator wurde
  nie gefragt (0 Berechtigungen angefasst), die Sitzung öffnete der Passwortschlüssel, **und auf
  dem Schirm stand nichts davon** — unter dem stehenden Hinweis „Umstellen auf Passkeys
  empfohlen". Der Kommentar über `usePasskey` behauptete das Gegenteil des Codes.
- **Der Hochladeweg war nie unterschrieben**, und die Bestätigung fiel auf dem Weg weg — bei
  **fünf** Aktionen, nicht einer. Ursache der falschen Annahme war eine Notiz, die den Vertrag
  falsch las.
- **Der Prüfstand war großzügiger als der Vertrag, dreimal an einem Tag**: `fake-link.js` setzte
  die Rollenstufe auf `off` (dort muss nichts bestätigt werden) → jeder Fall des Hochladens war
  grün, **während kein Paket hochladbar war**; dieselbe Attrappe schrieb `rootUsers` in ihre eigene
  Liste → jeder Fall zum Aktivieren lief in einer Welt, die die echte Oberfläche nie erzeugt.
- **Firefox:** die Maske nahm das Passwort an, ließ Argon2id laufen und antwortete mit dem Satz für
  ein **falsches Passwort**. Ursache: `exportKey("jwk")` eines aus PKCS#8-Saat importierten
  Ed25519-Schlüssels gibt dort `OperationError`, und `browserFits()` fragte nur die beiden
  `generateKey`. **Die Lüge aus Punkt 50, in neuer Gestalt.**

**Auf `main` behoben wurde in denselben Tagen:**
- **Die freien Anmeldeversuche wuchsen mit der Gleichzeitigkeit des Angreifers.** `login/finish`
  las die Wartezeit **außerhalb** der zählenden Sperre; ein Schwall las dieselbe freie Zeile.
  Gemessen am laufenden System: **7, 13, 14, 12, 8, 7, 12, 7 von 20** falschen Nachweisen
  durchgelassen, wo **3** erlaubt sind (seriell: genau 3). Jetzt Sperre je Zeile.
- **`tools/check.sh` druckte `check: ok`, obwohl etwas rot war — acht Löcher.** Das schlimmste ist
  der **Lieferkettenriegel**: `bad=$(go list … || true)` ließ ein **gescheitertes** `go list` leer
  zurück, der Riegel ging durch, **ohne je zu laufen**. Ein Prüfwerkzeug, das weniger prüft, als es
  vorgibt, ist die gefährlichste Stelle im Repo.
- **`pin-base.sh --verify` ließ einen `Containerfile` durch, der eine MARKE zieht** — es verglich
  nur die erste `FROM`-Zeile. Genau das, wovor Punkt 39 schützen soll.
- **Unangemeldete konnten die Platte per Protokoll füllen**: 4096 gleichzeitige `login/start` →
  **12 289 Protokollzeilen, 995 558 Byte Journal**, bis 480 Zeilen je Sekunde. Jetzt 1 Zeile,
  246 Byte, dazu ein Zähler.
- **`galera.cnf` schränkte nichts ein und brach den Server ab**: `socket.ssl_cipher` erreicht
  `SSL_CTX_set_cipher_list`, das nur TLS-1.2-Namen kennt → `no cipher match` → `Aborting`.
  **Richtiger Wert, falscher Knopf** — dieselbe Form wie Punkt 56.

**Beim Zusammenführen selbst gab es genau einen Konflikt, und er war einer der lehrreichen Sorte:**
`tools/check.sh` lag auf beiden Seiten geändert — hier die acht geschlossenen Löcher, dort der
PHP-Schritt der Oberfläche. **Beide Hälften stehen jetzt**, keine wurde zugunsten der anderen
geopfert.

**Weiter BLOCKED, und es ist ein S1:** eine Lizenz mit Ausstellungszeit in der **Zukunft** sperrt
jede spätere aus, während der Verwalter weiter „gültig" liest — mit dem **eigenen** Schlüssel des
Stores unterschrieben, also kein Fälschungsfall. `shared/store/catalog.go:321`, eingefroren.

## Punkt 64: Der erste Release, auf einer echten Maschine installiert — und das Terminal-Abbild gebaut

**Gemessen am 18.10.2026** auf einer **echten Debian-13-VM** (`ngc`, 10.2.3.208, Kernel
6.12.107, ext4, 62 GB frei) — nicht in WSL. Der Unterschied ist der Grund, warum es sie braucht:
**`CONFIG_FS_ENCRYPTION=y`**, `fscrypt` vorhanden. Auf dem WSL-Rechner ist das aus, und ohne
Dateiverschlüsselung startet dort **keine** NGC-App.

**Der Weg lief zum ersten Mal mit einem richtigen Release durch**, nicht mit Dateien aus einem
Arbeitsbaum:
1. Release als **Entwurf** gebaut: fünf Teile (`ngc-web-all`, `ngc-api` und `ngc-controller` je
   amd64 und arm64), unterschrieben mit dem echten Release-Schlüssel, **`release ok`** fünfmal und
   **`Inhalt in Ordnung`** für alle Archive.
2. Dateien auf die VM gelegt, `ngc-all install`: „The signature of the program files of ngc-api is
   good" — **die Unterschrift wurde auf einer fremden Maschine geprüft**, mit dem Schlüssel, der
   im Skript steht.
3. `ngc-web install`: Apache nahm die Einstellungen an (`Syntax OK`), Seite **200**, Setup **200**.
4. Im Browser eingerichtet und angemeldet, Kopplungscode geholt, Controller gekoppelt: `RC=0`,
   `ngc-controller.service` **active**, `/etc/ngc/trust-anchor.json` mit **`0644`** (die Behebung
   aus Punkt 61 trägt auf der echten Maschine).

**Ein Fund beim Bauen des Release, und das Werkzeug hat ihn selbst gefangen:** der erste
`ngc-web-all.tar` trug den Eintrag `./`, „den keine Installation auspackt" — abgewiesen von
`ngc-store contents`, dem Riegel, der am selben Tag entstanden ist. Ohne ihn wäre ein Release
hochgegangen, das die Skripte später abgelehnt hätten.

**Und das Terminal-Abbild ist zum ersten Mal gebaut:** `ngc-controller image -arch amd64` auf der
VM, podman 5.4.2 → `Successfully tagged localhost/ngc/terminal:1.0.0`, geschrieben nach
`/tmp/terminal-amd64.tar`. Punkt 51 hatte den Befehl als gebaut, aber **nie gelaufen** vermerkt.

**Was damit noch offen ist — und es ist die letzte Meile:** eine App läuft noch nicht. Dafür fehlt
ein Paket (`manifest.json`, `ui/`, `services/amd64.tar`) und ein **erreichbarer Store**, dessen
TLS-Kette die API annimmt (Punkt 45: sie verlangt sie wirklich). `podman ps` ist leer.

**Nebenbefund zur Umgebung:** die VM ist über `ssh ngc` erreichbar, `sudo` ohne Passwort, podman
und PHP schon da, Apache kam mit der Installation. Damit gibt es zum ersten Mal einen Ort, an dem
die Dinge gemessen werden können, die WSL nicht kann.

## Punkt 65: „Noch nicht geholt" wird dem Verwalter als „nicht echt" gezeigt

Am 19.10.2026, auf der Debian-VM, aus einem veröffentlichten Release installiert: angemeldet,
Controller gekoppelt, Laden läuft. Im Laden steht dann der Satz

> Der Katalog dieses Stores ist nicht echt.

**Der Katalog ist echt.** Gemessen am selben Stand:

- `catalog.json` von `https://store.ngc.test` → 200, `kid WM0yymuJtd9UxaUONGIXow`.
- Der Schlüssel in `/etc/ngc/api.json` (`storeKey`) hat **dieselbe** Kennung — nachgerechnet mit
  `sign.KeyID`.
- `sign.Verify(env, storeKey, sign.PurposeCatalog)` → `nil`. Kein Fehler.

Die Oberfläche unterscheidet drei Fälle und hat für jeden einen eigenen Satz
(`apps/web/public/assets/js/apps/store.js`, Zeile 307): nicht vertraut, alter Katalog, nicht echt.
Der dritte ist der `else`-Zweig — und in ihn fällt **alles andere**, auch der Fall, in dem die API
den Katalog einfach noch nicht geholt hat: `Stores.Catalog` liest die Zeile aus der Datenbank
(`BlobGet(BlobCatalog, id)`), und solange es keine gibt, schlägt der Aufruf fehl. Die Oberfläche
sieht einen Fehler, der weder `Untrusted` noch `Rollback` ist, und sagt „nicht echt".

Das ist dieselbe Klasse wie die Anmeldelüge: ein leerer Zustand wird als Angriff dargestellt. Wer
das liest, sucht an der falschen Stelle — und zwar lange, denn gegen eine gefälschte Unterschrift
kann ein Verwalter nichts tun. Der Satz nimmt ihm die Handlung, die es gäbe: warten.

**Zu bauen:** der leere Zustand bekommt einen eigenen Satz („Der Katalog ist noch nicht geholt.")
und der `else`-Zweig bleibt für das, was er benennt. Prüfbar: ein Stand mit leerer Katalogzeile
muss den neuen Satz zeigen, und ein Stand mit verbogener Unterschrift den alten — beide in einem
Lauf, sonst beweist keiner etwas.

## Punkt 66: Eine frische Anlage hat eine Stunde lang keinen Laden

Dieselbe Messung, die Punkt 65 erklärt, legt den zweiten Befund daneben.

Kataloge holt allein die Stundenrunde: `apps/server-api/classes/server/server.go` legt bei Zeile
386 `time.NewTicker(Hour)` an, und `Server.hour` (Zeile 421) ruft `Stores.Refresh`. Es gibt
**keinen** Abruf beim Start und **keine** Handauslösung. Der Kommentar bei Zeile 436 sagt das auch
so: „The first beat is an hour after the start and not at the start".

Für eine laufende Anlage ist das richtig gedacht — eine Anlage, die beim Hochkommen anfängt, ihre
eigenen Teile zu tauschen, ist schlimmer. Für eine **frische** Anlage heißt es: der Verwalter
richtet ein, koppelt, öffnet den Laden — und findet eine Stunde lang nichts, mit dem Satz aus
Punkt 65 darunter. Gemessen: Anlage um 16:45 installiert, API um 17:20 neu gestartet, erster
Katalogabruf dadurch bei ~18:20.

Der Neustart ist der zweite Teil des Befundes: **jeder** Neustart der API verschiebt den ersten
Abruf um eine volle Stunde. Eine Anlage, an der gearbeitet wird, holt also nie einen Katalog.

**Zu bauen — eins von beiden, Thomas entscheidet:**

- **Erster Abruf kurz nach dem Start** (z. B. eine Minute), die Stundenrunde danach unverändert.
  Preis: jede Anlage greift beim Hochkommen einmal nach außen. Das ist kein Tausch von Teilen,
  nur ein Lesen — die Begründung bei Zeile 436 trifft darauf nicht zu.
- **Ein Knopf „Jetzt nachsehen"** im Laden, für den Verwalter. Preis: ein Weg mehr, den jemand
  von außen auslösen kann; braucht also dieselbe Ratenbegrenzung wie jeder andere Aufruf.

**Mein Vorschlag:** beides, und der Knopf zuerst — er ist der kleinere Eingriff und die Antwort
auf die Frage, die ein Verwalter in diesem Augenblick wirklich hat. **Gemessen ist der Befund,
nicht der Vorschlag.**

## Punkt 67: Rust für NGC — ein Vertrag ist bitgleich nachgebaut, und genau das ist das Argument dagegen

Thomas hat ein Entscheidungspapier bestellt: kein Umbau, keine Meinung, sondern ein Versuch mit
Zahlen. Der steht hier. **Alles unter „Gemessen" ist gemessen; alles unter „Abgewogen" ist nicht
gemessen und als Abwägung gekennzeichnet.** Der Vorschlag am Ende ist eine Abwägung.

### Der Versuch

`shared/sign` ist der kleinste abgeschlossene Vertrag mit echter Krypto: 136 Zeilen Go, Ed25519,
SHA-256, base64url, strenges JSON — und er hat eine Vektordatei, aus der heute **beide**
bestehenden Fassungen prüfen (`shared/testdata/sign.json`). Deshalb er und nicht `enc` (zu
grundlegend, kein eigener Vektor) oder `task` (hängt an `wire`).

Er ist in Rust nachgebaut und gegen dieselbe Datei gerechnet — `versuch-rust/sign/`, Beleg und
kein Bestandteil. **Gemessen am 20.10.2026, Debian 13 unter WSL, rustc 1.99.1:**

```
32 OK, 0 FAIL
```

Darunter die harte Zeile: **die Vektordatei aus Rust neu gebaut ist Byte für Byte die Datei, die
Gos Golden-Test geschrieben hat.** Schlüssel aus dem Seed, Kennung, Nachrichtenbytes,
Unterschrift, JSON-Form — alles gleich. Dazu die **vollständige Ablehnungstabelle** aus
`shared/sign/sign_test.go` (18 Fälle: unbekanntes Mitglied, doppeltes Mitglied, Mitglied in
anderer Schreibweise, `null`, Daten dahinter, Polsterung im base64, Budget überschritten …) —
alle 18 lehnt Rust ab wie Go.

**Bitgleichheit ist also kein Problem.** Wer dagegen argumentiert, dass Rust auseinanderläuft,
argumentiert gegen eine Messung.

### Der Preis, in Zahlen

**Gemessen.** `cargo vendor` für diesen **einen** 136-Zeilen-Vertrag:

| | Kisten | Dateien | Bytes | Rust-Zeilen |
|---|---|---|---|---|
| Rust, nur `shared/sign` | **41** | 1 733 | 22 018 383 | **577 163** |
| Go, ganzes NGC heute | 12 | 692 | 16 616 004 | — |

Die vier größten: `libc` 133 739, `fiat-crypto` 83 364, `syn` 67 002 und ein **zweites** `syn`
(2.0.119) 65 391 Zeilen — zwei Fassungen desselben Makro-Übersetzers im selben Baum. Go bringt
Ed25519, SHA-256, base64url und JSON in der Standardbibliothek mit: für `shared/sign` ist die
Zahl **null**.

Das ist die Stelle, an der die Hausregel bricht. „Fremdcode wird ins Repo kopiert und selbst
gepflegt" (`CLAUDE.md`, `THIRD-PARTY.md`) ist heute tragbar, weil es 12 Module sind und das
Aufräumen am 18.10.2026 drei MB herausgeholt hat. 41 Kisten mit 577 000 Zeilen für einen Vertrag
pflegt niemand selbst. Lizenzen wären kein Hindernis (alles MIT/Apache/BSD, AGPL-verträglich) —
die Menge ist es.

### Die zweite Umsetzung: wie wird heute gleichgehalten?

**Gemessen.** Der Weg existiert und ist schmal:

- `shared/testdata/` hält die Vektoren, `shared/internal/golden/golden.go` schreibt und prüft sie
  aus Go, `apps/web/tests/fixtures/vectors.js` liest **dieselben Dateien** aus dem Repo
  („The shared test data is the common truth for Go and JavaScript").
- 27 Prüfdateien unter `apps/web/tests/contracts/` stehen gegen die Go-Pakete.

**Und der Weg trägt die Verträge nicht allein.** `apps/web/tests/contracts/sign.spec.js` sagt es
selbst: `sign.json` *„is the only vector there is for this contract, and it says nothing about
what has to be refused, so the cases below spell that out from the Go source"*. Die Vektoren
beweisen, was **angenommen** wird. Was **abgelehnt** wird, ist je Sprache von Hand nachgeschrieben
— 847 Zeilen in `sign.spec.js` gegen 339 Zeilen `sign_test.go`.

Die zweite Fassung ist auch keine Übersetzung. `sign.js` sind 420 Zeilen gegen 136 in Go, und der
Überhang ist **kein** Rauschen: er verteidigt gegen ein Bedrohungsmodell, das Go nicht hat (ein
Aufrufer, dessen `length`-Getter Code ist). Rust hätte dieses Problem so wenig wie Go — eine
Rust-Fassung wäre also die **dritte** Lesart von `sign`, nicht die zweite Kopie der ersten.

Und eine Lücke ist gemessen, die genau da sitzt, wo die großen Verträge stehen. Gos
`enc.DecodeStrict` lehnt ein doppeltes Mitglied **in jeder Tiefe** ab, auch innerhalb einer
`json.RawMessage`. Beide Seiten gemessen, derselbe Eingabestring `{"inner":{"a":1,"a":2}}`:

| | flach | eine Ebene tiefer |
|---|---|---|
| Go `enc.DecodeStrict` | abgelehnt | **abgelehnt** (`duplicate object member name "a" within "/inner"`) |
| Rust `serde_json` | abgelehnt | **genommen**, `inner = {"a":2}` — der letzte gewinnt |

`core`, `store` und `wire` ruhen auf dieser Zusage. Rust bekäme sie nicht aus `serde_json`; sie
wäre zu bauen. Dazu kommt `wire.ValidText`: `shared/wire/wire.go`, Zeile 282 bindet den Vertrag
ausdrücklich an *„Go's: Unicode 17.0 with Go 1.27"*. Eine Rust-Fassung bringt ihre eigenen
Unicode-Tabellen mit — eine **dritte** Tabellenfassung, die gleichzuhalten ist.

### Bauen und Ausliefern

**Gemessen, Go heute** (eigener `GOCACHE`, damit kein fremder Lauf gestört wird; drei
Binärdateien `cli`, `server-api`, `server-controller`):

| | kalt | warm (3 Läufe) |
|---|---|---|
| `linux/amd64` | 17,983 s | 2,143 / 1,796 / 1,699 s |
| `linux/arm64` | 17,850 s | — |
| `./shared/...` allein | 8,629 s | — |
| `go test ./shared/...` | 15,340 s | — |

**Gemessen, Rust:** der erste Bau dieses einen Vertrags mit 41 Kisten: **10,25 s**; `target/`
danach **682 MB**. Statische Binärdatei `x86_64-unknown-linux-musl`: 834 312 Bytes, auf Anhieb.

**Der arm64-Befund:** `aarch64-unknown-linux-musl` scheitert im Standardfall am Binder
(`collect2: error: ld returned 1 exit status`). Mit
`-C linker=rust-lld -C link-self-contained=yes` baut er — 811 040 Bytes, 5,5 s. Also **ein
Schalter, kein Kreuz-gcc**. Go braucht für dasselbe `GOARCH=arm64` und sonst nichts.

**Der Windows-Befund ist härter:** auf diesem Rechner ist **kein Rust installiert und kein
C-Übersetzer vorhanden** — kein Visual Studio, kein gcc. Der ganze Versuch läuft deshalb in der
Debian-WSL. Go baut hier beide Ziele direkt aus PowerShell. Rust auf Windows hieße: MSVC-Build-Tools
oder MinGW dazu, oder jeder Bau geht durch WSL.

Die Werkzeugkette selbst: 11,4 s Installation, **613 MB**, mit beiden Zielen **1,0 GB**.

**Nicht gemessen, Abwägung:** 90 Fuzz-Ziele laufen heute mit `go test -fuzz` und null
Zusatzwerkzeug. Rust braucht dafür `cargo-fuzz` (nightly, libFuzzer) oder AFL. Was das an
CI-Zeit und an Pflege kostet, ist nicht gemessen.

### Welche Teile kämen in Frage?

**Gemessen** (Zeilen ohne Tests): `apps/server-api` 15 435, `apps/server-controller` 15 336,
`apps/server-db` 7 210, `apps/cli` 2 258, `shared/` 9 468 — zusammen **49 707** Zeilen Go, dazu
92 640 Zeilen Tests und 1 791 Testfunktionen.

**Abgewogen, Teil für Teil:**

- **`ngc-controller`** — fasst podman an und Dateisysteme. Gemessen: podman läuft über
  `exec.CommandContext` (`runtime/podman.go:501`), nicht über eine Bibliothek; das Dateisystem über
  `syscall.Statfs` und zwei ioctls. Beides ist in Rust genauso erreichbar, über `libc` — das schon
  mitvendort ist. **Gewinn: keiner, der zu benennen wäre.** Wer Prozesse startet und ioctls
  ausführt, gewinnt an Speichersicherheit nichts, was Go nicht schon gibt. **Preis: 15 336 Zeilen
  plus 29 143 Zeilen Tests.**
- **`ngc-api`** — Netz und Krypto. Hier wäre der Gewinn am ehesten zu erwarten, und er ist am
  teuersten zu holen: Noise (`flynn/noise`) hätte in Rust kein Gegenstück, das dieselben Bytes
  erzeugt, ohne gegen die Go-Fassung gemessen zu werden; dasselbe für die XTS-verschlüsselte
  SQLite-Datei. **Preis: 15 435 + 34 700 Zeilen, und zwei Krypto-Verträge, deren Bitgleichheit
  jeweils ein eigener Versuch wie dieser wäre.**
- **`ngc-store` (`apps/cli`)** — 2 258 Zeilen, das kleine Werkzeug. **Der einzige Teil, bei dem
  die Rechnung aufgehen könnte:** es ist klein, es hat keine Noise-Sitzung, und es wäre der
  natürliche Ort für einen zweiten echten Versuch. **Preis: es müsste trotzdem `sign`, `enc`,
  `store` und `wire` ein zweites Mal haben — also die 9 468 Zeilen `shared/` zuerst.**
- **Die Oberfläche** — reines JavaScript ohne Bauschritt, läuft im Browser. Rust käme nur als
  WebAssembly, das heißt: ein Bauschritt, wo heute keiner ist. Steht gegen `CLAUDE.md`, Zeile 77.
  **Kommt nicht in Frage.**
- **`shared/` selbst** — ist **eingefroren**. Nichts daran ist angefasst worden; die Go-Gegenprobe
  zur Tiefen-Messung lief in einer Testdatei, die danach wieder weg war.

**Hochgerechnet, aus der einen gemessenen Zahl:** `sign.go` 136 Zeilen Go → 192 Zeilen Rust,
Faktor **1,41**. Auf `shared/` angewandt: etwa **13 300** Zeilen Rust, und das ist die *untere*
Schranke, denn `sign` ist der leichteste Fall — die strenge JSON-Schicht aus `enc` und die
Unicode-Tabellen aus `wire` sind im Faktor nicht drin, weil sie in Rust kein Gegenstück haben.
Die 32 Beweisfälle brauchten 159 Zeilen Prüfprogramm gegen 339 Zeilen `sign_test.go` — die
Prüfseite wäre also nicht billiger, nur anders geschnitten.

### Was gewinnt der Benutzer?

**Nichts Sichtbares.** Das ist die ehrliche Antwort, und sie ist nicht gemessen worden, weil es
nichts zu messen gibt: die Binärdateien wandern auf eine Maschine, die ohnehin podman laufen
lässt; 24,5 MB `server-api` gegen vielleicht 5 MB in Rust entscheidet auf keiner Anlage etwas.
Kein Benutzer sieht eine Antwortzeit, die von der Sprache kommt.

**Abgewogen, der eine Punkt, der zählen könnte:** Speichersicherheit in dem Teil, der Bytes von
außen entgegennimmt, bevor eine Unterschrift geprüft ist. Nur: Go ist dort auch speichersicher.
Was Rust zusätzlich gibt, ist Freiheit von Wettläufen bei geteiltem Zustand — und dafür ist nicht
gemessen, dass NGC ein Problem hätte. Ein `go test -race`-Befund, der eine Umschreibung
rechtfertigt, liegt nicht vor.

### Vorschlag

**Nein, und zwar nicht „später nochmal", sondern aus der Rechnung heraus.**

Die Begründung in einem Satz: der Versuch hat bewiesen, dass Rust den Vertrag **bitgleich**
treffen kann — und dieselbe Messung hat gezeigt, dass dafür 41 fremde Kisten mit 577 163 Zeilen
in ein Repo müssten, dessen Hausregel lautet, Fremdcode selbst zu pflegen. Das sind nicht zwei
Argumente, das ist eins: **die Bitgleichheit war nie der Engpass, die Hausregel ist es.** Wer Rust
in NGC will, muss zuerst die Regel „keine Laufzeitabhängigkeiten" abschaffen. Solange die Regel
gilt, ist die Antwort schon gegeben.

Dazu drei Dinge, die auch ohne Rust aus diesem Versuch mitzunehmen sind — und die mehr wert sind
als die Sprachfrage:

1. **Die Ablehnungsseite steht in keinem Vektor.** `sign.json` kennt einen gültigen Umschlag und
   sonst nichts; die 18 Ablehnungen stehen dreimal von Hand da (Go, JavaScript, und nun Rust).
   Eine Vektordatei, die auch die **abzulehnenden** Eingaben trägt, würde die zwei bestehenden
   Fassungen enger halten. Das ist eine Änderung in `shared/testdata` und damit an einem
   eingefrorenen Ordner — Thomas' Entscheidung, nicht meine.
2. **Die Tiefen-Lücke ist der Maßstab** für jede künftige Fremdsprache: wer `core`, `store` oder
   `wire` nachbaut, muss das doppelte Mitglied eine Ebene tiefer ablehnen. `serde_json` tut es
   nicht. Das gehört in die Vertragsdoku, bevor es jemand bemerkt, indem er es falsch macht.
3. **`CLAUDE.md` Zeile 74–76 ist überholt.** Dort steht noch, `tools/modules.go` halte die Module
   fest; `THIRD-PARTY.md` sagt, die Datei ist seit dem 18.10.2026 weg. Eine Zeile, zwei Wahrheiten.

**Gemessen ist der Versuch, nicht der Vorschlag.** Die Zahl, die trägt: 41 Kisten, 577 163 Zeilen,
für 136 Zeilen Go.
## Punkt 68: Fremde Programme nach NGC portieren — das Programm ja, die Oberfläche nie

Thomas' Frage, wörtlich: „können wir Rust und solche Programme einfach portieren oder nicht
oder was müssen wir schaffen dass wir es können". Anlass war Wordcraft, ein Word-Nachbau in
Rust (272 101 Zeilen, 405 fremde Kisten, MIT/Apache). Die Frage ist nicht die von Punkt 67 —
dort ging es um NGCs **eigenen** Quelltext, hier um NGCs **Apps**.

Die Antwort steht in zwei Zahlen aus `shared/store`:

> **`MaxPackage` = 4 GiB. `MaxUIFile` = 16 MiB.**
> (`shared/store/catalog.go:25`, `shared/store/pack.go:22`)

NGC gibt einem fremden **Programm** vier Gigabyte und seiner **Oberfläche** sechzehn Megabyte.
Genau deshalb portiert das eine und das andere nicht.

### Teil A: gemessen, es trägt — ein 120-MB-Paket aus fremdem Rust

Der Versuch liegt unter `versuch-port/`, Beleg und kein Bestandteil. Gemessen am 20.10.2026,
Debian 13 unter WSL, rustc 1.99.1, podman 5.4.2:

| Schritt | Ergebnis | Dauer |
|---|---|---|
| `wordcraft-cli` statisch, `x86_64-unknown-linux-musl` | 41 986 968 Bytes | 4 min 05 s |
| Adapter (Go, statisch, linux/amd64), 145 Zeilen | 3 940 512 Bytes | — |
| `podman build --arch amd64` | Abbild gebaut | 4,0 s |
| `podman save --format docker-archive` | 127 236 096 Bytes | 7,5 s |
| `ngc-store pack` | **127 244 288 Bytes**, SHA-256 `9aae8b99…` | 0,99 s |
| `store.Walk` liest es zurück | `services/amd64.tar` → `Art=image Arch=amd64`; Manifest: 3 Aktionen, 1 Dienst | 0,08 s |

**Kein musl-gcc nötig** — Rust bringt seine eigene musl-libc mit. Das Paket ist von NGCs
**eigenem Leser** angenommen worden, nicht nur von einem Packer geschrieben.

Der Dienstvertrag (`shared/service/service.go:19-37`) ist vollständig sprachneutral:
Umgebungsvariablen rein, stdin/stdout/stderr als Kanal, Ausstiegscode als Status. Es gibt
**keine Bibliothek**, die ein Dienst einbinden müsste — das Terminal liest die Variablen von
Hand (`terminal/terminal.go:447-477`), ein Rust-Programm kann genau dasselbe.

### Vier Dinge, die ein Port lernen muss — jedes im Versuch bezahlt

1. **Die Aktion kommt als Umgebungsvariable, nicht als Argument.** `NGC_ACTION` und
   `NGC_PARAMS` (`service.go:19-29`) gegen `wordcraft-cli convert <in> <out>`. **Jeder Port
   braucht einen Adapter**, und nichts in NGC bringt einen mit. Der hier sind 145 Zeilen Go,
   davon 61 Kommentar. Er bindet `shared/service` ein, statt die Namen abzuschreiben, und das
   kostet gemessen **1 093 632 Bytes**: 2 846 880 ohne, 3 940 512 mit — `shared/service` zieht
   `shared/trust` und `shared/wire` mit. Richtig ist es trotzdem, denn abgeschriebene Namen
   sind die zweite Stelle, an der ein Vertrag auseinanderläuft.
2. **Der Adapter darf keine Shell sein.** `NGC_PARAMS` ist JSON von außen; ein Dateiname in
   einer Shell-Zeile ist der Weg, auf dem ein Dokument zu einem Befehl wird. Gemessen beim
   Schreiben: die erste Fassung benutzte `path/filepath` und wurde auf Windows gebaut —
   `filepath.IsAbs("/etc/passwd")` ist dort **false**, und `../etc/passwd` wurde zu
   `\data\etc\passwd`. Ein Prüfer, dessen Antwort vom Baurechner abhängt, ist kein Prüfer.
   Richtig ist `path` und **ablehnen statt entschärfen**.
3. **Der Container muss von selbst oben bleiben.** Der Controller fährt mit `podman exec`
   hinein; NGCs eigene Apps halten den Container mit `ngc-controller hold`
   (`node/main.go:161`). Ein fremdes Abbild hat das Programm nicht und muss seinen eigenen
   Leerlauf mitbringen — hier `/bin/sleep infinity`.
4. **`ngc-controller image` kann genau eine App bauen: das Terminal.** Die Pfade sind
   Konstanten (`node/image.go:65` `terminalDir`, `:70` `imageRepo`). Wer eine andere App baut,
   ruft podman selbst — was dieser Versuch getan hat. **NGC hat kein Werkzeug für fremde
   App-Abbilder.**

### Zwei Messfehler, beide im Versuch bezahlt

Sie stehen hier, weil beide wie ein Erfolg aussahen.

**`tools/check.sh` war grün, und das war wertlos.** Der Wächter bei `check.sh:121` verlangt,
dass **jede von git erfasste** `.go`-Datei von mindestens einer der vier Konfigurationen
übersetzt wird. Der Adapter lag zuerst in einem **eigenen** Go-Modul, damit `./...` des
Hauptmoduls ihn nicht erreicht — „Beleg, kein Bestandteil". Lokal lief `check.sh` grün, weil
er **vor** `git add` lief und der Wächter untracked Dateien nicht sieht. Die CI sah sie und
sagte: `no checked configuration compiles these files`. Der Wächter hat recht: Beleg im Repo,
den nichts typprüft, ist Beleg, dem niemand trauen kann. Das Muster dafür steht längst da —
`tools/kaputt-probe/main.go` ist auch Wegwerf-Messcode und liegt im Hauptmodul. Der Adapter
liegt jetzt dort ebenfalls; ausgeliefert wird er nicht, weil `tools/release-gate.sh` die
Pakete einzeln benennt, die es baut.

**`podman save` hat eine vorhandene Datei nicht überschrieben.** Nach dem Umbau des Adapters
meldete derselbe Befehl `SAVE_MS=965` statt 7 500 und `ls` zeigte **dieselbe Größe und
denselben Zeitstempel** wie vorher. Gemessen wäre dann das alte Abbild gewesen. Erst
`rm services/amd64.tar` davor gab ein echtes Ergebnis — 127 236 096 statt 126 142 464 Bytes.
Eine Messung schreibt ihr Ziel vorher weg.

### Teil B: gemessen, es trägt nicht — die Oberfläche, vierfach versperrt

Wordcrafts WebAssembly-Bau ist wirklich gelaufen (`trunk build --release`, 7 min):

| Blocker | Beleg im Quelltext | Messung am Bau |
|---|---|---|
| ES-Modul | `appframe.js:731` lehnt `type="module"` ab (`NEEDS_CLASSIC`) | `dist/web/index.html` hat `<script type="module">`, die glue-JS hat `export` |
| kein Netz | `connect-src 'none'` (`Page.php:54`) | glue-JS benutzt `fetch(` und `instantiateStreaming` |
| kein WASM | `script-src data:` ohne `'wasm-unsafe-eval'` (`Page.php:52`) | der Browser verweigert `WebAssembly.instantiate` |
| zu groß | `MaxUIFile` 16 MiB (`pack.go:22`) | **44 027 637 Bytes** = 42 MB; als `data:`-Adresse 56 MB, also auch über `MaxUITotal` 64 MiB |

Die ersten drei wären behebbar. Der vierte nicht, ohne `shared/` anzufassen.

Und das trifft nicht nur WASM. Jede portierte Oberfläche steht vor: kein
`localStorage`/IndexedDB (opake Herkunft, `frame-boot.js:43`), keine Worker
(`worker-src 'none'`), kein `eval`, kein Datei-Download (kein `allow-downloads`,
`appframe.js:244`), `MAX_PARAMS` 8 KiB, `MAX_CALL_RESULT` 8 MiB, 8 Kanäle je Fenster. Die
ganze App-API sind 117 Zeilen: `ngc.call`, `ngc.open`, `ngc.theme`/`onTheme` — sonst nichts.

**Gesucht und nicht gefunden:** in ganz `apps/web/` gibt es **keinen** Datei-Download und
**keinen** Upload, auch nicht in der Shell selbst. Eine Dokument-App kann heute nichts vom
Rechner des Nutzers öffnen und nichts dorthin speichern.

### Die Hausregel steht nicht im Weg — aber sie sagt es nicht

`CLAUDE.md:72` bindet „keine Laufzeitabhängigkeiten" an **`go mod vendor`**, also an NGCs
eigenen Quelltext. `shared/store/manifest.go:41` hat dagegen ein Feld `Publisher`, und
`CoreApp` (`wire.go:307`) trennt fremde Apps von Kern-Apps. Eine App ist ein unterschriebenes
Paket eines Herausgebers, kein Teil von NGCs Quelltext. Die 405 Kisten von Wordcraft fallen
also **nicht** unter Punkt 67.

**Nur: kein Dokument sagt das.** Das ist eine Regellücke, keine Programmierfrage, und sie
gehört in `THIRD-PARTY.md` — ein Satz, der entscheidet, ob ein App-Abbild unter die
Fremdcode-Regel fällt. Vorschlag im Pull Request; geändert ist dort nichts.

### Was wir schaffen müssen

Nach Wirkung geteilt durch Preis, nicht nach Reihenfolge im Quelltext.

1. **`ngc.asset(path)` — eine App liest ihre eigenen geprüften Dateien als Bytes.**
   Heute bekommt eine App nur, was HTML referenziert; Bytes nie. Das ui-tar liegt schon
   geprüft im Speicher (`appframe.js:656-668`), also ist es eine kleine Ergänzung in
   `ngc.js` und `appframe.js`. Schließt auf: WASM ohne `fetch`, große Schriften,
   Wörterbücher, Vorlagen. **Billigster und wirksamster Einzelschritt, ohne
   Sicherheitspreis.**
2. **Datei herein und heraus, über die Shell.** Nicht `allow-downloads` an den Rahmen geben,
   sondern die Shell besitzt den Dialog und die App bekommt und schickt Bytes über die
   Brücke — dann bleibt die App eingesperrt. Ohne das hat **keine** Dokument-App Sinn.
3. **Ein Werkzeug für fremde App-Abbilder.** `ngc-controller image` mit Pfaden als
   Argumenten statt als Konstanten, oder ein eigener Unterbefehl in `ngc-store`. Heute baut
   jeder Herausgeber mit podman von Hand, und die Digest-Pflicht (`runtime.CheckBase`) prüft
   dabei niemand.
4. **Ein Adapter-Muster, einmal dokumentiert.** Vier Fallen stehen oben; jeder Port tritt
   sonst in alle vier. Gehört nach `apps/server-controller/templates/` neben das Terminal.
5. **`'wasm-unsafe-eval'` in `FRAME_CSP`** (`Page.php:52`) — eine Sicherheitsentscheidung.
   Erst nötig, wenn ein konkretes Programm es braucht. Der ES-Modul-Blocker ist dafür
   **nicht** zu lösen: `wasm-bindgen --target no-modules` erzeugt ein klassisches Skript,
   das ist App-Seite.
6. **Worker im Rahmen** (`worker-src`, `Page.php:54`) — kleiner als Nr. 5, gleiche Art.
7. **`MaxUIFile`/`MaxUITotal` anheben** — ⚠ `shared/` ist eingefroren, steht als **P14** im
   v1.3-Vorschlag. Ohne das passt kein WASM dieser Größenklasse je in ein Paket.
8. **Rahmenprotokoll mit Längenkopf, einmal in `shared/`** — ⚠ ebenfalls eingefroren, **P15**.
   `serve` gibt Streams ohne Nachrichtengrenzen durch; das Terminal hat sich dafür einen
   eigenen 3-Byte-Kopf erfunden (`terminal.go:76-96`), und jede weitere App erfindet ihn neu.
9. **Quote für `/data`.** Heute nur gemessen, nicht begrenzt (`runtime/data.go:223-258`), und
   die alten App-Abbilder liegen mit drin (`install/update.go:206-218`).

### Vorschlag

**Portieren ja — den Programmteil. Die Oberfläche schreiben wir immer selbst.**

Und das ist keine Schwäche, die zu beheben wäre: es ist der Grund, warum jede NGC-App im
selben Käfig sitzt und warum eine fremde App keinen eigenen Weg nach draußen hat. Wer die
Oberfläche portierbar macht, gibt genau diesen Käfig auf. Die vier Blocker aus Teil B sind
zusammen die Tür, und man öffnet sie nicht aus Bequemlichkeit.

Reihenfolge: **Nr. 1 zuerst** (`ngc.asset`), dann **Nr. 2** (Datei herein/heraus), dann
**Nr. 3 und 4** (Werkzeug und Adapter-Muster — sie kosten nichts an Sicherheit und nehmen
jedem künftigen Port die vier Fallen ab). Nr. 5 bis 9 erst, wenn ein benanntes Programm sie
braucht — und Nr. 7 sagt, dass das für Wordcraft ohnehin nicht reicht.

Zu Wordcraft selbst: was man bekommt, ist `wordcraft-cli` als Dienst — `convert`, `text`,
`info`, `render` — mit einer Oberfläche, die wir schreiben. Nie Wordcrafts Editor. Wer den
will, schreibt ihn neu: gemessen sind das für „docx lesen und anzeigen" etwa 22–25k Zeilen,
für „lesen, bearbeiten, speichern" 75–85k, dazu 8–15k Oberfläche.

**Gemessen ist der Versuch, nicht der Vorschlag.** Die Zahl, die trägt: ein 120-MB-Paket aus
fremdem Rust, von NGCs eigenem Leser angenommen — und 42 MB Oberfläche, die in 16 MiB nicht
passen.

## Punkt 69: Die Linux-Prüfung war drei Tage rot, und niemand hat sie gezählt

Gefunden beim Grünmachen des Pull Requests zu Punkt 68 — und es war nicht dessen Fehler.

**Gemessen:** die letzten drei Läufe von `Linux-Pruefung` auf `main` sind **rot**, am 18., 19.
und 20.10.2026. Jedes Mal dieselbe Zeile, und jedes Mal im Schritt `zaehlen`:

```
ci-count: /home/runner/work/_temp/test.json is empty - nothing ran, and that is not a result
```

`check.sh` selbst war in allen drei Läufen **grün** (`323 passed, 0 failed`). Rot war nur der
Zähler — über eine Datei, die niemand schrieb.

### Zwei Hälften, die auseinandergelaufen sind

`.github/workflows/linux.yml` setzt `NGC_CHECK_TEST_JSON` und sagt dazu:

> check.sh writes the JSON stream of its own test run here instead of running the suite a
> second time just to count it.

`tools/check.sh` sagte an derselben Stelle das Gegenteil, mit Datum:

> NOTE 18.10.2026: … NGC_CHECK_TEST_JSON, a -json stream teed to a file for
> tools/ci-count.sh. **It is NOT taken here** … Whoever wants the JSON builds it ON this
> mechanism, not beside it.

Der Satz ist richtig begründet: der Lauf schreibt `-v` in ein Protokoll, **weil** das
Protokoll zurückgelesen wird — für Tests, die sich still zur Seite stellen. Ein zweiter Lauf
nur zum Zählen war genau das, was vermieden werden sollte. Nur: der Workflow wurde nicht
mitgezogen. Seit dem 18.10.2026 lief `ci-count.sh` auf eine leere Datei, und die Meldung war
beide Male wahr — die Datei *war* leer.

**Das ist die Klasse „zwei Stellen, eine Wahrheit", und hier mit der schlimmsten Folge:** eine
Prüfung, die immer rot ist, prüft nichts mehr. Wer drei Tage lang rote Läufe sieht, hört auf
hinzusehen — und der nächste echte Befund geht darin unter. Punkt 68 ist genau so an einem
Fehler vorbeigelaufen, der gar nicht ihm gehörte.

### Behoben, ohne einen zweiten Testlauf

`go tool test2json` macht aus genau der `-v`-Ausgabe, die `check.sh` schon hat, den
JSON-Strom, den `ci-count.sh` zählt. Es gehört zur Go-Werkzeugkette, kostet also keine
Abhängigkeit, und das Textprotokoll bleibt das Erste — es ist weiter das, was zurückgelesen
wird. Also genau, was die NOTE verlangte: **auf** dem Mechanismus, nicht daneben.

Gemessen am 20.10.2026, beide Pfade:

| | Ergebnis |
|---|---|
| grüner Lauf, `NGC_CHECK_TEST_JSON` gesetzt | `check.sh` Ausstieg 0, 3 476 197 Bytes JSON; `ci-count` **3349 passed, 0 FAILED, 87 SKIPPED**, Ausstieg 0 |
| roter Lauf (ein Test mit `t.Fatal`) | `test2json` wandelt, `ci-count` nennt den Test, `ci-count: red`, Ausstieg 1 |

Zwei Kleinigkeiten, die dabei auffielen und mit im Satz stehen:

- **Der Lauf steht jetzt in einem `if`.** `check.sh` ist `set -euo pipefail`; ein bloßes
  `go test …` mit `rc=$?` dahinter hätte das Skript bei rotem Lauf sofort beendet, und die
  Wandlung wäre nie gelaufen — also hätte die CI **gerade dann** nicht gezählt, wenn es
  darauf ankommt. Der Workflow-Schritt läuft mit `if: always()`; jetzt hält die eine Hälfte
  das, was die andere verspricht.
- **`packages: 1 ok` ist kein Befund.** Ein gemischter `-v`-Strom von `go test ./...` sagt
  `test2json` nicht, zu welchem Paket eine Zeile gehört, also meldet `ci-count` ein Paket,
  wie viele auch liefen, und ein rotes Paket kommt ohne Namen heraus. Die **Fall**-Zahlen
  sind richtig, und nur die beurteilt `ci-count`. Steht als Kommentar an der Stelle, damit
  es niemand als Fund liest.

### Was offen bleibt

**Nicht gemessen:** ob die drei anderen Aufträge (`-race`, `root`, `fuzz`) ihre
JSON-Ströme richtig schreiben. Sie waren in allen Läufen grün, also schreiben sie etwas —
aber sie bauen ihren Strom selbst im Workflow, nicht über `check.sh`, und ob ihre Zahlen
stimmen, hat niemand gegengerechnet. Das ist dieselbe Frage wie hier, nur eine Stelle weiter.

**Und die eigentliche Lücke:** nichts sagt Bescheid, wenn `main` rot ist. Drei Tage lang hat
das niemand bemerkt, und bemerkt wurde es erst, weil ein Pull Request daneben rot war.

### Nachtrag 20.10.2026: derselbe Commit, einmal grün und einmal rot — und niemand konnte sagen warum

Nach der Behebung oben lief der **push**-Lauf vollständig **grün**, alle drei Aufträge. Der
**pull_request**-Lauf auf **demselben Commit** war **rot**. `main` hatte sich nicht bewegt —
`git merge-base` ist weiter `4c08ef7`, der Merge ist also inhaltsgleich mit der Zweigspitze.

Und in jedem Auftrag fiel ein **anderer** Test um:

| Auftrag | umgefallen |
|---|---|
| `check.sh + counted test run` | `TestAPackageLargerThanOneWindowGetsThrough` (`apps/server-controller/classes/serve`) |
| `controller cases that need root` | derselbe |
| `-race (Linux goroutines)` | `TestAPanicInTheHelloHookKeepsTheTunnel` (`apps/server-api/classes/controllers`) |

**Lokal gemessen, je 20 Läufe: beide 20/20 grün.** Beides sind Tests über Nebenläufigkeit und
Flusskontrolle — ein Paket, das größer als ein Kreditfenster ist, und ein Panik im Hello-Haken,
der den Tunnel halten soll. Das Bild passt zu Last, nicht zu einem Defekt in diesem Zweig.

**Nicht gemessen, und deshalb ausdrücklich offen:** ob diese zwei Tests wirklich wackeln. Ein
grüner und ein roter Lauf sind keine Rate. Dafür bräuchte es dieselben Tests mehrfach unter
Last auf dem Runner, und das ist hier nicht gelaufen.

#### Was daran wirklich kaputt war: die CI sagte nie, WARUM

Zu jedem dieser Fehlschläge stand in den Protokollen **nur der Name**:

```
--- FAIL: TestAPackageLargerThanOneWindowGetsThrough (0.01s)
```

Keine Zeile mehr. `go test -v` schreibt die Ausgabe eines Tests **vor** diese Zeile, und
`check.sh` grepte nur die Zeile selbst heraus. Bei den Aufträgen `-race` und `root` war es noch
dichter: sie leiten `go test -json` in eine Datei, die kein Schritt je zeigt, und `ci-count.sh`
nannte ebenfalls nur Namen. **Eine rote Prüfung, die den Grund verschweigt, lässt sich von einem
Wackler nicht unterscheiden — und genau das ist die einzige Frage, die ein roter Lauf
beantworten muss.**

Behoben an beiden Stellen:

- **`tools/check.sh`** druckt jeden umgefallenen Test mit seinem Block, von `=== RUN` bis
  `--- FAIL` — dort landen `t.Errorf` und `t.Fatal`. Unterfälle kommen mit den Zeilen ihres
  Elterntests.
- **`tools/ci-count.sh`** druckt zu jedem roten Fall die `Output`-Sätze aus dem JSON-Strom.
  Damit sagen auch `-race` und `root` den Grund, ohne dass eine Datei hochgeladen werden muss.

Gemessen am 20.10.2026 gegen ein echtes rotes Protokoll (ein Test mit zwei `t.Errorf`, einer mit
`t.Fatal` in `t.Run`): beide nennen Test, Datei, Zeile und Text; der grüne Lauf bleibt
unverändert bei 3349 passed, 0 FAILED, 87 SKIPPED und Ausstieg 0.

#### Zwei Messfallen auf dem Weg dahin, beide bezahlt

1. **Das Heredoc hat `\n` zu einem echten Zeilenumbruch gemacht.** Der erste awk-Block ging
   über eine Shell-Heredoc ins Skript und trug danach eine Zeichenkette über zwei Zeilen —
   ein ungültiges awk-Programm. `bash -n tools/check.sh` meldete **ok**, weil der Fehler
   innerhalb einer einfach zitierten Zeichenkette lag und erst zur Laufzeit zählt, also genau
   auf einem roten Lauf. Gefunden nur, weil der Block gegen ein echtes Protokoll geprüft wurde.
   **Ein Syntaxprüfer der Hülle prüft nicht den Inhalt.** Der Block steht jetzt ohne jeden
   Gegenstrich da und wird über eine Datei geschrieben, nicht über ein Heredoc.
2. **`substr($0, 10)` griff ein Zeichen zu früh.** `go test` schreibt drei Leerzeichen nach
   `RUN`, der Name kam mit führendem Leerzeichen heraus und passte auf nichts: der Block
   druckte **leer**, und das sieht aus wie „es gab nichts zu zeigen". Jetzt `$3`, weil ein
   Testname kein Leerzeichen trägt.

Beide gehören zusammen mit Punkt 69 selbst in eine Reihe: **eine Prüfung, die schweigt, ist
schlimmer als eine, die rot ist.**

### Nachtrag 20.10.2026, zweiter Teil: die CI hat sich selbst überfahren

Der Nachtrag oben nannte „Last" als Vermutung. Sie ist jetzt **gemessen**, und die Ursache ist
ein Fehler im Arbeitsablauf selbst.

`.github/workflows/linux.yml` läuft `on: push` **und** `on: pull_request`. Ein Zweig mit
offenem Pull Request löst also beides aus. Der Riegel dagegen war:

```yaml
concurrency:
  group: ${{ github.workflow }}-${{ github.ref }}
```

**`github.ref` ist für die zwei Ereignisse nicht dieselbe Zeichenkette:** `refs/heads/<zweig>`
beim Push, `refs/pull/<n>/merge` beim Pull Request. Zwei Gruppen, also hat `cancel-in-progress`
nichts abgebrochen — und **beide Läufe liefen vollständig durch**. Das ist der Beweis, nicht die
Vermutung: gemessen zweimal, am 20.10.2026 um 09:57 und um 10:43, Start zwei Sekunden
auseinander, beide bis zum Ende. Zwei komplette Suiten gleichzeitig, darunter zwei `-race`-Läufe
(der Auftrag mit 40 Minuten Zeitgrenze).

Und beide Male fiel **nur im pull_request-Lauf** ein Zeittest des Tunnels um, während der
push-Lauf auf **demselben Commit** grün war:

| Lauf | umgefallen |
|---|---|
| 09:57 | `TestAPanicInTheHelloHookKeepsTheTunnel` |
| 10:43 | `TestAPingLostToOurOwnWriteDoesNotKillTheTunnel/one_interval,_in_a_fraction_of_a_second` |

Der zweite nennt sich selbst „in a fraction of a second" und brauchte **2,46 s**, der
Elterntest **26,45 s**.

**Behoben:** die Gruppe wird auf den **Zweig** geschlüsselt statt auf `github.ref` —
`github.head_ref || github.ref_name` ergibt bei beiden Ereignissen dieselbe Zeichenkette, also
bricht der eine Lauf den anderen ab. Jeder Push wird weiter gemessen, wofür der `-race`-Auftrag
ausdrücklich da ist; was aufhört, ist zweimal zahlen und Zeitverhalten unter doppelter Last
messen.

**Nicht gemessen:** ob diese Zeittests unter einfacher Last verlässlich sind. Die doppelte Last
ist weg, die Frage nicht beantwortet.

#### Offen, und es gehört nicht mir

`apps/server-controller/classes/link/link_test.go`, Zeile 1312:

```
the tunnel counted as gone although the API answers every ping it sees
```

Die Stelle steht **nach** `close(drain)`: die Drosselung ist gelöst, und der Test wartet bis zu
30 Sekunden auf einen Ping — bricht aber ab, sobald `tn.dead` gesetzt ist. Auf einem langsamen
Läufer ist der Tunnel in der Drosselphase (dreimal 300 ms plus Einplanung) schon totgemeldet,
bevor der erste Ping durchkommt. Der Test setzt voraus, dass das Lösen der Drosselung den Ping
vor die Todesfrist bringt, und unter Last stimmt das nicht.

**Hier wird daran nichts geändert.** Es ist der Wächter eines anderen Teils, und dieses Projekt
hat für genau diesen Fall ein eigenes Muster — `tools/check.sh` nennt es im Kommentar: die
Uhrtests in `apps/server-api/classes/login` stellen sich zur Seite, wenn ihre eigene
Nullkontrolle sagt, dass die Maschine den Faktor heute nicht auflösen kann, und melden das als
`UNMEASURABLE`. Ob dieser Test dasselbe bekommen soll oder eine größere Frist, entscheidet, wem
er gehört. **Ein Wächter, den ein Fremder leise zum Überspringen bringt, ist schlimmer als ein
roter Lauf** — darum steht er hier als Befund und nicht als Änderungssatz.

### Nachtrag 20.10.2026, dritter Teil: der Zeittest hat eine Nullkontrolle — und was sie NICHT ist

Thomas hat entschieden, den Test zu härten, nach dem Muster der Uhrtests in
`apps/server-api/classes/login`. Das ist getan, und das Wichtigste daran ist eine
Einschränkung.

**Erst die Ursache, genau.** `ping()` (`link.go:736`) macht einen Tunnel nur für einen Ping
tot, der scheiterte, **während kein Schreibvorgang von uns lief**:

```go
mine := t.writing.Load() > 0
...
if mine { continue }
t.dead.Store(true)
```

Der Unterfall baut genau diesen Schild: der Stuffer hält einen Schreibvorgang im Steckplatz,
also überspringt jeder scheiternde Ping sich selbst. Nach `close(drain)` räumt der Drain-Faden
in `congested` aber **beides** weg — `p.free()` und dann `s.halt()`. Damit fällt der Schild,
und wenn der erste Ping danach noch im Rückstau scheitert, tut `ping()` genau das, was es
verspricht: `dead`. Kein Defekt, sondern das Aufräumen des Tests, das mit dem Rückstau um die
Wette läuft. Auf einem schnellen Rechner gewinnt der Ping, auf einem belasteten Läufer nicht.

**Die Nullkontrolle** liest das am Stuffer selbst ab, nicht per Abtastung: er schließt `done`,
wenn sein Faden zurückkommt, also beweist ein geschlossenes `done` in diesem Augenblick, dass
der Schild weg ist (`shieldGone`). Dann:

- Schild **oben** und Tunnel tot → **rot**, das ist der Defekt, für den der Test da ist.
- Schild **unten** und Tunnel tot → **`UNMEASURABLE`**-Skip, mit Zahlen. Der Lauf hat die
  Maschine gemessen, nicht `ping()`.

Dasselbe Wort und dieselbe Begründung wie in `login`: `go test` ohne `-v` druckt für einen
übersprungenen Test **nichts**, und `tools/check.sh` grept danach — ein Test, der sich still
zur Seite stellt, wäre schlimmer als ein falsches Rot.

#### Was die Kontrolle ausdrücklich nicht ist

**Sie nimmt keinen Wächter weg, und der Skip dort war nie der Wächter.** Gemessen, fünf grüne
Läufe in Folge: wenn diese Schleife zum ersten Mal hinsieht, ist der Stuffer **jedes Mal**
„already finished" — der Schild ist dort also immer unten, und der Test besteht, weil der Ping
nach **fünf bis sechs Millisekunden** da ist. Die `dead`-Prüfung in dieser Schleife war damit
von Anfang an nur eine Abkürzung, um nicht dreißig Sekunden auf einen längst toten Tunnel zu
warten.

Der eigentliche Wächter ist die Schleife **vor** dem Drain: dort steht nachweislich ein
Schreibvorgang von uns im Steckplatz (`waitWriting`), und ein toter Tunnel ist dort rot ohne
Ausweg. Dazu die eine Zusage, die dem Unterfall den Namen gibt: nach dem Öffnen **muss** ein
Ping die API erreichen — das ist die Frist von dreißig Sekunden. **Beides ist unverändert.**

#### Welche Stufe von „geprüft" das erreicht

Nach der Vierteilung: geschrieben / übersetzt / enthalten / **ausgeführt**.

- Die Entscheidung selbst ist **ausgeführt**: `TestShieldGoneReadsTheStuffer` hält `shieldGone`
  und `stufferState` an beide Antworten, absichtlich herbeigeführt.
- Der **Skip-Zweig selbst ist hier nicht gelaufen.** 25 Läufe des Unterfalls unter 128
  Lastschleifen auf 64 Kernen: **50 PASS, 0 FAIL, 0 SKIP**, erster Ping nach 5 bis 37 ms. Diese
  Maschine kommt nicht in die Lage.
- **Der Auslöser ist aber gemessen — in der CI.** Genau diese Zeile war am 20.10.2026 um 10:43
  rot (`the tunnel counted as gone although the API answers every ping it sees`), und davor um
  09:57 der Schwestertest. Dort wird aus dem Rot künftig ein benannter Skip, und `check.sh`
  nennt ihn Zeile für Zeile.

Nach der Änderung lokal: `check.sh: ok`, **3350 passed, 0 FAILED, 87 SKIPPED**, und
`0 guard(s) unmeasurable` — es stellt sich also nichts unbemerkt zur Seite.

**Offen bleibt:** ob das Aufräumen in `congested` die bessere Stelle wäre — erst den Rückstau
durchlassen und den Stuffer **danach** anhalten, dann gäbe es das Rennen gar nicht. Das wäre
eine Änderung am Helfer, die alle vier Fälle dieses Tests berührt, und sie ist **nicht
gemessen**. Wer den Teil besitzt, entscheidet das; die Kontrolle hier macht den Lauf
inzwischen ehrlich, statt ihn zu verdecken.

### Nachtrag 21.10.2026: drei Umbauten am Schild, alle verworfen — das Fazit

Der offene Punkt aus dem dritten Teil lautete: das Rennen nach dem Drain beseitigen, statt es
nur zu benennen. Dafür sind drei Umbauten gebaut und gemessen worden, alle unter 128
Lastschleifen auf 64 Kernen, immer derselbe Rechner. „Läufe" zählt Durchgänge des Unterfalls;
„Schild oben" ist, wie oft der Stuffer beim ersten Hinsehen noch schrieb:

| Umbau | Läufe | **Fehlschläge** | Schild oben |
|---|---|---|---|
| **Ausgangsstand**, nur Nullkontrolle | 30 | **0** | 0 von 30 |
| **1. Rückstau zuerst:** erst auf einen Schreibvorgang warten, dann `s.halt()`, dazu `quietStuffer` | 20 | **2** | 12 von 20 |
| **2. Byte-Zähler im Rohr:** `pipe.fwd`, `drained()` wartet auf Ruhe (50 **und** 200 ms geprüft) | 16 | **2** | 16 von 16 |
| **3. Drosselung:** 4 KiB statt `wire.MaxPayload` ab dem Drain | 30 | **3** | 27 von 28 |

Die zwei hinteren erreichen das Ziel — der Schild bleibt bis zum ersten Ping oben, wo er im
Ausgangsstand **nie** oben war. Nur kostet jeder von ihnen Fehlschläge, wo der Ausgangsstand in
30 Läufen keinen hat.

**Keiner ist ausgeliefert.** Alle Fehlschläge sind dieselbe Meldung, und sie ist nicht die
alte:

```
Run = link: the API cannot be reached, want an orderly end
```

**Der Satz, der alle drei erklärt:**

> Der Schild **ist** der Verkehr. Mehr Schild heißt mehr in Flug, und der geordnete Abschluss
> braucht das Gegenteil. Kleinere Frames senken die Menge und machen den Schild dafür lückig.

Die Einzelheiten, soweit sie etwas tragen:

- **Zu 1 und 2:** `ErrAPI` kommt aus der **Leseseite** (`link.go:283`), nicht von einer
  Schließnachricht, die im Rohr steckt — die API-Seite liest durchgehend (`apiSide.quiet`).
  Entscheidend ist die **Menge in Flug** beim Abschlussdialog: im roten Lauf 16 Frames à
  `wire.MaxPayload`, rund ein Megabyte, gegen 5 bis 12 vorher.
- **Zu 2:** ein Zähler im Rohr kann kein „leer" melden. Was der Controller hinter der Sperre
  aufstaute, liegt in **seinem** Sendepuffer — Megabytes auf Linux, wie der Kommentar bei
  `pipe.free` sagt —, und dort sieht kein Rohr hinein. Es bleibt „es kam eine Weile nichts
  durch", also eine Ruhezeit und kein Beweis; vierfache Ruhezeit änderte das Ergebnis nicht.
- **Zu 3:** `writing` ist nur oben, **während** `t.c.Write` läuft. Ein 4-KiB-Frame auf offenem
  Rohr ist in Mikrosekunden durch, der Schild bekommt also Lücken — und `ping()` liest den
  Zähler in einem einzigen Augenblick. Ein lückiger Schild ist schlechter als keiner, weil er
  die Lücke zufällig macht.
- **Und eine Messregel, teuer bezahlt:** Umbau 3 sah in den ersten zwei Stichproben wie die
  Lösung aus — 8 Läufe mit 16 PASS / 0 FAIL, dann 8 weitere mit 0 FAIL. Erst die dritte, mit
  14 Läufen des kurzen Unterfalls, brachte die 3 Fehlschläge. Umbau 1 zeigt dasselbe von der
  anderen Seite: eine Stichprobe davon war 12 von 12 grün. **Eine Stichprobe von acht ist keine
  Rate** — nach dem ersten Lauf ausgeliefert, wäre zweimal eine Verschlechterung eingezogen,
  die wie eine Verbesserung aussah.

**Nicht gemessen und offen:** ob der Unterfall überhaupt eine Aussage über `ping()` **nach** dem
Drain treffen kann, oder ob seine ganze Aussage davor liegt. Das entscheidet, wem der Teil
gehört.

### Nachtrag 21.10.2026: und dabei kam ein Fehler in meiner eigenen Nullkontrolle heraus

Umbau 3 hat den Test diese Zeile drucken lassen:

```
the tunnel counted as gone 22ms after the way opened although a write of ours
still stands in the socket (writing 0)
```

**Die Meldung widerspricht sich selbst** — „a write still stands" und „writing 0" in einem
Satz. Die Ursache ist mein `shieldGone`: es fragt, ob der **Stuffer-Faden lebt**, und das ist
nicht dasselbe wie **ein Schreibvorgang läuft**. Bei Drosselung lebt der Faden und schreibt
gerade trotzdem nicht.

Dahinter steckt der tiefere Fehler: `ping()` liest `t.writing` in **seinem** Augenblick, direkt
nachdem der Ping aufgab (`link.go:736`). Ein toter Tunnel heißt also, **dieser** Lesevorgang sah
Null. Der Augenblick ist vorbei, wenn die Schleife hinsieht, und keine spätere Lesung von
`writing` und kein Zustand des Stuffers holt ihn zurück. **Ein rotes Urteil in dieser Schleife
war von Anfang an nicht begründbar** — es hätte eine Maschine für einen Defekt erklärt.

**Behoben:** dort ist ein toter Tunnel jetzt **immer** `UNMEASURABLE`, mit allen Zahlen (Zeit,
Pings, `writing` jetzt, Zustand des Stuffers) und der Begründung im Code. Der rote Zweig ist
weg, weil er nie einen hätte geben dürfen.

**Es nimmt keinen Wächter weg**, dieselbe Begründung wie im dritten Teil: der Wächter ist die
Schleife **vor** dem Drain, wo `waitWriting` beweist, dass ein Schreibvorgang steht, und die
Frist von dreißig Sekunden, in der ein Ping ankommen muss. Beide sind unverändert rot, wenn sie
brechen.

**Stand danach:** 22 Läufe unter derselben Last **22 PASS, 0 FAIL, 0 SKIP**; `check.sh: ok`,
**3350 passed, 0 FAILED, 87 SKIPPED**, `0 guard(s) unmeasurable`.

Und: **der Skip-Zweig ist jetzt wirklich gelaufen.** Im dritten Teil musste er als „nur
übersetzt" ausgewiesen werden; in Umbau 3 hat er zum ersten Mal gefeuert und seine Zeile
gedruckt. Er steht damit auf der vierten Stufe — durch einen Versuch, der selbst verworfen
wurde.

### Nachtrag 23.10.2026: die Behebung war richtig — und lag drei Tage auf einem Zweig

Der Abschnitt „Behoben, ohne einen zweiten Testlauf" oben stimmt in jedem Punkt. Der Haken
ist, was er nicht sagt: **wo** die Behebung lag.

**Gemessen:**

```
git rev-list --ancestry-path 92ee9f1..origin/main      -> erster main-Commit: 6e35852
git log --merges --ancestry-path 92ee9f1..origin/main | tail -3
   -> 1b9f206  2026-10-23 17:19:05  Merge pull request #91
      36b3511  2026-10-23 17:19:24  Merge pull request #93
      bc09575  2026-10-23 17:20:22  Merge pull request #86
```

`92ee9f1` („fix: write the test JSON stream …") ist vom **20.10.2026 11:57**. In `main` kam er
am **23.10.2026 17:20** an — **drei Tage und fünf Stunden** später. In dieser Zeit stand die
Behebung hier als erledigt, und die Prüfung war weiter rot.

**Darum waren auch `pull_request`-Läufe rot, und das ist der Teil, der am meisten täuscht.** Ein
`pull_request`-Lauf prüft den Merge — aber der Merge entsteht gegen den damaligen Stand von
`main`, und der hatte den Schreiber nicht:

| | gemessen |
|---|---|
| `fix/stille-ablehnung`, Spitze `64e2d52` (23.10. 16:21) | `test2json` **0×** in `tools/check.sh` |
| `docs/sechs-waende`, Spitze `9eb7be4` | `test2json` **0×** |
| Basis, gegen die dessen Merge gerechnet wurde (`b782635`) | enthält `92ee9f1` **nicht** |

**Die Größe des Schadens, über alle 182 Läufe von `Linux-Pruefung`:**

| | |
|---|---|
| rote Läufe | 93 |
| davon mit rotem `check.sh + counted test run` | 82 |
| davon mit **grünem `check.sh`** — rot nur am Zählschritt | **42** |

Die 42 liegen auf 20 Zweigen; 18 hatten einen Pull Request: **3 offene** (#71, #82, #88) und
**15 gemergte**, 14 davon am 23.10. Von den am 23.10. gemergten Pull Requests waren **14 durch
dieses eine Loch rot**, während sie zusammengeführt wurden.

Alle Zahlen sind **Stand 23.10.2026 22:00**, und sie laufen weg: `main` nimmt in dieser Phase
etwa zehn Merges je Stunde. Beim Nachrechnen eine Stunde später waren es schon 21 am 23.10.
gemergte Pull Requests statt 20 und 6 offene statt 5. Die drei offenen oben waren es zu beiden
Zeitpunkten.

**Die Klasse ist eine neue, und sie ist schlimmer als „zwei Stellen, eine Wahrheit":** eine
Behebung, die geschrieben, gemessen und ins Entscheidungsbuch eingetragen ist, aber nicht in
`main` liegt. Das Buch sagt dann die Unwahrheit, ohne dass jemand gelogen hat — und wer hier
nachliest, hört auf zu suchen. Die vier Stufen aus `messen.md` (geschrieben · übersetzt · im
Abbild enthalten · ausgeführt) haben für CI-Werkzeug offenbar eine fünfte: **in `main`.**

**Behoben wird daran hier nichts** — der Schreiber ist seit 17:20 drin. Was dazukommt
(PR #109): die Zählung ist auf Zusicherungen gestellt (`tools/ci-count-test.sh`, 5 Zusicherungen,
7 Mutanten tot, 1 Kontrolle lebendig), `ci-count.sh` trennt **fehlende** von **leerer** Datei
und nennt den Schritt davor als Befund statt die Datei, und die Prüfung hält jetzt fest, dass
Arbeitsablauf und `check.sh` sich über `NGC_CHECK_TEST_JSON` noch einig sind — genau die
Prüfung, deren Fehlen die zwei Hälften drei Tage auseinanderlaufen ließ. Beide Riegel bleiben
rot; ein Zählwerk, das bei leerer Eingabe grün wird, wäre schlimmer als der Zustand davor.

**Offen, und jetzt das größere Loch:** oben stand „nichts sagt Bescheid, wenn `main` rot ist".
Dazu kommt: **nichts sagt Bescheid, wenn eine als behoben verbuchte Behebung nicht in `main`
liegt.** Beides ist nicht gebaut.

## Punkt 70: Ein Paket über eine Fenstergröße kommt unter Last nicht durch — `E_PROTOCOL` statt `ok`

Gefunden, weil die CI endlich sagt, **warum** ein Test umfällt (Punkt 69). Der Befund gehört
nicht zu dem Zweig, auf dem er auftrat: `apps/server-controller/classes/serve/`, `shared/` und
`apps/server-api/` sind dort **byteweise der Stand von `main`** (`git diff origin/main --stat`
auf diese Pfade: leer). Also ein Befund auf `main`.

### Was umfällt

`TestAPackageLargerThanOneWindowGetsThrough`
(`apps/server-controller/classes/serve/activate_test.go:276`). Der Fall schickt **6 Rahmen à
60 000 Bytes** = 360 000, also mehr als `wire.DefaultWindow` (262 144), und hält damit
`chanReader.give`: jeder an den Installierer gegebene Rahmen muss den Kredit sofort
zurückzahlen.

Gemessen am 21.10.2026, derselbe Rechner, `go test -count=20`:

| | Fehlschläge |
|---|---|
| ohne Last | **0 von 20** |
| unter 128 Lastschleifen auf 64 Kernen | **4 von 20** |

Und die Meldungen sind dieselben drei, jedes Mal:

```
CLOSE = {Status:error Code:E_PROTOCOL Message:Ungültige Nachricht.}, want ok
result = "", want the empty object
the installer read 60000 bytes, want 360000
```

**Die gelesene Menge ist immer ein Vielfaches von 60 000** — gemessen 0, 60 000, 60 000 und
120 000 Bytes. Der Transfer bricht also an einer **Rahmengrenze** ab, nicht mitten in einem
Rahmen, und zwar nach null, einem oder zwei von sechs.

Dazu im `-race`-Auftrag derselben CI-Läufe, im gleichen Paket:
`TestAPeerThatHoldsItsCreditDoesNotHoldTheChannel` (`app_test.go:672`) meldet `E_INTERNAL`,
wo `E_TIMEOUT` erwartet ist. Nicht lokal nachgestellt — hier gibt es kein `-race` (kein
C-Übersetzer), also **nicht gemessen**, ob es dieselbe Ursache ist.

### Die Ursache — gemessen am 23.10.2026, und es war der Test

**Dieser Abschnitt hieß zuerst „Was ich dazu NICHT sage" und sagte, die Ursache sei nicht
gemessen. Jetzt ist sie es.** Die zwei Vermutungen, die hier als widerlegt standen, waren
beide richtig widerlegt — nur war die dritte Stelle die richtige:

```go
case wire.TypeData:
    n := uint64(len(f.Payload))
    if n > ch.in || !ch.room() {        // channel.go:629
        ch.claimLocked(closeProtocol)
```

`ch.in` ist der Kredit, den **diese** Seite gewährt hat und der noch nicht verbraucht ist. Der
erste GRANT gibt `wire.DefaultWindow` = 262 144 Bytes. Der Test schob **sechs** Rahmen à 60 000
sofort hinaus, ohne auf weitere GRANTs zu warten: vier passen (240 000), beim **fünften** fehlen
37 856 Bytes, und wenn der GRANT für die schon gelesenen Rahmen noch nicht da ist, lehnt der
Kanal ab — **völlig zu Recht**. Ein Sender dieses Vertrags darf seinen Kredit nicht überziehen.

**Die Zahl, die es beweist**, kommt aus der Mutationsprobe unten: `22144 bytes of credit left
and 60000 to send` — und 262 144 − 4 × 60 000 ist genau 22 144.

**Es war also der Test und nicht der Kanal.** Die gute Nachricht steckt darin: an `ActActivate`
ist nichts zu reparieren, und die Sorge aus dem Absatz weiter unten — ein Verwalter liest
„Ungültige Nachricht" über ein heiles Paket — trifft nicht zu, solange die API ihren Kredit
einhält. Die echte API tut das; nur der Test tat es nicht.

### Behoben: die API-Seite des Tests zahlt

Neu ist `apiSide.dataPaid` (`serve_test.go`): wo der Kredit nicht reicht, wartet die Senderseite
erst auf den GRANT und schreibt dann. Das ist, was jeder echte Gegenpart tut.

| | Fehlschläge |
|---|---|
| vorher, Linux unter Last | 5 von 20 |
| vorher, Windows unter Last | 4 von 20 |
| **nachher, Linux unter Last** | **0 von 20** |

**Und der Wächter ist nicht weicher geworden, sondern schärfer** — Mutationsprobe: `give` so
verändert, dass es keinen Kredit mehr zurückgibt. Dann endet der Test nicht grün und nicht mit
`E_PROTOCOL`, sondern mit

```
waiting for a GRANT on channel 1, 22144 bytes of credit left and 60000 to send: context dead
Run did not end
```

Vorher gaben ein kaputtes `give` und eine belastete Maschine **dasselbe** Rot. Jetzt sind sie zu
unterscheiden: das eine wartet auf einen GRANT und nennt den Kredit, das andere ist kein Rot
mehr.

`check.sh: ok`, 3350 passed, 0 FAILED, 87 SKIPPED.

### Die zwei Vermutungen davor, als widerlegt notiert

- *Die API-Seite schickt DATA, bevor der Kanal offen ist.* Nein: `apiSide.open`
  (`serve_test.go:420`) wartet auf das GRANT, bevor der Test weiterläuft.
- *`Channel.room()` schlägt zu.* Die Stelle wäre die richtige — ihr Kommentar nennt
  ausdrücklich `E_PROTOCOL` für einen Gegenpart, der seinen Kredit in Stücke unter
  `queueCost` (32 Bytes) zerlegt (`channel.go:698`). Nur passt die Rechnung nicht: bei
  262 144 Bytes Kredit erlaubt `(ch.in+ch.bytes)/32+2` über achttausend wartende Rahmen, und
  es sind sechs.

Beide waren an der falschen Stelle, und das Raten hat erst aufgehört, als die Linux-Messung
eine verlässliche Nachstellung gab (5 von 20) — damit ließ sich der Zweig mutieren, statt ihn zu
erraten.

### Was es für den Nutzer heißt: nichts — und das stand nicht von Anfang an fest

`ActActivate` ist der Weg, auf dem ein **App-Paket** zum Controller geht. Solange die Ursache
offen war, stand hier die Sorge: sitzt der Abbruch im Kanal, dann scheitert auf einer belasteten
Maschine die Installation eines Pakets über eine Fenstergröße, und der Verwalter liest
„Ungültige Nachricht" — die Meldung für ein kaputtes Paket — über ein heiles. Dieselbe Klasse wie
Punkt 65.

**Die Sorge trifft nicht zu.** Der Kanal hat recht gehandelt, und die echte API hält ihren
Kredit ein; nur der Test tat es nicht. An `ActActivate` ist nichts zu reparieren.

**Und das ist der Grund, einen solchen Befund nicht als Wackler abzulegen, bevor man ihn
kennt** — auch wenn er sich dann als einer herausstellt. Er stand als Frage da und nicht als
Antwort, und die Antwort kam aus einer Messung, nicht aus einer Einschätzung.

### Nachstellen

Der Fall ist behoben, also läuft er jetzt durch. Um die **alte** Erscheinung zu sehen, muss
`dataPaid` im Test wieder durch `data` ersetzt werden; dann gilt:

```powershell
go test -count=20 -run TestAPackageLargerThanOneWindowGetsThrough .\apps\server-controller\classes\serve\
```

Ohne Last läuft das durch. Mit Last — 128 beschäftigte Schleifen neben dem Lauf — fielen vier
von zwanzig um, auf Linux fünf von zwanzig. Der Lauf braucht etwa zwanzig Sekunden.

**Auf Linux** (wo die CI steht), mit dem für `linux/amd64` übersetzten Testbinär:

```powershell
go test -c -o D:\claude\rust\serve.test .\apps\server-controller\classes\serve\
wsl -d Debian -u root -- /mnt/d/claude/rust/serve.test -test.run TestAPackageLargerThanOneWindowGetsThrough -test.count=20
```

### Was noch zu entscheiden ist

Für diesen Fall nichts mehr. Der zweite
(`TestAPeerThatHoldsItsCreditDoesNotHoldTheChannel`, `E_INTERNAL` statt `E_TIMEOUT` im
`-race`-Auftrag) stand hier als „nicht gemessen, weil es hier kein `-race` gibt". **Auch der ist
inzwischen gemessen und behoben** — siehe den letzten Nachtrag. Er hatte eine andere Ursache als
der Kanal, und anders als dieser war er **kein** Testfehler.

### Nachtrag 22.10.2026: ein dritter Zeittest, und seine Schranke ist eine Haaresbreite

Derselbe Weg wie oben: die CI sagt seit Punkt 69, **warum** ein Test umfällt, und damit fällt
der dritte auf. Er gehört ebenfalls nicht zum Zweig, auf dem er auftrat — `apps/` und `shared/`
sind dort byteweise `main` (`git diff origin/main --stat` auf diese Pfade: leer).

`TestAFaultEndsTheTerminalAtOnce` (`apps/server-controller/classes/terminal/terminal_test.go:654`),
im `root`-Auftrag:

```
it took 1.002616831s: the fault waited for the shell instead of ending the terminal
```

**Die Schranke ist `took > time.Second`** (Zeile 653), und überschritten wurde sie um **2,6
Millisekunden**.

**Hier stand zuerst, die Schranke trenne gegen `time.Minute`, also gegen 60 Sekunden, und fünf
Sekunden würden dasselbe trennen. Das war falsch**, und der Test sagt es selbst drei Zeilen
weiter oben (Zeile 619): *„a shell told to `sleep 1` could not have finished in under a second
either way"*. Die Eingabe ist `sleep 1`, die Schranke ist eine Sekunde — **die zwei Seiten der
Entscheidung waren dieselbe Zahl.** Eine Schranke von fünf Sekunden allein hätte den Wächter
abgeschafft: ein Lauf, der das `sleep 1` wirklich abgewartet hätte, wäre durchgegangen.

**Behoben wurde deshalb der Abstand, nicht die Schranke:** die Shell schläft jetzt **30
Sekunden** — dieselbe Zahl, die der Nachbartest in Zeile 574 schon benutzt — und die Schranke
heißt `faultWithin` und steht bei **5 Sekunden**. Der Nachbar bindet 10 Sekunden gegen dieselben
30; 5 gegen 30 ist strenger und hat denselben Abstand im Rücken.

**Und jetzt ist der Fall gemessen, nicht nur aus der CI abgelesen.** Unter Windows stellt er
sich selbst zur Seite — 20 von 20 Läufen, jedes Mal mit `NICHT GEMESSEN auf windows: /dev/ptmx
gibt es nur unter Linux`. Also wurde das Testbinär für `linux/amd64` übersetzt und in der
Debian-WSL gelaufen, wo es `/dev/ptmx` gibt:

| | Ergebnis |
|---|---|
| 10 Läufe unter Linux | **10 PASS**, je 0,00–0,01 s |
| Mutationsprobe, `faultWithin` auf 1 ns | **3 von 3 rot** — die Behauptung greift also |
| was die Mutation nebenbei misst | das Terminal endet in **1,2 bis 1,9 ms** |

Damit stehen alle drei Zahlen nebeneinander: richtig endet es in **1,5 ms**, die Schranke liegt
bei **5 s**, und ein Lauf, der auf die Shell wartet, kann nicht vor **30 s** zurückkommen. Das
ist der Abstand, den die alte Fassung nicht hatte.

### Drei Zeittests, ein Muster

Mit Punkt 70 oben und dem Tunnel-Test aus Punkt 69 sind es drei, alle im Controller, alle nur
unter Last, alle in anderen Teilen als dem Zweig, auf dem sie auftraten:

| Test | Schranke | gemessen | Alternative, die er trennt |
|---|---|---|---|
| `TestAFaultEndsTheTerminalAtOnce` | 1 s → **5 s** | 1,0026 s (CI), 1,5 ms (Linux) | `sleep 1` → **`sleep 30`** |
| `TestAPackageLargerThanOneWindowGetsThrough` | Kredit 262 144 | **behoben** (`dataPaid`), 0 von 20 | der Test überzog beim 5. Rahmen |
| `TestAPingLostToOurOwnWriteDoesNotKillTheTunnel` | — | Nullkontrolle, 2 von 14 `UNMEASURABLE` | Wettlauf bleibt, ehrlich benannt |

Der erste ist der einfachste von allen und **ist behoben** (siehe oben): zwei Zahlen, die
dieselbe waren. Die anderen zwei sind Wettläufe und bleiben offen; sie lassen sich ohne Kenntnis
des Protokolls nicht entscheiden.

**Was daraus folgt und bisher nirgends stand:** eine Prüfung, die auf einem belasteten Läufer
drei verschiedene Tests verlieren kann, ist keine Prüfung mehr, die einen echten Befund von
Rauschen trennt. Das ist die Fortsetzung von Punkt 69 mit anderen Mitteln: dort war sie rot und
schwieg, hier ist sie rot und meint es verschieden.

### Nachtrag 23.10.2026: die zwei Wettläufe unter Linux — und Windows war das falsche Messgerät

Beide offenen Fälle sind jetzt dort gemessen, wo die CI sie verliert: Testbinäre für
`linux/amd64` übersetzt und in der Debian-WSL gelaufen, 128 Kerne, 256 Lastschleifen daneben.

#### Der Kanal: bestätigt, und keine Windows-Eigenheit

`TestAPackageLargerThanOneWindowGetsThrough`, 20 Läufe: **5 Fehlschläge**.

| | Fehlschläge |
|---|---|
| Windows unter Last | 4 von 20 |
| **Linux unter Last** | **5 von 20** |
| Windows ohne Last | 0 von 20 |

Dieselbe Erscheinung, dieselbe Rate, dieselben Zahlen: `CLOSE` mit `E_PROTOCOL`, und der
Installierer las **60 000 Bytes dreimal und 120 000 Bytes zweimal** — also nach einem oder zwei
von sechs Rahmen abgebrochen, an der Rahmengrenze. **Der Befund aus Punkt 70 steht damit auf
zwei Betriebssystemen**, und die Ursache bleibt ungemessen.

#### Der Tunnel: die Nullkontrolle feuert, und sie hat recht

`TestAPingLostToOurOwnWriteDoesNotKillTheTunnel/one_interval`, 14 Läufe:
**12 PASS, 0 FAIL, 2 `UNMEASURABLE`-Skips.**

Die zwei Skips sind die Kontrolle aus Punkt 69, wörtlich im Protokoll:

```
UNMEASURABLE: the tunnel counted as gone 1.135s after the way opened - ping() decided
on a count of running writes that no test can see afterwards
```

**Und jetzt die Zahl, die alles erklärt.** Wie lange der erste Ping nach dem Öffnen braucht:

| | Ping-Ankunft |
|---|---|
| Windows unter Last | 5 bis 37 ms |
| **Linux unter Last** | **507 ms bis 1,147 s** |

Zwischen zwanzig- und zweihundertfach länger. Das Ping-Intervall des Falls ist **300 ms** —
auf Linux liegt die Ankunft also regelmäßig **jenseits** eines Intervalls, auf Windows nie in
seiner Nähe. Darum hat dieser Rechner den Fall in 22 Läufen nie verloren und die CI zweimal.

**Die zwei Skips liegen mitten in derselben Verteilung wie die Durchläufe** — gefeuert bei
869 ms und 1,135 s, bestanden bei 866 ms, 912 ms und 1,099 bis 1,147 s. Es ist in diesem Fenster
ein Münzwurf, und genau das heißt „unmeasurable". **Ein rotes Urteil dort wäre Rauschen
gewesen**, und die Korrektur aus Punkt 69, die den roten Zweig ganz entfernt hat, ist damit
nicht nur begründet, sondern gemessen.

#### Was daraus für das Messen folgt

**Windows war für diese zwei Fälle das falsche Messgerät**, und das war vorher nicht
sichtbar — es sagt nicht „übersprungen", es sagt grün. Ein Fall, der auf Zeiten im Netz oder
auf Scheduling beruht, ist auf dem Betriebssystem zu messen, auf dem er läuft; dafür reicht
`go test -c -o <datei>` und ein Lauf in der WSL, und das kostet eine Minute. Dieselbe Regel wie
„ein Testergebnis gilt für das Laufwerk, auf dem es gemessen wurde" — nur eine Ebene höher.

### Nachtrag 23.10.2026: der letzte offene Fall — und er war kein Wackler, sondern eine Lüge im Fehlercode

`TestAPeerThatHoldsItsCreditDoesNotHoldTheChannel` war der Rest: im `-race`-Auftrag kam
`E_INTERNAL`, wo `E_TIMEOUT` erwartet ist. Oben stand „nicht gemessen, es gibt hier kein
`-race`". Das gilt nicht mehr: in der Debian-WSL steht Go 1.27.2 und gcc, also läuft `-race`
jetzt auf diesem Rechner.

**Nachgestellt**, `go test -race` auf Linux, 256 Lastschleifen auf 128 Kernen:

| Läufe | Fehlschläge |
|---|---|
| 10, ohne Last | 0 |
| 40, unter Last | 1 |
| 150, unter Last | **2** |

#### Die Ursache, mit einer Sonde gemessen

Eine Sonde an `app.go` bei der Zeile, die den Fehlercode setzt, hat den ganzen Unterschied
gedruckt:

```
148x  outErr=context deadline exceeded  endStatus="error"  endCode="E_TIMEOUT"
  2x  outErr=context deadline exceeded  endStatus="ok"     endCode=""
```

**Beide Male war `inCtx.Err()` die abgelaufene Frist.** Die Uhr war also aus, und der Versuch,
die schon versiegelte Nachricht hinauszuschicken, starb daran. Nur `RunService` antwortete
**ok** — weil das Programm zufällig sauber endete, bevor die Frist es erreichte. Und dann greift

```go
if end.Status == wire.StatusOK {
    end = wire.Error(wire.CodeInternal)      // app.go
}
```

und macht aus einer Zeitüberschreitung einen **internen Fehler**.

**Der Quelltext hatte das vorhergesagt.** Der Kommentar über `inCtx` (Zeile 265–271) beschreibt
genau dieses Symptom als schon einmal gemessen und nennt als Behebung „eine Uhr für alles". Die
eine Uhr ist da — und reicht nicht, denn das Rennen ist nicht zwischen zwei Fristen, sondern
zwischen **zwei Beobachtern derselben Frist**: dem Sender, der an ihr scheitert, und
`RunService`, das ein sauber beendetes Programm sieht.

#### Behoben, ohne eine dritte Uhr

Der Grund des Sendefehlers entscheidet, nicht mehr der Status von `RunService`:

```go
if errors.Is(out.err, context.DeadlineExceeded) || errors.Is(inCtx.Err(), context.DeadlineExceeded) {
    end = wire.Error(wire.CodeTimeout)
} else {
    end = wire.Error(wire.CodeInternal)
}
```

Die Auskunft lag schon da; sie wurde nur nicht gelesen. Ein Sendefehler aus einem **anderen**
Grund bleibt der interne Fehler, der er war.

| | Fehlschläge |
|---|---|
| vorher, 150 Läufe `-race` unter Last | 2 |
| **nachher, 150 Läufe `-race` unter Last** | **0** |

Dazu: das **ganze** Paket unter `-race` grün, und die `E_INTERNAL`-Erwartungen der anderen Tests
liegen alle im Installierer-Pfad (`activate_test.go`), nicht hier.

#### Was der Benutzer davon hat — diesmal etwas

Anders als beim Kanal ist das **kein** Testfehler. Eine Aktion, deren Ausgabe die Uhr
abgeschnitten hat, meldete dem Browser gelegentlich „Interner Fehler" statt
„Zeitüberschreitung". Das ist dieselbe Klasse wie Punkt 65: ein Zustand wird als die falsche
Ursache gezeigt — nur hier mit einer Handlung dahinter, denn auf eine Zeitüberschreitung
reagiert ein Benutzer anders (länger warten, kleinere Ausgabe) als auf einen internen Fehler
(melden).

#### Und eine Messregel, die drei Runden gekostet hat

**`/tmp` in der WSL ist zwischen zwei Aufrufen leer.** Der erste Sondenlauf schrieb sein
Protokoll nach `/tmp`, und beim Auswerten im nächsten Aufruf war die Datei weg — „PROBE-Zeilen:
(nichts)" sieht aus wie „die Sonde hat nicht gefeuert" und war „das Protokoll ist nicht mehr
da". Lauf **und** Auswertung gehören in einen Aufruf, und das Protokoll nach `/mnt/d`.

**Und zum dritten Mal in dieser Reihe: ein Heredoc frisst `\n`.** Die Sonde ging als
`fmt.Fprintf(..., "…%v\n", …)` über ein Shell-Heredoc in den Quelltext und stand dort als echter
Zeilenumbruch mitten in der Zeichenkette — `string literal not terminated`. Patches mit Escapes
gehören in eine Datei, nicht in ein Heredoc.
## Punkt 71: Sechs Wände auf einem Weg — vom Release bis zur laufenden App, und vier standen nicht im geprüften Code

**Gemessen vom 19. bis 21.10.2026** auf der Debian-VM aus Punkt 64, von einem **veröffentlichten
Release** aus: installieren, einrichten, anmelden, Controller koppeln, Laden öffnen, App
einrichten. Der Weg bis „die App läuft" ist in diesen drei Tagen **nicht ein einziges Mal**
durchgelaufen. Nicht an einer großen Stelle, sondern an sechs kleinen hintereinander.

Jede der sechs ist für sich vertretbar begründet. Das ist der Befund, nicht die Zahl.

**1. Die Unit lässt den Ordner nicht schreiben, in dem die Datei liegt.** `ngc-api.service` wird
mit `ProtectSystem=strict` geschrieben, und die schreibbaren Pfade sind
`ReadWritePaths=${NGC_DATA} ${NGC_TLS}` — also `/var/lib/ngc` und `/etc/ngc/tls`
(`ulh`, `apps/cli/assets/scripts/ngc-api.sh:1065`, Zweig `ngc-kette`; `ngc-all.sh:1322` schreibt
dieselbe Zeile). **`/etc/ngc` selbst steht nicht dabei.** Der Anker dort wird aber **atomar**
geschrieben: `config.SaveAnchor` (`shared/config/config.go:451`) ruft `writeAtomic`, und das legt
erst eine Datei `.trust-anchor.json.tmp-*` **im selben Ordner** an (`config.go:464`). Ein Ordner,
in dem keine Datei entstehen darf, lässt kein atomares Ersetzen zu — auch dann nicht, wenn die
Zieldatei selbst `0644` trägt. Gemessen im Journal:

> the trust anchor outside the database could not be moved up; this API still pins the older
> version — `pinned=1 stored=2`

Das ist die einzige der sechs, die den Grund **selbst nennt** (`state/trust.go:249`). Die Folge
steht in derselben Zeile: die Anlage bleibt auf der alten Vertrauensliste festgenagelt.

**2. Mein eigener Fehler: die Release-Binärdateien hießen `0.0.0-dev`.** Ich habe beim Bauen
`-ldflags "-X main.Version=…"` vergessen; der Vorgabewert steht im Quelltext und ist genau dafür
da (`apps/server-api/main.go:43`: „Version is set when building", dann `var Version = "0.0.0-dev"`).
Kosmetisch — bis der Controller seine Version im Hallo mitschickt. Die Oberfläche prüft jede
Rechnerzeile mit `checkNodeInfo` (`contracts/core.js:724`), dort `optVersion`, dort
`validVersion` (`contracts/text.js:84`) gegen `^(0|[1-9][0-9]{0,5})\.…$` — **genau**
MAJOR.MINOR.PATCH. `0.0.0-dev` fällt durch. Und der Vertrag der Liste ist
`every(v, checkNodeInfo)` (`core.js:585`): **eine** unpassende Zeile verwirft **die ganze
Antwort**. Auf dem Schirm stand:

> Die Rechner konnten nicht geladen werden. Bitte das Fenster neu öffnen.

(`apps/web/public/assets/js/apps/store.js:548`) — und das Fenster neu öffnen hilft nicht, denn die
Version bleibt dieselbe.

**3. Eine Stunde kein Katalog.** Das ist Punkt 65 und 66, hier nur als Glied der Kette:
`apps/server-api/classes/server/server.go:386` legt den Stundenticker an, `Server.hour` (ab `:421`)
holt die Kataloge, es gibt keinen Abruf beim Start und keine Handauslösung. Bis dahin steht „Der
Katalog dieses Stores ist nicht echt." — wo „noch nicht geholt" gemeint ist.

**4. Der frisch aufgenommene Controller darf niemandem etwas erlauben.** Die Oberfläche trägt ihn
mit `rootUsers: []` und `rootRoles: []` in die Vertrauensliste ein
(`apps/web/public/assets/js/apps/users.js:941` in `askJoin`, dasselbe bei `:973` in `askJoinAPI`),
und **keine Seite der Oberfläche konnte diese beiden Listen danach ändern**. `trust.MayRoot`
(`shared/trust/trust.go:419`) fragt genau sie: der Benutzer steht in `RootUsers`, oder eine seiner
Rollen in `RootRoles` — sonst `false`. Geprüft wird das an jeder Root-Aufgabe
(`shared/task/task.go:199`, `shared/core/uiproof.go:132`). Zwei leere Listen, die niemand füllen
kann, heißen: **auf diesem Controller kann nie eine App eingerichtet werden.**

**Nachtrag zu (4), gemessen am 23.10.2026 an PR #87:** die fehlende Oberfläche ist dort gebaut.
`rootPicker` zeichnet die Auswahl aus der **geprüften** Vertrauensliste, beim Aufnehmen und
danach jederzeit änderbar, und `askJoin` schreibt nicht mehr `rootUsers: []`, sondern das
Angekreuzte. Für `askJoinAPI` bleibt die Liste leer, und das ist richtig: `MayRoot` gibt für
alles, was kein `NodeController` ist, ohnehin `false`. Die Haken **beginnen leer** — damit kann
eine Anlage weiterhin mit einem Controller enden, auf dem niemand etwas darf, nur jetzt
sichtbar und mit einem Weg heraus.

**5. `0700` auf dem Ordner über dem Heimatverzeichnis.** Der Controller legt `/var/lib/ngc/apps`
mit `0o700` an (`apps/server-controller/classes/runtime/data.go:108`), und der Kommentar darüber
begründet es richtig: zwischen `mkdir` und `chown` darf niemand anderes hineinsehen. Nur gehört
der Ordner **root**, und darunter liegt das Heimatverzeichnis des App-Benutzers. Rootloses
`podman load` dieses Benutzers bekam `EACCES` — **auf sein eigenes Heimatverzeichnis**, weil es
den Weg dorthin nicht durchlaufen durfte. Gefunden wurde das mit `strace` am laufenden Dienst;
podman meldete es nicht, das Journal nannte es nicht.

**6. Kein Recht ohne App, keine App ohne Recht.** Der Schreibtisch zeigt eine App nur, wenn der
Anfragende das Recht `benutzen` hält: `apps()` überspringt jede Zeile ohne
`rq.Rights[a.ID+"/"+core.RightUse]` (`apps/server-api/classes/coreapp/desktop.go:161`). Die Seite
„Rollen und Rechte" baut ihre Auswahl aus `this.apps` (`users.js:416`) — und das ist genau diese
gefilterte Liste, durchgereicht aus `appCtx()` (`shell/desktop.js:579`: `apps: this.appList`).
Eine neu eingerichtete App, die noch niemand benutzen darf, steht also in keinem Schreibtisch und
damit in keiner Rechteliste. **Der Haken, der sie sichtbar machen würde, kann nur dort gesetzt
werden, wo sie schon sichtbar ist.**

### Was das heißt

**Die blinde Zone hat einen Namen: es ist alles, was kein Go und kein JavaScript ist.** Vier der
sechs lagen nicht im Code, den die Prüfsätze abdecken, sondern in einer **systemd-Unit** (1), einem
**Bau-Flag** (2), einem **Dateimodus** (5) und einer **Vorgabe in einem Dialog** (4). Die
Prüfsätze sind gut — sie haben hier nur nichts zu prüfen gehabt. Die Unit entsteht in einem
anderen Repo, zur Laufzeit, aus Shell-Variablen; das Flag entsteht beim Bauen; der Modus ist eine
Zahl, die für sich richtig ist; die leeren Listen sind ein gültiger Wert. Jedes Stück für sich
besteht jede Prüfung, die es gibt.

**Dreimal war der Satz auf dem Schirm nicht die Ursache.** „Der Katalog ist nicht echt" (3) für
„noch nicht geholt", „Die Rechner konnten nicht geladen werden" (2) für „eine Zeile trug eine
Version, die ich nicht lesen kann", und das stille Fehlen der App (6) für „dir fehlt ein Recht".
Das ist dieselbe Klasse wie die Anmeldelüge aus **Punkt 50**, über die **Punkt 52** entscheidet und
die **Punkt 65** am Katalog wiederfindet: ein **leerer Zustand** wird als **Angriff** oder als
**Ablehnung** dargestellt. Wer das liest, sucht an der falschen Stelle, und zwar lange.

**Dreimal protokollierte nichts den Grund.** Bei (2) nicht, weil es im Browser keine einzige
Stelle gibt, die etwas schreibt: `grep -rc 'console\.'` über `apps/web/public/assets/js` ergibt
**0**. Eine verworfene Antwort hinterlässt nirgends eine Spur. Bei (4) und (6) nicht, weil kein
Fehler auftrat — die Oberfläche tat genau, was im Code steht. Bei (5) war die Ursache **nur mit
`strace` am laufenden Dienst** zu finden. Allein (1) sagte selbst, was fehlt.

**Zwei der sechs sind keine Fehler, sondern fehlende Oberflächen.** Systemrechte je Server (4) und
Rechtevergabe für eine neue App (6) hat niemand gebaut. Nicht aus Nachlässigkeit, sondern weil
**nie jemand den Weg zu Ende gegangen war**: jede einzelne Teilstrecke war geprüft, und die Lücke
lag zwischen zwei geprüften Teilstrecken. Das ist der Grund, warum dieser Durchlauf etwas gefunden
hat, was siebzig Punkte vorher nicht gefunden haben — nicht weil schlechter geprüft wurde, sondern
weil zum ersten Mal **eine** Person **einen** Weg von Anfang bis Ende gegangen ist.

**Der Schluss daraus, und er ist unbequem:** ein Durchlauf von Anfang bis Ende gehört in die
Prüfung, nicht in die Woche eines Menschen. Solange er von Hand läuft, läuft er selten, und was
nur selten läuft, bricht zwischen zwei Läufen an der nächsten kleinen Stelle.

### Offene Entscheidungen — gemessen ist das oben, abgewogen ist das hier

**a) Bekommt die einrichtende Person die Rechte automatisch?** Durch PR #87 ist das nicht mehr
die ganze Frage, sondern eine kleine: dort wird beim Aufnehmen **gefragt**, und die Haken beginnen
leer. Offen ist nur noch, ob der Haken **der unterschreibenden Person** auf diesem einen
Controller **vorgekreuzt** ist.

> **Mein Vorschlag: ja — vorgekreuzt, nicht erzwungen, und nur sie, nur dieser Controller, nur
> `rootUsers`.** Wer eine Version der Vertrauensliste unterschreiben kann, hat die Anlage ohnehin
> in der Hand; ihr den Haken anzubieten fügt **keine** Macht hinzu, sondern nimmt den Fall weg, in
> dem jemand einen Controller aufnimmt und erst beim ersten Einrichten merkt, dass er dort nichts
> darf. Abwählbar bleibt er, denn es gibt Anlagen, in denen genau diese Person es nicht sein soll.
> **Nicht** über `rootRoles`: eine Rolle gibt das Recht an alle weiter, die sie heute oder morgen
> tragen, und das ist eine andere Entscheidung als „ich für diesen Rechner".

**b) Verwirft ein kosmetisch ungültiges Feld weiter die ganze Antwort?** Heute ja (2): eine
Versionszeichenkette, die niemand anzeigt, nimmt die Liste aller Rechner mit.

> **Mein Vorschlag: nein — aber die Trennlinie ist nicht „kosmetisch", sondern „trägt eine
> Entscheidung".** `version` und `arch` einer Rechnerzeile werden angezeigt, nicht gerechnet; ist
> der Wert unlesbar, gehört er **leer** gemacht und die Zeile behalten. Ein Schlüssel, ein
> Fingerabdruck, eine Kennung, eine Prüfsumme bleiben hart: fällt dort etwas durch, ist die Antwort
> zu verwerfen, und zwar ganz. Der Vertrag ist dann nicht weicher, er benennt nur, was er wirklich
> braucht.
>
> **Und beide Hälften davon sind nur prüfbar, wenn sie zusammen geprüft werden:** ein Lauf, in dem
> eine kaputte Version die Liste **nicht** mehr verwirft, und ein Lauf, in dem ein verbogener
> Schlüssel sie **immer noch** verwirft. Einer allein beweist nichts.

**c) Woher kommt das nächste Mal der Grund?** Offen gelassen, weil es eine eigene Entscheidung ist:
ohne eine einzige Stelle im Browser, die festhält **was** verworfen wurde, ist (2) bei der nächsten
Gelegenheit genauso stumm. Dagegen steht, dass ein solcher Platz schnell dorthin schreibt, wo
jemand mitliest.

> **Mein Vorschlag:** kein Protokoll, aber der **Satz auf dem Schirm nennt das Feld**: „Eine Angabe
> dieses Servers ist unlesbar (`version`)." Das benennt die Stelle, ohne irgendwo etwas abzulegen.

**Nachtrag vom 23.10.2026: alle drei sind entschieden und gebaut, die Prüfung steht.** Thomas hat
a) mit „vorgekreuzt, abwählbar, nur `rootUsers`, nur dieser Controller" entschieden — gebaut in
PR #105, das damit eine Entscheidung von PR #87 umdreht (dessen Begründung, eine Vorgabe entscheide
in dem Augenblick, in dem niemand hinsieht, steht dort daneben). b) und c) liegen in PR #104: die
Anzeigefelder `version` und `arch` werden leer gemacht statt die Antwort zu verwerfen, die harten
Felder fallen weiter ganz, und der Satz nennt das Feld. **Was daran gemessen ist und was nicht,
steht in den beiden PRs** — in allen drei Motoren laufen nur die Abnahmefälle von b) und c), und
auf einer echten VM ist keines von beiden geprüft.

**Was damit von den sechs Wänden noch steht,** nachgesehen am 23.10.2026 gegen `main`:

- **(2)** beantwortet in PR #104, **(4)** in PR #87 und #105, **(3)** liegt in PR #86.
- **(5) ist still behoben:** `main` legt `/var/lib/ngc/apps` mit `0o711` an und setzt den Modus
  danach noch einmal (`runtime/data.go:122` und `:131`), während das Verzeichnis der App selbst
  `0o700` bleibt. Damit darf der App-Benutzer den Weg durchlaufen, ohne lesen zu können — genau
  das, was `podman load` brauchte.
- **(1) ist offen, und zwar woanders:** die Unit steht nur auf dem Zweig `ngc-kette` von `ulh`,
  der nicht verschmolzen ist; auf dessen `main` gibt es die NGC-Skripte gar nicht. Dort schreibt
  `ngc-api.sh` weiter `ReadWritePaths` ohne `/etc/ngc`.
- **(6) ist behoben, und ich hatte es zuerst falsch abgeschrieben.** `apps()` filtert in `main`
  tatsächlich unverändert auf `core.RightUse`, und die Rechteseite baut ihre Auswahl unverändert
  aus dieser Liste — nur ist das nicht mehr der Kreis, weil die Liste für einen Verwalter
  vollständig ist: `rights.Of` ruft für einen Admin `adminApps`
  (`apps/server-api/classes/rights/rights.go:56`), und das setzt `<app>/use` **und** jede Aktion
  jeder eingerichteten App, **ohne Zeile in der Datenbank**. Damit steht die App im Menü, also
  auch in der Rechteliste. Behoben am **21.10.2026** mit `c0f0022` — am letzten Tag der Messung,
  weshalb der Befund oben gilt und dieser Nachtrag ihn nicht aufhebt.
  Dass das Recht in **keiner** Zeile liegt, ist dabei der Kern: ein Recht, das in einer Zeile
  läge, könnte man dem Verwalter wegnehmen, indem man die Seite ohne den Haken speichert.
  **Was davon bleibt,** klein, aber echt: `adminApps` und das Menü lassen eine **abgeschaltete**
  App beide aus (`!a.Active`). Deren Rechte lassen sich also nicht vorbereiten, solange sie aus
  ist — das ist so gewollt und hier nur vermerkt, nicht als Befund.

## Punkt 72: Die Härtung des App-Containers liegt an — bis auf die drei Mengen-Grenzen, die der Kern nicht durchsetzt

Punkt 36 hat die Hälfte offen gelassen, die ohne Linux-Server nicht zu haben ist: **ob podman die
Schalter einhält.** Hier ist sie gemessen, am laufenden Terminal-Container der Debian-VM
(24.10.2026, Kern 6.12.113+deb13-amd64, podman 5.4.2, Container `ngc-terminal-439982dd`,
`dc15f0724b9f`, zu diesem Zeitpunkt 7 Stunden auf).

**Welche Maschine.** Die **Einzel-VM aus Punkt 64** (`ssh ngc`, 10.2.3.208), auf der die
eingerichtete Terminal-App steht — **nicht** die verteilte Anlage unter `ngc.mein.lan`, deren Apps
seit dem 24.10.2026 auf `svc-1` und `svc-2` laufen. Jede Zahl dieses Punktes gilt für diese eine
Maschine. Das ist nicht Vorsicht um ihrer selbst willen: Befund 1 ist eine Aussage über **eine
systemd-Einheit und ihre cgroup**, und ob `Delegate=` auf `svc-1` und `svc-2` genauso steht, ist
hier **nicht gemessen** und darf aus diesen Zahlen nicht geschlossen werden. Zwei Maschinen mit
demselben Dienst sind genau die Falle, in der eine Zählung über den falschen Baum entsteht.

**Wie gemessen wurde.** podman läuft dort rootlos als der App-Benutzer, also wurde jeder Aufruf
aus `/tmp` heraus mit `setpriv --reuid=997 --regid=985 --clear-groups` und mit
`HOME=/var/lib/ngc/apps/org.sorglos.terminal`, `XDG_RUNTIME_DIR=/run/ngc/ngc-terminal-439982dd`,
`PATH=/usr/bin:/bin` gestartet — genau die vier, die `Podman.run` mitgibt. Das Werkzeug wurde vor
der ersten Zählung geprüft (`podman --version` antwortet 5.4.2), und die entscheidende Probe (das
Netz) trägt eine Gegenprobe, die *gelingt*. Gelesen wurden `podman inspect` **samt
`.Config.CreateCommand`**, `/proc/946/status`, `/proc/946/uid_map`, `/proc/946/mountinfo`,
`/proc/946/cgroup` und die cgroup-Dateien selbst.

### Was der Controller mitgibt, und was anliegt

Die Liste steht in `apps/server-controller/classes/runtime/podman.go`, in `createArgs`
(Zeilen 247–298); die Begründung je Schalter steht dort im Quelltext und ist hier verkürzt.

| Schalter | wozu laut Quelltext | gemessen |
|---|---|---|
| `--network none` (ohne `NeedNetwork`) | ohne Gewährung kein Netz; wie ein Netz entsteht sagt der Code bewusst nicht | **liegt an** — im Container nur `lo`, `/proc/net/route` leer |
| `--read-only` | das Abbild bleibt unschreibbar, geschrieben wird nur unter `/data` | **liegt an** — `/` ist `ro`, Schreiben auf `/` und `/usr` fällt mit *Read-only file system* |
| `--cap-drop ALL` (ohne `NeedRoot`) | wer ausbricht, soll nichts mitbringen | **nicht gesetzt** — Terminal hat `needs:["root"]`, also greift der `if !c.Root`-Zweig nicht (siehe unten) |
| `--security-opt no-new-privileges` | zweite Lage gegen Rechtegewinn | **liegt an** — `NoNewPrivs: 1` in `/proc/946/status` |
| `--volume …:/data:rw,nosuid,nodev` | set-user-id-Bit und Gerätedateien in den eigenen Daten zählen nicht | **liegt an** — `/data rw,nosuid,nodev` in `mountinfo` |
| `--volume …/ngc-hold:ro,nosuid,nodev` | der Haltevorgang kommt nur aus `config.BinDir` und nur lesbar | **liegt an** — `ro,nosuid,nodev`, Quelle `/usr/lib/ngc/ngc-controller` |
| `--user 0:0` | 0 im Container ist draußen der App-Benutzer | **liegt an** — `uid_map` ist `0 997 1`, der Prozess ist draußen `997:985` |
| rootlos als System-Benutzer der App (`credential`) | wer ausbricht, landet auf einem Benutzer ohne eigene Rechte | **liegt an** — `/proc/946/status` `Uid: 997 997 997 997` |
| `--cpus 1.000` | guter Nachbar sein | **kommt nicht an** (siehe Befund 1) |
| `--memory 256m` | dito | **kommt nicht an** (siehe Befund 1) |
| `--pids-limit 512` | die ganze Antwort auf eine Fork-Bombe (`PIDsLimit`) | **kommt nicht an** (siehe Befund 1) |

Dazu die Härtung, die nicht in `createArgs` steht, aber dieselbe Trennung tragen soll:
`/var/lib/ngc/apps` ist `0711 root`, der Datenordner der App `0700 997:985` und laut `lsattr`
verschlüsselt (Merker `E`); `/run/ngc` ist `0711 root`, `/run/ngc/ngc-terminal-439982dd` ist
`0700 997:985`. Alles vier wie in `data.go` und `run.go` beschrieben. Der App-Benutzer hat
`/usr/sbin/nologin`.

### Befund 1 — die drei Mengen-Grenzen stehen im Aufruf und wirken nicht

`podman inspect` meldet `PidsLimit 512`, `Memory 268435456`, `NanoCpus 1000000000`, und
`.Config.CreateCommand` zeigt, dass alle drei wirklich mitgegeben wurden. Durchsetzen muss sie die
cgroup — und der Containerprozess hat keine eigene:

```
/proc/946/cgroup                 ->  0::/system.slice/ngc-controller.service
cgroup.procs dieser cgroup       ->  690 (ngc-controller, uid 0), 894 catatonit, 944 conmon, 946 ngc-hold
pids.max                         ->  9409      (nicht 512)
memory.max                       ->  max       (nicht 256M)
cpu.max                          ->  Datei gibt es nicht
cgroup.controllers               ->  memory pids
```

Der Containerprozess sitzt also **in der cgroup des Controllers, zusammen mit dem Controller
selbst.** Keine der drei Grenzen ist im Kern hinterlegt; die 9409 sind `TasksMax` der
systemd-Einheit und werden mit dem Controller **geteilt**.

Das ist die Art Abweichung, die Punkt 36 gesucht hat: der Schalter wird gesetzt, podman nimmt ihn
an und schreibt ihn in seine eigenen Angaben — und verwirft ihn still. Die Ursache ist gemessen und
liegt nicht in podman:

```
systemctl show ngc-controller.service -p Delegate   ->  Delegate=no
/sys/fs/cgroup/system.slice/ngc-controller.service  ->  drwxr-xr-x root root
   Schreibprobe als 997                             ->  Keine Berechtigung
loginctl show-user 997                              ->  User ID 997 is not logged in or lingering
podman-Warnung bei jedem Aufruf                     ->  "no systemd user session available …
                                                        Falling back to --cgroup-manager=cgroupfs"
podman inspect .HostConfig.CgroupManager            ->  cgroupfs
podman inspect .State.CgroupPath                    ->  /system.slice/ngc-controller.service
```

Ohne `Delegate=` an der Einheit bekommt der App-Benutzer keinen eigenen Zweig im cgroup-Baum, darf
in den vorhandenen nicht schreiben, und rootloses podman legt dann keine an — es sagt das nur als
Warnung über den Kanal, den der Controller nicht liest (`p.run` gibt `errOut` für `create` als
`nil` mit).

**Was das praktisch heißt.** Der lange Kommentar an `PIDsLimit` nennt die Zahl „die ganze Antwort
auf eine Fork-Bombe in einer App". Diese Antwort liegt auf dieser Maschine nicht an. Schlimmer als
die fehlende Grenze ist das geteilte Fach: die Aufgaben einer App zählen gegen `TasksMax` des
Controllers, eine App am Anschlag nimmt also dem Controller den Platz, Aufgaben zu starten. Das ist
**abgewogen, nicht gemessen** — eine Fork-Bombe wäre auf dieser Maschine zerstörerisch, weil die
anderen Sitzungen am laufenden Dienst arbeiten, und wurde darum nicht gefahren.

### Befund 2 — `--cap-drop ALL` ist auf dieser Maschine überhaupt nicht zu messen

Der Container hat 11 Fähigkeiten: `CapEff: 00000000800405fb` in `/proc/946/status`, dieselben 11 in
`EffectiveCaps` und `BoundingCaps` — podmans Vorgabesatz, darunter `CAP_DAC_OVERRIDE`,
`CAP_SETUID` und `CAP_SETFCAP`. Kein Fehler des Codes: Terminal trägt `needs:["root"]`
(`apps/server-controller/templates/terminal/manifest.json`), `activate.go:467` setzt daraus
`Root: true`, und `createArgs` lässt den `--cap-drop ALL`-Zweig dann aus. `.Config.CreateCommand`
bestätigt es: der Schalter steht nicht im Aufruf.

Der Befund ist die Lücke, nicht der Fehler: **Terminal ist die einzige App dieser Anlage, und sie
ist genau die, an der dieser Schalter nicht anliegt.** Der am häufigsten genannte Härtungsschalter
ist damit weiter ungemessen — gebaut, im Fall-Satz grün, auf keiner laufenden App der Welt je
nachgesehen. Dazu passt eine Lesart, die der Quelltext selbst nicht ausspricht: der Kommentar sagt,
die Fähigkeiten „kommen zurück", nennt aber nicht, welche. Gemessen sind es die 11 von podman —
eine Vorgabe, die wie die 2048 aus Punkt 36 **nicht im Repo steht** und sich mit einer
podman-Fassung ändern kann.

### Befund 3 — ein Riegel liegt an, den niemand gesetzt hat

`/proc/946/status` meldet `Seccomp: 2` und `Seccomp_filters: 1`: der Container läuft hinter einem
seccomp-Filter. Im Repo steht davon nichts — kein `--seccomp-profile`, kein
`--security-opt seccomp=…`. Es ist podmans eingebautes Profil. Das ist kein Mangel, aber dieselbe
Art Abhängigkeit wie die 11 Fähigkeiten: eine Wand, die trägt, solange podman sie stellt, und die
kein Fall dieses Repos halten würde, wenn sie wegfiele.

### Befund 4 — ein App-Benutzer ohne App

`getent passwd` nennt neben `ngc-terminal-439982dd` einen zweiten:
`ngc-all-73e95399:x:999:987::/var/lib/ngc/apps/no.app.at.all:/usr/sbin/nologin`. Das zugehörige
Heimverzeichnis gibt es nicht, jeder podman-Aufruf unter ihm scheitert mit
`cannot resolve /var/lib/ngc/apps/no.app.at.all`. Ein Überrest einer früheren Messung, der kein
Loch aufmacht (nologin, kein Ordner, keine Gruppe mit Rechten) — aber `DropUser` hat ihn
offensichtlich nicht geholt, und ein Benutzer, der nach seiner App übrig bleibt, gehört in den
Fall-Satz des Aufräumens.

### Die drei Proben: trägt die Härtung?

Gewählt wurden die drei Wände, die die Trennung wirklich tragen muss. Vorher angekündigt und
danach entfernt wurde ein **Attrappen-Ordner** `/var/lib/ngc/apps/zz.probe.other`
(`0700`, Benutzer `999:987`, eine Geheimdatei `0600`) — die Gestalt einer zweiten App, weil es
auf dieser Anlage keine zweite gibt. Sonst wurde nichts verändert; der Terminal-Container war
nach allen Proben unverändert auf.

**Probe 1 — kommt die App an die Daten einer anderen App? Nein, zweifach.**
Im Container existiert der Pfad nicht einmal: nur `/data` ist eingehängt, `ls /var/lib/ngc/apps`
antwortet *No such file or directory*. Und in der Stellung, auf die es ankommt — nach einem
Ausbruch, als `997` auf dem Wirt:

```
ls /var/lib/ngc/apps                                 ->  Keine Berechtigung   (0711 root)
cat /var/lib/ngc/apps/zz.probe.other/secret.txt      ->  Keine Berechtigung
cat /var/lib/ngc/controller/state.db                 ->  Keine Berechtigung
ls /var/lib/ngc/apps/org.sorglos.terminal            ->  lesbar  (die eigenen)
```

Die Zeile aus `podman_linux.go` — „wer aus dem Container ausbricht, landet auf einem Benutzer ohne
eigene Rechte" — trägt. Dass `CAP_DAC_OVERRIDE` im Container liegt, ändert daran nichts: die
Fähigkeit gilt nur im Benutzer-Namensraum, und der kennt `999` nicht.

**Probe 2 — kann der Container neue Rechte erlangen? Nein, aber eine Lage ist nicht trennbar
gemessen.**

```
Schreiben auf /  und /usr          ->  Read-only file system
mount -t proc  /  mount /dev/sda2  ->  permission denied   (CAP_SYS_ADMIN nicht im Satz)
mount -o remount,rw /              ->  permission denied
mount -o remount,suid /data        ->  permission denied
dd if=/dev/sda2                    ->  No such file or directory  (kein Gerät im Container)
NoNewPrivs                         ->  1
```

Kein Weg heraus gefunden — und die vier abgewiesenen `mount`-Aufrufe sind der schärfste Teil davon,
weil `--read-only` und die `nosuid`-Optionen ohne sie zurückdrehbar wären.

**Die Lage, die nicht trennbar ist, und das gehört dazu:** ob `nosuid` auf `/data` wirkt, ließ sich
an *diesem* Container nicht einzeln zeigen. Eine set-user-id-Datei nützt nur einem Benutzer, der
nicht schon 0 ist, und `/data` ist `0700` — der einzige Benutzer im Container, der hineinkommt, ist
die 0, für die das Bit nichts ändert. Eine erste Zählung hat genau das als Beweis gelesen
(`euid=0` vor **und** nach dem Aufruf) und war keiner. Als Gegenprobe wurde dieselbe Datei auf
`/tmp` gelegt und als `1000` gestartet: `euid` blieb `1000`, der Kern lässt das Bit auf einer
`nosuid`-Einhängung in diesem Container also fallen. Dass `/data` dieselbe Option trägt, ist aus
`mountinfo` gemessen — die Wirkung auf `/data` selbst ist daraus **geschlossen, nicht gemessen**.
Nebenbei aufgefallen: die Wurzel-Einhängung selbst trägt `ro,relatime` und **kein** `nosuid`. Sie
ist unschreibbar, eine set-user-id-Datei könnte also nur aus dem Abbild der App kommen — und wäre
drinnen die 0, draußen `997`. Kein Loch, aber die einzige Einhängung des Containers ohne diese
Option.

**Probe 3 — erreicht der Container das Netz der Anlage? Nein, und die Gegenprobe gelingt.**
Dieselbe Perl-Probe, zweimal gefahren — im Container und als `997` auf dem Wirt:

| Ziel | im Container | als `997` auf dem Wirt |
|---|---|---|
| `127.0.0.1:7443` (ngc-api) | Connection refused (eigenes `lo`) | **erreicht** |
| `10.2.3.208:7443` (die VM selbst) | Network is unreachable | **erreicht** |
| `10.2.3.208:443` (apache) | Network is unreachable | **erreicht** |
| `10.2.3.208:22` | Network is unreachable | **erreicht** |
| `10.2.3.1:22` (Tor der Anlage) | Network is unreachable | **erreicht** |
| `8.8.8.8:53` | Network is unreachable | **erreicht** |

`--network none` trägt vollständig, und die rechte Spalte ist der Grund, warum man das glauben
darf: dieselbe Probe, dasselbe Perl, nur eine andere Stellung — und sie erreicht alles. Eine Null
ohne diese Spalte wäre keine Messung gewesen. Die rechte Spalte sagt aber auch, wie weit der
Schalter reicht: **er trennt den Container, nicht den App-Benutzer.** Wer wirklich ausbricht, hat
das ganze Netz der Anlage, einschließlich `ngc-api`.

### Was daraus zu bauen wäre

1. **`Delegate=yes` an `ngc-controller.service`**, damit die drei Grenzen überhaupt eine cgroup
   haben, in der sie stehen können — oder, wenn das nicht geht, `loginctl enable-linger` für den
   App-Benutzer und ein `XDG_RUNTIME_DIR`, das zur Sitzung passt. Welcher der beiden Wege richtig
   ist, ist eine Entscheidung und steht hier nicht.
2. **Der Controller muss das Verwerfen bemerken.** Heute gibt `p.run` für `create` kein `errOut`
   mit, und podmans Warnung über die fehlende Sitzung fällt auf den Boden. Ein Schalter, der still
   verworfen wird, ist schlimmer als einer, der fehlt: der Fall-Satz bleibt grün, und `inspect`
   bestätigt ihn. Ein Fall nach der Aktivierung, der `/proc/<pid>/cgroup` gegen die drei Grenzen
   liest, wäre die einzige Messung, die das fängt.
3. **Eine App ohne `needs:["root"]` auf diese Maschine**, allein damit `--cap-drop ALL` ein
   einziges Mal an einer laufenden App gemessen ist. Solange Terminal die einzige App ist, ist
   dieser Schalter ungeprüft.
4. **`nosuid` auf die Wurzel-Einhängung**, oder eine Zeile im Quelltext, die sagt, warum sie dort
   nicht nötig ist.
5. **Den Überrest `ngc-all-73e95399` aufräumen** und einen Fall, der einen App-Benutzer ohne
   Datenordner als Fehler zählt.
6. Was der Quelltext über `NeedRoot` sagt, um die **11** ergänzen — oder die Fähigkeiten selbst
   nennen, statt podmans Vorgabe zu erben.
7. **Dieselbe Messung auf `svc-1` und `svc-2`**, den App-Knoten der verteilten Anlage. Befund 1
   hängt an `Delegate=` einer systemd-Einheit, also an der Einrichtung der jeweiligen Maschine —
   er könnte dort anliegen oder fehlen, und keine Zahl von hier sagt, welches. Dabei gilt die
   Warnung aus der Anlage selbst: die Rolle am laufenden Dienst prüfen, nicht am Namen der
   Maschine.

**Getrennt gehalten:** gemessen sind die Tabelle der Schalter, die vier Befunde, die cgroup-Dateien
und die drei Proben samt Gegenproben. Abgewogen sind die Folge des geteilten `TasksMax`, die
Wirkung von `nosuid` auf `/data` und jeder der sechs Punkte dieser letzten Liste.

## Punkt 73: Ein zweiter Mensch — der Vertrag konnte es die ganze Zeit, die Oberfläche hatte keine Tür

**Gemessen am 24.10.2026** gegen `origin/main` (`9753a8c`) und gegen die laufende Anlage
(`ssh ngc`, Vertrauensliste in Fassung 6).

Jede Messung der letzten Wochen lief mit **einem** Menschen, dem Verwalter aus der
Ersteinrichtung. Dieser Punkt sagt, was einen zweiten aufgehalten hat, was davon jetzt gebaut
ist, und was nur ein Mensch tun kann.

### Die vier Stationen, einzeln gemessen

| Station | Stand vor dem 24.10.2026 | Beleg |
|---|---|---|
| Ein Verwalter legt einen zweiten Benutzer an | **gibt es gar nicht** | Im ganzen Webinterface fasst **keine** Zeile `users` oder `roles` einer neuen Fassung an. `buildNextVersion` (`apps/web/public/assets/js/apps/users.js:188`) ist allgemein, hat aber genau **einen** Aufrufer (`:1349`, in `joinNode`), und jeder `change`-Rumpf, der dort hineingeht, schreibt nur `next.nodes`. |
| Der zweite Benutzer bekommt eine **Rolle**, die nicht `admin` ist | **gibt es gar nicht, und das ist die zweite Lücke** | Eine Anlage verlässt die Ersteinrichtung mit **genau einer** Rolle: `admin` (`apps/web/public/assets/js/auth/setup.js:178`–`:181`, „One role"). Gegengeprobt an den Testdaten: `shared/testdata/trust.json`, Fassung 1, `roles` = `[{"name":"admin","level":"critical"}]`. Und `trust.List.Validate` weist einen Benutzer ab, dessen Rolle nicht in derselben Liste steht (`shared/trust/trust.go:581`, `l.Role(name)` → `ErrInvalid`). **Ein zweiter Benutzer ohne neue Rolle ist also zwangsläufig ein zweiter Verwalter.** |
| Der zweite Benutzer meldet sich an | **nicht messbar** | Es gab keinen zweiten Benutzer. Die Station liegt hinter der ersten und wurde deshalb nicht behauptet, weder so noch so. |
| Er sieht die Apps, für die seine Rolle `benutzen` hat | **nicht messbar, aus demselben Grund** | Der Weg ist gebaut und gelesen (`rights.Of` über die Rollen des Benutzers, `apps/server-api/classes/rights/rights.go:31`–`:54`), aber ohne zweiten Benutzer läuft er nie durch. |

### Was `shared/` dafür gebraucht hat: **keine Zeile**

Das ist das Ergebnis, und es ist eine Antwort auf die Anfrage aus
`docs/superpowers/specs/2026-10-23-zweiter-benutzer.md`, Abschnitt 3.1. Nachgemessen, Zeile für
Zeile, und jede davon steht unverändert:

- Die Aktion, eine neue Fassung einzuliefern: `shared/core/core.go:142` (`ActTrust`, `FlowIn`,
  `MaxTrustIn`).
- Ein Verwalter darf in einer neuen Fassung **jede** Änderung unterschreiben, nicht nur die an
  Knoten: `shared/trust/trust.go:303` — `onlyOwnKeys` greift nur, wenn der Unterzeichner
  **kein** Admin ist.
- Form eines Benutzers und seines Schlüssels: `shared/trust/trust.go:117`,
  `shared/proof/proof.go:123`.
- Bis zu 10 000 Benutzer und 256 Rollen: `shared/trust/trust.go:26`.
- Der Datenbankteil muss nichts tun: die Zeile in `ngc_user` entsteht bei der **ersten
  Anmeldung** (`apps/server-api/classes/login/finish.go:285`), und sie trägt keinen Schlüssel.

**Damit ist Abschnitt 3.1 beantwortet: ja, eine Shell darf eine Fassung mit einem neuen Benutzer
bauen, ohne dass `shared/core` eine eigene Aktion dafür bekommt.** Gebaut wurde genau so, und
nichts unter `shared/` ist angefasst. Der Einladungsablauf (Abschnitt 3.2, Weg (a)) bleibt die
offene Anfrage; er ist hier **nicht** beantragt und **nicht** gebaut.

### Was gebaut ist

Zwei Formulare in `apps/web/public/assets/js/apps/users.js`, beide über den schon vorhandenen
`joinNode` — dieselbe Maschinerie, die einen Server aufnimmt:

1. **Neue Rolle** (Reiter „Rollen und Rechte"). Sie darf zunächst nichts; was sie darf, wird
   über die Haken derselben Seite vergeben. `level: critical`, wie die Rolle `admin` — nicht
   `off`.
2. **Neuer Benutzer** (Reiter „Benutzer"). Anmeldename, Rollen, ein erstes Passwort. Der
   Schlüssel wird im Browser des Verwalters gerechnet (Argon2id → Ed25519); der Startwert wird
   in einem `finally` überschrieben, und auf die Leitung geht **nur** der öffentliche Schlüssel.

Das ist **Weg (b)** des Entwurfspapiers, und sein Preis steht im Fenster, bevor unterschrieben
wird: „Bis »<Name>« es ändert, kennen Sie seinen Schlüssel und könnten in seinem Namen
unterschreiben." Weg (a) löst ihn später ab, ohne ihn umzubauen.

### Zwei Regeln dieser Woche, hier eingehalten

- **Rechte sind kein Beiwerk.** Unter den Rollenhaken des Benutzerformulars steht der Satz, der
  das sagt: eine App sieht nur, wessen Rolle das Recht `benutzen` dafür hat. Er steht dort, wo
  der Haken gesetzt wird, nicht erst, wenn jemand über einen leeren Schreibtisch klagt.
- **Ein leerer Zustand sieht nicht wie ein Angriff aus.** Ohne Sitzung oder ohne geprüfte
  Vertrauensliste zeichnen beide Formulare **keinen** Knopf, sondern einen Satz, der das
  **fehlende Ding** nennt: „Dafür ist eine Anmeldung nötig. Es kann gerade kein Benutzer
  angelegt werden." Kein `role="alert"`, kein Wort über einen Fehler. Ein Fall prüft genau das
  und prüft zusätzlich, dass die beiden Worte dieser Oberfläche für einen Defekt
  („nicht in Ordnung", „antwortet gerade nicht") **nicht** darin stehen.

### Wie es gemessen ist

Neun Fälle in `apps/web/tests/shell/users.spec.js`, chromium, und je Zusicherung **ein
sterbender Mutant und eine lebende Kontrollmutation** (`Z:\GitHub\claude-md\messen.md`).
Mutiert wurde in einer **Kopie** des Baums, nie im Arbeitsbaum; der Treiber bricht ab, wenn eine
Ersetzung nicht genau einmal greift.

| Zusicherung | Mutant, der stirbt | Kontrolle, die lebt |
|---|---|---|
| Eine neue Rolle geht als Fassung N+1 hinaus, und sonst bewegt sich nichts | `LEVEL_CRITICAL` → `LEVEL_OFF` | Lokale Variable im Formular umbenannt |
| Der neue Benutzer trägt **einen** Passwortschlüssel in der Form, die `proof.UserKey.Validate` nimmt | `credId` von 0 auf 1 Byte | Das Benutzerobjekt umbenannt |
| Das erste Passwort verlässt den Browser nie | Das Passwort reitet im Salz des Schlüssels mit | Die Ableitung umbenannt |
| Ein Benutzer ohne Rolle wird abgewiesen, bevor irgendwer unterschreibt | Die Rollenprüfung abgeschaltet | Die Sperrliste umbenannt |
| Der leere Zustand nennt das fehlende Ding, nicht einen Defekt | Der Satz durch „nicht in Ordnung" ersetzt | Die Wache umbenannt |

**Und eine Messung hat sich selbst berichtigt:** die erste Fassung des Passwort-Falls durchsuchte
nur den **Umschlag** (`{"payload": "<base64url>", …}`). Der Mutant „Passwort im Salz" **lebte**
dadurch, weil ein Passwort *innerhalb* der Fassung in diesem base64url verschwindet. Der Fall
dekodiert jetzt jede gesendete Fassung und durchsucht beide Schichten; danach ist der Mutant
gestorben, und zwar an der Nadel, die die Passwortbytes nennt — nicht an einem Bauabbruch.

`tools/check.sh` druckt `check: ok`, Ausstiegscode 0, ohne Pipe gelesen. Alle 62 Fälle von
`users.spec.js` sind grün.

### Am laufenden System

**Gebaut ist nicht gelaufen**, also auf `ngc.test` nachgesehen. Die Anlage läuft auf `0.2.0`,
also **älter als `main`**; darum wurde nicht `main` darübergelegt, sondern **nur dieser eine
Unterschied** auf die dortigen Dateien gesetzt (`users.js`, `shell.css`), mit Sicherung unter
`/root/zweiter-benutzer-backup-20261024/`. Beide Dateien sind über HTTPS ausgeliefert, und in
einem echten Browser lädt das Modul samt seinem ganzen Importbaum ohne einen einzigen
`pageerror`.

**Was damit noch nicht gemessen ist, und das ist die Wahrheit über diesen Punkt:** dass ein
zweiter Mensch wirklich angelegt wird, sich anmeldet und sein Startmenü sieht. Dafür wird eine
neue Fassung der Vertrauensliste **im Browser mit dem Schlüssel der Person** unterschrieben, und
dieser Schlüssel ist ihr Passwort. Kein Agent darf es tippen. Die Stationen 3 und 4 oben bleiben
deshalb **ungemessen**, bis Thomas die Schritte gegangen ist; sie werden danach hier nachgetragen.

### Nachtrag vom 24.10.2026, nachmittags: **welche** Anlage gemessen wurde

Seit heute gibt es **zwei** Anlagen, und eine Zahl ohne ihren Namen ist hinterher wertlos.

| | Einzel-VM | Verteilte Anlage |
|---|---|---|
| Name | `ngc.test` (10.2.3.208) | `ngc.mein.lan` → api-1 (10.2.3.210) |
| Fassung von NGC | `0.2.0` | `0.2.1` |
| Vertrauensliste | Fassung 6, `RB1CPN7b…` | Fassung 6, `JtyqTFme…` — **dieselbe Nummer, andere Anlage** |
| `rp` | leer | `ngc.mein.lan`, Origin `https://ngc.mein.lan` |
| Dieser Zweig eingespielt? | **ja**, die zwei Dateien | **nein** |

**Alles oben in diesem Punkt ist an der Einzel-VM gemessen** — Einspielen, Auslieferung,
Browserprobe. **Die Stationstabelle gilt trotzdem für beide**, und das ist jetzt stärker belegt
als heute Vormittag: die verteilte Anlage läuft **exakt `origin/main`**. Gemessen, nicht
geschlossen: ihre `users.js`, über HTTPS geholt, hat den git-Hash `83344ee` — Byte für Byte die
Datei aus `origin/main`. Sie hat also ebenfalls **keine Tür** für einen zweiten Benutzer.

**Die Namen lügen teilweise, am laufenden Dienst nachgesehen** (eine Warnung der
Koordinationssitzung, hier nicht geglaubt, sondern geprüft):

- **api-1** und **api-2** tragen wirklich eine NGC-API: beide antworten auf
  `GET /api/v1/trust` mit `E_LOGIN` und auf `GET /api/v1/setup` mit `E_NOTFOUND` — also hat
  jede von beiden schon einen Verwalter. api-1 liefert zusätzlich das Webinterface.
- **db-1**, **svc-1**, **svc-2** haben von 22, 80, 443, 5432, 7443, 7444, 8443, 9443, 7000, 7001
  **nur 22** offen. Für zwei Controller ist das richtig — „nobody dials a controller" —, für
  die Datenbank sagt es nur, dass sie nach außen nichts anbietet.
- **store** nimmt auf 443 eine Verbindung an, bricht aber im TLS-Handschlag ab
  (`SEC_E_ILLEGAL_MESSAGE`). **Dort läuft kein brauchbares HTTPS.**

### Zwei API-Server — was das für einen zweiten Benutzer heißt

Die verteilte Anlage hat **zwei** APIs, und ihr Webinterface weiß das:
`/config.json` nennt `https://api-1.ngc.mein.lan:7443` **und**
`https://api-2.ngc.mein.lan:7443`. Ein zweiter Benutzer kann also an einem anderen API-Server
landen als dem, an dem er angelegt wurde.

**Gelesen, nicht gemessen:** das soll der Vertrag schon tragen. `ngc.users/api/trust` nimmt die
Fassung nicht nur an, sondern verteilt sie — `AddVersion`, `settle`, **`pushTrust`**
(`apps/server-api/classes/coreapp/users.go:250`, `:257`). Der zweite API-Server bekäme die
Fassung damit, ohne dass die Oberfläche etwas dazutut.

**Nicht gemessen, und es soll niemand glauben, es sei geprüft:** dass ein an api-1 angelegter
Mensch sich an api-2 anmelden kann. Dafür müsste dieser Zweig auf der verteilten Anlage liegen,
und dafür fehlt der Zugang (die sechs VMs stehen in keiner SSH-Konfiguration, und der
NGC-Schlüssel öffnet sie nicht). **Das ist die nächste Messung, nicht ein Ergebnis dieser.**

### Und ein Befund, der nicht zu dieser Arbeit gehört, aber gemessen ist

**Die Einzel-VM `ngc.test` ist seit etwa 12:40 nicht mehr erreichbar**: 1 von 3 Ping-Antworten,
kein SSH, kein HTTPS. Um 12:0x lief sie noch — da wurden die zwei Dateien eingespielt und die
Browserprobe lief grün gegen sie. Was sie umgeworfen hat, ist **nicht gemessen**, und zwei
statische Dateien einer Oberfläche sind keine plausible Ursache. Solange sie unten ist, kann
niemand die Schritte aus `docs/zweiter-benutzer-schritte.md` gehen.

## Punkt 74: Eine App auf einer anderen Maschine einrichten lässt sie auf der alten weiterlaufen

Gemessen am 24.10.2026 auf der verteilten Anlage, an den App-eigenen Containerspeichern der
beiden Dienstmaschinen — nicht an den Namen der Maschinen und nicht an der Datenbank allein.

Das Terminal läuft auf **beiden** Dienstmaschinen:

```
svc-1:  ngc-terminal-439982dd | localhost/ngc-terminal:1.0.0 | Up About an hour
svc-2:  ngc-terminal-439982dd | localhost/ngc-terminal:1.0.0 | Up 39 minutes
ngc_app.node: svc-2        (eine Zeile, eine Maschine)
```

Der Verlauf aus den Protokollen der beiden Controller:

```
svc-1  10:56:53  container create / init / start      erste Einrichtung
svc-1  11:05:40  Controller neu gestartet -> container init / start
svc-2  11:28:10  useradd ngc-terminal-439982dd
svc-2  11:28:14  container create / init / start      zweite Einrichtung
ngc_app.updated  etwa 11:28
```

**Was gemessen ist:** das Terminal wurde zuerst auf `svc-1` eingerichtet, später auf `svc-2`.
Der Container auf `svc-1` wurde nie entfernt. Mehr noch: der Controller auf `svc-1` hat ihn nach
seinem eigenen Neustart um 11:05 **wieder angeworfen** — die alte Einrichtung ist also nicht nur
ein vergessener Prozess, sie ist dauerhaft und übersteht einen Neustart. `ngc_app` trägt nur die
neueste Maschine, also weiß die Oberfläche von `svc-1` nichts und kann dort auch nichts
abräumen.

**Was das kostet, soweit es hier zu sehen ist:** CPU und Speicher einer App, die niemand zählt;
ein Container mit den Rechten der App (beim Terminal: `root` in seinem Container) auf einer
Maschine, auf der die Anlage ihn nicht mehr verzeichnet; und bei einer App mit eigenen Daten
zwei Datenstände, von denen die Oberfläche einen nicht kennt.

**Was nicht gemessen ist, und was darum zu entscheiden wäre:**

- Ob das so **gemeint** ist. Dass eine App absichtlich auf mehreren Maschinen laufen soll, ist
  möglich — dann fehlt aber die Stelle, die das verzeichnet, denn `ngc_app.node` ist ein
  einzelnes Feld. Im Vertrag ist dazu nichts nachgelesen worden.
- Ob eine Einrichtung auf einer neuen Maschine die alte **abräumen** soll (dann fehlt der
  Schritt), oder ob die Anlage **beide** verzeichnen soll (dann fehlt die Spalte).

Beides ist eine Entscheidung und keine Messung. Die Messung ist: es läuft zweimal, und die
Anlage sagt einmal.

**Unabhängig bestätigt:** die Erstlauf-Sitzung sieht dasselbe von ihrer Seite und ohne von
diesem Punkt zu wissen — ihre Station D4 zählt **zwei** vollständig eingerichtete Apps auf
`svc-1` und eine auf `svc-2`, gemessen an `useradd`, `image loadfromarchive` und
`container start` im Journal sowie am laufenden `conmon` des jeweiligen App-Benutzers. Zwei
Messwege, dasselbe Ergebnis.

**Eine Folge, die über die Buchhaltung hinausgeht** und die beim Abschalten einer Maschine
auffallen wird: wer `svc-2` ausschaltet und feststellt, dass das Terminal weiter antwortet,
hat **keinen** Ausfallschutz gemessen, sondern diesen Befund. Die Anlage nennt `svc-2`, laufen
tut es auch auf `svc-1`.

Herkunft und die ganze Messreihe: `docs/uhr-in-den-laden.md`, Abschnitt 8.
---

## Punkt 75: Die Maschine, auf der die ersten Messungen entstanden, ist aus — die Messungen gelten, die Anweisungen nicht

> **Stand: gilt.** Art: Umgebung. Nummer 74 war beim Anhängen schon von einem anderen Zweig
> belegt (PR 124); darum 75, damit nicht zwei Punkte dieselbe Nummer tragen.

**Gemessen am 24.10.2026.** Die Einzel-VM (`ssh ngc`, 10.2.3.208, Name `ngc.test`), auf der die
erste wirklich laufende Anlage stand, ist **abgeschaltet**:

| Was | Ergebnis |
|---|---|
| `ssh ngc` | `connect to host 10.2.3.208 port 22: Connection timed out` |
| TCP 10.2.3.208:22 | zu |
| VM 208 auf dem Proxmox-Wirt | `stopped` |

Sie antwortet nicht langsam und sie ist nicht gestört. Sie läuft nicht.

**Was weiter läuft**, Rolle am laufenden Dienst abgefragt statt aus dem Namen geschlossen — denn
die Namen lügen: 10.2.3.210 `api-1` trägt `ngc-api` **und** `ngc-controller`, 10.2.3.211
`api-2` nur `ngc-api`, 10.2.3.212 `db-1` nur `ngc-controller` (**keine Datenbank**), 10.2.3.213
`svc-1` und 10.2.3.214 `svc-2` `ngc-controller` + podman, 10.2.3.215 `store` kein NGC, nur
nginx. `https://ngc.mein.lan` zeigt auf api-1.

**Warum das ein eigener Punkt ist, und nicht nur eine Notiz.** 64 Zeilen in 9 Dateien zeigen auf
diese Maschine, 30 davon in diesem Papier. Wer ihnen heute folgt, misst ins Leere — und schreibt
dann „nicht erreichbar" als Befund über NGC auf, obwohl der Befund über eine ausgeschaltete
Maschine lautet. Ein falscher Befund ist teurer als eine fehlende Messung.

**Die Trennung, die hier gilt:**

- **Die Messungen dieses Papiers bleiben unangetastet.** Keine der 30 Zeilen ist geändert. Sie
  wurden gemacht, als die Maschine lief; ein ausgeschalteter Rechner macht eine richtige Messung
  nicht falsch. Eine Messung umzuschreiben, weil die Maschine weg ist, wäre Geschichtsfälschung.
  Betroffen sind die Punkte **56 bis 59, 64, 71, 72, 73** und die Nachträge dazu: sie gelten
  weiter, sind aber **an dieser Maschine nicht mehr nachprüfbar**.
- **Anweisungen, die dorthin zeigen, sind umgeschrieben** — in
  `docs/zweiter-benutzer-schritte.md`, `docs/installation-kleines-system.md`, `docs/erstlauf.md`
  und `README.md`. Die gefährlichste Stelle war eine `hosts`-Zeile zum Abtippen
  (`10.2.3.208   ngc.test`); sie ist jetzt ein Platzhalter mit der alten Zeile als
  gekennzeichnetes Muster darunter.
- **`ngc.test` bleibt in der Anleitung ein Beispielname** und ist jetzt ausdrücklich als solcher
  benannt: `.test` ist dafür reserviert (RFC 2606), der ganze Prüfsatz benutzt ihn, und ein
  echter Name wäre ein fremdes Heimnetz. Die rund 250 Vorkommen in `shared/` und `apps/` sind
  Prüfdaten und **nicht** diese Maschine.

**Befund, der bleibt: vier Dinge lagen nur auf ihrem Datenträger** — die zwei Skripte des
Prüfladens in `/home/berry/ngcapp/` (nicht im Repo, Punkt 71 / `app-weg.md` Befund 3), der
Ladeninhalt unter `/srv/ngc-store` samt Katalog in Folge 1, ein von Hand übersetztes `ngc-store`
in `~/uhr-bau/`, und die Sicherung `/root/zweiter-benutzer-backup-20261024/` zu Punkt 73. Nichts
davon ist herübergeholt worden; das braucht eine laufende Maschine und eine Entscheidung. **Der
eigentliche Befund ist nicht der Dateiverlust, sondern die Arbeitsweise: was nur auf einer
Maschine liegt, ist nicht gesichert.**

**Was damit ungemessen wird.** Der kleine Fall (alles auf einer Maschine) ist ein ausdrücklich
unterstützter Fall mit eigener Anleitung und eigenem Vertragspunkt (47), und er hat die ersten
sechs Hürden ans Licht gebracht (Punkt 71). Die verteilte Anlage kann ihn nicht ersetzen: sie
unterscheidet sich genau dort, wo der kleine Fall seine eigenen Fehler hat — SQLite statt eigener
Datenbankmaschine, ein Skript statt vier, ein Name statt mehrerer. **Ab heute ist der kleine Fall
ohne Maschine und damit ungemessen.**

**Zu entscheiden (Thomas).** Vorgelegt in `docs/alte-vm-verweise.md`, Abschnitt 5: (a) soll der
kleine Fall weiter geprüft werden — Vorschlag **ja**; (b) alte Maschine zurück oder neue dazu —
Vorschlag **neue dazu**, weil die alte ein über Monate gewachsener Handbau ist und niemand sie
ein zweites Mal aufbauen kann, eine neue aus Release und Anleitung dagegen den kleinen Fall
**und** die Anleitung zugleich prüft; (c) was mit der alten — Vorschlag **einmal starten**, die
vier Dinge bergen, dann weg. **Nicht vorher löschen.**

## Punkt 76: Eine fehlgeschlagene Einrichtung reißt eine fremde, laufende App aus der Leitung

Gemessen am 24.10.2026 um 12:14:38 im Protokoll des Controllers auf `svc-1`. Drei Zeilen
hintereinander:

```
WARN  tunnel failed err="link: the API broke the contract of the tunnel"
WARN  Die Verbindung einer App wurde abgebrochen. app=org.sorglos.terminal code=E_PROTOCOL
      grund="Der Dienst der App endete mit einem Fehler."
INFO  tunnel up                                            (eine Sekunde später)
```

Gearbeitet wurde in diesem Moment an einer **Einrichtung** — und abgebrochen wurde
`org.sorglos.terminal`, also eine **andere, bereits laufende** App. Die Oberfläche sagte dazu
„Das hat der Rechner nicht angenommen. Bitte es noch einmal versuchen." Im Protokoll der API
steht dazu nichts.

**Das ist von der Ursache des Abbruchs unabhängig, und darum steht es als eigener Punkt.** Ob
der Tunnel wegen der API-Fassung brach (dazu unten), wegen Last oder aus einem dritten Grund:
dass die Arbeit an App A eine laufende App B aus der Leitung wirft, ist für sich ein Fehler. Wer
im Terminal gerade etwas tippte, hat seine Sitzung verloren, weil jemand anders eine App
einrichtete. Die Trennung, die ein Container leistet, endet offenbar am gemeinsamen Tunnel.

**Was die Fassung damit zu tun hat — gemessen und abgewogen getrennt:**

- **Gemessen:** vor dem Tausch (API bis 12:20) scheiterte das Aktivieren mit `E_PROTOCOL`; nach
  dem Tausch auf 0.2.2 ist eine Einrichtung auf derselben Maschine `svc-1` **durchgelaufen**
  (12:25:37 bis 12:28:04, `ngc_app` mit `active 1`, Behälter läuft). In den Protokollen aller
  fünf NGC-Maschinen gibt es in diesen Stunden **genau einen** Treffer auf `E_PROTOCOL`, und das
  ist der um 12:14:38. Nach 12:21 keinen.
- **Abgewogen, nicht bewiesen:** dass die API-Fassung die Ursache war. Sie ist die einfachere
  Erklärung, weil sie genau zwischen den beiden Versuchen gewechselt hat. Dagegen steht, dass
  zur selben Zeit eine andere Sitzung Last auf `svc-1` und `svc-2` fuhr — dann wäre es
  **Punkt 70** unter anderem Namen (ein Paket über eine Fenstergröße kommt unter Last nicht
  durch, `E_PROTOCOL` statt `ok`). Ob um 12:14 schon Last lief, ist nicht gemessen.
- **Was den Beweis brächte:** eine Einrichtung gegen 0.2.2 unter **angesagter** Last. Das ist ein
  eigener Auftrag und nicht Teil dieser Messreihe.

**Zu entscheiden ist zweierlei, und beides ist keine Messung:**

1. Ob ein Fehler auf dem Tunnel überhaupt eine fremde App treffen darf — und wenn nicht, wo die
   Trennung hingehört.
2. Ob die Oberfläche beim Abbruch sagen muss, **welche** App es getroffen hat. „Das hat der
   Rechner nicht angenommen" stand bei der Einrichtung; dass daneben eine Terminal-Sitzung
   starb, hat niemand erfahren.

Der Abbruch selbst ist von der Koordinationssitzung gemessen worden; die Zeile im Protokoll von
`svc-1`, die fehlende Wiederholung nach 12:21 und die durchgelaufene Einrichtung um 12:25 sind
hier nachgemessen. Herkunft: `docs/uhr-in-den-laden.md`, Abschnitt 12.

## Punkt 77: Das Protokoll schreibt einer App 477 Byte stderr zu, die sie nicht geschrieben hat

Gemessen am 24.10.2026 an `org.sorglos.uhr` auf `svc-1`. Die App ist dafür nur der Anlass: sie
schreibt absichtlich **eine** Zeile in jede Richtung, und darum fällt an ihr auf, was bei einer
größeren App niemandem auffiele.

**Was der Controller protokolliert**, bei vier Aufrufen der Aktion `zeit`:

```
12:25:44  a service wrote to stderr  app=org.sorglos.uhr action=zeit bytes=510
12:25:59  ... bytes=510
12:26:00  ... bytes=510
12:29:55  ... bytes=510      <- dieser Lauf zeigte im Fenster die Uhrzeit
```

**Was der Dienst wirklich schreibt**, im selben Behälter direkt gemessen, beide Ströme getrennt:

```
Rückgabewert 0
STDOUT 21 Byte:  2026-10-24T12:31:52Z
STDERR 33 Byte:  ngc-uhr 1.0.0 beantwortet "zeit"
```

**33 geschrieben, 510 protokolliert: 477 Byte Fremdtext, der der App zugeschrieben wird.** Bei
allen vier Aufrufen gleich groß, also deterministisch und keine zufällige Störung.

**Was ausgeschlossen ist, damit das keine Vermutung bleibt:**

- **Keine Obergrenze.** `service.MaxStderr = 64 << 10` (64 KiB). 510 ist kein Anschlag.
- **Kein falscher Zähler.** `ring.Write` hängt mit `append` an, `ring.bytes` gibt `r.b` zurück,
  `logStderr` schreibt `len(b)` (`apps/server-controller/classes/serve/call.go`). Die Zahl ist
  eine echte Byte-Zahl und kein Puffermaß.
- **Nicht die `sudo`-Warnung des Messwegs.** Derselbe Aufruf ohne `cd /tmp` schreibt
  `cannot chdir to /root: Permission denied` — 41 Byte — und scheitert dabei ganz. Das erklärt
  die 477 nicht.
- **Kein Anzeichen für einen Fehlschlag.** Der erfolgreiche Lauf um 12:29:55 schreibt dieselben
  510 Byte wie die drei davor. (Hier stand zuerst das Gegenteil: dass 510 auf fehlgeschlagene
  Läufe deute. Das war falsch und ist durch die vierte Zeile widerlegt.)

**Nicht gemessen: was die 477 Byte sind.** Und das ist der zweite Teil des Befundes, denn es
lässt sich **nicht nachlesen**: den Text gibt es nur mit `serve.CallInput.LogStderr`, der von
Haus aus aus ist, und `/etc/ngc/controller.json` hat dafür **keinen Schlüssel** (`anchor`,
`apis`, `db`, `dbKey`, `name`, `noiseKey`, `podman`, `releaseKey`, `roles` — und keinen
weiteren). Ein Betreiber kann also die Zahl sehen und den Text nicht, ohne neu zu bauen.

**Zu entscheiden ist dreierlei:**

1. Wo die 477 Byte entstehen, und ob sie überhaupt in den Ring der App gehören. Was nicht die
   App geschrieben hat, sollte ihr das Protokoll nicht zuschreiben.
2. Ob der Schalter für den Text in die Konfiguration gehört. Solange er nur im Quelltext steht,
   ist die eine Zeile, für die ein Protokoll da ist, im Ernstfall nicht lesbar.
3. Ob die Zahl ohne den Text überhaupt etwas wert ist. Sie sollte „die App hat geredet" sagen;
   gemessen sagt sie „die App und 477 Byte von irgendwem".

Für eine Uhr ist das Kosmetik. Für eine App, die erklären soll, warum sie nicht konnte, ist es
der Unterschied zwischen einem Protokoll und einem Rätsel.

Herkunft und die ganze Messreihe: `docs/uhr-in-den-laden.md`, Abschnitt 16.
