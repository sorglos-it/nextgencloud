# Ergebnis: API-Gesamtprüfung

Geprüft: `origin/api` bei `af2c186`, in einem eigenen Worktree, danach entfernt.
Nichts committet, nichts gepusht, `git status` in beiden Bäumen leer.

Der Auftrag nennt 44 Commits — gemessen sind es **46**
(`git log c7658d9..HEAD | wc -l`). Die 18 Dateien stimmen.

**Kann der Zweig zusammengeführt werden: noch nicht.** Kritisches: keines.

## Wichtig

### W1 — Bei der gewöhnlichen Abfrage antworten die Knoten verschieden

Stellen: `live.go:199–202`, `knowngap_test.go:204–206`,
`TestOneFaultIsOneAnswerOnEveryNode`.

- Kommentar: „What answers 500 for GET session.PathTrust is … state.Chain"
- `knowngap_test.go`: „the status is right"
- Commit `1b4e911`: **„The two nodes agree on it"**
- Der Test prüft **nur `from=1`**.

Die Shell fragt mit **`from = neueste+1`** (`login/trust.go:21–23`). Gemessen bei
drei Fassungen:

| Schaden | warm, `from=4` | kalt, `from=4` |
| --- | --- | --- |
| Zeile 2 fehlt | 204 | 500 |
| alle Zeilen weg | 204 | 500 |

Bei `from=5` antwortet der warme Knoten mit leerer Datenbank **404**.

Die Knoten stimmen nur für `from ≤ neueste` überein. Für die häufigste Anfrage
bekommt die Shell auf dem warmen Knoten „nichts Neues" über eine Liste, die die
Datenbank verloren hat.

### W2 — Eine Sitzung, die zwischen Lesen und SessionTouch verschwindet, antwortet 503 statt 401

Stelle: `live.go:259–260`.

Ein Store, der die Zeile vor dem Touch löscht (gleichzeitiger Logout oder Purge),
ergibt `ok=false`, `code="E_UNREACHABLE"`. Laut `dbapi.go:166` antwortet
`SessionTouch` dort `ErrNotFound`.

Drei Entscheidungen für denselben Fehler:

- `shared/README.md:328` verlangt **E_LOGIN**
- der Kommentar an `sessionRow` (`live.go:49–50`) nennt `ErrNotFound` den Weg zu
  **E_LOGIN**
- `Fault` (`fault.go:579–585`) nennt es „an answer", also **E_INTERNAL**

Dasselbe Muster mit `dbapi.ErrInvalid`: `Fault` schließt es seit `1210adb`
ausdrücklich von 503 aus — liefert `SessionGet` aber `ErrInvalid`, antworten
`Live` und `Logout` trotzdem **503 E_UNREACHABLE** (gemessen).

### W3 — Der Kopf von knowngap_test.go behauptet einen Schaden, den die Messung nicht zeigt

Stelle: `knowngap_test.go:38–43`, Commit `af2c186`.

Der Text: auf „that same twelve-version chain" genüge ein Statement, „Live still
says `ok=true` … cheaper than either … a statement about the danger".

Gemessen auf `withdrawnAtTwelve`, nur Zeile 12 gelöscht, warmer Knoten: `Refresh`
antwortet **Fassung 12, `err=nil`**. Die Sitzung des zurückgezogenen Benutzers
wird aber mit **`ok=false`, `code="E_LOGIN"`** abgelehnt. `ok=true` kommt nur für
einen Benutzer, der **nie** zurückgezogen wurde.

Der warme Knoten entscheidet auf der richtigen, neuesten Liste — es wird nichts
zurückgerollt. Die Rechnung „1 gegen 11" vergleicht **zwei verschiedene Schäden**,
nicht zwei Preise für denselben. Dasselbe Muster, das `af2c186` selbst berichtigen
wollte.

### W4 — Keine Mutation von Logout beim Fehler von SessionDelete wird rot

Stelle: `live.go:297–301`. Fehler ignoriert, sodass `Logout` **204** sagt und den
Cookie löscht: **die ganze Suite bleibt grün.**

Der Kommentar (`live.go:285–289`): „Two steps are left … both carry
E_UNREACHABLE". Getestet ist nur der erste
(`TestLogoutEndsASessionTheRestOfLiveCannotRead`, Fall `SessionGet`). Kein Test
hält, dass ein Logout nie „abgemeldet" sagt, während die Sitzung noch offen ist.

## Klein

- **K1** `setup.go`: „it is not only the two names below" — die beiden Namen
  (`errKey`, `lock.ErrLock`) hat `9b60c69` aus `fault()` entfernt; unter dem
  Kommentar steht keiner mehr.
- **K2** `fault.go:609–610` beschreibt Code, den es nicht mehr gibt. `setup.fault`
  behandelt die beiden nicht mehr namentlich und nicht vorher — es fragt zuerst
  `Fault` und macht danach aus jedem 503 ein 500. `setup/fault` liest sich wie ein
  Pfad, gemeint ist die Funktion.
- **K3** `live.go:257–258` („Seen moves only once everything above has held")
  hält kein Test: `SessionTouch` vor `VerifyClient` → alles grün. Folgen heute nur,
  wenn `Box.Open` scheitert.
- **K4** `live_test.go` und Commit `d76bee8` nennen
  `TestLiveSeesAWithdrawalAnotherAPIWrote` „two commits later"; gemessen mit
  `git log -S`: `ec0db22` → `5820503`, der **direkt folgende** Commit.
- **K5** „Three ends" bedeutet zweimal etwas anderes: in `sessionRow`
  (`live.go:70`) Zertifikat, Leerlauf, Lebensdauer — in `ec0db22` ist die
  Vertrauensliste „the third of the three ends".
- **K6** „124 tests" in `71d4201`, `f075c75`, `af2c186` enthält die Fuzz-Ziele.
  Gemessen mit `go test -list`: **117 `Test*` und 7 `Fuzz*`** (login 59+3,
  state 27+1, setup 17+1, httpapi 14+2).
- **K7** `setup/knowngap_test.go:113` und `knowngap_test.go:236` fehlt der
  Hinweis „correct the comment … and retire this test", den die anderen haben.

## Was niemand ruft (außerhalb der Tests)

`login.Handler.Purge` und damit `lock.Purge`. Die Felder `Live.Row`,
`Live.Verified` und `Live.APIKey`. Nur `Live.Trust` wird benutzt, von
`login/trust.go`.

## Geprüft und in Ordnung befunden

- `bash tools/check.sh` → `check: ok`; `go vet` und
  `go test ./apps/server-api/...` grün; `FuzzTrustRequest` 30 s ohne Fund
  (**779 104 Läufe**).
- **Tote Verweise:** alle `Test*`/`Fuzz*`-Namen aus Commit-Nachrichten und
  hinzugefügten Zeilen maschinell gegen die vorhandenen abgeglichen — keiner
  fehlt. Ebenso alle `paket.Name`-Verweise in neuen Kommentaren.
  `apps/server-controller/main.go` bricht wirklich mit „not built yet" ab.
  `LoadAnchor`, `SaveAnchor`, `AnchorFile` ohne Aufrufer außerhalb
  `shared/config`.
- **31 Mutanten, alle übersetzt und gelaufen** (Q und Z erst im zweiten Anlauf).
  Getötet haben sie jeweils die Tests, die die Commits nennen, an der eigenen
  Zusicherung: Fehler von `VerifyClient` verworfen → die zwei Tests aus
  `d76bee8`; Cache statt `Refresh` → `…AnotherAPIWrote`; M7/M8 → nur
  `…OneAnswer…`; Proof nach der Liste gelesen →
  `TestADamagedProofCostsNoLookAtTheList`; Logout über `Live` → beide Teilfälle;
  `OncePurge` mit unverschobener Uhr → `TestPurgeFreesOnlyWhatIsPastItsTime`;
  `from` vor der Sitzung geprüft → nur `FuzzTrustRequest/seed#8`; `Chain` holt
  sich selbst eine Version → `TestTrustLooksAtTheListOnce`; `Fault` → jeder Zweig
  von `TestFaultOfTheState`; `ownRows` →
  `TestRefreshNamesTheFaultOfItsOwnRows`; `handedIn` →
  `TestAddVersionForeignSigner`; M1 → die beiden Tests aus `68c0b47`; M3 →
  `TestRefreshReadsOnAfterARace`.
- **Die vier bekannten Lücken:** die Reparatur „Live liest die gecachte Zeile
  zurück" macht **nur** `TestKnownGap_AWarmNodeDecidesOnAListTheStoreLost` rot,
  `knowngap_test.go:242`, eigene Meldung. Alle vier `TestKnownGap_*` sagen, dass
  sie eine Lücke festhalten und kein Sollverhalten, woran sie hängen, und dass Rot
  heißt: Kommentar berichtigen, Test in Rente.
- **Zahlen:** ein warmer `Live`-Aufruf kostet **2** Fragen an die
  Vertrauenszeilen, gemessen auf 8 Fassungen — stimmt mit `live.go:138` und `:205`
  überein. Eine Zeile der Fixture in `state` ist 2191–2192 Bytes (27a8252 sagt
  2192). Die Rechnung 10 gegen 11 Statements stimmt.
- **„Nur Kommentare":** `d12891f`, `9281f85`, `d76bee8`, `c6242c2` ändern keine
  Zeile außer Kommentaren; `af2c186` ändert nur zwei Meldungstexte; `ade111a` nur
  gofmt-Ausrichtung.

## Nicht prüfbar

- Bytegleichheit alter Zwischenstände (2324, 2651, 11307 Bytes) — durch spätere
  Commits überholt, nicht nachgebaut.
- Reparatur-Mutanten für die gekürzte Kette, die gesenkte `Seq` und die wieder
  offene Setup-Tür: dafür bräuchte es einen Anker außerhalb der Datenbank, der
  nicht verdrahtet ist. Eine Nachbildung wäre Raterei.
- `govulncheck` nicht installiert. `-race` laut Auftrag nicht wiederholt.
