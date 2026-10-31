# Der Laden der ersten Einzel-Anlage — als Beleg, nicht als Werkzeug

Diese zwei Dateien kommen von der **ersten wirklich laufenden NGC-Anlage** (`ssh ngc`,
10.2.3.208, Name `ngc.test`). Diese Maschine ist am 24.10.2026 um 12:37 vom Proxmox-Wirt aus
angehalten und danach samt Platte und Schnappschuss gelöscht worden (Punkte 75 und 83).

**Darum liegen sie hier:** sie waren die einzigen beiden Dateien aus der Bergung, die es
nirgends sonst gab. Alles andere ist entweder im Repo (die drei Prüfladen- und die vier
Messskripte unter `tools/`), aus der Geschichte wiederherstellbar (die zwei
Oberflächendateien von vor Punkt 73 liegen als Blobs im Repo, Commit `1c8b695`; das
Terminal-Manifest ist bytegleich mit
`apps/server-controller/templates/terminal/manifest.json`) oder aus dem Repo neu baubar (die
Pakete, über `ngc-store pack`).

| Datei | was es ist |
|---|---|
| `catalog.json` | der Katalog dieses Ladens, **Folge 1**, mit **einer** App (`org.sorglos.terminal` 1.0.0), unterschrieben mit der Kennung `WM0yymuJtd9UxaUONGIXow` |
| `store-key.pem` | der **öffentliche** Teil des Ladenschlüssels, so wie `ngc-store catalog` ihn neben den Katalog legt |

## Was daran der Befund ist

**Der geheime Teil dieses Ladenschlüssels ist weg** und war schon vor dem Abschalten nicht zu
finden (`docs/app-weg.md`, Befund 2). Diesem Laden kann darum **nie wieder jemand eine App
hinzufügen**: jeder neue Katalog bräuchte Folge 2 und dieselbe Unterschrift. Das merkt man erst
bei der zweiten App — und genau deshalb ging die zweite App über „Hochladen" und nicht über den
Laden.

Das ist der einzige erhaltene Katalog, den eine NGC-API wirklich geholt und dessen Unterschrift
sie wirklich geprüft hat. Als Beleg dafür, wie ein ausgelieferter Katalog aussah, ist er
brauchbar.

## Was er NICHT ist

**Kein Werkzeug und keine Vorlage.** Er zeigt auf `https://store.ngc.test/terminal-1.0.0.ngc`,
eine Adresse, die es nicht mehr gibt, und auf ein Paket, das nicht mitgesichert ist (103 MB,
aus dem Repo neu baubar). Wer einen Prüfladen braucht, nimmt `tools/pruefladen-tls.sh` und
`tools/pruefladen-stellen.sh` und zieht sich mit `ngc-store catalog` einen **eigenen**
Schlüssel. Nichts hier gehört in eine Vertrauensliste.

Beides ist öffentlich: ein unterschriebener Katalog und ein öffentlicher Schlüssel. Ein
Geheimnis liegt in diesem Ordner nicht.
