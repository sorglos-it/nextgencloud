# Verträge V1.2 — Sammelliste für den nächsten Schnitt

Stand 24.09.2026. Gepflegt vom Koordinator. `shared/` ist eingefroren auf
`vertraege-v1.1`; alles hier ist aufgelaufen, WEIL es nicht geändert werden
durfte. Diese Liste ist der Auftrag für den Schnitt V1.2 — nichts davon darf
still unter den Tisch fallen.

Herkunft steht je Punkt dabei: Strang und Ledger-Zeile.

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

6. **`state.NewestSeq` ohne Vollscan**, und ein Weg, **den Anker vorzurücken** —
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

23. **`dbtest` kann einen falschen Store nicht von einem richtigen
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

10. **Das unauthentifizierte GET-Angebot ist Klartext**: Installations-ID,
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

35. **Zwei Sätze des Vertrags behaupten eine Prüfung, die es nicht gibt.**
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

33b. **Wer die Datenbank schreiben kann, nimmt jede Rücknahme zurück — und kann die
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

## shared/dbapi — die obere Grenze einer Protokollseite

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

## shared/trust — eine Speicher-Obergrenze, die unter -race FLATTERT

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
