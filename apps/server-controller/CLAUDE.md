# Server-Controller

Dein Ordner: `apps/server-controller/`, dazu `scripts/ngc-controller.sh` im ulh-Repo
(`shared/ulh.md`). Verträge: V1, V2, V3, V4, V5, V6, V7.

Aufbau: `main.go` ist der Einstiegspunkt von `ngc-controller`, die Logik liegt in `classes/`
(Regeln: `CLAUDE.md` im Stamm, Abschnitt „Aufbau“).

## Aufgabe

Der Dienst auf jedem Server. Er redet nur mit der API und läuft als root, deshalb bleibt sein Code
so klein wie möglich.

- Tunnel zu jeder API (`tunnel`), Wiederaufbau mit wachsenden Pausen (`tunnel.ReconnectMin` bis
  `tunnel.ReconnectMax`). Kopplung mit Code: `tunnel.PairRequest` per POST an
  `https://<Host und Port aus url des Codes>` + `tunnel.PathPair`, TLS mit `tunnel.PinnedTLS` und
  dem Fingerabdruck des Codes (`tunnel.PairCode.TLS`). Den Tunnel baust du ebenso nur mit
  `tunnel.PinnedTLS` und dem Fingerabdruck dieser API auf (`controller.json`, später aus der
  Vertrauensliste). Den Anker nimmst du nur aus dem Kopplungscode (`tunnel.ParsePairCode`), den die
  Shell vorher gegen ihre Vertrauensliste geprüft hat (`tunnel.PairCode.Check`); ulh zeigt Nummer
  und Fingerabdruck des Ankers zum Vergleich.
- Steuerkanal nach `wire.ControlAllowed`: zuerst `hello` mit den Rollen des Servers und der
  Vertragsfassung (`tunnel.Hello`, `tunnel.Contract`), dann `status` (`tunnel.Status`, höchstens
  `tunnel.MaxStatusApps` Apps), `update` (`store.UpdateReport`) und `notify` (`core.Notice`). In
  `hello` nennt `node` den Knoten deines Noise-Schlüssels (leer, solange du nicht in der
  Vertrauensliste stehst). Die neuere Seite spricht die Fassung der älteren; API und Controller
  liegen höchstens eine Fassung auseinander.
- Vertrauensliste selbst prüfen (`trust`), Fassungen in SQLite; neue Fassungen kommen über den Kanal
  `ngc.users/controller/trust` (`session.ParseTrustChain`, `trust.Advance`). Du antwortest mit
  `core.TrustResult` oder schließt mit `E_VERIFY`.
- Kanäle: `tunnel.VerifyOpen` prüft das OPEN samt Client-Zertifikat. Der Benutzer eines Kanals ist
  nur `Opened.Session.Cert().User`, und `Opened.Session` beweist ihn nur zusammen mit dem
  Ende-zu-Ende-Handschlag oder einer Aufgabe: Die API kann jedes Zertifikat anhängen, das sie
  kennt. Auf einem Kanal ohne beides berechtigst und protokollierst du nie nach ihm; die Kanäle,
  die die API ohne Benutzer öffnet (`core.WithoutUser`), tragen gar keins. Rollen
  mit `passwordToo` hat die API bei der Anmeldung geprüft; du verlässt dich auf den Passkey-Beweis,
  den `session.VerifyClient` dafür verlangt. Kanäle ohne Benutzer nimmst du nur für
  `core.WithoutUser` an, `install` ohne Benutzer nur mit `core.Unattended`. Aufgaben prüfst du mit
  `task.Check` und `core.RuleFor` bzw. `store.RuleFor` (Root einer App: das Manifest verlangt
  `root`), Aufgaben-IDs merkst du dir (`dbapi.OnceTask`). Kanalnummern höchstens
  `wire.MaxTunnelChannels` je Tunnel, gezählt, bis sie wieder frei sind (CLOSE gesendet und
  empfangen); ein Frame darüber beendet den Tunnel. Was du auf einem Kanal zuerst schicken darfst,
  gibt `Opened.Window` frei (die Freigabe der API auf dem Tunnel), nicht `window` im OPEN des
  Browsers: Das gilt nur zwischen Browser und API. Was die API bei `transfer`
  (`ngc.store/controller/activate`, `ngc.store/controller/stage`) und bei
  `ngc.users/controller/trust` schickt, begrenzt `core.Action.MaxIn`; bei mehr schließt du mit
  `E_TOOLARGE`. Kopf und Datei von `transfer` kommen immer von der API, auch bei `activate`, das
  die Shell mit Aufgabe geöffnet hat.
- Ende zu Ende (`shared/README.md`, Abschnitt „Kanäle“): Prolog `noisep.E2EPrologue` mit deiner
  Node-ID, App, Dienst, Aktion und den Bytes aus `Opened.Open.Params` und
  `Opened.Open.TaskBytes()`; Noise-Nachricht 1 und 2 ohne Nutzlast. Den Dienst startest du erst,
  wenn die erste Nachricht der Shell entschlüsselt ist (höchstens `wire.E2EFirstTimeout` Sekunden
  warten). Ausgabe als `wire.E2EData`, Ergebnis als `wire.E2EEnd` mit `service.Result`; danach
  schickst du nichts mehr. Kommt EOF oder CLOSE vor dem Ende der Shell, ist die Eingabe
  abgeschnitten: Dienst beenden, Fehler melden. Bei Aktionen, die kein Datenstrom sind, höchstens
  `wire.MaxCallResult` Byte Ausgabe; wäre es mehr, beendest du mit `E_TOOLARGE` im Ende (die Shell
  setzt die Grenze als Empfängerin durch). Ebenso bleiben deine Ergebnisse der Kern-Aktionen
  darunter; dort prüft sie die API.
- Dienste in Podman-Containern starten (`service`): ein Container je App, eigener Systembenutzer,
  kein Netz (außer mit `network`), Grenzen für CPU und Speicher, verschlüsselter Datenordner als
  `/data`, Startaktion nach jedem Start, auch ohne API. Die Parameter sind mit Aufgabe die der
  Aufgabe, sonst die des OPEN (`service.Call`); auf Ende-zu-Ende-Kanälen bindet der Prolog das
  Feld `params` des OPEN oder, mit Aufgabe, deren Bytes. `NGC_PARAMS` nie ins Log.
- Container nur über eine eigene Schnittstelle ansprechen, damit LXC später danebenpassen kann.
- Apps aktivieren, deaktivieren, deinstallieren (`core`, `store`): Paket prüfen (`store.Walk`,
  höchstens `store.MaxDirs` Verzeichnisse), Kauf-Apps entschlüsseln, Image laden, Daten löschen
  oder aufheben. Aktivieren nur, wenn `grants` genau die `needs` des Manifests enthält, sonst
  `E_FORBIDDEN`. Über das unverschlüsselte Paket rechnest du `store.PackageUI`: Bei `activate` muss
  es zu `ui` aus `core.ActivateParams` passen, und bei Store-Apps muss es bei jedem Aktivieren,
  Installieren und Update, mit oder ohne Benutzer, zu `ui` des Katalogeintrags passen; sonst
  `E_VERIFY`.
- Die Lizenz einer Kauf-App kommt beim Aktivieren und bei Updates im Kopf der Übertragung
  (`core.Transfer`), eine erneuerte über `ngc.store/controller/license` (`in`, ohne Benutzer,
  Parameter `app`, Antwort `core.LicenseResult`). Du prüfst sie selbst mit
  `store.VerifyLicenseFor`: der Store aus `dbapi.App.Store`, wie ihn deine Vertrauensliste führt,
  deine Installation und die App. Ob die App laufen darf, beantwortet erst
  `store.License.State(now)`; die Prüffunktionen sehen den Ablauf nicht an.
  - Du behältst je App die neueste (`issued`) in `dbapi.BlobLicense`; eine gleich alte oder
    ältere ändert nichts, und deine Antwort nennt den Zustand der, die du behältst.
  - Nach Ablauf plus Übergangsfrist: Dienste der App stoppen, keine mehr starten (auch nicht nach
    einem Neustart), Kanäle zu ihr mit `E_LICENSE` ablehnen, `unlicensed` melden
    (`tunnel.StateUnlicensed`). Mit einer gültigen Lizenz startest du sie wieder; die Daten
    bleiben.
  - Aktivieren einer Kauf-App mit abgelaufener Lizenz: `E_LICENSE`. Eine App, die du nicht
    ausführst: `E_NOTFOUND`. Eine Lizenz, die nicht prüft: `E_VERIFY`.
- Updates und Aktivieren: Du installierst bei Store-Apps nur aus dem Katalog von
  `dbapi.App.Store` (sonst `E_VERIFY`; bei einer hochgeladenen App steht dort `core.LocalStore`,
  zu dem es keinen Katalog gibt) und nur, wenn `store.Granted` die neuen `needs` in
  `dbapi.App.Grants` findet (sonst `E_FORBIDDEN`; ohne Benutzer meldest du `needsgrants`,
  `store.UpdateNeedsGrants`). Die Rechte nimmst du dabei immer aus deiner eigenen Datenbank, wie
  sie beim Aktivieren hineingeschrieben wurden, nie aus einem Wert, den eine API schickt: Sonst
  könnte eine übernommene API einer App mehr erlauben. Danach läuft die App mit den Rechten auf
  die neuen `needs` zusammengestrichen.
  Diese beiden Regeln prüfst du auch ohne Benutzer selbst: `core.Unattended` sieht weder die
  Zeile der App noch den Katalog, sein Ja heißt nur Dauerfreigabe und neuere Version.
  - `activate` einer installierten App mit derselben oder einer neueren Version ersetzt sie samt
    Rechten und behält die Daten; eine ältere Version gibt `E_FORBIDDEN`.
  - Eine App mit Diensten nur auf einem Server mit Rolle `services`, sonst `E_FORBIDDEN`.
  - `minNgc` vergleichst du mit `store.SupportsMinNGC` gegen deine eigene NGC-Version; ein
    Entwicklungsstand (keine Release-Version) gilt als älter als jedes `minNgc`.
- Updates einspielen und bei Fehlschlag zurücknehmen; ohne Unterschrift nur mit Dauerfreigabe
  (`core.Unattended`) und nie eine ältere Version. Ohne Benutzer zählt dabei nur, was Release- oder
  Store-Schlüssel signiert haben: Version und SHA-256 nimmst du aus dem geprüften Release oder
  Katalogeintrag der bereitgelegten Datei (dessen `seq` du gegen den neuesten gesehenen geprüft
  hast) und lehnst Parameter ab, die davon abweichen; die Parameter eines solchen Kanals sind nicht
  unterschrieben. Releases prüfst du nur mit deinem eigenen Release-Schlüssel
  (`config.Controller.ReleaseKey`); eine Dauerfreigabe für einen NGC-Teil zählt nur mit diesem
  Schlüssel.
- Mit Rolle `db` spielst du die Migrationen aus der Datei `ngc-api` ein, mit dem Zugang aus
  `controller.json` (`migrate`), wenn die API, die das Update steuert, dich dafür bestimmt: Sie
  nimmt unter den verbundenen Controllern mit Rolle `db` den mit der kleinsten Node-ID, scheitert
  er, den nächsten. MariaDB erreichst du dabei nur über `config.DB.TLSConfig` (CA aus
  `migrate.ca`).
- `controller.json` liest du mit `config.LoadController` und schreibst es nur mit
  `config.SaveController`. Schlüssel und Passwörter darin sind `config.Secret` bzw.
  `config.SecretText`; den Wert geben nur `Bytes()` bzw. `Text()`, nie ins Log.
- Mit Rolle `web`: Adressen und Fingerabdrücke nimmst du aus der Vertrauensliste, die Adresse
  hinter Apache leitet `trust.WebURL` aus `url` des Knotens ab; du holst jedes Zertifikat mit
  `tunnel.PinnedTLS` und schreibst `config.APITrustFile` und `web.json` (`config.SaveWeb`; `apis`
  mit `url` = `https://<Host>:<Port>` und `tls`), daraus `config.BalancerFile`, genau in der Form
  der Vorlage in `shared/ulh.md` (Regel „Lastverteilung in Apache“). Erst `apachectl configtest`,
  dann Apache neu laden – sobald sich die API-Server der Vertrauensliste ändern. Außerdem
  vergleichst du bei jedem Aufbau eines Tunnels und einmal am Tag das Zertifikat, das du über
  `tunnel.PinnedTLS` bekommen hast, mit dem in `config.APITrustFile`; weicht es ab (derselbe
  Schlüssel, das beweist der Fingerabdruck), schreibst du die Datei neu und lädst Apache neu.
  Vertrauen trägt nur der Fingerabdruck, nicht das Zertifikat; so sperrt weder ein erneuertes
  Zertifikat noch ein Datum Apache aus. Den Anker schreibst du mit `config.SaveAnchor`. Vor
  deiner ersten Fassung der Vertrauensliste hat ngc-web `config.APITrustFile` aus `api_pins`
  geschrieben.
- Der Dienst „virtuelles Terminal" als Beispiel-Dienst.

## Terminal-App bauen

Das Abbild der Terminal-App entsteht auf einem **Baurechner**, nie auf dem Zielserver: der
Zielserver hat keinen Bauer und soll keinen brauchen. **Zwei Zeilen je Architektur**, aus dem
Stamm des Repos:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-X main.Version=$(cat apps/server-controller/VERSION)" -o build/ngc-controller ./apps/server-controller
go run ./apps/server-controller image -arch amd64 -out services/amd64.tar
```

Dasselbe mit `arm64` an beiden Stellen und `services/arm64.tar` als Ziel. Das Paket baut danach
die Sitzung API mit `ngc-store pack` aus `templates/terminal/manifest.json`, der `ui/` der
Sitzung Webinterface und den beiden Tars.

Bis zum 12.10.2026 standen hier **drei** Zeilen: die beiden `podman`-Aufrufe von Hand. Seit es
das Kommando `image` gibt, ruft es sie selbst – `podman build --arch … --file … --tag …
--pull=always` und danach `podman save --format docker-archive --output …`
(`classes/runtime/build.go`, `buildArgs` und `saveArgs`). Und es nimmt dabei zwei Fehler ab, die
von Hand niemand bemerkt: den **Namen** des Abbilds liest es aus `manifest.json` (`ngc/terminal:`
plus `version`), also kann der Tag nie von der Fassung im Manifest abweichen, und die
**Architektur** prüft es am ELF-Kopf des Programms in `build/` – wer `GOARCH` in der ersten
Zeile vergisst, bekommt sonst ein Abbild, das baut, lädt, einbaut und erst auf dem Server an
„exec format error" stirbt.

**Zwei Fallen der zweiten Zeile, beide gemessen am 12.10.2026:**
- `GOOS`/`GOARCH` gelten **nur** für die erste Zeile. Mit `GOOS=linux` davor übersetzt `go run`
  sich selbst für Linux und kann es dann auf Windows nicht starten
  (`exec: "…\exe\server-controller": executable file not found`). Darum `go run` und nicht
  `build/ngc-controller`: das Programm in `build/` ist das **Ziel**-Programm, auf einem
  amd64-Baurechner zufällig lauffähig und für `arm64` nie.
- `-out` darf ein Unterverzeichnis nennen – `services/amd64.tar` geht, gemessen am Lauf oben.
  Was `validOut` (`classes/node/main.go:384`) verbietet, ist allein `..`; hier stand zuerst, es
  nehme überhaupt kein Verzeichnis, und das war am Code falsch. Das Verzeichnis muss es geben:
  geschrieben wird es von `podman save`, und was der mit einem fehlenden Verzeichnis tut, ist
  hier **nicht gemessen**.

**Welche Stufe erreicht ist** (geschrieben · übersetzt · im Abbild enthalten · ausgeführt), am
12.10.2026 im Arbeitsbaum auf `C:` gemessen:
- Zeile 1: **ausgeführt**, für beide Architekturen. `file build/ngc-controller` sagt
  „ELF 64-bit … x86-64, statically linked" bzw. „ARM aarch64".
- Zeile 2: **ausgeführt bis zum Bauer**, nicht weiter. Der Aufruf antwortet
  „podman ist auf diesem Rechner nicht zu finden …" mit Code 1 – und weil diese Meldung
  **nach** `imageSpec` kommt, ist damit gemessen, dass Manifest und Pfade dieser Zeile stimmen
  (`msgNoApp` würde vorher kommen). Gesucht wurde podman in `PATH`, in
  `C:\Program Files\RedHat\Podman`, `C:\Program Files\Docker`,
  `C:\ProgramData\chocolatey\bin`, `C:\Program Files\Rancher Desktop`,
  `%LOCALAPPDATA%\Programs\podman` und im `PATH` der WSL – nirgends.
- **Es fehlen** „im Abbild enthalten" und „ausgeführt" für das Abbild selbst: kein Container ist
  je gebaut, gespeichert, geladen oder gestartet worden.

Der Dienst selbst ist `ngc-controller terminal`; er läuft **im Container**, liest die Rahmen des
Kanals von stdin und schreibt die rohe Ausgabe der Shell nach stdout. `ngc-controller hold` hält
den Container zwischen zwei Kanälen am Leben, damit das Abbild kein eigenes Programm braucht.

## Tests

- Gegen die Vektoren in `shared/testdata/`, den Beispiel-Dienst
  `shared/testdata/service/echo.sh` und `dbapi/dbtest` für die lokale SQLite.
- Neustart: Container und Startaktionen kommen ohne API wieder hoch.
- Angriffe: gefälschte Aufgabe, ausgetauschte Aufgabe, fehlende Bestätigung, fremder
  Controller-Schlüssel, Ausbruch aus dem Container, wieder eingespieltes OPEN, geänderte Parameter,
  abgeschnittene Eingabe, eine untergeschobene Oberfläche (Paket, dessen UI-Prüfsumme nicht passt).

## Fertig, wenn

Eine App lässt sich mit Unterschrift aktivieren, das Terminal liefert Ausgaben, der Server läuft
nach einem Neustart ohne API weiter, ein kaputtes Update wird selbst zurückgenommen.
