# Server-Controller

Deine Ordner: `controller/`, dazu `scripts/ngc-controller.sh` im ulh-Repo (`contracts/ulh.md`). Verträge: V2, V3, V4, V5, V6, V7.

## Aufgabe

Der Dienst auf jedem Server. Er redet nur mit der API und läuft als root, deshalb bleibt sein Code
so klein wie möglich.

- Tunnel zu jeder API (`tunnel`), Kopplung mit Code, Wiederaufbau mit wachsenden Pausen.
- Vertrauensliste selbst prüfen (`trust`), Anker aus dem Kopplungscode, Fassungen in SQLite.
- Kanäle: Client-Zertifikat prüfen (`session.VerifyClient` liefert ein `session.Verified`), Ende zu
  Ende beantworten (`noisep.E2EPrologue`), Aufgaben und erneute Bestätigung mit diesem
  `session.Verified` prüfen (`task.Check`), Wiedereinspielen verhindern (`dbapi` `OnceTask`).
  `wire.MaxChannels` ist nur eine Konstante; die Grenze für offene Kanäle je Sitzung oder Link muss
  der Controller selbst durchsetzen.
- Dienste in Podman-Containern starten (`service`): ein Container je App, eigener Systembenutzer,
  kein Netz, Grenzen für CPU und Speicher, verschlüsselter Datenordner als `/data`, Startaktion nach
  jedem Start, auch ohne API.
- Container nur über eine eigene Schnittstelle ansprechen, damit LXC später danebenpassen kann.
- Apps aktivieren, deaktivieren, deinstallieren (`core`, `store`): Paket prüfen, Kauf-Apps
  entschlüsseln, Image laden, Daten löschen oder aufheben.
- Updates einspielen und bei Fehlschlag zurücknehmen; ohne Unterschrift nur mit Dauerfreigabe
  (`store.Approved`) und nie eine ältere Version.
- Auf dem Webserver: Lastverteilung in Apache aus `web.json` einrichten und den Anker schreiben.
- Der Dienst „virtuelles Terminal" als Beispiel-Dienst.

## Tests

- Gegen die Vektoren in `contracts/testdata/`, den Beispiel-Dienst `service/echo.sh` und
  `dbapi/dbtest` für die lokale SQLite.
- Neustart: Container und Startaktionen kommen ohne API wieder hoch.
- Angriffe: gefälschte Aufgabe, fehlende Bestätigung, fremder Controller-Schlüssel, Ausbruch aus
  dem Container.

## Fertig, wenn

Eine App lässt sich mit Unterschrift aktivieren, das Terminal liefert Ausgaben, der Server läuft
nach einem Neustart ohne API weiter, ein kaputtes Update wird selbst zurückgenommen.
