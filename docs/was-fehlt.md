# Was fehlt, bis das jemand anderes benutzen kann

**Geschrieben am 30.10.2026.** Diese Datei baut nichts und entscheidet nichts. Sie ist eine
Landkarte.

**Die Frage:** NGC läuft. Acht Maschinen, zwei Apps, drei Datenbankknoten, Paketfilter, 95
Punkte im Entscheidungsbuch, drei veröffentlichte Fassungen. Trotzdem fehlt eine Antwort auf:
**was hält einen Fremden auf?**

**Woher das kommt.** Gesammelt wurden vier Quellen, dann gezählt:

| Quelle | Stellen |
|---|---:|
| `docs/thomas-entscheidet.md` — 32 Fragen + 3 Messungen | **35** |
| `docs/bis-zur-veroeffentlichung.md` — H1–H10 + H4a, R1–R17, B1–B8 | **36** |
| `docs/messungen-m1-m3.md` — offene Lücken | **3** |
| `docs/vertraege-v1.2.md`, Punkte 63–95 — „zu bauen" / „offen" / „nicht gemessen" | **24** |
| **gefunden, mit Doppelungen** | **98** |

Nach Abzug der Doppelungen (dieselbe Sache steht oft an drei Stellen) bleiben **54 eigene
Einträge**: **12 sperren**, **27 halten auf**, **15 stören nur uns**.

**Was sich seit dem 18.10.2026 geschlossen hat** — am 30.10.2026 selbst nachgesehen, nicht
übernommen:

| Alt | Heute |
|---|---|
| H1 Repo ist privat | **zu.** `gh repo view` sagt `PUBLIC`. |
| H2 kein Release | **zu.** Drei: 0.2.0, 0.2.1, 0.2.2 (`gh release list`). |
| H3 kein `ngc-*`-Skript auf ulh-`main` | **zu.** Alle fünf liegen dort, und `catalog.yaml` kennt NextGenCloud. |
| H7 `Updater: nil` | **zu.** `run.go:667` verdrahtet den Updater. |
| H8 eine Installation hat einen Benutzer | **zu.** `users.js:783` schreibt in `next.users` (Punkt 73). |
| H9 vier Behebungen auf unverbundenen Zweigen | **zu** (Punkt 63 ff.). |

Das ist der halbe Weg. Was bleibt, steht unten.

---

## 1. Sperrt — ohne das kann ein Fremder NGC nicht benutzen

| # | Worum | Wo gemessen | Was es braucht |
|---|---|---|---|
| **S1** | **Es gibt keinen Laden.** `NGC_STORE_KEY_DEFAULT="REPLACE-THIS-LINE-WITH-THE-NGC-STORE-KEY"` steht am 30.10.2026 unverändert auf ulh-`main` (`ngc-api.sh:97`). Jede Installation muss ihren eigenen Store nennen — es gibt keine Apps von NGC. | Punkt 56, 57 · Frage 4 | **Entscheidung** (Thomas), danach bauen |
| **S2** | **Zwei Apps, und beide sind Beispiele.** Im Repo liegen `calculator` und `terminal`. Auf der Anlage läuft als zweite eine **Uhr**. NGC verspricht einen Schreibtisch mit Apps in Containern. | Punkt 95 · B3 | bauen |
| **S3** | **Der Release-Schlüssel ist eine Datei auf einer Testmaschine.** Der öffentliche Schlüssel in den vier Skripten ist der mit der Kennung `pN6Z6a9NR0aG6aEJIJ5EHg` — der dateibasierte, nicht der vom Stick. Drei veröffentlichte Fassungen sind damit unterschrieben. Wer auf dieser einen Maschine root ist, unterschreibt NGC für alle. | Punkt 57 · H4 · Frage 2/3 | **Entscheidung** (Thomas) |
| **S3a** | **Der Kommentar darüber sagt das Gegenteil.** „The placeholder below is not a key, so every install stops at the check" — darunter steht ein echter, gültiger Schlüssel. Es stoppt nichts, und das Repo ist jetzt öffentlich. | H4a, am 30.10. auf ulh-`main` nachgesehen | drei Zeilen |
| **S4** | **Punkt 19 ist nicht gebaut.** `shared/sign/sign.go:112` unterschreibt die rohe Nachricht, nicht einen Digest. Darum **kann** der Stick nicht unterschreiben, selbst wenn Thomas sich für ihn entscheidet. Änderung in `shared/` — nur im v1.3-Schnitt. | Punkt 19 (gilt) | bauen, nach P10 |
| **S5** | **Ein Fremder kann die Kette nicht mit eigenem Schlüssel prüfen.** `NGC_RELEASE_KEY="…"` ist eine nackte Zuweisung, anders als alles daneben. Nur mit `sed` in vier Skripten zu ändern — und wer die Skripte verbiegt, prüft nicht mehr die Skripte. | Punkt 62, Befund 2 · Frage 6 | bauen |
| **S6** | **„Mit Passkey anmelden" kann nicht gehen.** `credentials.create` hat auf `main` **null** Treffer in `apps/web/public/` (am 30.10. gezählt). Die Oberfläche empfiehlt Passkeys selbst. 22 Vertragsposten leben nur im Prüfstand. | R2 | bauen |
| **S7** | **Ein `ngc-web`-Update kann nicht gehen.** Die Skripte bauen Namen im Wurzelverzeichnis, der Controller packt nur `ui/` aus, `store.WalkUI` weist das Wurzel-tar ab. Ein Fremder installiert und aktualisiert die Oberfläche nie. | P3 · Punkt 56 (als falsch gemessen) · Frage 7 | **Entscheidung**, danach bauen |
| **S8** | **Die Selbstaktualisierung endet am ersten Knoten.** Vier Läufe in Folge `ngc-api 0.2.2 db-1 E_INTERNAL`. Ursache: ein unbeaufsichtigter Lauf darf kein NGC-Teil einsetzen — die stehende Zustimmung ist gebaut und wirkungslos. | Punkt 82 | **Entscheidung** (a oder b), danach bauen |
| **S9** | **Benutzerdaten werden nicht gesichert.** Zwei Stellen im Code schicken den Admin an `ngc-db backup`. **Das Programm existiert nicht** — `apps/server-db/` hat kein `main.go`, auf keiner Maschine liegt ein `ngc-db`. Gesichert wird nur, was die Anlage ohnehin wiederbekäme. | Punkt 81 | bauen |
| **S10** | **Für `.lan` stellt keine Zertifizierungsstelle aus.** Alles seit Punkt 58 ist mit selbst unterschriebenem Zertifikat gemessen, in einem Browser, der es für den Lauf annimmt. Die Setup-Seite weigert sich über `http://`. Was ein Fremder mit `letsencrypt` erlebt, weiß niemand. | Frage 32 · R10 | **Messung** auf einer Maschine mit echtem Namen |
| **S11** | **Die Oberfläche liegt auf einer Maschine.** Die Rolle `web` hat nur VM 301 (`ngc-web-api-1`). Fällt sie, ist NGC weg — auch wenn die anderen fünf laufen. | `docs/proxmox-sechs.md:33` | **Entscheidung** + bauen |
| **S12** | **Die Oberfläche ist deutsch.** Die fünf Sprachdateien tragen je rund 21 Schlüssel, alle aus Anmeldung und Einrichtung; rund 233 Zeichenketten stehen als deutscher Text im Code. `lang.js` nennt fünf Sprachen und `FALLBACK = "en"` — ein englischer Nutzer bekommt trotzdem Deutsch. Die README ist englisch. | B1, Zählung am 30.10. bestätigt | bauen |
| **S13** | **Eine Lizenz mit Ausstellungszeit in der Zukunft sperrt jede spätere aus** — dauerhaft, und der Verwalter liest „gültig". Die Zeile liegt in eingefrorenem `shared/store/catalog.go:321`. | R1 (S1) · Punkt 63 | **Entscheidung** P10 zuerst |

**Das sind 12 Sachen** (S3a gehört zu S3).

---

## 2. Hält auf — es geht, kostet ihn aber eine Stunde oder einen Befund, den wir kennen

### Er sieht nicht, was los ist

| # | Worum | Wo | Was es braucht |
|---|---|---|---|
| **A1** | `E_VERIFY` beim Aktivieren ist nicht erklärt. Weder API noch Controller schreibt eine Zeile über die Abweisung. | R5 | messen |
| **A2** | Ist die Datenbank des Controllers weg, kommt der Dienst `active` hoch und hat einfach keine Apps. Bewusst so gelassen. | R14 | bauen |
| **A3** | Eine migrierte, nicht eingerichtete Anlage sagt bei **jeder** Anmeldung „die Datenbank konnte nicht erreicht werden" — über eine Datenbank, die einwandfrei antwortet. 12 288 solcher Zeilen in 3,4 s gemessen. | B7, hängt an P9 | **Entscheidung** |
| **A4** | Ein fehlendes Mitglied des Lastverteilers wird dem Benutzer als `HTTP 500` zugestellt, und nichts schreibt es auf. Warum api-2 sechs `500` gab und api-1 keine: offen. | Punkt 90, 91, 92 | messen |
| **A5** | Die Anlage fragt stündlich nach ihrem Laden, bekommt keine Antwort und sagt es niemandem. | Punkt 79 | bauen |
| **A6** | Eine Kanalnummer gilt als belegt, nachdem die API die Freigabe schon gelesen hat — das nächste `OPEN` darauf verschwindet lautlos. | Punkt 86 | bauen |
| **A7** | Nach „Aufnehmen" zeigt die Liste den Server weiter, der Anker bleibt bei `seq: 1`. Ob ausbleibende Auffrischung oder halber Vorgang: **nicht gemessen**. | B6 · Punkt 62 | messen, dann bauen |
| **A8** | Die fünfte Schutzeinstellung der Oberfläche ist nicht gemessen — die Oberfläche sagt über sich selbst, dass sie eine nicht kennt. | B8 · Punkt 56 | messen |

### Riegel, die gebaut sind und nicht greifen

| # | Worum | Wo | Was es braucht |
|---|---|---|---|
| **A9** | `E_LICENSE` verlässt das Controller-Paket nicht. Ein App-Kanal läuft auch für eine App mit abgelaufener Lizenz. | R6 | bauen |
| **A10** | `--pids-limit 512` und `--memory 256m` liegen **nicht an** — gemessen im Verhalten: 600 Aufgaben über einer Decke von 512, keine schlug fehl. Ursache: kein App-Container hat eine eigene cgroup (`Delegate=no` auf allen drei gemessenen Knoten). | Punkt 72 (Posten 1) · Punkt 95 | **Entscheidung** (Delegate oder linger), dann bauen |
| **A11** | Der Controller bemerkt nicht, dass podman einen Schalter verwirft. Ein still verworfener Schalter ist schlimmer als ein fehlender: der Prüffall bleibt grün. | Punkt 72 (Posten 2) | bauen |
| **A12** | `--read-only` lässt vier beschreibbare Ordner offen (`/tmp`, `/run`, `/var/tmp` je 3,9 GiB, `/dev/shm`). Zusammen mit `memory.max = max` ist das ein Weg, den Speicher des Wirts zu füllen, ohne eine Grenze zu verletzen, die irgendwo steht. | Punkt 95 | **Entscheidung** (cgroup oder Größe der tmpfs) |
| **A13** | Der Einmal-Code wird vor der MAC-Prüfung verbraucht: wer die Adresse kennt, verbrennt jeden wartenden Beitrittscode. **Blockiert** — keine Methode in `shared/dbapi` liest einen Einmal-Wert, ohne ihn zu verbrauchen. | Punkt 26 · Frage 23 | **Entscheidung** P10, dann bauen |
| **A14** | Eine zweite Beitrittsanfrage für denselben Namen überschreibt die erste, und der Admin erfährt nichts davon. | Punkt 27 · Frage 24 | bauen |
| **A15** | `noexec` auf `/data` fehlt: eine App darf dort abgelegte Skripte ausführen. Verhaltensänderung für jede Anlage, die das heute tut. | Punkt 51 · Frage 25 | **Entscheidung** |
| **A16** | Das Terminal-Abbild hängt an `debian:13-slim`, nicht an einem Digest — wer den Tag beherrscht, beherrscht einen Container mit `needs:["root"]`. Und: **wer hebt den Digest an?** | Punkt 39 · Frage 26 | **Entscheidung** |
| **A17** | Terminal: Zeilenfeld statt Rohtasten — ein Admin tippt irgendwann sein `sudo`-Passwort in ein Feld, das es anzeigt. | Punkt 41 · Frage 27 | bauen |
| **A18** | Auf db-3 steht die Protokollverschlüsselung weiter auf `OFF`. | Punkt 93 | bauen |
| **A19** | Bei verlorener Mehrheit ist auch das **Lesen** zu (`ERROR 1047` beim `SELECT`). Ob `wsrep_dirty_reads` an soll: offen — veraltete Daten gegen gar keine. | Punkt 92 | **Entscheidung** |

### Die Anlage von Hand nachbessern

| # | Worum | Wo | Was es braucht |
|---|---|---|---|
| **A20** | `ReadWritePaths` nennt `/etc/ngc` nicht (`ngc-api.sh:1065` auf ulh-`main`, am 30.10. nachgesehen). Die API kann den Vertrauensanker nicht hochziehen — die Vertrauensliste kann nicht wachsen. | R9 · Punkt 62 · Frage 5 | zwei Zeilen; Push nur durch Thomas |
| **A21** | `/var/lib/ngc/staged` legt kein Skript an. Wer ohne Release-Adresse installiert, legt den Ordner selbst. | Punkt 62, Befund 3 | eine Zeile |
| **A22** | Eine Sicherung geht nur über die Befehlszeile, eine Rücksicherung gar nicht. „backup" kommt in `apps/web/public/` **nirgends** vor (am 30.10. gegengeprüft). Das ist das Erste, was ein DSM-Nutzer sucht. | B2 | bauen |
| **A23** | Der Weg zurück geht — er bleibt an einem Systembenutzer hängen, den keine Sicherung kennt. | Punkt 87 | bauen |
| **A24** | Eine fehlgeschlagene Einrichtung reißt eine fremde, laufende App aus der Leitung. Die Trennung, die ein Container leistet, endet am gemeinsamen Tunnel. | Punkt 77 | bauen |
| **A25** | Eine frische Anlage hat eine Stunde lang keinen Laden; der Knopf „Jetzt nachsehen" fehlt. | Punkt 66 · Frage 29 | bauen |
| **A26** | Der Kopplungscode gilt unbefristet, und es gibt keinen sichtbaren Weg, einen neuen auszugeben. | Frage 18 (P6) | bauen |
| **A27** | Zwei NGC-Anlagen können auf einem WSL-Rechner nicht nebeneinander laufen (alle Instanzen teilen ein Netz). Fehlt nur in der Anleitung — und genau dort probiert ein Fremder NGC zuerst. | R8 · Punkt 62 | ein Absatz |
| **A28** | `docs/installation-kleines-system.md` nennt als Erstes „zwei Entscheidungen von Thomas" samt Selberbauen und Selberunterschreiben. Mit drei veröffentlichten Fassungen ist die Anleitung an dieser Stelle überholt. | eigener Vergleich 30.10. | schreiben |

**Das sind 27 Sachen.**

---

## 3. Stört uns — technische Schuld, die ein Fremder nie sieht

| # | Worum | Wo |
|---|---|---|
| **U1** | **Elf von siebzehn ungemessenen Vertragsposten sind Sicherheitsriegel.** Das Schwerste ist ein einziges `if` (`trust.go:489`): `MaxNodes`, `MaxStores`, `MaxAuto` haben **keinen** Fall, ebenso `MaxKeysPerUser`, `MaxRoot`, `MaxEntries`. `session.go:455` indiziert ein Feld fester Länge aus einer Zahl, die aus der verteilten Vertrauensliste kommt. | R3 · `vertragsdeckung.md` |
| **U2** | Vier Prüffälle in Ordnung bringen: einer lässt einen Systembenutzer auf der Maschine zurück, einer startet den echten Controller und hängt 10 Minuten, E15 maß das **falsche** Dateisystem. | R7 |
| **U3** | `tools/release-gate.sh` Schritte 4–6 sind nie ausgeführt; `govulncheck` nie auf echtem Debian; `TestReleaseScript` nie mit echtem `jq`. Das Tor vor dem ersten Release ist selbst ungemessen. | R12 |
| **U4** | Der Weg der Migrationen ist nie gelaufen. Dazu verlangt `ngc-api migrate` eine ganze `api.json`, die ein reiner db-Knoten nicht hat. | R13 · P7 · Frage 19 |
| **U5** | **P10 ist das Tor:** darf ein Vertrag von `shared/` überhaupt etwas dazubekommen? Ohne P10 bleibt jede Ergänzung für immer ein Bruch — S4, S13, U6, U7 und A13 hängen daran. | Frage 11 · Punkt 54 |
| **U6** | P8 — eigener Schließgrund für die Leerlaufgrenze. Ein Leerlauf bleibt von einem Absturz nicht zu unterscheiden. | Frage 12 · Punkt 35 |
| **U7** | P9 — eigener Code `E_BUSY`. Heute liest der Benutzer bei drei verschiedenen Lagen immer „Dienst nicht erreichbar". | Frage 13 |
| **U8** | P1 — `store.ValidDownloadURL` wird nicht ausgeführt; Pfad und Query einer Download-Adresse prüft dauerhaft niemand. | Frage 14 · Punkt 25 |
| **U9** | P11, P12, P5, P14, P15 — vier Sätze in den Vertrag und ein Verweisfeld in `dbapi.App` (sonst bleibt ein Abbild ohne Verweis für immer liegen und zählt als Benutzerdaten). | Fragen 15–17, 21, 22 |
| **U10** | Welche der fünf Nummern ist die Fassung eines Releases? Heute gibt es fünf Programmnummern und keine Release-Nummer. | Frage 31 (D4) |
| **U11** | `/var/lib/ngc` ist `0755` und von jedem App-Benutzer auflistbar, `release.seq` ist `0644`. Kein Loch, aber die App erfährt den Aufbau des Servers und die laufende Release-Nummer. | Punkt 95 |
| **U12** | Der Kommentar an `--read-only` („the only place it may write") ist falsch. Genau diese Sorte Satz wird sonst als Zusicherung weitergelesen. | Punkt 95 |
| **U13** | Nicht geklärt: die 477 Byte stderr, die das Protokoll einer App zuschreibt, die sie nicht geschrieben hat. | Punkt 78 |
| **U14** | Nicht geklärt: warum ein Prüffall auf einem GitHub-Runner häufiger flattert als auf dem eigenen Rechner. | Punkt 85 |
| **U15** | `docs/STAND.md` ist vom **10.10.2026** und beschreibt eine Lage, die es nicht mehr gibt („nichts davon ist je gelaufen"). Wer es zuerst liest, liest 20 Tage Rückstand. | eigener Vergleich 30.10. |

**Das sind 15 Sachen.**

---

## 4. Nie gemessen — die blinden Flecken

Zehn Tage, dieselben Leute, dieselbe Hardware. Das Folgende ist **nicht** offen im Sinne von
„noch nicht gebaut" — es ist nie angesehen worden. Keine Zahl, keine Vermutung.

- **Ein Fremder.** Niemand außer Thomas und den Sitzungen hat NGC je installiert. Jede
  Anleitung ist von denen geprüft, die wussten, was gemeint war.
- **Mehrere Benutzer gleichzeitig.** Ein zweiter Mensch ist seit Punkt 73 anlegbar. Dass ein an
  api-1 angelegter Benutzer sich an api-2 anmelden kann, steht dort ausdrücklich als **nicht
  gemessen**. Zwei Menschen gleichzeitig am Schreibtisch: nie.
- **Datenmengen.** Die echte Anlage hat **einen** Benutzer mit **einem** Schlüssel, **null**
  Stores und zwei Apps. Die Vertragsgrenzen liegen drei Größenordnungen darüber (U1). Eine
  große Datei, ein voller App-Ordner, ein Katalog mit hundert Einträgen: nie.
- **Lange Laufzeit.** Die verteilte Anlage läuft seit dem 23.10.2026. Kein Dauerlauf, keine
  Messung des Speicherverbrauchs über Tage, kein Wachstum von Protokoll oder Platte über Zeit.
- **Ein Neustart der ganzen Anlage.** Gemessen sind Teilausfälle: eine Maschine aus, ein Dienst
  gestoppt, zwei von drei Datenbankknoten weg. **Alle sechs gleichzeitig kalt hochfahren** —
  die Lage, in der Galera über `gvwstate.dat` entscheidet, ob es von selbst wiederkommt — ist
  nie gefahren.
- **Eine volle Platte.** `ErrDisk` ist gebaut und im Ernstfall nie gelaufen. Was ein Verwalter
  sieht, wenn `/var/lib/ngc` voll ist, weiß niemand.
- **Ein Upgrade von Fassung zu Fassung.** Es gibt 0.2.0, 0.2.1 und 0.2.2. Dass eine laufende
  Anlage von einer auf die nächste kommt, ist nie durchgelaufen — die Runde endet am ersten
  Knoten (S8).
- **Eine zweite echte App im Repo.** Auf der Anlage läuft eine Uhr. Was eine *andere* App
  braucht, ist nicht gemessen, weil es keine gibt; `ngc/portversuch` startet `sleep infinity`.
- **MAC-Fehlversuche.** NGC zeichnet sie **nirgends** auf. „Null Fehlversuche" heißt „nichts
  gesehen", nicht „nichts passiert". Die Schwelle 3 in Frage 23 steht darum unter Vorbehalt.
- **Ein Knoten mit `Delegate=yes`.** Ob `PIDsLimit = 512` im Betrieb trägt, ist erst dort
  messbar. Folge laut Punkt 72: 512 reißen bei rund 53 Terminal-Sitzungen — unter den 64, die
  ein Browser öffnen darf.
- **Andere Hardware, andere Distribution.** Alles läuft auf Debian 13 auf einem Proxmox-Wirt.
  Ein anderer Wirt, ein Blech ohne Virtualisierung, ein anderes podman mit anderer
  `containers.conf` (dort ist der Prozessdeckel eine andere Zahl): nie.
- **Der Weg über ein echtes, öffentliches Zertifikat.** Siehe S10. Es ist der erste Schirm,
  den ein Fremder sieht.
- **Die Einzel-VM 208 ist absichtlich abgeräumt.** Die Messungen der Punkte 72 und 95, die auf
  ihr entstanden, sind auf dieser Maschine **nicht wiederholbar**. Wer sie nachfahren will,
  braucht eine neue Maschine.

---

## 5. Was nur Thomas entscheiden kann

Hier steht nichts Neues — nur, welche der Zeilen oben keinem Agenten gehören.

- **Der Release-Schlüssel** (S3): Stick oder Datei. Wird es der Stick, kommt S4 dazu, und das
  ist eine Änderung in `shared/`.
- **Der Laden** (S1): eigener Store-Schlüssel oder ein NGC-Store, und wer ihn betreibt.
- **Ein Push nach ulh-`main`** (A20): erreicht per Auto-Update sofort alle ulh-Nutzer.
- **P10** (U5): das Tor vor jeder Vertragsergänzung — und damit vor S4, S13, U6, U7 und A13.
- **Verhaltensänderungen für fremde Anlagen:** `noexec` auf `/data` (A15), Digest statt Tag und
  wer ihn anhebt (A16), `wsrep_dirty_reads` (A19), die stehende Zustimmung (S8).
- **Ein öffentlicher Name** für die Messung aus S10.

---

## 6. Vermutet, nicht geprüft

- **Die Gruppen sind ein Urteil**, keine Messung. „Sperrt" heißt: ich sehe keinen Weg, wie ein
  Fremder NGC damit sinnvoll benutzt. Wer anders urteilt, verschiebt eine Zeile.
- **Keine Zahl in Stunden oder Tagen.** Die Spalte „Was es braucht" sagt nur: bauen, messen
  oder entscheiden. Schätzungen wären erfunden.
- **Die 98 enthalten Doppelungen**, und die Zahl der eigenen Einträge (54) ist von Hand
  zusammengeführt. Wer anders schneidet, kommt auf eine andere Zahl.
- **Die Übernahmen aus `bis-zur-veroeffentlichung.md` sind vom 18.10.2026.** Was ich am
  30.10. selbst nachgesehen habe, steht in der Quellenspalte mit Datum; der Rest ist
  übernommen und kann seitdem gefallen sein — so wie H1, H2, H3, H7 und H8 oben.
- **Die Zählung der Sprachschlüssel** ist am 30.10. über die fünf Dateien gezählt; die 233
  deutschen Zeichenketten sind eine Musterzählung vom 18.10., kein Abgleich.
- **Den Code hinter den meisten Einträgen der Gruppe 3 habe ich nicht angesehen.** Sie stehen
  dort, weil ihre Quelle sie so hinterlässt.
