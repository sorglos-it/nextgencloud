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
| `ngc-api` | API 1 und 2 | `ngc-api` und `ngc-store` nach `/usr/lib/ngc/`, `api.json` schreiben, Schlüssel erzeugen oder von einer laufenden API übernehmen, `ngc-api.service` starten, Einmal-Code anzeigen |
| `ngc-web` | Web | Apache und PHP einrichten, `web/` nach `/var/www/ngc/`, Zertifikat (Let's Encrypt, selbst signiert oder Import), `web.json` schreiben, Lastverteilung vorbereiten |
| `ngc-controller` | alle | `ngc-controller` nach `/usr/lib/ngc/`, Schlüssel erzeugen, mit dem Kopplungscode koppeln, `controller.json` schreiben, `ngc-controller.service` starten, Podman installieren, wenn der Server Dienste trägt |
| `ngc-all` | Pi | alles auf einer Maschine mit SQLite statt MariaDB, koppelt den Controller selbst |

Jedes Skript kann `install`, `configure` und `uninstall`. Ein zweiter Lauf von `install` ändert
nichts, was schon stimmt.

## answer.yaml

Damit die Skripte ohne Rückfragen laufen. Schlüssel je Skript:

- `ngc-db`: `nodes` (Liste der Knoten), `bootstrap` (erster Knoten: ja/nein), `password_ngc`,
  `password_migrate`
- `ngc-api`: `node` (z. B. `api-1`), `db_driver`, `db_nodes`, `db_name`, `db_user`, `db_password`,
  `store_url`, `join_code` (leer beim ersten API-Server, sonst der Code einer laufenden API)
- `ngc-web`: `hostname`, `cert_mode` (`letsencrypt`, `selfsigned`, `import`), `cert_file`,
  `key_file`, `apis` (Liste der API-Adressen)
- `ngc-controller`: `name`, `pair_code`, `parts` (`web`, `api`, `db`, `services`)
- `ngc-all`: `hostname`, `cert_mode`, `admin_name`

## Regeln

- Programmdateien lädt ein Skript nur herunter, wenn die Signatur mit dem Release-Schlüssel stimmt
  (`store.VerifyRelease`, Prüfsumme je Datei).
- Schlüssel erzeugt immer der Server selbst; nichts Privates geht über die Leitung, außer beim
  Koppeln weiterer API-Server, und dort verschlüsselt und nur mit Unterschrift eines Admins.
- Konfigurationsdateien mit Schlüsseln gehören root und haben Modus 0600 (`config`).
- `uninstall` fragt, was mit den Daten passiert: löschen oder aufheben. Aufgehobene Daten bleiben
  unter `/var/lib/ngc/` liegen, damit eine Neuinstallation sie wiederfindet.
- Am Ende zeigt `ngc-api` beziehungsweise `ngc-all` den Einmal-Code für das erste Admin-Konto an
  (`session.ValidSetupCode`), `ngc-controller` seinen Fingerabdruck (`tunnel.Fingerprint`).
- Nach der Installation spielt der Controller die Updates ein (4.8); ulh bleibt der Notweg.
