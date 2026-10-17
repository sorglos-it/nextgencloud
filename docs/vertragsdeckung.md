# Vertragsdeckung — was von `shared/` je ein echter Lauf berührt hat

**Stand 17.10.2026.** Dieses Dokument ändert nichts. In `shared/` ist nichts angefasst.

Die Frage war: `shared/` ist eingefroren und gilt als der Vertrag des ganzen Systems. Seit dem
16./17.10.2026 ist vieles davon zum ersten Mal **im echten Betrieb** gefahren. **Welche Teile
kamen dabei wirklich vor — und welche nur in Fällen?** Ein Vertragsteil, den nie ein echter Lauf
berührt hat, ist eine Zusage auf Papier.

---

## Wie gemessen wurde — und über welchen Baum

Das ist hier kein Vorspann, sondern der Punkt, an dem am 17.10. schon einmal ein Befund
entstanden ist, der keiner war (Punkt 60, berichtigt durch Punkt 61).

- Arbeitsbaum `D:\claude\ngc-vertragsdeckung`, Zweig `vertragsdeckung`, von `origin/main`
  (**`9436732`**, „Merge ngc-abbild-digest"). Lokale Platte, nicht Z:.
- Baum vollständig: `git diff --name-only --diff-filter=D HEAD` = **0** Zeilen.
- **Gegenprobe der Grundmenge gegen etwas, das dort sicher liegt:** dieser Baum trägt
  `docs/vertraege-v1.2.md` mit **1864 Zeilen bis einschließlich Punkt 61**. Der Hauptcheckout
  `Z:\GitHub\nextgencloud` steht auf `feldzug-echter-server` und hat dort **1207 Zeilen, Ende bei
  Punkt 38** — wer dort gezählt hätte, hätte die Punkte 44–61 gar nicht gesehen.
- **Der falsche Baum von Punkt 60, nachgestellt:** `apps/web` hat auf `HEAD` **2** Dateien, auf
  `origin/web` **128**. Die Oberfläche liegt auf `web`. Für diese Zählung ändert das nichts, und
  zwar gemessen: `git diff --stat HEAD origin/web -- shared/` nennt **eine** Datei,
  `shared/ulh.md` (die eine gewährte Ausnahme der Einfrierung) — **kein `.go` von `shared/`
  unterscheidet sich zwischen `main` und `web`.** Der Vertrag ist auf beiden Zweigen derselbe.
- **Die Aufrufer liegen vollständig auf `main`:** `origin/api`, `origin/controller` und
  `origin/db` sind `ahead=0` gegenüber `HEAD`, also zusammengeführt. Die Oberfläche auf `web` ist
  PHP und JavaScript und importiert kein Go-Paket von `shared/` — sie kann hier also keinen
  Aufrufer liefern und verfälscht die Zählung nicht.

**Gezählt wurde mit `go/ast`, nicht mit `grep`.** Drei Durchläufe über denselben Baum:

1. die öffentlichen Deklarationen je Paket (Typen, Funktionen, Methoden mit öffentlichem
   Empfänger, Konstanten, Variablen) aus allen `.go`-Dateien außer `_test.go`;
2. jede qualifizierte Benutzung `paket.Name` im ganzen Baum, aufgelöst über die Importzeilen der
   jeweiligen Datei (auch bei umbenanntem Import), getrennt nach `apps/`, `shared/` und `_test.go`;
3. jede unqualifizierte Benutzung **innerhalb** des erklärenden Pakets, einmal über die
   Produktionsdateien und einmal über dessen eigene `_test.go`.

### Was diese Methode nicht kann — und wo sie mich schon getäuscht hat

- **Methoden werden über den Namen gezählt, nicht über den Empfänger.** Bei `Apply`, `Format`,
  `String` trifft das auch Fremdtypen. Die Zahl ist dort eine obere Schranke.
- **Aufrufe über eine Schnittstelle sieht kein Namenszähler.** Das ist hier keine Theorie: mein
  erster Durchlauf meldete `config.Secret.LogValue`, `API.LogValue`, `DB.LogValue` und
  `Controller.LogValue` als „von niemandem genannt". **Falsch** —
  `shared/config/config.go:701`, `:726` und `:755` setzen `String()` als
  `return x.LogValue().String()` um, und `TestLogValues` (`config_test.go:755`) fährt `String()`.
  Zehn der sechsundzwanzig Treffer meiner ersten Liste waren von dieser Sorte. Jeder verbliebene
  Treffer der dritten Liste unten ist darum **einzeln am Code nachgesehen**, nicht aus dem Zähler
  übernommen.
- **„Auf dem Weg" heißt: in einer Datei, die in den Punkten 56–61 ausgeführt wurde.** Ob der
  einzelne Zweig *innerhalb* dieser Datei gelaufen ist, sagt diese Messung **nicht**. Eine
  Fehlerbehandlung in `setup/setup.go` zählt hier als „gefahren", obwohl sie vielleicht nie
  ausgelöst wurde. Die erste Liste ist damit **großzügig** — und die dritte Liste, die der
  eigentliche Fund ist, dadurch **kleiner als die Wahrheit**, nicht größer.

### Was als „echter Lauf" gilt

Nur die Wege der Punkte 56–61, also 16./17.10.2026 auf Debian 13. Ältere echte Läufe (Punkt 44,
API↔Controller am 13.10., und Punkt 45, echter Store am 13.10.) zählen hier **nicht** mit; wo das
den Ausschlag gibt, steht es dabei.

| Weg | Punkt | Dateien, über die zugeordnet wurde |
|---|---|---|
| Release unterschreiben und prüfen | 56, 57 | `apps/cli/classes/{run,release,check}.go` |
| `ngc-all` / `ngc-web` installieren, Dienst startet | 56, 57 | `apps/server-api/main.go`, `classes/{apitls,dbopen,lock,secrets,server}`, `apps/server-db/classes/{register,sqlite,migrate,migraterun}`, `apps/server-controller/classes/apache` |
| Einrichtungsseite im Browser | 58, 59 | `apps/server-api/classes/{httpapi,setup}` |
| Anmeldung mit Passwort, Schreibtisch | 59 | `apps/server-api/classes/{login,rights,audit,browser,guard,state}`, `classes/coreapp/{dispatch,desktop,users}.go`, `apps/server-db/classes/sqlstore` |
| Kopplungscode | 59, 61 | `apps/server-api/classes/coreapp/users.go` |
| Kopplung, Vertrauensanker | 61 | `apps/server-controller/classes/{pairing,chain,state}`, `apps/server-api/classes/controllers` |
| Tunnel steht | 61 | `apps/server-api/classes/{relay,link}`, `apps/server-controller/classes/{link,node,serve}` |

**Ausdrücklich abseits** (gebaut, aber am 16./17.10. nicht gefahren, und Punkt 61 sagt das selbst:
„ob mit gekoppeltem Controller nun wirklich Apps laufen … ist der nächste Messpunkt"):
`appstore` bis auf die Liste des Schreibtischs, `updates`, `join`, und beim Controller
`install`, `license`, `runtime`, `terminal`, `update`, `access`, `e2e`, dazu
`apps/server-db/classes/mariadb` und in `apps/cli/classes` alles außer `run`, `release`, `check`.

---

## 1. Der Vertrag, ausgezählt

650 öffentliche Posten in den zehn Paketen.

| Paket | öffentlich | gefahren | nur in Fällen | gar nichts |
|---|---:|---:|---:|---:|
| `wire` | 82 | 60 | 22 | **0** |
| `core` | 132 | 88 | 44 | **0** |
| `store` | 105 | 41 | 61 | **3** |
| `trust` | 64 | 42 | 15 | **7** |
| `tunnel` | 63 | 48 | 15 | **0** |
| `config` | 67 | 45 | 18 | **4** |
| `session` | 71 | 47 | 22 | **2** |
| `sign` | 21 | 11 | 10 | **0** |
| `dbapi` | 26 | 21 | 5 | **0** |
| `enc` | 19 | 8 | 10 | **1** |
| **Summe** | **650** | **411** | **222** | **17** |

**Die Zahl, die zählt: 411 von 650 Posten, 63 %, sind in einem echten Lauf vorgekommen.** Vor dem
16.10. wäre diese Spalte in jeder Zeile **0** gewesen — die Punkte 56–61 sind der Unterschied
zwischen einem Vertrag auf Papier und einem gefahrenen.

Drei Zeilen fallen heraus und sind erklärbar:

- **`store` ist das schwächste Paket mit 41 von 105.** Der Store wurde am 16./17.10. nur zum
  **Unterschreiben und Prüfen** eines Release gefahren (`ngc-store release`, `ngc-store check`).
  Packen, Verschlüsseln, Katalog und Lizenzen liefen am 13.10. (Punkt 45) — nach der hier
  gesetzten Grenze also „nur in Fällen". Wer Punkt 45 mitzählen will, verschiebt rund 20 Posten
  nach links.
- **`enc` ist mit 8 von 19 das kleinste und am dünnsten gefahrene.** Das täuscht: `enc` ist der
  gehärtete JSON-Leser, und seine Arbeit steckt in `Decode`/`Budget`, die **jede** geladene
  Konfiguration und **jede** Nachricht durchlaufen. Was hier als ungefahren zählt, sind seine
  **Grenzen und Fehler** — also genau die Zweige, die nur ein Angreifer auslöst.
- **`dbapi` hat keinen einzigen Posten ohne Deckung** und als einziges Paket **keine eigenen
  Tests** (`owntest = 0`): es ist reine Schnittstelle, und die Deckung kommt vollständig aus den
  Aufrufern.

---

## 2. Die erste Liste — gefahren (411)

Vollständig aufzuzählen hilft niemandem. Stattdessen die Stellen, an denen ein echter Lauf etwas
zum **ersten Mal** berührt hat, je mit dem Aufrufer, der es belegt:

| Posten | Punkt | Beleg (wer ruft) |
|---|---|---|
| `store.VerifyCatalog` (`catalog.go:300`) | 56/57 | `apps/cli/classes/check.go` — `release ok`, dreimal |
| `sign.Verify` über den Release-Digest | 56/57 | `apps/cli/classes/release.go`, Kennung `pN6Z6a9NR0aG6aEJIJ5EHg` |
| `config.LoadAPI`, `config.Secret.UnmarshalJSON` | 56/57 | `apps/server-api/main.go` → `/etc/ngc/api.json`; Befund 1 von Punkt 56 ist genau diese Stelle |
| `trust.ValidTunnelURL` | 57 | `apps/server-api/classes/apitls/apitls.go` |
| `session.SetupOffer`, `session.OfferMAC`, `session.VerifySetup` | 58/59 | `apps/server-api/classes/setup/setup.go:301`, `:354` |
| `config.LoadAnchor` | 59/61 | `apps/server-api/classes/state/trust.go` |
| `dbapi.App` | 59 | `apps/server-api/classes/coreapp/desktop.go` — die Kacheln des Schreibtischs |
| `core.ActPairCode` (`core.go:48`) | 59/61 | `apps/server-api/classes/coreapp/users.go:255` |
| `tunnel.PairCode`, `tunnel.ParsePairCode`, `tunnel.PairLifetime` | 61 | `apps/server-api/classes/controllers/pair.go`, `apps/server-controller/classes/node/main.go` |
| `trust.Anchor` | 61 | `apps/server-api/classes/controllers/pair.go` — der Anker entsteht |
| `store.ValidVersion` | 61 | `apps/server-controller/classes/state/state.go` |
| `wire`-Rahmen, Kanäle, `CtlStatus` | 61 | `apps/server-controller/classes/link/`, `apps/server-api/classes/relay/` |

---

## 3. Die zweite Liste — nur in Fällen (222)

Kein echter Lauf, aber ein Prüfstand deckt es ab. Die Gruppen, nach Umfang:

- **`store`, 61 Posten.** Alles um Packen, Verschlüsseln, Katalog, Lizenzen und Oberflächen-Archiv:
  `MaxEntries`, `MaxApps`, `MaxVersions`, `MaxCatalogVersions`, `PriceBuy`,
  `PriceSubscription`, `PeriodMonth`, `GraceDefault`, `MaxGrace`, `MaxDirs`, `FileIDSize`,
  `RuntimePodman`, `WrappedKey`, `UI`, `NewUIWriter`, `Action`, `Granted`, `SupportsMinNGC`,
  `ValidPath`. Prüfstände: `apps/cli/classes/*_test.go`, `shared/store/{pack,ui,catalog}_test.go`,
  `apps/server-api/classes/appstore/*_test.go`.
- **`core`, 44 Posten.** Ergebnis- und Meldungsformen, die nur ein laufender App-Kanal erzeugt:
  `NoticeError`, `NoticeWarn`, `NoticeInfo`, `LicenseValid`, `CatalogResult`, `FlowCall`,
  `ActDeleteData`, `All`. Prüfstände: `coreapp/{fuzz,store}_test.go`,
  `server-controller/classes/{license,node}/*_test.go`.
- **`session`, 22 Posten.** Der **Passschlüssel**-Zweig der Anmeldung vollständig:
  `LoginChallenge`, `LoginFinish`, `ParseLoginChallenge`, `VerifyAPI`, `Setup`, `SetupMAC`,
  `ParseSetupOffer`, `ParseTrustHint`, `ErrExpired`, `ErrMessage`. Punkt 59 ist mit **Passwort**
  angemeldet, nicht mit Passkey — die Oberfläche empfiehlt Passkeys dort selbst, und genau dieser
  Weg ist bis heute nur ein Prüfstand.
- **`wire`, 22 Posten** (u. a. `HeaderSize`, `MaxBrowserOpen`, `Status`), **`trust`, 15**
  (`MaxUsers` — `trust_test.go:635` —, `MaxRoles`, `ErrInvalid`, `ErrSigner`), **`tunnel`, 15**
  (u. a. `RoleAPI`), **`config`, 18** (u. a. `WebRoot`, `MaxConfig`, die `LogValue`-Reihe,
  `SecretText.UnmarshalJSON` über `TestRedactedFileRefused`, `config_test.go:824`),
  **`sign`, 10**, **`store`/`dbapi`** Rest.

---

## 4. Die dritte Liste — gar nichts (17)

Weder ein echter Lauf noch ein Fall. **Das ist der eigentliche Fund.** Jeder Posten unten ist
einzeln am Code nachgesehen; die Spalte „wo es überhaupt steht" sagt, was es gibt außer der
Erklärung selbst.

| Posten | Art | Ort | Wo es überhaupt steht |
|---|---|---|---|
| `trust.MaxNodes` | const 256 | `trust.go:27` | durchgesetzt `trust.go:490`, Budget `:212` |
| `trust.MaxStores` | const 64 | `trust.go:29` | durchgesetzt `trust.go:490`, Budget `:215` |
| `trust.MaxKeysPerUser` | const 16 | `trust.go:30` | durchgesetzt `trust.go:578`, Budget `:208`, dazu `session.go:103/455/479` |
| `trust.MaxAuto` | const 64 | `trust.go:32` | durchgesetzt `trust.go:490`, Budget `:216` |
| `trust.MaxRoot` | const 256 | `trust.go:34` | durchgesetzt `trust.go:526`, Budget `:213/:214` |
| `trust.MaxEntries` | const 256 | `trust.go:31` | durchgesetzt `trust.go:633`, Budget `:210/:211` |
| `enc.MaxDepth` | const 32 | `enc.go:136` | durchgesetzt `enc.go:318`, Stapel `:271`, Pfad `:273` |
| `session.ChallengeKeyLabel` | const | `session.go:323` | benutzt `session.go:502` |
| `session.ErrChallenge` | var | `session.go:265` | zurückgegeben `session.go:377`, `:400`, `:417`, `:420` |
| `config.Secret.MarshalJSON` | method | `config.go:133` | nur die Erklärung |
| `config.SecretText.MarshalJSON` | method | `config.go:181` | nur die Erklärung |
| `config.APIPort` | const 7443 | `config.go:41` | **nirgends sonst, im ganzen Baum ein Treffer** |
| `config.WebFile` | const | `config.go:31` | **nirgends sonst, im ganzen Baum ein Treffer** |
| `store.DefaultCPU` | const | `manifest.go:29` | nur abseits: `server-controller/classes/runtime/podman.go` |
| `store.DefaultMemory` | const | `manifest.go:30` | nur abseits: `server-controller/classes/runtime/podman.go` |
| `store.PeriodYear` | const | `catalog.go:24` | nur paketintern |
| `trust.NodeKind` | type | `trust.go:48` | nur paketintern |

**Was „nur paketintern" hier heißt:** die Zeile, die die Konstante durchsetzt, **steht** —
`trust.go:490` vergleicht wirklich gegen `MaxNodes`. Gelaufen ist sie, sooft eine Vertrauensliste
geprüft wurde. Was **nie** gelaufen ist, ist der Zweig **dahinter**: die Grenze wurde nie erreicht,
nicht im Betrieb und nicht in einem Fall. Niemand hat je gesehen, was bei 257 Knoten passiert.

---

## 5. Sicherheitsriegel ohne jede Messung, nach Schwere

Elf der siebzehn sind Riegel. Die Reihenfolge ist die, in der ich sie messen würde.

### 1. Die sechs Größengrenzen der Vertrauensliste (`trust.MaxNodes`, `MaxStores`, `MaxKeysPerUser`, `MaxAuto`, `MaxRoot`, `MaxEntries`)

**Warum zuoberst:** die Vertrauensliste ist das einzige Stück des Systems, das **unterschrieben und
verteilt** wird. Sie sagt, welche Knoten es gibt, welche Stores, wer welche Rechte hat und wer auf
einem Controller root werden darf. Punkt 61 hat sie zum ersten Mal entstehen lassen
(`/etc/ngc/trust-anchor.json`), und Punkt 61 hat auch gezeigt, dass sie **jedem Browser**
weitergereicht wird (`index.php` liest sie, darum musste der Modus von `0600` auf `0644`).

**Was gemessen ist und was nicht**, nebeneinander gelegt — **ein einziger `if`**,
`trust.go:489–491`, sechs Grenzen:

```
MaxUsers   = 10000   trust_test.go:635 fährt die Grenze              ← gemessen
MaxRoles   = 256     core_test.go:346/1255/1256 als Budget           ← gemessen
MaxOrigins = 8       config_test.go:382 und :399, beide Seiten       ← gemessen
MaxNodes   = 256     kein Fall, kein Lauf
MaxStores  = 64      kein Fall, kein Lauf
MaxAuto    = 64      kein Fall, kein Lauf
```

**Drei von sechs Grenzen desselben `if` sind gefahren, drei nicht** — und `MaxOrigins` sogar auf
beiden Seiten (acht grün, neun rot). Das ist kein Vollständigkeitsproblem, sondern eine
**ungleiche** Lücke: wer die Fälle für `MaxUsers` und `MaxOrigins` geschrieben hat, hatte die
drei daneben vor Augen und hat sie ausgelassen. Dieselbe Ungleichheit außerhalb dieses `if`:
`MaxKeysPerUser` (`:578`), `MaxRoot` (`:526`) und `MaxEntries` (`:633`) haben ebenfalls keinen.

**Und der echte Lauf kam nicht in die Nähe:** die Anlage aus Punkt 61 hat **einen** Knoten, **null**
Stores, **einen** Benutzer mit **einem** Schlüssel. Die Grenzen sind um drei Größenordnungen
entfernt von allem, was je gelaufen ist.

**Was daran gefährlich ist, konkret:** `MaxKeysPerUser` hängt an `session.go:455`
(`passkeys [trust.MaxKeysPerUser + 1]int`) — ein **Feld fester Länge**, indiziert aus einer Zahl,
die aus der Liste kommt. Der Schutz davor ist `trust.go:578`. Fällt der, schreibt `session` über
das Feld hinaus. Dass die beiden Zeilen zusammengehören, steht in keinem Fall.

### 2. `enc.MaxDepth` — die Verschachtelungsgrenze des gehärteten Lesers

`enc` steht vor **jeder** Nachricht und **jeder** Konfiguration. `MaxDepth = 32` begrenzt die
Schachtelung und trägt den Stapel fester Länge `stack [MaxDepth]frame` (`enc.go:271`) — wieder ein
Feld, dessen Länge von dieser Zahl kommt.

**Der Nachbar hat einen Fall, dieser nicht.** `MaxMembers` direkt darunter wird in
`enc_test.go:201–205` auf beiden Seiten der Grenze gefahren (`MaxMembers` grün, `MaxMembers+1`
gibt `ErrBudget`). Für `MaxDepth` gibt es dieselbe Zeile **nicht** — und das ist der Riegel gegen
eine tief geschachtelte Nachricht, also die billigste Art, einen Leser umzubringen.

### 3. `session.ChallengeKeyLabel` — die Trennung, die Punkt 52 trägt

`const ChallengeKeyLabel = "ngc/login-unknown/v1"` (`session.go:323`) geht bei `session.go:502` in
das HMAC, das die **erfundene** Antwort für einen unbekannten Benutzer erzeugt. Das ist der Riegel
aus Punkt 52: die Anmeldung soll nicht verraten, ob es den Benutzer gibt. Die Bereichstrennung
hängt genau an dieser Zeichenkette.

**Kein Fall nagelt den Wert fest.** Wer das Etikett ändert — oder es versehentlich mit dem Etikett
eines anderen HMAC zusammenfallen lässt — bricht die Trennung, und **jeder** Prüfstand bleibt grün,
weil keiner den Wert kennt. Der Riegel selbst mag wirken; **was niemand misst, ist, dass er ein
eigener Riegel bleibt.**

### 4. `session.ErrChallenge` — vier Ablehnungen, keine davon geprüft

`ErrChallenge` wird an **vier** Stellen zurückgegeben (`:377`, `:400`, `:417`, `:420`) — u. a.
„ein Passwortschlüssel ohne KDF". Kein Fall behauptet diesen Fehler. Vier Ablehnungszweige, die
niemand je ausgelöst hat; eine davon still durch `nil` zu ersetzen bliebe unbemerkt.

### 5. `config.Secret.MarshalJSON` und `config.SecretText.MarshalJSON` — der geringste der fünf

Die **schwärzende** Fassung: `json.Marshal` eines Geheimnisses gibt `"[secret]"` statt des
Schlüssels. **Warum das trotzdem unten steht:** derselbe Schutz steht über eine zweite Tür, und die
**ist** gemessen — `String()`/`Format()` geben ebenfalls `"[secret]"`, und `TestSecret`
(`config_test.go:784`) fährt sogar `fmt.Sprintf("%#v", s)`. Der Gegenriegel beim Lesen ist
ebenfalls gemessen (`TestRedactedFileRefused`, `config_test.go:824`). Was fehlt, ist allein der
Fall für den Weg über `json.Marshal` — und im Baum ruft ihn heute niemand.

### Nicht als Riegel gewertet

`config.APIPort`, `config.WebFile`, `store.DefaultCPU`, `store.DefaultMemory`, `store.PeriodYear`,
`trust.NodeKind`. Bequemlichkeiten und Vorgaben. `APIPort` und `WebFile` sind dabei die einzigen
beiden Posten des ganzen Vertrags, die im **ganzen Baum genau einen Treffer** haben: ihre eigene
Erklärung.

---

## 6. Gegenprobe gegen die v1.3-Vorlage

### Gehört etwas aus der dritten Liste dort hinein? — Nein, und zwar aus dem Grund, den die Vorlage selbst nennt

**P10 (`vertraege-v1-3-vorschlag.md:48`): „Kein Vertrag von `shared/` darf etwas dazubekommen."**
Die sechs `trust.Max*`, `enc.MaxDepth`, `ChallengeKeyLabel` und `ErrChallenge` **stehen bereits im
Vertrag** — die Zahlen sind da, die Zeilen setzen sie durch. Es fehlt kein Vertragsteil, es fehlt
eine **Messung**. Diese Posten gehören darum in den Messplan, nicht auf eine Entscheidungsvorlage,
die mit JA oder NEIN zu beantworten ist. Sie sind kein Beschluss, sie sind Arbeit.

**Eine Frage bleibt für v1.3 übrig, und sie ist klein:** `config.APIPort` (`config.go:41`) und
`config.WebFile` (`config.go:31`) sind tote Konstanten des eingefrorenen Vertrags. Streichen ist
kein „Dazubekommen" im Sinne von P10, aber es ist trotzdem eine Vertragsänderung. **Entscheidung
nötig: mitnehmen oder stehen lassen?** Ich empfehle **stehen lassen** — zwei Konstanten kosten
nichts, und jede Zeile, die der Schnitt anfasst, ist eine Zeile, die jemand nachprüfen muss.

### Umgekehrt: ist ein Posten der Vorlage längst gefahren?

| Posten | Zeile | Was diese Messung dazu sagt |
|---|---|---|
| **P6** — Kopplungscode gilt 24 Stunden | `:420`, Zahl bei `:424` | **Der Weg ist jetzt gefahren, die Zahl nicht.** `tunnel.PairLifetime` steht im echten Lauf — `apps/server-api/classes/controllers/pair.go`, Punkt 61 —, zusammen mit `PairCode` und `ParsePairCode`. Die Vorlage ist am 15.10. **am Code** nachgemessen; seit dem 17.10. ist derselbe Pfad **im Betrieb** gelaufen. Das 24-Stunden-Fenster selbst hat niemand ablaufen lassen. **Der Posten gilt unverändert** — aber er betrifft kein totes Stück mehr, sondern den Weg, über den jede Kopplung läuft. Das macht ihn dringender, nicht weniger. |
| **P5** — Referenzfeld in `dbapi.App` | `:364` | **`dbapi.App` ist gefahren** (`coreapp/desktop.go`, Punkt 59 — die Kacheln des Schreibtischs). Die fehlende Referenz steckt also in einem Typ, der im Betrieb benutzt wird, nicht in einem ungenutzten. `dbapi` ist zudem das einzige Paket mit **100 % Deckung und null eigenen Tests**. |
| **P3** — Was eine `ngc-web`-Release-Datei ist | `:288`, `WebRoot` bei `:292` | **Gilt unverändert.** Punkt 56 hat beantwortet, was in der `.tar` **liegt**, aber `config.WebRoot` steht nach dieser Zählung weiter **nur in Fällen** (`server-controller/classes/update/install_test.go`), nicht in einem echten Lauf. Die Messung bestätigt die Behauptung der Vorlage bei `:292` wörtlich: in keiner weiteren `.go`-Datei von `shared/`. |
| **P1** — Die Adressprüfung ist nicht ausgeführt | `:149` | **Gilt, und ist jetzt auch ohne Lauf.** `apps/server-api/classes/appstore/fetch.go` liegt abseits der Wege 56–61 — der Weg, auf dem die schwächere zweite Grammatik sitzt, ist am 16./17.10. **gar nicht gefahren**. Die Einschätzung der Vorlage („eine Naht ohne Riegel, kein offenes Tor") stützt sich auf `store.VerifyCatalog` davor — und **das** ist gefahren (`apps/cli/classes/check.go`, Punkt 56/57). Die Begründung trägt also. |
| **P11** — `store.ValidPath` | `:703` | Gilt. `ValidPath` ist **nur in Fällen** (4 Prüfstände), kein echter Lauf. |
| **P12** — Rechte des Katalogeintrags gegen das Manifest | `:760` | Gilt. `store.Granted` ist **nur in Fällen**, und seine Aufrufer (`appstore/activate.go`, `updates/rows.go`) liegen abseits. |
| **P4** — Balancer-Vorlage | `:343` | Steht schon als **ERLEDIGT**; Punkt 56, Befund 2 hat es am laufenden Apache belegt. Stimmt mit dieser Messung überein. |

---

## Was ich daraus als nächsten Schritt vorschlagen würde

1. **Sechs Grenzfälle für `shared/trust`** — je einer bei `Max` und bei `Max+1`, in einer Tabelle
   neben dem bestehenden `MaxUsers`-Fall (`trust_test.go:635`). Das ist eine knappe Stunde Arbeit
   und schließt die schwerste Lücke dieses Berichts.
2. **Ein Grenzfall für `enc.MaxDepth`**, als Zwilling von `enc_test.go:201–205`.
3. **Ein Fall, der `session.ChallengeKeyLabel` festnagelt** — und zugleich prüft, dass es mit
   keinem anderen Etikett in `session` zusammenfällt.
4. **Der Passschlüssel-Zweig** (`session`, 22 Posten in der zweiten Liste) ist der größte Block,
   der nur in Fällen lebt, und die Oberfläche empfiehlt ihn dem Benutzer aktiv. Ein echter Lauf
   dafür wäre der nächste Punkt nach „laufen Apps?".
