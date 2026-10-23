# NGC – Entwurf: einen zweiten Benutzer anlegen

**Stand:** 23.10.2026 · **Status:** Entwurf, nicht freigegeben. Nichts davon ist gebaut.
**Einstieg für die nächste Sitzung:** [`docs/STAND.md`](../../STAND.md)
**Kernentwurf:** [`2026-09-19-ngc-kern-design.md`](2026-09-19-ngc-kern-design.md)

---

## 0. Das Ergebnis in vier Sätzen

Thomas sagte am 23.10.2026: „ich kann dort keine user anlegen die funktion gibt es nicht." Das ist
richtig — es gibt keinen Knopf und keinen Befehl dafür, auf keiner Ebene.

**Aber der Befund, dass das eine Lücke im Vertrag ist, hält nicht.** Gemessen am Code: Der Vertrag
kann das schon vollständig. Eine Fassung der Vertrauensliste, die einen zweiten Benutzer trägt,
nimmt die API heute an, prüft sie und verteilt sie — die Aktion dafür steht in `shared/core`, der
Umschlag in `shared/trust`, und `trust.Advance` erlaubt einem Admin als Unterzeichner **jede**
Änderung, nicht nur die an Knoten.

Was fehlt, fehlt **allein im Webinterface**: ein Formular, das eine solche Fassung baut, und ein
Ablauf, der an den Schlüssel des neuen Benutzers kommt, ohne dass sein Passwort durch einen
fremden Browser geht. Deshalb ist Teil 3 dieses Papiers kurz: **`shared/` braucht für den
Grundfall keine einzige Zeile.** Eine Anfrage an den Koordinator gibt es nur für die eine
Bequemlichkeit, die ohne Vertrag nicht geht (Abschnitt 3.2) — und die ist nicht nötig, um
loszulegen.

---

## 1. Gemessen: was heute wirklich möglich ist

Alles in diesem Abschnitt steht am Stand von `origin/main` am 23.10.2026.

### 1.1 Die Liste der Wege

| Weg | Geht? | Beleg |
|---|---|---|
| Kern-Aktion „Benutzer anlegen" in `shared/core` | **gibt es nicht** | `shared/core/core.go:44`–`:53`: die Aktionen von `ngc.users` sind `list`, `rights`, `set-rights`, `trust`, `pair-code`, `join-code`, `pending`, `pending-api`, `nodes`, `log`. Kein `add-user`, kein `invite`. |
| Eine neue **Fassung der Vertrauensliste mit einem zweiten Benutzer** an die API schicken | **geht, heute, ohne Vertragsänderung** | `shared/core/core.go:142`: `{AppUsers, ServiceAPI, ActTrust, true, false, true, FlowIn, nil, MaxTrustIn}` — der Öffner schickt eine `trust.Signed` als JSON, bis 6 MiB (`:74`). Der Handler nimmt sie ungefiltert: `apps/server-api/classes/coreapp/users.go:226`–`:258` liest den Rumpf, `trust.ParseSigned`, `rq.S.AddVersion`, fertig. Nichts darin sieht sich die Benutzer an. |
| Ein Admin darf in einer neuen Fassung einen Benutzer **hinzufügen** | **ja** | `shared/trust/trust.go:288` `Advance`: die Einschränkung `onlyOwnKeys` greift nur, wenn der Unterzeichner **kein** Admin ist (`:303`: `if !slices.Contains(u.Roles, RoleAdmin) && !onlyOwnKeys(...)`). Ein Admin unterschreibt jede Änderung. |
| Mehrere Benutzer in einer Liste | **ja, bis 10 000** | `shared/trust/trust.go:26` `MaxUsers = 10000`; `:489` prüft nur `len(l.Users) == 0` und die Obergrenze. `validateUsers` (`:572`) verlangt je Benutzer eine ID, einen eindeutigen Anmeldenamen, mindestens eine Rolle und **mindestens einen Schlüssel** (`:578`) — und dass am Ende mindestens ein Admin übrig bleibt (`:616`). Eine Obergrenze für Admins gibt es nicht. |
| Das Webinterface **baut** eine solche Fassung | **nein: es gibt keine Stelle, die Benutzer anfasst** | `apps/web/public/assets/js/apps/users.js:186` `buildNextVersion(version, change, …)` ist allgemein — der Rumpf `change(next)` darf alles. Aufgerufen wird sie aber genau einmal: `:1266`, in `joinNode` (`:1260`), und jeder `change`-Rumpf, der dort hineingeht, fasst nur `next.nodes` an. |
| Der Reiter „Benutzer" zeigt ein Formular | **nein, nur eine Tabelle** | `apps/web/public/assets/js/apps/users.js:506`–`:532` `buildUsers`: eine Überschrift, eine Statuszeile, eine Tabelle aus `ngc.users/list`. Kein Knopf, kein Eingabefeld, keine Aktion. |
| Die Ersteinrichtung legt **mehr als einen** Benutzer an | **nein — hart auf einen verdrahtet** | `apps/web/public/assets/js/auth/setup.js:168`–`:176`: `users: [{ … roles: [trust.ROLE_ADMIN], keys: [{ … kind: proof.KIND_PASSWORD … }] }]` — ein Objekt, kein Array, das die Maske füllen könnte. Die Maske fragt nach genau einem Namen und einem Passwort (`setup.js:26`). |
| Der Vertrag **verbietet** mehrere Benutzer in Fassung 1 | **nein** | `session.VerifySetup` (`shared/session/session.go:909`) vergleicht Installation, Knoten, Store und die Dauerfreigaben gegen das Angebot (`:918`–`:941`). Über `Users` sagt es **kein Wort**. Fassung 1 mit zwei Admins bestünde die Prüfung. Das ist nur kein Weg für später: sobald es einen Admin gibt, antwortet die Ersteinrichtung `E_NOTFOUND` (`apps/server-api/CLAUDE.md`, Abschnitt Ersteinrichtung). |
| Ein Benutzer tauscht **seine eigenen** Schlüssel, ohne Admin zu sein | **im Vertrag ja, in der Oberfläche nein** | `shared/trust/trust.go:327` `onlyOwnKeys`, in JavaScript nachgebaut in `apps/web/public/assets/js/contracts/trust.js:381` und `:405`. Keine Oberfläche ruft das: `grep` über `apps/web/public/assets/js/` findet `onlyOwnKeys` nur in `contracts/trust.js` selbst. |
| `set-rights` legt Benutzer an | **nein — es rührt die Vertrauensliste nicht an** | `apps/server-api/classes/coreapp/users.go:117`–`:138`: es prüft, dass die **Rolle** in der Vertrauensliste steht, und schreibt dann nur `rights.Set(ctx, rq.S.DB, *p)`, eine **Datenbankzeile** je Rolle. Es ersetzt die Rechte *einer Rolle*, nie einen Benutzer. Parameter ist `core.RoleRights` (`core.go:141`), nicht ein Benutzer. |
| `ngc-store` (`apps/cli/`) | **nein** | `apps/cli/classes/run.go:23`–`:36`, der ganze Hilfetext: `pack`, `encrypt`, `catalog`, `license`, `release`, `check`, `contents`. Keine Benutzer, keine Vertrauensliste. `apps/cli/CLAUDE.md`: „Apps packen und verschlüsseln, Kataloge und Test-Lizenzen signieren, Release-Dateien packen und prüfen." |
| `apps/cli-recovery/` | **existiert nicht** | `ls apps/` am 23.10.2026: `cli`, `server-api`, `server-controller`, `server-db`, `web`. Kein `cli-recovery`, kein `cli-stick`, kein `server-recovery`. |
| Der Recovery-Modus als Weg | **nein, und zwar doppelt** | Erstens ist er nicht gebaut (Zeile darüber). Zweitens zielt er auf eine andere Sache: `docs/recovery-modus.md`, Abschnitt 2, Punkt 5 — „cli-recovery kann genau zwei Dinge (recover, enroll)". Er legt einen **Not-Admin** an, in einer netzlosen signierten Boot-Umgebung, mit einem Quorum an Root-Sticks, und sein Admin-Begriff ist ein anderer (signierte Admin-Datensätze in der Datenbank, `docs/recovery-modus.md`, Abschnitt 1, Zeile „Benutzerdatenbank"), nicht ein Benutzer der Vertrauensliste. Er ist der Weg zurück ins Haus, nicht die Tür für den Mitbewohner. |
| Auf der Test-VM gibt es ein Werkzeug dafür | **nein** | `ngc-store` ist dort nicht installiert, und es könnte es ohnehin nicht (Zeile darüber). |

### 1.2 Wo ein Benutzer steht, und was ihn wo ausmacht

Das ist der Grund, warum es keinen einfachen „`INSERT INTO ngc_user`"-Weg gibt.

- **Vertrauensliste** (`shared/trust/trust.go:117` `List.Users`, Typ `User`): ID, Anmeldename,
  Rollen, **Schlüssel**. Das ist die Wahrheit. `ngc.users/list` sagt es selbst:
  „Who exists and which roles they hold comes out of the signed list and out of nothing else"
  (`apps/server-api/classes/coreapp/users.go:69`–`:72`).
- **Datenbank** (`apps/server-db/sql/0001-core.sql:50`–`:61`, `ngc_user`): `id`, `name`, `roles`,
  `totp`, `locked`, `failures`, `last_login`. **Kein Schlüssel.** Name und Rollen stehen dort nur
  als Abschrift.
- Die Zeile entsteht **bei der ersten Anmeldung** und nicht vorher:
  `apps/server-api/classes/login/finish.go:285`–`:288` schreibt sie mit Name und Rollen *aus der
  Liste*, nicht aus einer älteren Zeile. `ngc.users/list` rechnet damit, dass sie fehlt
  (`coreapp/users.go:81`–`:83`: „A user of the list without a row is no fault").

**Folge für den Entwurf:** Wer einen Benutzer anlegt, schreibt **keine** Datenbankzeile. Er
schreibt eine neue Fassung der Vertrauensliste. Das ist der ganze Vorgang.

### 1.3 Was damit heute wirklich möglich ist — und was nicht

**Möglich, ohne eine Zeile in `shared/`:** Ein Admin könnte, hätte er die Oberfläche dafür, eine
Fassung `seq+1` bauen, die einen zweiten Benutzer trägt, sie mit seinem eigenen Schlüssel
unterschreiben (`confirm.js`, Zweck `trustlist/v1`) und über `ngc.users/api/trust` schicken. Die
API nimmt sie an, `trust.Advance` lässt sie durch, `settle` und `pushTrust` verteilen sie
(`coreapp/users.go:250`, `:257`). Der neue Benutzer kann sich danach anmelden, und seine
Datenbankzeile entsteht dabei von selbst.

**Nicht möglich, und das ist die ganze Schwierigkeit:** Der Admin hat den **Schlüssel** des neuen
Benutzers nicht. Ein Benutzer ohne Schlüssel ist keine gültige Liste
(`shared/trust/trust.go:578`: `len(u.Keys) == 0` → `ErrInvalid`), und der Schlüssel eines
Passwort-Benutzers *ist* sein Passwort: Ed25519 aus Argon2id über das getippte Wort
(`shared/proof/proof.go:49`–`:62`). Das Passwort darf den Browser des Benutzers nie verlassen
(`apps/web/CLAUDE.md`, Abschnitt Anmeldung). Beide sitzen nicht gleichzeitig am selben Browser.
**Das ist die Entwurfsfrage, und sie ist keine Vertragsfrage.**

---

## 2. Abwägung: wie der Schlüssel und die Unterschrift zusammenkommen

Die Zwangslage in einem Satz: Der neue Benutzer muss sein Passwort selbst wählen, aber seine
Fassung muss ein Schlüssel unterschreiben, der in der Fassung **davor** steht
(`shared/trust/trust.go:288` `Advance` → `:314` `verifySigner` → `prev.KeyOwner`).

Was jeder Ablauf in jedem Fall erfüllen muss — das verlangt der Vertrag heute schon:

- Der öffentliche Schlüssel muss zu `proof.UserKey.Validate` passen
  (`shared/proof/proof.go:123`–`:141`): `ID == sign.KeyID(Public)`, bei `password` genau
  `EdDSA`, 32 Byte, ein `kdf` **und kein** `credId`; bei `passkey` ein `credId`, kein `kdf`.
  Der Admin kann also keinen Platzhalter eintragen, der „später gefüllt" wird.
- `trust.List.Validate` verlangt eindeutige IDs, eindeutige Anmeldenamen, eindeutige
  **Schlüssel-IDs** über alle Benutzer (`:573`, `:596`), sortierte Benutzer und Rollen, und
  mindestens einen Admin (`:616`). Hat irgendein Benutzer einen Passkey, muss die Liste eine
  Relying Party mit mindestens einem Origin tragen (`:616`).
- Das Webinterface baut jede Fassung aus der **geprüften** vorigen, prüft sie selbst mit
  `validateList`, unterschreibt **danach** und prüft die eigene Kette mit `trust.advance`, bevor
  sie geht (`apps/web/.../users.js:186`–`:208`). Kein Ablauf darf das umgehen.

### (a) Einladungscode — der Admin unterschreibt erst, wenn der Schlüssel vorliegt

Der Admin erzeugt eine Einladung. Der neue Benutzer öffnet damit eine Seite, tippt sein Passwort,
der Browser rechnet daraus Argon2id → Ed25519 und schickt **nur den öffentlichen Schlüssel** samt
`kdf` und Anmeldenamen. Diese Anfrage liegt als Wartender. Der Admin sieht sie, vergleicht einen
Fingerabdruck, und **erst dann** baut seine Shell die Fassung mit diesem Schlüssel und
unterschreibt sie.

- **Was es kostet:** eine Warteliste und eine Einladung mit Geheimnis, also entweder eine neue
  Kern-Aktion oder ein Umweg (Abschnitt 3).
- **Was der Angreifer davon hat:** Wer den Einladungscode abfängt, kann einen Schlüssel **anbieten**
  — und zwar nur das. Angenommen wird er erst durch die Unterschrift eines Admins, und der
  vergleicht vorher einen Fingerabdruck, genau wie bei einem wartenden Controller
  (`apps/web/CLAUDE.md`, Abschnitt „Wartende Controller"). Das Passwort sieht er nie: es verlässt
  den Browser nicht, und der öffentliche Ed25519-Schlüssel gibt es nicht her. Wer die Warteliste
  vollschreiben will, braucht je Eintrag ein gültiges Einmal-Geheimnis.
- **Das ist genau das Muster, das NGC für Server schon hat** (`tunnel.JoinCode` →
  `tunnel.JoinRequest` → `ngc.users/pending-api` → Admin unterschreibt,
  `shared/tunnel/tunnel.go:103`–`:122`). Ein zweites Muster für Menschen zu erfinden, wo dieses
  bewährt ist, wäre die schlechtere Wahl.

### (b) Der Admin setzt ein Erstpasswort, der Benutzer muss es ändern

Der Admin tippt ein Erstpasswort, **sein** Browser rechnet daraus den Schlüssel und trägt ihn in
die neue Fassung ein. Der Benutzer meldet sich damit an und ersetzt den Schlüssel über
`onlyOwnKeys` (`shared/trust/trust.go:327`) — der Vertrag erlaubt ihm genau diese eine Änderung
ohne Admin-Unterschrift.

- **Was es kostet: nichts am Vertrag.** Kein neues Feld, keine neue Aktion, keine Warteliste.
  Zwei Formulare im Webinterface, und das Zweite (eigenen Schlüssel tauschen) ist eine
  Oberfläche, die sowieso fehlt — ohne sie kann heute **niemand** sein Passwort ändern.
- **Was der Angreifer davon hat, und das ist der Preis:** Das Erstpasswort geht durch den Browser
  des Admins. Also kennt der Admin bis zum Wechsel den Schlüssel des Benutzers vollständig und
  könnte in dessen Namen alles unterschreiben, was ein Benutzerschlüssel unterschreibt. Dazu
  kommt der Weg des Erstpassworts zum Menschen: ein Zettel, eine E-Mail, ein Anruf. Und
  „muss es ändern" ist eine **Bitte**, kein Zwang — die Vertrauensliste hat kein Feld, das
  einen Schlüssel als vorläufig markiert, und ein solches Feld wäre eine Vertragsänderung
  (`shared/trust/trust.go:117`, `proof.UserKey`). Bis zum Wechsel kann der Admin jede Handlung
  des Benutzers vortäuschen, und nachher kann niemand sagen, wann der Wechsel war.
- **Trotzdem nicht wertlos:** Für eine Anlage, in der ein Admin ohnehin alles kann, ist das kein
  neues Risiko gegen *ihn* — aber es ist eines gegen jeden, der die Liste später liest und eine
  Unterschrift dem Benutzer zuordnet. Für den Grundfall „Thomas legt ein Testkonto an" reicht es
  völlig.

### (c) Passkey-Einladung

Wie (a), nur erzeugt der Browser des Eingeladenen keinen Passwortschlüssel, sondern einen Passkey
(WebAuthn), und meldet `credId` und den öffentlichen Schlüssel zurück.

- **Was es kostet:** alles von (a), und dazu: die Liste braucht dann `rp.id` und mindestens ein
  Origin (`shared/trust/trust.go:616`). Eine Anlage, die mit Fassung 1 ohne Passkeys angelegt
  wurde, hat ein leeres `rp` (`setup.js:163`–`:164`: „It is empty as long as this installation has
  no passkeys"). Der erste Passkey überhaupt ist also **zusätzlich** eine Änderung an `rp` — und
  wessen Origin dort steht, entscheidet, welche Seite den Passkey je abfragen darf. Das ist eine
  eigene Entscheidung und sollte nicht an einer Benutzeranlage hängen.
- **Was der Angreifer davon hat:** weniger als bei (a) — es gibt kein Passwort, das jemand
  erraten, wiederverwenden oder weitersagen kann, und WebAuthn bindet den Schlüssel an das
  Origin. Dagegen steht, dass der Eingeladene ein Gerät braucht, das das kann, und dass ein
  verlorener Passkey ohne zweiten Schlüssel ein verlorenes Konto ist.

### Empfehlung

**(b) jetzt, (a) als das Ziel, (c) danach.**

- **(b) jetzt**, weil es ohne eine Zeile in `shared/` geht, weil es die fehlende Oberfläche
  „eigenes Passwort ändern" mitbringt, die ohnehin fehlt, und weil es den Grundfall sofort löst.
  Der Preis wird **im Formular benannt**, nicht verschwiegen: „Bis <Name> das Passwort geändert
  hat, kennst du seinen Schlüssel."
- **(a) als das Ziel**, weil nur dort das Passwort nie durch einen fremden Browser geht, und weil
  NGC dasselbe Muster für Server schon fährt. Es ist auch der Ablauf, der (b) ablöst, ohne ihn
  umzubauen: dieselbe Fassung, derselbe Unterzeichner, nur der Schlüssel kommt von weiter weg.
- **(c) danach**, weil es die Entscheidung über `rp` erzwingt, die größer ist als diese Frage.

---

## 3. Anfrage an den Koordinator: was `shared/` dafür bräuchte

`shared/` ist eingefroren. Dieser Abschnitt baut nichts, er fragt.

### 3.1 Für den Grundfall (b): **nichts.** Keine Zeile.

Das ist das Ergebnis des Messens, und es ist die wichtigste Zeile dieses Papiers. Gebraucht werden:

| Was | Gibt es | Beleg |
|---|---|---|
| Aktion, um eine neue Fassung einzuliefern | `ngc.users/api/trust` | `shared/core/core.go:142` |
| Obergrenze dafür | `core.MaxTrustIn`, 6 MiB | `shared/core/core.go:74` |
| Ein Admin darf Benutzer hinzufügen | `trust.Advance` | `shared/trust/trust.go:303` |
| Form eines Benutzers und seines Schlüssels | `trust.User`, `proof.UserKey` | `shared/trust/trust.go:117`, `shared/proof/proof.go:123` |
| Prüfung der ganzen Liste | `trust.List.Validate` | `shared/trust/trust.go:484` |
| Der Benutzer tauscht später seinen Schlüssel selbst | `trust.onlyOwnKeys` | `shared/trust/trust.go:327` |
| Der Datenbankteil muss nichts tun | Zeile entsteht bei der ersten Anmeldung | `apps/server-api/classes/login/finish.go:285` |

Der Antrag an den Koordinator für (b) lautet deshalb nur: **bestätigen, dass dieser Weg der
gewollte ist** — dass eine Shell eine Fassung mit einem neuen Benutzer bauen darf, ohne dass
`shared/core` eine eigene Aktion dafür bekommt. Wenn ja, ist das eine Aufgabe für die Sitzung Web
allein, und kein Teil wartet auf einen anderen.

### 3.2 Für den Einladungsablauf (a): drei Dinge, Zeile für Zeile

Hier **wäre** eine Vertragsänderung nötig, und zwar diese:

**1. Zwei Aktionsnamen, als Konstanten neben `ActSetRights`** in
`shared/core/core.go`, im Block ab Zeile 44 (die Aktionen von `ngc.users`), alphabetisch bzw. in
der Reihenfolge des Ablaufs eingeordnet:

```go
ActInviteCode = "invite-code"   // neben ActPairCode (:48) und ActJoinCode (:49)
ActPendingUser = "pending-user" // neben ActPending (:50) und ActPendingAPI (:51)
```

*Begründung je Zeile:* `invite-code` steht neben `pair-code` und `join-code`, weil es dasselbe
ist — ein Einmal-Geheimnis, das die API ausgibt, einmal gilt und nie ins Log kommt. Die Regel
dafür ist schon geschrieben und wird für die neue Aktion unverändert gelten
(`apps/server-api/classes/coreapp/users.go:7`–`:18`: „nothing here ever hands a code, or anything
derived from one, to the log, to a message or into a URL"). `pending-user` steht neben `pending`
und `pending-api`, weil es die dritte Warteliste derselben Art ist und die Shell sie genauso
behandelt: anzeigen, Fingerabdruck selbst rechnen, Admin vergleicht, Admin unterschreibt.

**2. Zwei Zeilen in der Aktionstabelle** `var actions` in `shared/core/core.go`, zwischen Zeile
147 (`ActNodes`) und 148 (`ActLog`) — also am Ende des `ngc.users`-Blocks, damit die Reihenfolge
der Tabelle der Reihenfolge der Konstanten folgt:

```go
{AppUsers, ServiceAPI, ActInviteCode,  true, false, true,  FlowCall, nil, 0},
{AppUsers, ServiceAPI, ActPendingUser, true, false, false, FlowCall, nil, 0},
```

*Begründung je Feld:* `Admin: true` wie jede Aktion von `ngc.users`. `Root: false`, weil beides auf
der API läuft und keinen Controller berührt. `Changes: true` bei `invite-code`, weil es ein
Geheimnis verbraucht und eine Zeile schreibt — genau wie `pair-code` und `join-code` (`:143`,
`:144`); `false` bei `pending-user`, weil es nur liest, wie `pending` und `pending-api` (`:145`,
`:146`). `FlowCall` bei beiden, weil die Parameter ins OPEN passen und das Ergebnis klein ist.
`Params: nil` bei beiden, aus demselben Grund, aus dem `pair-code` keine hat: es gibt nichts zu
wählen. `MaxIn: 0`, weil `call` keine Daten annimmt.

**3. Zwei Ergebnisformen in `shared/core/results.go`**, bei den anderen Ergebnissen von
`ngc.users`:

```go
// InviteCode is what ngc.users/invite-code answers: one use, valid for …
type InviteCode struct {
    Code   string `json:"code"`   // the text the admin hands over
    Expires int64 `json:"expires"`
}

// PendingUser is one row of ngc.users/pending-user: a key somebody offered
// under an invitation, not yet in any version.
type PendingUser struct {
    Name string         `json:"name"` // trust.ValidLoginName
    Key  proof.UserKey  `json:"key"`  // passes proof.UserKey.Validate
    Seen int64          `json:"seen"`
}
```

*Begründung:* `InviteCode` trägt nur den Code und seine Frist — nichts, was man aus ihm ableiten
könnte, und nichts, das zweimal gezeigt werden darf. `PendingUser` trägt genau die drei Dinge,
die der Admin für seine Entscheidung braucht, und den Schlüssel in der Form, die
`proof.UserKey.Validate` schon prüft; der Fingerabdruck, den der Admin vergleicht, steht
**nicht** darin, sondern wird von der Shell gerechnet — dieselbe Regel wie bei den wartenden
Servern (`apps/web/CLAUDE.md`, Abschnitt „Wartende Controller"). `ngc.users/pending-user` ist als
Ganzes eine Liste und gehört damit zu den Ergebnissen, die leer als `[]` geschrieben werden
müssen (`core.EncodeResult`, `apps/server-api/CLAUDE.md`).

**Und wohin der Einladungscode gehört: nach `shared/session`, nicht nach `shared/tunnel`.**

*Begründung:* `shared/tunnel` ist der Vertrag **zwischen Servern** — `PairCode` und `JoinCode`
tragen beide eine Tunnel-Adresse, einen Noise-Schlüssel, einen TLS-Fingerabdruck und einen Anker
der Vertrauensliste (`shared/tunnel/tunnel.go:82`–`:88`, `:103`–`:109`), weil der Empfänger eine
Maschine ist, die eine Verbindung aufbauen und pinnen muss. Ein Mensch mit einem Browser braucht
nichts davon: seine Seite kommt vom Webserver, und ihre Adresse steht in `web.json`. Was er
braucht, ist ein Geheimnis und eine Form, es zu beweisen — und genau diese Art Ding steht schon
in `shared/session`: `ValidSetupCode`, `NormalizeSetupCode`, `SetupMAC`, `OfferMAC`
(`shared/session/session.go:972`, `:984`, `:1009`, `:1013`). Der Einladungscode ist die Ersteinrichtung für
den zweiten Menschen; er hat dasselbe Alphabet, dieselbe Länge, dieselbe Regel „nie ins Log",
dieselbe Prüfung per MAC über genau die Bytes, die unterschrieben werden. Ihn nach `tunnel` zu
legen hieße, ihm Felder zu geben, die er nicht hat, oder `tunnel` einen Code ohne Tunnel.

### 3.3 Was dieses Papier ausdrücklich **nicht** beantragt

- **Kein Feld „Schlüssel ist vorläufig"** in `trust.User` oder `proof.UserKey`. Es wäre das
  einzige Mittel, aus „muss es ändern" einen Zwang zu machen — und es wäre ein Feld in dem
  Dokument, das die ganze Anlage trägt, für eine Bequemlichkeit. Wer (b) nicht tragen will,
  nimmt (a).
- **Keine Aktion „Benutzer löschen".** Sie braucht keine: ein Admin baut eine Fassung ohne den
  Benutzer, und `Validate` passt auf, dass nicht der letzte Admin verschwindet
  (`shared/trust/trust.go:616`). Dass die Datenbankzeile dann verwaist, ist derselbe Fall wie
  eine Zeile, die es noch nicht gibt (`coreapp/users.go:81`), und sollte vor dem Bauen gemessen
  werden, nicht hier behauptet.
- **Keine Änderung an der Ersteinrichtung.** Sie *könnte* zwei Admins anlegen (Abschnitt 1.1),
  aber sie läuft genau einmal, und zwei Menschen am selben Browser in derselben Minute sind kein
  Ablauf, auf den man eine Anlage stellt.

---

## 4. Wenn es gebaut wird: wie man es misst

Nichts davon ist gebaut, und dieses Papier baut es nicht. Was es messen müsste, nach
`Z:\GitHub\claude-md\messen.md` — zu jeder Zusicherung ein sterbender Mutant **und** eine lebende
Kontrollmutation:

- **„Das Passwort des neuen Benutzers verlässt seinen Browser nie."** Mitschnitt aller Anfragen,
  wie der Test, der das für die Anmeldung schon zeigt (`apps/web/CLAUDE.md`, Abschnitt Tests).
  Sterbender Mutant: das Passwort statt des Schlüssels senden. Kontrollmutation: den
  **öffentlichen** Schlüssel senden — muss weiter grün sein, sonst prüft der Test nur „es wird
  etwas gesendet".
- **„Eine Fassung mit einem neuen Benutzer, von einem Nicht-Admin unterschrieben, wird
  abgelehnt."** Sterbender Mutant: in `onlyOwnKeys` das `return false` bei fehlendem Benutzer
  zu `true` machen. Kontrollmutation: derselbe Nicht-Admin tauscht nur **seinen** Schlüssel —
  muss durchgehen.
- **„Der Einladungscode kommt nie ins Log."** Derselbe Fall, der das für `pair-code` und
  `join-code` schon zeigt (`TestAOneTimeCodeReachesNoLog`,
  `apps/server-api/classes/coreapp/users.go:19`–`:20`), um die neue Aktion erweitert — mit der
  Kontrolle, dass der Code wirklich da war.
- **Fuzz** auf jeden neuen Parser (die Antwort des Eingeladenen), mit `-fuzzminimizetime`, falls
  die Saaten über etwa ein Kilobyte gehen (`CLAUDE.md`, Abschnitt Tests).
- Und die Regel, die über allem steht: **gebaut ist nicht gelaufen.** Der Nachweis ist ein
  Durchlauf auf der VM, in dem sich ein zweiter Mensch anmeldet — nicht ein grüner Bau.
