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
| `ngc-db` | DB 1 bis 3 | MariaDB mit Galera und TLS einrichten, Verschlüsselung einschalten, Konten `ngc` (lesen und schreiben) und `ngc_migrate` (Schema) anlegen |
| `ngc-api` | API 1 und 2 | `ngc-api` und `ngc-store` nach `/usr/lib/ngc/`, `api.json` schreiben, Schlüssel erzeugen oder mit `join_code` von einer laufenden API übernehmen, `ngc-api.service` starten, Einmal-Code anzeigen |
| `ngc-web` | Web | Apache und PHP einrichten, `web/` nach `/var/www/ngc/`, Zertifikat (Let's Encrypt, selbst signiert oder Import), `web.json` schreiben, Lastverteilung vorbereiten |
| `ngc-controller` | alle | `ngc-controller` nach `/usr/lib/ngc/`, Schlüssel erzeugen, mit dem Kopplungscode koppeln, `controller.json` schreiben (mit `roles`, bei Rolle `db` auch `migrate`), `ngc-controller.service` starten, Podman installieren, wenn der Server Dienste trägt |
| `ngc-all` | Pi | alles auf einer Maschine mit SQLite statt MariaDB, koppelt den Controller selbst |

Jedes Skript kann `install`, `configure` und `uninstall`. Ein zweiter Lauf von `install` ändert
nichts, was schon stimmt.

## answer.yaml

Damit die Skripte ohne Rückfragen laufen. Schlüssel je Skript:

- `ngc-db`: `nodes` (Liste der Knoten), `bootstrap` (erster Knoten: ja/nein), `password_ngc`,
  `password_migrate`
- `ngc-api`: `node` (z. B. `api-1`), `db_driver`, `db_nodes`, `db_name`, `db_user`, `db_password`,
  `store_url`, `join_code` (leer beim ersten API-Server, sonst der Beitrittscode einer laufenden API:
  `ngc-join:` und base64url des JSON, geprüft mit `tunnel.ParseJoinCode`)
- `ngc-web`: `hostname`, `cert_mode` (`letsencrypt`, `selfsigned`, `import`), `cert_file`,
  `key_file`, `apis` (Liste der API-Adressen)
- `ngc-controller`: `name` (Name des Servers nach `wire.ValidName`: Kleinbuchstaben, Ziffern und
  Bindestrich, beginnt mit einem Buchstaben, höchstens 32 Zeichen), `pair_code` (der Kopplungscode
  aus der Shell: `ngc-pair:` und base64url des JSON, geprüft mit `tunnel.ParsePairCode`), `roles`
  (`web`, `api`, `db`, `services`); mit Rolle `db` zusätzlich `migrate_nodes`, `migrate_name`,
  `migrate_user` (`ngc_migrate`) und `migrate_password` für `migrate` in `controller.json`
- `ngc-all`: `hostname`, `cert_mode`

## Regeln

- Programmdateien übernimmt ein Skript nur, wenn die Signatur des Release stimmt. Es lädt
  `release.json` (ein `sign.Envelope`) und die Dateien seines Teils und prüft mit `openssl` ab 3.0,
  `jq` und `sha256sum` aus den Paketquellen (Vorlage und Testdaten: `contracts/testdata/release/`,
  Skript `verify.sh`):
  1. `payload` und `sig` aus `release.json` holen und von base64url in Bytes umwandeln
     (`tr -- '-_' '+/'`, mit `=` auf ein Vielfaches von 4 auffüllen, `base64 -d`).
  2. Die Nachricht ist genau `ngc/release/v1`, ein Null-Byte und die Nutzlast:
     `{ printf 'ngc/release/v1\0'; cat payload.bin; } > message.bin`.
  3. `openssl pkeyutl -verify -pubin -inkey release-key.pem -rawin -in message.bin -sigfile sig.bin`
     muss gelingen. `release-key.pem` ist der Release-Schlüssel als PEM, fest im Skript.
  4. Für jede Datei, die das Skript übernimmt, ihren Eintrag in `files` der Nutzlast nehmen (`part`,
     `arch`): `sha256` von base64url in Hex umwandeln (`od -An -tx1 | tr -d ' \n'`), mit
     `sha256sum` und der Größe (`size`) vergleichen. Stimmt etwas nicht, bricht das Skript ab.
  5. Nie ein älteres Release: `seq` der Nutzlast (`jq -r .seq payload.bin`) mit der Nummer in
     `/var/lib/ngc/release.seq` vergleichen (fehlt die Datei, ist noch nichts installiert). Ist sie
     kleiner, bricht das Skript ab; dieselbe Nummer darf zur Reparatur noch einmal installiert
     werden. Nach einer erfolgreichen Installation schreibt das Skript die Nummer dorthin. Go prüft
     genauso (`store.VerifyRelease` mit dieser Nummer als `lastSeq`, sonst `store.ErrRollback`).
- Release-Dateien benennen Softwareteile: `ngc-web` (`all`), `ngc-api` und `ngc-controller` (`amd64`,
  `arm64`). Die Migrationen stecken in `ngc-api`.
- Rollen eines Servers (`roles`): `web`, `api`, `db`, `services`. Welche Softwareteile dazu gehören,
  sagt `tunnel.Components`: immer `ngc-controller`, dazu `ngc-web` bzw. `ngc-api` je nach Rolle.
- Mit Rolle `db` kann der Controller beim Update die Migrationen einspielen. Genau einen dieser
  Controller bestimmt die API, die das Update steuert (sie hält `dbapi.LockUpdate`): unter den
  verbundenen Controllern mit Rolle `db` den mit der kleinsten Node-ID, scheitert er, den nächsten.
  Dafür steht der Zugang des Kontos `ngc_migrate` (auf dem Pi: SQLite-Datei und Schlüssel) in
  `controller.json` unter `migrate`. Andere Server bekommen diesen Eintrag nicht.
- Konfigurationsdateien sind striktes JSON (`contracts/README.md`, „Striktes JSON“): nie `null`,
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
  (`session.ValidSetupCode`). `ngc-controller` zeigt seinen Fingerabdruck (`tunnel.Fingerprint`
  seines Schlüssels) und den Anker aus dem Kopplungscode (Nummer und `tunnel.Fingerprint` des
  Hashes); der Admin vergleicht beides mit der Anzeige der Shell.
- Nach der Installation spielt der Controller die Updates ein (4.8); ulh bleibt der Notweg.
