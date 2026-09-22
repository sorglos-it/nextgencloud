# Installation über ulh

NGC wird mit [ulh](https://github.com/sorglos-it/ulh) installiert, konfiguriert und deinstalliert.
Die Skripte liegen direkt im öffentlichen ulh-Repo: `scripts/<skript>.sh`, eingetragen in dessen
`config.yaml` unter der Kategorie „NextGenCloud". Aufbau nach ulhs Vorlage `scripts/_template.sh`
und ulhs `DOCS.md` (Abschnitt „Script Development"); `SCRIPTS.md` und `TREE.md` in ulh werden
mitgepflegt. Jedes Teil schreibt das Skript für seinen Server auf einem eigenen Branch im ulh-Repo
(`ngc-web`, `ngc-api`, `ngc-controller`, `ngc-db`) und liefert per Pull Request; der Koordinator
hält diese Konventionen fest und führt zusammen. Jede ulh-Installation holt beim Start `main`,
deshalb kommt nur Geprüftes dorthin.

## Skripte

| Skript | Server | Was es tut |
|---|---|---|
| `ngc-db` | DB 1 bis 3 | MariaDB mit Galera und TLS einrichten, Verschlüsselung einschalten, Konten `ngc` (lesen und schreiben) und `ngc_migrate` (Schema) anlegen; zeigt am Ende das Zertifikat der CA der Installation (PEM) |
| `ngc-api` | API 1 und 2 | `ngc-api` und `ngc-store` nach `/usr/lib/ngc/`, `api.json` schreiben, Schlüssel und TLS-Zertifikat erzeugen oder mit `join_code` von einer laufenden API übernehmen, `ngc-api.service` starten; zeigt am Ende den Einmal-Code (erster API-Server), den TLS-Fingerabdruck (`pin.sh`) und den Noise-Fingerabdruck (`tunnel.Fingerprint`), beim Beitritt `tunnel.JoinFingerprint` |
| `ngc-web` | Web | Apache und PHP einrichten, das Webinterface nach `/var/www/ngc/` (Webroot nur dessen `public/`), Zertifikat (Let's Encrypt, selbst signiert oder Import), `web.json` schreiben, Lastverteilung vorbereiten; prüft das Zertifikat jeder API gegen ihren Fingerabdruck und schreibt `config.APITrustFile` |
| `ngc-controller` | alle | `ngc-controller` nach `/usr/lib/ngc/`, Schlüssel erzeugen, mit dem Kopplungscode koppeln, `controller.json` schreiben (mit `roles`, bei Rolle `db` auch `migrate`), `ngc-controller.service` starten, Podman installieren, wenn der Server Dienste trägt |
| `ngc-all` | Pi | alles auf einer Maschine mit SQLite; `configure` koppelt den Controller nach der Ersteinrichtung |

Jedes Skript kann `install`, `configure` und `uninstall`. Ein zweiter Lauf von `install` ändert
nichts, was schon stimmt.

## answer.yaml

Damit die Skripte ohne Rückfragen laufen. Schlüssel je Skript:

- `ngc-db`: `nodes` (Liste der Knoten), `bootstrap` (erster Knoten: ja/nein), `password_ngc`,
  `password_migrate`. Der erste Knoten erzeugt die CA der Installation und zeigt ihr Zertifikat
  (PEM); wie die anderen Knoten ihre Zertifikate bekommen, legt der Plan der Datenbank fest. Der
  private Schlüssel der CA kommt nie in answer.yaml, in eine Konfigurationsdatei oder ins Repo.
- `ngc-api`: `node` (z. B. `api-1`; auch der Name in der Vertrauensliste und Teil des
  Fingerabdrucks beim Beitritt), `hostname` (Name, unter dem Controller und Apache den Server
  erreichen; daraus `url` = `wss://<hostname>:7443/tunnel` in `api.json` und das TLS-Zertifikat für
  diesen Namen), `db_driver`, `db_nodes`, `db_name`, `db_user`, `db_password`, `db_ca` (MariaDB:
  Zertifikat der CA von `ngc-db`), `store_url`, `join_code` (leer beim ersten API-Server, sonst der
  Beitrittscode einer laufenden API: `ngc-join:` und base64url des JSON, geprüft mit
  `tunnel.ParseJoinCode`)
- `ngc-web`: `hostname`, `cert_mode` (`letsencrypt`, `selfsigned`, `import`), `cert_file`,
  `key_file`, `apis` (Adressen `https://<hostname>:7443`), `api_pins` (je API der Fingerabdruck,
  den `ngc-api` zeigt, in derselben Reihenfolge)
- `ngc-controller`: `name` (Name des Servers nach `wire.ValidName`: Kleinbuchstaben, Ziffern und
  Bindestrich, beginnt mit einem Buchstaben, höchstens 32 Zeichen), `pair_code` (der Kopplungscode
  aus der Shell: `ngc-pair:` und base64url des JSON, geprüft mit `tunnel.ParsePairCode`), `roles`
  (`web`, `api`, `db`, `services`); mit Rolle `db` zusätzlich `migrate_nodes`, `migrate_name`,
  `migrate_user` (`ngc_migrate`), `migrate_password` und `migrate_ca` (Zertifikat der CA von
  `ngc-db`) für `migrate` in `controller.json`
- `ngc-all`: `hostname`, `cert_mode` für `install`; `pair_code` für `configure` nach der
  Ersteinrichtung

## Regeln

- Programmdateien übernimmt ein Skript nur, wenn die Signatur des Release stimmt. Die Vorlage mit
  Testdaten ist `shared/testdata/release/verify.sh`, aufgerufen je Teil als
  `verify.sh <Verzeichnis> <Teil> <Arch>` (etwa `ngc-api amd64` oder `ngc-web all`). Im
  Verzeichnis liegen `release.json` (ein `sign.Envelope`), `release-key.pem` (der Release-Schlüssel
  als PEM, fest im Skript) und die geladenen Dateien. Werkzeuge aus den Paketquellen: `sh`, `jq`,
  `openssl` ab 3.0, `base64`, `sha256sum`, `od`, `tr`, `cut`, `wc` und `mktemp`. Die Schritte:
  1. `payload` und `sig` aus `release.json` (je ein Text) streng von base64url in Bytes umwandeln:
     nur `A-Z a-z 0-9 - _`, und die Bytes ergeben wieder genau diesen Text.
  2. Die Nachricht ist genau `ngc/release/v1`, ein Null-Byte und die Nutzlast;
     `openssl pkeyutl -verify -pubin -inkey release-key.pem -rawin` muss sie mit `sig` bestätigen.
  3. `files` der Nutzlast ist eine Liste mit genau einem Eintrag für Teil und Arch; darin sind `url`
     und `sha256` Text und `size` eine Zahl.
  4. Der Dateiname ist das letzte Stück der `url` nach dem letzten `/` und passt auf
     `^[A-Za-z0-9][A-Za-z0-9._-]*$`. Nur diese Datei wird geprüft und danach installiert; andere
     Dateien im Verzeichnis und Dateien anderer Teile zählen nicht.
  5. `sha256` wie in 1 umwandeln, in Hex (`od -An -tx1 | tr -d ' \n'`, genau 64 Zeichen) mit
     `sha256sum` der Datei vergleichen, ebenso ihre Größe mit `size`.
  6. Nie ein älteres Release (das tut das ulh-Skript, nicht `verify.sh`): `seq` der Nutzlast mit der
     Nummer in `/var/lib/ngc/release.seq` vergleichen (fehlt die Datei, ist noch nichts
     installiert). Ist sie kleiner, bricht das Skript ab; dieselbe Nummer darf zur Reparatur noch
     einmal installiert werden. Nach einer erfolgreichen Installation schreibt das Skript die Nummer
     dorthin. Go prüft genauso (`store.VerifyRelease` mit dieser Nummer als `lastSeq`, sonst
     `store.ErrRollback`).

  `verify.sh` legt Arbeitsdateien in einem Verzeichnis von `mktemp -d` ab, das es am Ende löscht,
  und schreibt nichts in das Verzeichnis der Dateien. Geht etwas schief, meldet es
  `release wrong: <Grund>` und endet mit 1, bei falschem Aufruf mit 2; nur wenn alles stimmt, gibt es
  `release ok` aus und endet mit 0. `TestReleaseScript` in `shared/store` prüft das in 31 Fällen;
  auf den Linux-Servern muss er mit dem echten `jq` bestehen. `release.json` bleibt im Format
  `release/v1`; spätere Formate kommen in eigene Dateien (Regel „Signierte Dokumente, die bleiben“
  in `shared/README.md`).
- Release-Dateien benennen Softwareteile: `ngc-web` (`all`), `ngc-api` und `ngc-controller` (`amd64`,
  `arm64`). Die Migrationen stecken in `ngc-api`.
- Rollen eines Servers (`roles`): `web`, `api`, `db`, `services`. Welche Softwareteile dazu gehören,
  sagt `tunnel.Components`: immer `ngc-controller`, dazu `ngc-web` bzw. `ngc-api` je nach Rolle.
- Mit Rolle `db` kann der Controller beim Update die Migrationen einspielen. Genau einen dieser
  Controller bestimmt die API, die das Update steuert (sie hält `dbapi.LockUpdate`): unter den
  verbundenen Controllern mit Rolle `db` den mit der kleinsten Node-ID, scheitert er, den nächsten.
  Dafür steht der Zugang des Kontos `ngc_migrate` (auf dem Pi: SQLite-Datei und Schlüssel) in
  `controller.json` unter `migrate`. Andere Server bekommen diesen Eintrag nicht.
- Konfigurationsdateien sind striktes JSON (`shared/README.md`, „Striktes JSON“): nie `null`,
  ein leeres Feld fehlt ganz. Es gelten die Regeln von `config`: in `controller.json` ein bis vier
  verschiedene `roles`, `migrate` genau mit Rolle `db`, `webConfig` mit Rolle `web`; in `web.json`
  `rp.id` und `rp.origins` beide oder keins, `rp.id` ein Hostname in Kleinbuchstaben. Kein Passwort
  darf wörtlich `[secret]` lauten.
- Schlüssel erzeugt immer der Server selbst; nichts Privates geht über die Leitung, außer beim
  Beitritt weiterer API-Server (`join_code`), und dort verschlüsselt und erst nach der Unterschrift
  eines Admins.
- Konfigurationsdateien mit Schlüsseln gehören root und haben Modus 0600 (`config`).
- `uninstall` fragt, was mit den Daten passiert: löschen oder aufheben. Aufgehobene Daten bleiben
  unter `/var/lib/ngc/` liegen, damit eine Neuinstallation sie wiederfindet.
- Kopplungs- und Beitrittscodes zeigt die Shell erst, wenn sie zu ihrer neuesten geprüften Fassung
  der Vertrauensliste passen (`tunnel.PairCode.Check`, `tunnel.JoinCode.Check`).
- Am Ende zeigt `ngc-api` beziehungsweise `ngc-all` den Einmal-Code für das erste Admin-Konto an
  (`session.ValidSetupCode`); ihn liest nur root, und er kommt in kein Log. `ngc-controller`
  zeigt seinen Fingerabdruck (`tunnel.Fingerprint` seines Schlüssels) und den Anker aus dem
  Kopplungscode (Nummer und `tunnel.Fingerprint` des Hashes); der Admin vergleicht beides mit der
  Anzeige der Shell.
- **TLS intern, nie ungeprüft:** Kein Skript und kein Programm schaltet eine Zertifikatsprüfung
  ab. Den Fingerabdruck eines TLS-Zertifikats rechnet `shared/testdata/tls/pin.sh` (SHA-256 des
  öffentlichen Schlüssels, base64url; Testdaten `tls/api.pem`, `tunnel.json` `tlsPin`, geprüft von
  `TestTLSPinScript`). Kann `pin.sh` die Datei nicht lesen, gibt es keinen Fingerabdruck aus und
  endet mit 1, bei falschem Aufruf mit 2; die Skripte prüfen den Exit-Status und brechen sonst ab.
  `ngc-web` holt das Zertifikat jeder API
  (`openssl s_client -connect <host>:7443 -servername <host>`), vergleicht den Fingerabdruck mit
  `api_pins`, schreibt passende Zertifikate nach `/etc/ngc/tls/api-trust.pem`
  (`config.APITrustFile`) und `web.json` (`apis` mit `url` und `tls`) und richtet Apache mit
  `SSLProxyVerify require`, `SSLProxyCACertificateFile` = diese Datei, `SSLProxyCheckPeerName on`,
  `SSLProxyCheckPeerExpire on`, `SSLProxyProtocol TLSv1.3` ein. Ist der Controller des Webservers
  gekoppelt, übernimmt er das aus der Vertrauensliste. Den Beitritt und die Kopplung führen die
  Programme aus (`ngc-api`, `ngc-controller`); sie prüfen TLS mit dem Fingerabdruck aus dem Code.
- **`ngc-all`:** `install` richtet alles ein, startet die API und zeigt den Einmal-Code; `web.json`
  bekommt Adresse und Fingerabdruck der lokalen API ohne Rückfrage. Der Controller läuft erst nach
  `configure` mit `pair_code`, einem Kopplungscode aus der Shell nach der Ersteinrichtung (alle vier
  Rollen, `migrate` auf die SQLite-Datei). Bis dahin kennt nur der Browser der Ersteinrichtung den
  Anker.
- Nach der Installation spielt der Controller die Updates ein (4.8); ulh bleibt der Notweg.
