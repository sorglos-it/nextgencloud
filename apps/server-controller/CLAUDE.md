# Server-Controller

Dein Ordner: `apps/server-controller/`, dazu `scripts/ngc-controller.sh` im ulh-Repo
(`shared/ulh.md`). Verträge: V1, V2, V3, V4, V5, V6, V7.

Aufbau: `main.go` ist der Einstiegspunkt von `ngc-controller`, die Logik liegt in `classes/`
(Regeln: `CLAUDE.md` im Stamm, Abschnitt „Aufbau“).

## Aufgabe

Der Dienst auf jedem Server. Er redet nur mit der API und läuft als root, deshalb bleibt sein Code
so klein wie möglich.

- Tunnel zu jeder API (`tunnel`), Kopplung mit Code (`tunnel.PairRequest` an `tunnel.PathPair`),
  Wiederaufbau mit wachsenden Pausen (`tunnel.ReconnectMin` bis `tunnel.ReconnectMax`). Den Anker
  nimmst du nur aus dem Kopplungscode (`tunnel.ParsePairCode`), den die Shell vorher gegen ihre
  Vertrauensliste geprüft hat (`tunnel.PairCode.Check`); ulh zeigt Nummer und Fingerabdruck des
  Ankers zum Vergleich.
- Steuerkanal nach `wire.ControlAllowed`: zuerst `hello` mit den Rollen des Servers und der
  Vertragsfassung (`tunnel.Hello`, `tunnel.Contract`), dann `status` (`tunnel.Status`, höchstens
  `tunnel.MaxStatusApps` Apps), `update` (`store.UpdateReport`) und `notify` (`core.Notice`). Die
  neuere Seite spricht die Fassung der älteren; API und Controller liegen höchstens eine Fassung
  auseinander.
- Vertrauensliste selbst prüfen (`trust`), Fassungen in SQLite; neue Fassungen kommen über den Kanal
  `ngc.users/controller/trust` (`session.ParseTrustChain`, `trust.Advance`). Du antwortest mit
  `core.TrustResult` oder schließt mit `E_VERIFY`.
- Kanäle: `tunnel.VerifyOpen` prüft das OPEN samt Client-Zertifikat. Der Benutzer eines Kanals ist
  nur `Opened.Session.Cert().User`. Rollen mit `passwordToo` hat die API bei der Anmeldung geprüft;
  du verlässt dich auf den Passkey-Beweis, den `session.VerifyClient` dafür verlangt. Kanäle ohne
  Benutzer nimmst du nur für `core.WithoutUser` an, `install` ohne Benutzer nur mit
  `core.Unattended`. Aufgaben prüfst du mit `task.Check` und `core.RuleFor` bzw. `store.RuleFor`
  (Root einer App: das Manifest verlangt `root`), Aufgaben-IDs merkst du dir (`dbapi.OnceTask`).
  Kanalnummern höchstens `wire.MaxTunnelChannels` je Tunnel, gezählt, bis sie wieder frei sind
  (CLOSE gesendet und empfangen); ein Frame darüber beendet den Tunnel. Was du auf einem Kanal
  zuerst schicken darfst, gibt `Opened.Window` frei (die Freigabe der API auf dem Tunnel), nicht
  `window` im OPEN des Browsers: Das gilt nur zwischen Browser und API. Was die API bei `transfer`
  (`ngc.store/controller/activate`, `ngc.store/controller/stage`) und bei
  `ngc.users/controller/trust` schickt, begrenzt `core.Action.MaxIn`; bei mehr schließt du mit
  `E_TOOLARGE`.
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
  `E_FORBIDDEN`.
- Updates einspielen und bei Fehlschlag zurücknehmen; ohne Unterschrift nur mit Dauerfreigabe
  (`core.Unattended`) und nie eine ältere Version. Ohne Benutzer zählt dabei nur, was Release- oder
  Store-Schlüssel signiert haben: Version und SHA-256 nimmst du aus dem geprüften Release oder
  Katalogeintrag der bereitgelegten Datei (dessen `seq` du gegen den neuesten gesehenen geprüft
  hast) und lehnst Parameter ab, die davon abweichen; die Parameter eines solchen Kanals sind nicht
  unterschrieben.
- Mit Rolle `db` spielst du die Migrationen aus der Datei `ngc-api` ein, mit dem Zugang aus
  `controller.json` (`migrate`), wenn die API, die das Update steuert, dich dafür bestimmt: Sie
  nimmt unter den verbundenen Controllern mit Rolle `db` den mit der kleinsten Node-ID, scheitert
  er, den nächsten.
- Konfiguration über `config.LoadController`, schreiben nur mit `config.SaveController`. Schlüssel
  und Passwörter darin sind `config.Secret` bzw. `config.SecretText`; den Wert geben nur `Bytes()`
  bzw. `Text()`, nie ins Log.
- Auf dem Webserver: Lastverteilung in Apache aus `web.json` einrichten und den Anker schreiben
  (`config.SaveAnchor`).
- Der Dienst „virtuelles Terminal" als Beispiel-Dienst.

## Tests

- Gegen die Vektoren in `shared/testdata/`, den Beispiel-Dienst
  `shared/testdata/service/echo.sh` und `dbapi/dbtest` für die lokale SQLite.
- Neustart: Container und Startaktionen kommen ohne API wieder hoch.
- Angriffe: gefälschte Aufgabe, ausgetauschte Aufgabe, fehlende Bestätigung, fremder
  Controller-Schlüssel, Ausbruch aus dem Container, wieder eingespieltes OPEN, geänderte Parameter,
  abgeschnittene Eingabe.

## Fertig, wenn

Eine App lässt sich mit Unterschrift aktivieren, das Terminal liefert Ausgaben, der Server läuft
nach einem Neustart ohne API weiter, ein kaputtes Update wird selbst zurückgenommen.
