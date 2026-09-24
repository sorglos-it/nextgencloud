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

## shared/README.md und Querschnitt

14. **`shared/README.md:229` sagt „Wert", wo „Container" stehen muss.**

15. **Ein gemeinsamer Helfer für Dateirechte.** Heute legt `OpenDB` keine fest
    (der Schutz kommt aus dem Verzeichnis), und jeder Strang löst es selbst.
    (db, Zeile 12)

16. **Die Falle im Vertragsentwurf benennen:** Ein RAW-Pfad über einer Liste hebt
    deren Grenze auf — so macht es Go. Das gehört als Satz in den Vertrag, nicht
    nur in den Kopf des nächsten Umsetzers. (web, Zeile 18)

## Nicht vergessen

- Die Zahlen aus Punkt 1 und 3 sind **gemessen**, nicht geschätzt. Wer sie in den
  Vertrag schreibt, schreibt die Messung dazu.
- Punkt 7 ist der einzige mit einer harten Reihenfolge: vor dem zweiten
  API-Server.
