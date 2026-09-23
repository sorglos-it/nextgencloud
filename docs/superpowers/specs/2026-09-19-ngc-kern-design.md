# NGC – Entwurf für den Kern

**Stand:** 19.09.2026 · **Status:** Von Thomas am 19.09.2026 freigegeben. Nächster Schritt: der Umsetzungsplan
**Einstieg für die nächste Sitzung:** [`docs/STAND.md`](../../STAND.md)

---

## 1. Zielbild

Eine offene Alternative zu Synologys DSM, „nur besser und offen": ein neuer Arbeitsplatz im
Browser. Eine Seite, die alles kann, einfach und effizient, bei maximaler Sicherheit und
Kompatibilität.

- Das Webinterface sieht aus wie ein Desktop und verhält sich auch so: Fenster verschieben,
  Taskleiste, Startmenü. Eine Oberfläche für Tablet und Handy („Controller") ist integriert.
- Nach der Installation gibt es nur einen Grund-Login und den App-Store. Alles andere sind
  Apps, die man bei Bedarf aktiviert. Auf einem Raspberry Pi entstehen so kleine
  Cloud-Lösungen, eine Firewall oder was auch immer.
- Der App-Store ist „die erste App": Er bindet mehrere Stores an, darunter einen globalen
  Store von uns mit Gratis- und Kauf-Apps (Abo oder Kauf, pro Benutzer oder pro Site). Eigene
  Apps lassen sich hochladen.
- NGC hat vier Teile: Webinterface, API, Datenbank und Server-Controller. Alle vier können auf
  einer Maschine laufen, müssen aber nicht. Das Webinterface spricht mit x APIs, die mit y
  Datenbanken arbeiten; die APIs können z Server-Controller haben.
- Endausbau, nicht der Anfang: Webinterface auf einem Webserver, API auf einem
  Application-Server, Datenbank auf einem Datenbankserver, auf jedem läuft ein Controller.
  Ein vierter Server bekommt die App LDAP/Active Directory zugewiesen, ein weiterer wird
  Fileserver und so weiter.

## 2. Grundsätze

### Sicherheit zuerst

Thomas: „sehr, sehr viel Sicherheit … das Ding muss sicher sein." Jede Entscheidung wird
zuerst daran gemessen. Das Sicherheitsmodell steht in 4.5, die verbindlichen Regeln für das
Programmieren in 4.6.

### Wir bauen nur die Pipeline

Thomas: „praktisch ein virtuelles Netzwerk".

```
App-Oberfläche (Browser)  ⇄  API  ⇄  Server-Controller  ⇄  Dienst
```

- Jede Station kennt nur die nächste. Die Oberfläche kennt nur die API. Die API weiß, auf
  welchem Server ein Dienst läuft. Der Controller liefert an den Dienst in der Form, die der
  Dienst braucht.
- API und Controller sind wie VPN-Server und VPN-Client. Der Controller baut die Verbindung
  selbst auf, auf den Servern muss dafür kein Port von außen offen sein.
- Wie ein Dienst programmiert ist, ist egal, solange er die Schnittstelle des Controllers
  spricht (4.3).
- Beispiele von Thomas: Beim Mail-Client läuft die Oberfläche im Browser. Erst beim Senden
  geht die Mail über die API zum Controller des Mailservers, der sie verschickt. Beim Terminal
  geht `cp -r` über API und Controller an den Dienst „virtuelles Terminal", die Ausgabe kommt
  denselben Weg zurück.

### Jeder Server läuft auch ohne API weiter

Über die API kommen Änderungen, den laufenden Betrieb braucht sie nicht. Der Controller hält
auf jedem Server alles vor, was der Server zum Weiterlaufen braucht, und stellt es nach einem
Verbindungsabbruch oder Neustart selbst wieder her (4.6). Beispiel von Thomas: Eine Firewall
muss weiterlaufen, auch wenn die Verbindung zur API abreißt.

### Rollen der vier Teile

| Teil | Rolle |
|---|---|
| Webinterface | Liegt auf jedem Webserver, Standard ist Apache. HTML, CSS, JS und PHP; PHP nur für das Webinterface selbst. |
| API | Eigener, dauerhaft laufender Dienst. Vermittler mit zwei Seiten (Webinterface und Controller) und „die Firewall zwischen GUI und Server". Macht Anmeldung, Rollen und Rechte und die Weiterleitung. Führt nie App-Code aus. Mehrere API-Server teilen sich die Last (4.2). |
| Server-Controller | Eigener, dauerhaft laufender Dienst auf jedem Server. Redet nur mit der API und tut, was sie sagt. Root-Aufgaben führt er nur mit Unterschrift des Benutzers aus (4.5). Hält seinen Zustand lokal in SQLite und startet die Dienste der Apps in Containern. |
| Datenbank | MariaDB als Standard, als Galera-Cluster mit mehreren Knoten oder als einzelner Server. SQLite, wenn alles auf einem Pi läuft. |

### Apps laufen auf dem Client

App-Oberflächen bestehen nur aus HTML, CSS und JavaScript und laufen nur im Browser. Kein Code
aus einem Store läuft auf dem Webserver oder in der API. Was auf einem Server passieren muss,
erledigt ein Dienst der App über den Controller. Die Dienste einer App dürfen ihre Daten in
SQLite ablegen. Jede App bekommt für ihre Dienste einen eigenen Container (4.3).

### Keine Abhängigkeiten

API, Controller und Werkzeuge sind je eine einzelne Go-Datei ohne Laufzeitumgebung. Fremdcode
wird ins Repo kopiert (`vendor/`), seine Herkunft steht in `THIRD-PARTY.md`. Das Webinterface
kommt ohne Framework und ohne Build-Schritt aus. Von außen kommen nur Apache, PHP, MariaDB
mit Galera und Podman aus den Paketquellen von Debian und Ubuntu, und die installieren die ulh-Skripte.

### Lizenz

NGC steht unter der **AGPL-3.0**. `ngc.js` und die Verträge in `shared/` stehen unter
**MIT**, damit auch geschlossene, bezahlte Apps möglich sind. Solange alle Rechte bei Thomas
liegen, kann er Firmen zusätzlich eine kommerzielle Lizenz verkaufen. Nimmt er Code von anderen
Entwicklern an, braucht er dafür deren schriftliche Zustimmung. Bevor NGC öffentlich wird,
schaut jemand mit Ahnung von Lizenzrecht drauf.

### Sprachen und Plattformen

- API, Controller und Werkzeuge in **Go**, Webinterface in **HTML, CSS, JS und PHP**.
- **Debian und Ubuntu**, jeweils **amd64 und arm64**, damit NGC auch auf dem Pi läuft.
- **Windows** wird nicht umgesetzt. Der Entwurf soll es nur nicht ausschließen.

## 3. Teilprojekte

| # | Teilprojekt | Inhalt |
|---|---|---|
| 1 | **Kern** | Die Pipeline von Anfang an verteilt auf sieben Servern: ein Webserver, zwei API-Server mit Lastverteilung, drei Datenbankknoten (MariaDB Galera) und ein Server für die Dienste der Apps, auf jedem ein Controller. Dasselbe läuft auch komplett auf einem Pi mit SQLite. Dazu App-Modell, App-Store mit mehreren Quellen, verschlüsselte Kauf-Apps, Benutzer & Rechte, Anmeldung mit Passwort oder Passkey, Sicherheitsmodell mit Ende-zu-Ende-Verschlüsselung, Dienste in Containern, Autoupdate für alle Teile, Beispiel-Apps und die Installation über ulh. |
| 2 | **NGC-Zertifizierungsstelle** | Eigene App: Zertifikate für Server und Geräte, damit auch ein Heimnetz ohne Aufwand ein vertrauenswürdiges Zertifikat bekommt und auf Passkeys umstellen kann; PIV-Zertifikate für USB-/NFC-Sticks |
| 3 | **Verteilung** | Mehrere Server für Dienste (z Controller) und Dienste an bestimmte Server zuweisen. Mehrere APIs und Datenbanken mit Lastverteilung gibt es schon im Kern. |
| 4 | **Datenbanken** | MySQL, MSSQL und Oracle hinter derselben Schnittstelle wie MariaDB und SQLite |
| 5 | **Handy und Tablet** | Eine zweite Oberfläche für Touch, dieselben Apps im Vollbild |
| 6 | **Store-Server** | Serverseite des globalen Stores: Entwickler veröffentlichen Apps, Kunden bezahlen, der Server stellt die Lizenzen aus, jeweils mit dem Schlüssel der App für die Installation des Käufers |
| 7 | **Apps** | Endausbau: LDAP/AD, Fileserver, Firewall … |

Der Kern kommt zuerst, weil er die Schnittstellen festlegt, an die alles andere andockt. Direkt
danach kommt die Zertifizierungsstelle, weil sie das Problem mit Zertifikaten im Heimnetz löst,
einer der Gründe für NGC. Die
Reihenfolge von 2 bis 7 können wir noch ändern. Jedes Teilprojekt bekommt einen eigenen
Entwurf, einen Plan und wird dann gebaut.

## 4. Der Kern

### 4.1 Erfolgskriterium

Der Kern ist fertig, wenn der folgende Ablauf zweimal klappt: einmal verteilt auf sieben
Servern und einmal komplett auf einem Pi mit SQLite. Installiert wird jeweils über ulh.

1. NGC installieren und als erster Admin anmelden: mit Benutzer, Passwort und TOTP, solange das
   Zertifikat nicht vertrauenswürdig ist, sonst mit Passkey.
2. Im App-Store die Terminal-App aktivieren. Das Aktivieren ist eine Root-Aufgabe und gelingt
   nur mit der Unterschrift des Admins, die der Controller prüft.
3. Im Terminal-Fenster `cp -r` eintippen. Der Befehl läuft über API und Controller zum Dienst
   „virtuelles Terminal" in dessen Container, die Ausgabe kommt zurück.
4. Ein Benutzer ohne das Recht sieht die Terminal-App gar nicht.
5. Eine App aus einem Test-Store installieren und eine Kauf-App mit einer Test-Lizenz
   freischalten. Eine fremde Installation kann dieselbe Kauf-App nicht öffnen.
6. Die Angriffe aus dem Gesamttest scheitern (4.6).
7. Die Verbindung zwischen API und Controller wird getrennt, und der Server mit dem Controller
   wird neu gestartet. Der Controller stellt den Zustand seiner Apps trotzdem wieder her
   (Test-App mit Startaktion, 4.6).
8. Ein NGC-Update und ein App-Update werden im Modus „automatisch" selbst eingespielt. Ein
   absichtlich kaputtes Update wird selbst zurückgenommen, und eine ältere Version lässt sich
   nicht einspielen (4.8).
9. Ein Mitschnitt auf dem API-Server zeigt vom Inhalt der Kanäle nichts, nur verschlüsselte
   Daten. Eine manipulierte Vertrauensliste lehnen Browser und Controller ab (4.5).
10. Im laufenden Betrieb fallen eine API und ein Datenbankknoten aus. Alles läuft weiter, und
    angemeldete Benutzer bleiben angemeldet (4.2).
11. Die Anmeldung mit einem YubiKey 5 NFC und einem Nitrokey 3 NFC klappt, jeweils mit PIN,
    per USB am Rechner und per NFC am Handy (4.5).
12. Mit einem vertrauenswürdigen Zertifikat stellt der Admin sich und eine Rolle auf Passkeys
    um. Danach ist für sie die Anmeldung per Passwort gesperrt. Root-Aufgaben gehen in beiden
    Stufen nur mit Unterschrift des Benutzers (4.5).

### 4.2 Überblick (Abschnitt 1, abgestimmt; an die späteren Abschnitte angepasst)

Aufbau auf sieben Servern:

```
Browser ─HTTPS─► Web (Apache + apps/web/) ─/api/─► API 1 oder API 2 ─TLS─► DB 1 bis 3 (Galera)
                                                          ▲
                                      Tunnel zu jeder API │
                                                          │
                                     Dienste: ngc-controller ─stdin/stdout─► Dienst (Container)
                                            │
                                          SQLite (lokaler Zustand)
```

Auf jedem der sieben Server läuft außerdem ein Controller. Er hält einen Tunnel zu jeder API
und spielt die Updates für den Teil ein, der auf seinem Server liegt (4.8). Der Server
„Dienste" ist der Server für die Dienste der Apps.

**Lastverteilung** (am 19.09.2026 abgestimmt). Thomas: In der Konfiguration des Webinterface
stehen mehrere Server, auf denen die API läuft, und die Last verteilt sich über diese Server;
dasselbe für die Datenbank.

- In der PHP-Konfiguration des Webinterface steht die Liste der API-Server mit Hostname, Port
  und TLS-Fingerabdruck. Daraus richtet der Controller auf dem Webserver die Lastverteilung in Apache ein
  (`mod_proxy_balancer`) in einer eigenen Datei (`config.BalancerFile`), die vor seiner Kopplung
  ngc-web schreibt. Ändert sich die Liste, stellt er Apache selbst um.
- Apache prüft laufend, welche APIs antworten, und schickt jede neue Verbindung an eine davon.
  Fällt eine API aus, verbindet sich der Browser neu und landet bei einer anderen. Weil die
  Sitzungen in der Datenbank liegen, geht es ohne neue Anmeldung weiter.
- Jeder Controller hält zu jeder API einen Tunnel, so erreicht jede API jeden Controller
  direkt.
- Ein neuer API-Server kommt über die Vertrauensliste dazu, also nur mit Unterschrift eines
  Admins. Einen fremden Server, der sich als API ausgibt, nehmen Controller und Browser nicht
  an. Er meldet sich mit Adresse und TLS-Fingerabdruck; der Admin vergleicht einen
  Fingerabdruck über alle Werte mit der Ausgabe des ulh-Skripts.
- Alle APIs einer Installation teilen sich deren Schlüssel,
  etwa den für die Kauf-Apps. Ein neuer API-Server bekommt sie beim Koppeln von einer laufenden
  API, über eine verschlüsselte Verbindung und nur mit Unterschrift eines Admins.
- In der Konfiguration der API steht ebenso die Liste der Datenbankknoten (4.6).

Alles auf einem Pi: dieselben Teile auf einer Maschine, mit einer API und SQLite statt MariaDB.

Apache reicht `/api/` an die APIs weiter. Der Browser kennt damit nur den Webserver, und die
API muss nicht offen im Netz stehen.

| Teil | Ordner | Aufgabe | Server im Kern-Aufbau |
|---|---|---|---|
| Webinterface | `apps/web/` | Desktop aus dem Prototyp (Fenster, Taskleiste, Startmenü). Anmeldung mit Passwort, TOTP oder Passkey, Verschlüsselung im Browser bis zum Dienst, Prüfung der Vertrauensliste, App-Fenster und `ngc.js`, die Oberflächen der mitgelieferten Apps. PHP liefert die Desktop-Seite aus, verwaltet die Liste der APIs und merkt sich den Admin-Schlüssel der Vertrauensliste. | Web |
| API | `apps/server-api/`, `ngc-store` in `apps/cli/` | Sitzungen und Sitzungszertifikate, Rechte, Weiterleitung, Tunnel zu den Controllern, Apps, Stores, Lizenzen und die Schlüssel der Kauf-Apps, Protokoll, Verteilung der Vertrauensliste und der Updates. Dazu das Werkzeug `ngc-store`, das Apps packt, verschlüsselt und Kataloge signiert. | API 1 und 2 |
| Controller | `apps/server-controller/` | Tunnel zur API, Kopplung, Dienste in Containern starten, Ende-zu-Ende-Verschlüsselung, Vertrauensliste und Root-Unterschriften prüfen, Kauf-Apps entschlüsseln, lokaler Zustand, Updates einspielen und zurücknehmen, der Dienst „virtuelles Terminal" | alle sieben |
| Datenbank | `apps/server-db/` | MariaDB-Galera-Cluster einrichten und absichern, Datenzugriff für API und Controller (MariaDB und SQLite), Migrationen, Verschlüsselung der Geheimnisse | DB 1 bis 3 |
| Verträge | `shared/` | Schnittstellen zwischen den Teilen und Testdaten; gehört dem Koordinator (4.7) | – |
| Installation | im ulh-Repo: `scripts/ngc-*.sh` | ulh-Skripte je Teil und für „alles auf einem Pi" | – |
| Mitgelieferte Apps | Oberflächen in `apps/web/`, Dienst des Terminals in `apps/server-controller/` | Fest dabei: **App-Store** und **Benutzer & Rechte**. Zum Aktivieren: **Rechner** aus dem Prototyp (nur Oberfläche) und **Terminal** (Oberfläche und Dienst). | – |
| Fremdcode | `vendor/` (Go), `apps/web/public/assets/vendor/` (Browser), `THIRD-PARTY.md` | Kopiert und mit Herkunft notiert, Kandidaten in Kapitel 7 | – |
| Prototyp | `docs/prototype.html` | Unverändert als Vorlage für den Desktop | – |
| Skripte | `tools/` | `check.sh` vor jedem Commit, später Bauen, Release und der Gesamttest | – |
| Tests | neben dem Code (Go), `apps/web/tests/` (Browser) | Browser-Tests mit Playwright (4.6); der Gesamttest über alle Teile als Skript in `tools/` | – |

**Regel für mehrere APIs:** Sitzungen und alles Wichtige liegen in der Datenbank, nicht im
Speicher einer einzelnen API. Jede API kann jede andere ersetzen.

**Nicht im Kern:** die Teilprojekte 2 bis 7.

### 4.3 Die Pipeline (Abschnitt 2, abgestimmt)

**Kanäle.** Alles, was durch NGC läuft, ist ein Kanal, ähnlich wie ein Tunnel im VPN: öffnen,
Daten in beide Richtungen, schließen. „Mail senden" ist ein Kanal mit einer Nachricht hin und
einer zurück, ein Terminal ist ein Kanal, der offen bleibt. In einer App:

```js
await ngc.call("mail", "send", mail)            // Auftrag, danach kommt die Antwort zurück
const t = await ngc.open("terminal", "shell")   // Datenstrom, der offen bleibt
```

**Öffnen.**

1. Die App fragt die Shell. Eine App kann nur ihre eigenen Dienste erreichen.
2. Die Shell gibt die Anfrage an die API weiter und sagt dazu, welche App fragt.
3. Die API prüft das Recht, sucht den Controller, auf dem der Dienst liegt, und öffnet dort
   den Kanal. Dabei hängt sie an, wer fragt: Benutzer, App und Aktion. Diese Angabe kommt nur
   von der API, der Browser kann sie nicht fälschen.
4. Der Controller prüft selbst, dass am anderen Ende wirklich die Sitzung dieses Benutzers
   sitzt (4.5, Ende-zu-Ende-Verschlüsselung), und startet den Dienst.

Kanalnummern gelten immer nur für eine Verbindung. Der Browser kennt den Controller, auf dem die
Dienste einer App laufen, weil die Ende-zu-Ende-Verschlüsselung dessen Schlüssel braucht; die
Oberfläche zeigt ihn normalen Benutzern nicht an. Der Dienst bekommt die Sitzung des Browsers nie
zu sehen.

**Schnittstelle zum Dienst.** Für jeden Kanal startet der Controller das Programm des Dienstes
im Container der App.

- Daten aus dem Kanal kommen auf stdin an, was das Programm auf stdout schreibt, geht zurück,
  stderr landet im Log. Exit-Code 0 heißt Erfolg.
- Wer fragt, steht in den Umgebungsvariablen `NGC_USER`, `NGC_APP` und `NGC_ACTION`.
- Das beherrscht jede Programmiersprache, auch ein Bash-Skript.
- Verlangt ein Update mehr Rechte, spielt der Controller es nicht ein; der Admin aktiviert dann
  die neue Version mit den neuen Rechten.
- Jeder Container läuft unter dem eigenen Systembenutzer seiner App. Root-Rechte, das Netz des
  Servers oder andere Sonderrechte bekommt ein Dienst nur, wenn die App das im Manifest angibt
  und der Admin es beim Aktivieren erlaubt. Er erlaubt immer alles, was das Manifest verlangt,
  oder aktiviert die App nicht.
- Was in den Daten steht, regeln App und Dienst unter sich, beim Terminal zum Beispiel
  Tastendrücke und Fenstergröße.

**Übertragung.** Zwischen den Stationen läuft ein WebSocket mit binären Nachrichten: ein kurzer
Kopf mit Art und Kanalnummer, dahinter die Daten ohne Umweg über Base64. Es gibt fünf Arten:
öffnen, Daten, Ende der Eingabe, schließen (mit Erfolg oder Fehler) und Freigabe. Die Freigabe
regelt die Menge: Ein Sender schickt pro Kanal nur so viel, wie der Empfänger freigegeben hat.
So kann eine große Datei die API nicht überlasten. Wie die Leitungen zusätzlich verschlüsselt
sind und wie Shell und Controller den Inhalt Ende zu Ende verschlüsseln, steht in 4.5.

**Container** (am 19.09.2026 abgestimmt). Jede App bekommt für ihre Dienste einen eigenen
Container, gestartet mit Podman.

- Podman startet dieselben Images wie Docker, braucht aber keinen Hintergrunddienst, der
  dauerhaft als root läuft. Jeder Container läuft unter dem eigenen Systembenutzer seiner App.
  Wer aus einem Container ausbricht, landet bei einem Benutzer ohne Rechte.
- Ein Container hat standardmäßig keinen Netzzugang. Netz oder weitere Rechte bekommt er nur
  über das Manifest und die Freigabe des Admins.
- Pro App gibt es Grenzen für CPU und Arbeitsspeicher.
- Der verschlüsselte Datenordner der App (4.6) wird in den Container eingebunden.
- Nach einem Neustart des Servers startet systemd die Container selbst, auch ohne API.
- Das Terminal arbeitet innerhalb des Containers seiner App. Ein Terminal direkt auf dem Server
  braucht eine ausdrückliche Freigabe.
- **LXC bleibt vorbehalten:** Der Controller spricht Container nur über eine eigene
  Schnittstelle an, hinter der heute Podman steckt. Dort lässt sich LXC jederzeit als zweite
  Laufzeit einbauen, etwa für Apps, die ein komplettes System brauchen wie der AD-Server. Das
  Manifest gibt an, welche Laufzeit ein Dienst braucht.

**Nicht im Kern:** Dienste, die dauerhaft laufen und über einen Socket angebunden sind. Die
braucht man erst, wenn es zu langsam wäre, für jeden Kanal ein Programm zu starten.

### 4.4 Apps und App-Store (Abschnitt 3, abgestimmt)

**App-Paket.**

```
org.sorglos.terminal/
├── manifest.json        Name, Version, Aktionen, Dienste
├── ui/                  nur HTML, CSS, JS und Bilder
└── services/terminal/   Container-Image des Dienstes für amd64 und arm64
```

Im Manifest meldet die App ihre **Aktionen** an, beim Terminal etwa `shell` für „Befehle auf
dem Server ausführen". Aus den Aktionen werden die Rechte, die ein Admin an Rollen vergibt.
Dazu hat jede App das Grundrecht **„benutzen"**; wer es nicht hat, sieht die App nicht. Das
Manifest sagt außerdem, welches Programm ein Dienst im Container startet, welche Laufzeit er
braucht (heute Podman) und ob er Netz, root-Rechte oder andere Sonderrechte braucht.

**Quellen.** Der App-Store verwaltet mehrere Quellen gleichzeitig:

- **Globaler Store:** unser Store, ab Werk eingetragen, abschaltbar für Netze ohne Internet.
  Dort stellen wir Apps bereit, gratis oder zum Kauf.
- **Weitere Stores:** per Adresse eingetragen, zum Beispiel ein firmeninterner Store oder der
  Store eines anderen Anbieters.
- **Eigene Apps:** Ein Administrator lädt ein App-Paket hoch, das nur in dieser Installation
  gilt.

**Was ein Store ist.** Ein Store besteht aus Dateien auf einem beliebigen Webserver: einem
Katalog mit Apps, Versionen und Preisen sowie den Paketen. Der Store signiert seinen Katalog,
und im Katalog steht zu jedem Paket eine Prüfsumme. So fällt jede Manipulation auf.

- Den Schlüssel des globalen Stores bringt NGC mit. Bei jedem weiteren Store bestätigt der
  Admin dessen Schlüssel einmal beim Eintragen.
- Eigene Apps haben keine Signatur und lassen sich nur mit ausdrücklicher Bestätigung
  installieren.
- Mit `ngc-store` kann jeder Apps packen und einen signierten Katalog erzeugen, also einen
  eigenen Store aufsetzen.
- Wer Apps verkaufen will, braucht zusätzlich einen Store-Server (Teilprojekt 6), der die
  Lizenzen ausstellt. Die Kauf-Apps selbst sind verschlüsselt und können wie Gratis-Apps als
  einfache Dateien bereitliegen (siehe „Verschlüsselte Kauf-Apps").

**Gratis und Kauf.** Im Katalog steht für jede App: gratis, Kauf oder Abo, jeweils pro Site
oder pro Benutzer. Bezahlt wird auf der Webseite des Stores, nie in NGC, deshalb sieht NGC keine
Zahlungsdaten. Danach stellt der Store eine signierte Lizenz für diese Installation aus. NGC
prüft sie selbst, auch ohne Internet:

- **Pro Benutzer:** Nur so viele Benutzer bekommen das Recht „benutzen", wie die Lizenz
  erlaubt.
- **Abo:** Die Lizenz wird regelmäßig beim Store erneuert. Ist der Store nicht erreichbar,
  läuft die App noch eine Übergangsfrist weiter. Danach wird sie deaktiviert, aber nicht
  gelöscht.

**Verschlüsselte Kauf-Apps** (Idee von Thomas, 19.09.2026). Eine Kauf-App lässt sich nur in der
Installation öffnen, für die sie gekauft wurde:

1. Der Store verschlüsselt jede Kauf-App einmal mit einem zufälligen AES-256-Schlüssel. Die
   verschlüsselte Datei darf jeder herunterladen, ohne den Schlüssel ist sie wertlos.
2. Jede Installation bekommt bei der Einrichtung ein eigenes Schlüsselpaar. Beim Kauf
   verschlüsselt der Store den Schlüssel der App mit dem öffentlichen Schlüssel dieser
   Installation und legt ihn in die signierte Lizenz.
3. Nur die API dieser Installation kann ihn mit ihrem privaten Schlüssel entschlüsseln. Beim
   Aktivieren gibt sie ihn über den verschlüsselten Tunnel nur an die eigenen, gekoppelten
   Controller weiter. Erst dort werden die Container-Images der
   Dienste entschlüsselt und geladen. Die Oberfläche entschlüsselt die API für den Browser.
4. Läuft ein Abo aus, gibt die API den Schlüssel nicht mehr heraus. Nach der Übergangsfrist
   starten die Controller den Dienst nicht mehr. Erneuerte Lizenzen bringt die API den
   Controllern über `ngc.store/controller/license`; Unterschrift, Installation und App prüfen die
   Controller selbst, den Schlüssel der Installation nur die API.

Grenzen:

- Die Oberfläche einer App läuft im Browser und muss dort unverschlüsselt ankommen. Jeder
  Benutzer der Installation kann sie im Browser ansehen. Geschützt ist sie nur auf dem
  Übertragungsweg und gegen die Weitergabe an andere Installationen.
- Wer auf einem Server der Installation root ist, kann das entschlüsselte Container-Image eines
  Dienstes kopieren.
- Wer die ganze Installation samt Schlüssel auf eine andere Maschine kopiert, nimmt die Apps
  mit. Später hilft dagegen, den privaten Schlüssel im TPM-Chip des Servers abzulegen (offene
  Frage 8).
- Wer NGC selbst umbaut, kann Lizenzprüfung und Verschlüsselung umgehen. Einfaches Weitergeben
  und das Herunterladen ohne Kauf bringen aber nichts mehr.

Bis der Store-Server fertig ist, besteht der globale Store nur aus einem signierten Katalog mit
Gratis-Apps. Kauf-Apps testet der Kern mit einem Test-Store: `ngc-store` verschlüsselt die
Test-Apps und stellt Test-Lizenzen aus.

**Aktivieren.** Die API prüft das Manifest und trägt die App in die Datenbank ein. Die
Container-Images der Dienste schickt sie über die Pipeline an den Controller, der für jede App
einen eigenen Systembenutzer und Container anlegt. Weil das root-Rechte braucht, ist das
Aktivieren eine Root-Aufgabe mit Unterschrift des Admins (4.5). Rechte an einer neuen App hat zuerst nur die
Rolle Administrator, alle anderen sehen die App erst nach der Freigabe.

**Deaktivieren.** Die App verschwindet aus allen Startmenüs, ihre offenen Kanäle werden
geschlossen. Gelöscht wird nichts, die App lässt sich jederzeit wieder aktivieren.

**Deinstallieren** (am 19.09.2026 von Thomas ergänzt). Wer eine App nicht mehr braucht, kann sie
deinstallieren und so Speicher freigeben. Container, Images und alle Versionen der App werden
entfernt. Dabei wählt der Admin, was mit den Daten der App passiert:

- **Daten löschen:** Auch der Datenordner und die Einstellungen der App werden gelöscht, der
  Speicher ist komplett frei.
- **Daten aufheben:** Der verschlüsselte Datenordner und die Einstellungen bleiben liegen.
  Installiert man die App später neu, ist alles wieder da. Der App-Store zeigt aufgehobene
  Daten mit ihrer Größe an, damit man sie auch später noch löschen kann.

Deinstallieren ist eine Root-Aufgabe mit Unterschrift des Admins. Eine gekaufte Lizenz bleibt
erhalten, auch wenn die App gelöscht wird.

**Im Browser.**

- Jede App-Oberfläche läuft in einem abgeschotteten iframe. Sie kommt weder an die Shell noch
  an andere Apps heran und darf keine eigenen Verbindungen aufbauen, das blockiert der Browser
  über die Sicherheitsrichtlinie (CSP).
- Mit der Außenwelt redet sie nur über `ngc.call` und `ngc.open`, und zwar nur mit ihren eigenen
  Diensten. Daten speichern kann sie deshalb nur über ihren Dienst.
- Die Bibliothek `ngc.js` teilt der App mit, ob die Shell hell oder dunkel ist und welche
  Akzentfarbe gilt.
- Apps müssen in jeder Fenstergröße funktionieren, weil die Oberfläche für Handy und Tablet sie
  später im Vollbild zeigt.
- Die Shell führt eine Oberfläche nur aus, wenn ihr ui-tar zu einer signierten Prüfsumme passt:
  aus dem Katalog des Stores oder aus der Freigabe des Admins beim Aktivieren. Eine übernommene
  API kann keine Oberfläche unterschieben.

**Fest im Kern:** App-Store und Benutzer & Rechte. Beide sind immer aktiv und nur für
Administratoren sichtbar.

### 4.5 Sicherheit (Abschnitt 4, abgestimmt; am 19.09.2026 um die Anmeldung mit Passwort erweitert)

**Vorgaben von Thomas:** AES-256 symmetrisch beziehungsweise asymmetrisch, wo nötig. Root-Aufgaben
aus dem Webinterface müssen vom Benutzer signiert sein, mit einem eigenen Zertifikat pro
Benutzer, das nur für die Sitzungsdauer gilt. Passkeys sowie YubiKey 5 NFC und Nitrokey 3 NFC
mit PIN müssen funktionieren. Im Heimnetz ohne vertrauenswürdiges Zertifikat beginnt NGC mit
Benutzer und Passwort, TOTP lässt sich dazuschalten, und man stellt auf Passkeys um, sobald es
geht.

**Ersteinrichtung.** Das ulh-Skript der API zeigt am Ende einen Einmal-Code an. Nur mit diesem
Code lässt sich das erste Admin-Konto anlegen. So kann niemand im Netz eine frisch
installierte Instanz übernehmen, der nur mithört oder sich zwischen Browser und API schaltet
(Grenze siehe unten). Der Code geht nie über die Leitung: Browser und API weisen sich mit ihm
gegenseitig aus (MAC über das Angebot der API und über Fassung 1).

**Anmeldung in zwei Stufen** (am 19.09.2026 abgestimmt).

1. **Benutzer und Passwort:** Das funktioniert immer, auch im Heimnetz ohne vertrauenswürdiges
   Zertifikat. Ein zweiter Faktor per Authenticator-App (TOTP) lässt sich zuschalten, und NGC
   empfiehlt das.
2. **Passkey oder Stick:** sobald die Installation ein vertrauenswürdiges Zertifikat hat.
   Umgestellt wird pro Benutzer oder für ganze Rollen. Für die Umgestellten ist die Anmeldung
   per Passwort danach abgeschaltet. Pro Rolle kann zusätzlich zum Passkey das Passwort verlangt
   werden.

**Passwortschlüssel.** Damit das Sicherheitsmodell auch mit Passwort hält:

- Das Passwort verlässt den Browser nie. Der Browser berechnet daraus einen Schlüssel, mit
  einem absichtlich langsamen Verfahren, damit Raten teuer wird. Mit diesem Schlüssel meldet er
  sich an, und die API kennt nur dessen öffentlichen Teil.
- Der Passwortschlüssel übernimmt die Rolle des Passkeys: Er unterschreibt das
  Sitzungszertifikat, die Root-Aufgaben und die Vertrauensliste. Eine übernommene API kann also
  auch mit Passwort keine Root-Aufgaben fälschen und kein Passwort mitschneiden.
- TOTP prüft die API. Es schützt die Anmeldung, falls jemand das Passwort stiehlt. Das
  Geheimnis dafür liegt verschlüsselt in der Datenbank.
- Nach Fehlversuchen werden weitere Versuche verzögert und das Konto kurz gesperrt.
- Mit Passwort-Anmeldung zeigt NGC dauerhaft den Hinweis, auf Passkeys umzustellen, sobald es
  geht.

**Passkeys und Sticks.** Passkeys vom Handy, aus Windows Hello und aus Passwortmanagern
funktionieren genauso wie Sicherheitsschlüssel. Jedes Konto sollte zwei haben, etwa einen Stick
und das Handy. Thomas wollte außerdem Sticks, auf die der Admin ein Zertifikat schreibt und die
mit PIN als Single Sign-on gelten. Solche Sticks gibt es schon, zum Beispiel den YubiKey 5 NFC
oder den Nitrokey 3 NFC (Open Source), per USB und per NFC am Handy.

- **FIDO2 (im Kern):** dasselbe Verfahren wie Passkeys. Der Admin richtet den Stick für einen
  Benutzer ein und gibt ihn aus, Name und Schlüssel liegen dann auf dem Stick. Der Benutzer
  steckt ihn ein oder hält ihn ans Handy, gibt seine PIN ein und ist angemeldet, für alle Apps
  und Server von NGC. **Pflicht (Thomas, 19.09.2026):** YubiKey 5 NFC und Nitrokey 3 NFC
  funktionieren als Anmeldung mit PIN, per USB am Rechner und per NFC am Handy. NGC verlangt
  dabei immer die PIN des Sticks, bloßes Antippen reicht nicht.
- **PIV (mit der Zertifizierungsstelle, Teilprojekt 2):** Der Stick arbeitet als Smartcard,
  der Admin schreibt ein Zertifikat unserer Zertifizierungsstelle darauf. Das ist der Weg zu
  Single Sign-on über NGC hinaus, etwa für die Windows-Anmeldung mit der späteren
  LDAP/AD-App.

**Wiederherstellung.** Wer sein Passwort oder alle Schlüssel verliert, hat drei Wege zurück:
Wiederherstellungscodes, ein Admin setzt das Konto zurück, oder im Notfall ein Befehl direkt auf
dem Server.

**Sitzung und Sitzungszertifikate.**

1. Der Benutzer meldet sich an, mit Passwort oder mit Passkey beziehungsweise Stick.
2. Der Browser erzeugt sein Schlüsselpaar, die API erzeugt ihres. Beide gelten nur für die
   Dauer der Sitzung. Den privaten Schlüssel des Browsers kann niemand auslesen, auch die Shell
   nicht.
3. Die API stellt zwei Zertifikate aus, eines für den öffentlichen Schlüssel des Clients und
   eines für ihren eigenen. Ihren öffentlichen Schlüssel schickt sie an den Client. Das
   Client-Zertifikat unterschreibt zusätzlich der Passkey oder der Passwortschlüssel. So bleibt
   die Kette bis zum Benutzer erhalten, und der Controller kann sie bei Root-Aufgaben und beim
   Aufbau jedes Kanals prüfen.
4. Ab da wird jede Nachricht zwischen Webclient und API zusätzlich zu TLS verschlüsselt: Mit
   den beiden Schlüsselpaaren vereinbaren Client und API einen gemeinsamen AES-256-Schlüssel,
   der nie übertragen wird, und verschlüsseln damit jede Nachricht (AES-256-GCM). Jede
   Nachricht direkt asymmetrisch zu verschlüsseln wäre viel zu langsam. Der Vorteil: Auch der
   Webserver, der nur durchreicht, kann nichts mitlesen, wichtig, wenn er später in der DMZ
   steht.
5. Beim Abmelden werden beide Schlüsselpaare verworfen. Spätestens nach der maximalen
   Sitzungsdauer sind die Zertifikate ungültig.

Die Sitzung selbst steckt in einem Cookie, an das kein JavaScript herankommt. In der Datenbank
liegt nur ein Hash davon. Die Sitzung gilt bei jeder API und endet nach einer Zeit ohne
Aktivität oder beim Abmelden.

> **Abweichung vom ursprünglichen Wunsch, abgestimmt am 18.09.2026:** Thomas wollte, dass die
> API auch den privaten Schlüssel des Clients erzeugt und ihn schickt. Dann würde die API ihn
> kennen und könnte im Namen des Benutzers unterschreiben, und die Signatur bei Root-Aufgaben
> würde nichts mehr beweisen. Außerdem ginge der private Schlüssel über die Leitung. Deshalb
> erzeugt der Browser seinen privaten Schlüssel selbst. Thomas: „passt".

**Root-Aufgaben.**

- Alles, was auf einem Server als root läuft, unterschreibt die Shell mit dem
  Sitzungsschlüssel. Dazu gehört auch das Installieren und Deinstallieren von App-Diensten.
- Der Controller prüft die ganze Kette selbst, bis zurück zum Passkey oder Passwortschlüssel.
  Außerdem prüft er, ob der Benutzer auf diesem Server Root-Aufgaben erledigen darf.
- Diese Liste ist Teil der Vertrauensliste (siehe unten). Der Controller hält sie in seiner
  lokalen SQLite-Datei, nur ein Admin kann sie ändern, die API hält nur eine Kopie zur
  Anzeige.
- Ohne gültige Unterschrift läuft nichts als root. Die API allein kann Root-Aufgaben damit
  nicht fälschen.
- Für automatische Updates unterschreibt der Admin einmal eine Dauerfreigabe pro Teil oder
  App (4.8).
- Jede unterschriebene Aufgabe kommt ins Protokoll. So lässt sich nachweisen, wer sie
  angeordnet hat.

**Erneute Bestätigung** (am 19.09.2026 abgestimmt). Thomas: „einstellbar von überhaupt nicht
bis hardcore, inkl. eigener White- und Blacklist". Bestätigt wird per Passkey oder, mit
Passwort-Anmeldung, durch erneute Eingabe des Passworts. Die Stufe gilt pro Rolle; hat ein
Benutzer mehrere Rollen, gilt die strengste.

| Stufe | Erneute Bestätigung bei … |
|---|---|
| Aus | nie, die Anmeldung reicht |
| Kritisch (Standard) | Admins und Root-Berechtigungen ändern, Stores und ihre Schlüssel eintragen, eigene Apps hochladen, einem Dienst root-Rechte erlauben, Dauerfreigaben unterschreiben, eine App samt Daten löschen |
| Root | jeder Root-Aufgabe, also auch beim Aktivieren einer App und bei einem Update per Klick |
| Hardcore | jeder Aktion, die etwas ändert, und jedem Kanal zu einem Dienst |

- **Blacklist:** Was hier steht, braucht immer eine Bestätigung, auch wenn die Stufe niedriger
  ist.
- **Whitelist:** Was hier steht, braucht nie eine Bestätigung, auch wenn die Stufe höher ist.
- Ein Eintrag kann eine einzelne Aktion, eine ganze App oder ein ganzer Server sein. Steht
  etwas auf beiden Listen, gewinnt die Blacklist.
- Stufe und Listen zu ändern verlangt immer eine erneute Bestätigung, egal was eingestellt
  ist. Sonst könnte jemand mit einer gekaperten Sitzung zuerst die Stufe herunterdrehen.
- Bei Root-Aufgaben prüft der Controller die Einstellung selbst, wie seine Liste der
  Root-Berechtigungen, damit die API allein sie nicht aushebeln kann. Für alles andere prüft
  die API.
- In der Stufe Hardcore prüft der Controller die Bestätigung jeder Aufgabe selbst, nicht nur
  bei Root-Aufgaben (offene Frage 4, Variante B). Eine übernommene API kann dann gar nichts
  mehr auslösen.

**Vertrauensliste** (am 19.09.2026 abgestimmt). Eine Liste, die ein Admin mit seinem Passkey
oder Passwortschlüssel unterschreibt. Sie enthält:

- die Admins und die Schlüssel aller Benutzer, also Passkeys und Passwortschlüssel,
- die Schlüssel aller Controller und aller API-Server,
- die Root-Berechtigungen je Server,
- die Stufen der erneuten Bestätigung mit ihren Listen.

Browser und Controller prüfen die Unterschrift selbst. Die API verteilt die Liste nur und kann
sie nicht fälschen. Den Admin-Schlüssel bekommt der Browser vom Webserver, nicht von der API,
und merkt ihn sich beim ersten Besuch. Spätere Fassungen müssen sich über eine lückenlose Kette
von Admin-Unterschriften darauf zurückführen lassen, sonst schlägt der Browser Alarm.

- Jede neue Fassung trägt eine höhere Nummer. Ältere Fassungen
  werden abgelehnt, damit niemand eine überholte Liste zurückspielen kann.
- Neue Benutzer trägt ein Admin ein. Einen weiteren eigenen
  Schlüssel, etwa einen zweiten Stick, bestätigt ein Benutzer selbst mit einem seiner
  vorhandenen Schlüssel, ohne dafür einen Admin zu brauchen. Dasselbe gilt, wenn er sein
  Passwort ändert.

**Ende-zu-Ende-Verschlüsselung** (am 19.09.2026 abgestimmt). Thomas: „Verschlüsselung vom
Browser bis direkt zum Dienst muss rein. nicht später. jetzt."

- Jeder Kanal bekommt eine eigene Verschlüsselung zwischen der Shell im Browser und dem
  Controller auf dem Server des Dienstes: Schlüsselaustausch nach dem Noise-Protokoll, danach
  AES-256-GCM. Erst auf dem Server selbst übergibt der Controller die Daten an den Dienst in
  seinem Container.
- Den Schlüssel des Controllers nimmt die Shell nur aus der geprüften Vertrauensliste. Eine
  übernommene API kann also keinen falschen Controller unterschieben.
- Der Controller prüft beim Aufbau jedes Kanals, dass am anderen
  Ende wirklich die Sitzung dieses Benutzers sitzt, über die Kette bis zu dessen Passkey oder
  Passwortschlüssel. Eine übernommene API kann sich damit in keiner Stufe als Benutzer
  ausgeben.
- Die API sieht nur, wer welchen Dienst mit welcher Aktion aufruft, weil sie das für die
  Weiterleitung und die Rechteprüfung braucht. Den Inhalt sieht sie nicht, der Webserver auch
  nicht.

**Rechte.** Die Shell blendet nur aus, was jemand nicht darf. Entscheiden tut die API, bei
jedem Kanal neu, anhand von Benutzer, Rolle, App, Aktion und Lizenz.

**Controller.** API und Controller weisen sich gegenseitig mit Schlüsseln aus. Einen Controller
auf einem anderen Server koppelt man einmalig mit einem Kopplungscode, den die API ausgibt;
läuft alles auf einer Maschine, koppelt ngc-all den Controller nach der Ersteinrichtung mit einem
Kopplungscode aus dem Browser. Der Controller startet nur
Container aktivierter Apps, jeweils unter dem eigenen Systembenutzer der App. Er selbst läuft
als root, weil er diese Benutzer anlegen muss. Deshalb bleibt sein Code so klein wie möglich.

**Verschlüsselung.**

| Strecke oder Ort | Verfahren |
|---|---|
| Browser ↔ Apache | TLS 1.3, ausschließlich AES-256-GCM |
| Browser ↔ API | zusätzlich Sitzungsschlüssel und AES-256-GCM (siehe Sitzung, Schritt 4) |
| Browser ↔ Dienst | Ende zu Ende pro Kanal zwischen Shell und Controller: Schlüsselaustausch nach Noise, AES-256-GCM; die API sieht nur verschlüsselte Daten |
| Apache ↔ API (getrennte Server) | TLS 1.3 mit AES-256-GCM; Apache bietet der API nur diese Verschlüsselung an und prüft das Zertifikat der API gegen deren Fingerabdruck (aus der Vertrauensliste, vorher aus ulh) |
| API ↔ Controller | Tunnel wie bei einem VPN nach dem Noise-Protokoll, auf dem auch WireGuard aufbaut: AES-256-GCM, beide Seiten prüfen gegenseitig ihre Schlüssel. Außen herum zusätzlich TLS, damit der Tunnel durch Firewalls und Proxys kommt, geprüft gegen den Fingerabdruck der API; die Echtheit sichert Noise. |
| API ↔ MariaDB | TLS; MariaDB nimmt nur verschlüsselte Verbindungen an; API und Controller prüfen das Zertifikat gegen die CA der Installation |
| Datenbankknoten untereinander | TLS für die Galera-Replikation |
| MariaDB | eingebaute Verschlüsselung von MariaDB, AES-256 |
| SQLite (API auf dem Pi, Controller) | ganze Datei mit AES-256 verschlüsselt (XTS) |
| Daten der Apps | verschlüsselter Datenordner pro App (4.6) |
| Kauf-Apps | pro App AES-256; der Schlüssel steckt in der Lizenz, verschlüsselt mit dem öffentlichen Schlüssel der Installation (4.4) |
| Geheimnisse in der Datenbank | zusätzlich von der API mit eigenem Schlüssel verschlüsselt, etwa TOTP-Geheimnisse und Zugangsschlüssel zu Stores. So bleiben sie auch in einer anderen Datenbank oder in einer Sicherung geschützt. |
| Asymmetrisch | Passkeys, Passwortschlüssel, Sitzungszertifikate, Signaturen von Katalogen, Lizenzen, Programmdateien und Updates, Dauerfreigaben, Vertrauensliste, Schlüssel der Kauf-Apps, Schlüsselaustausch im Tunnel und Ende zu Ende |

**Zertifikate für die Webseite.** Das ulh-Skript des Webinterface bietet drei Wege an:

- Let's Encrypt über `mod_md`, das in Apache schon eingebaut ist. Dafür braucht es einen
  öffentlichen Namen.
- Ein selbst signiertes Zertifikat. Damit warnt der Browser beim ersten Aufruf, und die
  Anmeldung läuft mit Passwort. Passkeys gehen erst, wenn das Zertifikat auf jedem Gerät als
  vertrauenswürdig eingetragen ist.
- Import eines Zertifikats von einer anderen Zertifizierungsstelle.

Die NGC-Zertifizierungsstelle wird eine eigene App und direkt nach dem Kern gebaut
(Teilprojekt 2). Thomas: Sie ist ein Grund, NGC überhaupt zu bauen. Mit ihr wird der Umstieg
vom Passwort auf Passkeys im Heimnetz zu ein paar Klicks.

**Folge für Passkeys:** Sie funktionieren nur, wenn NGC über einen Namen statt einer IP-Adresse
aufgerufen wird und der Browser dem Zertifikat vertraut. Das ulh-Skript fragt deshalb nach dem
Namen. Fehlt eins von beiden, bleibt es bei der Anmeldung mit Passwort.

**Browser.** Das Webinterface läuft nur über HTTPS. Eine strenge Sicherheitsrichtlinie (CSP)
verbietet fremde Skripte und Code direkt in der HTML-Seite. Das eingebettete Skript aus dem
Prototyp kommt deshalb in eigene Dateien. Die Apps sind abgeschottet wie in 4.4.

**Protokoll.** Alle Anmeldungen und Fehlversuche, alle Änderungen an Benutzern, Rechten, Apps,
Stores und Lizenzen, jede Root-Aufgabe mit ihrer Unterschrift und jedes Öffnen eines Kanals
werden mit Zeit und Benutzer in der Datenbank festgehalten. Administratoren sehen das Protokoll
in der App Benutzer & Rechte.

**Grenzen.**

- Wer NGC selbst umbaut, kann Lizenzprüfung und Verschlüsselung der Kauf-Apps umgehen (4.4).
- Die API entscheidet weiterhin über die Rechte bei Aufgaben ohne root. Eine übernommene API
  könnte einem echten Benutzer also etwas erlauben, was seine Rechte nicht hergeben, und sie
  sieht, wer wann welchen Dienst aufruft. Den Inhalt sieht sie nicht, und als Benutzer oder
  Controller ausgeben kann sie sich nicht. In der Stufe Hardcore kann sie gar nichts mehr
  auslösen (offene Frage 4, abgestimmt am 19.09.2026).
- Wer den Webserver übernimmt, der die Shell ausliefert, kann einem angemeldeten Benutzer
  veränderten Code unterschieben und dessen Sitzungsschlüssel benutzen, solange die Sitzung
  offen ist. Neuen Geräten könnte er außerdem einen falschen Admin-Schlüssel ausliefern. Dagegen
  helfen die strenge CSP, ein gehärteter Webserver, kurze Sitzungen und die erneute
  Bestätigung: Was bestätigt werden muss, geht ohne den Benutzer nicht.
- Die Oberfläche einer Store-App darf jeder Store der Vertrauensliste belegen, dessen Katalog die
  App in dieser Version mit dieser Prüfsumme führt. Eine übernommene API kann deshalb zusammen
  mit einem feindlichen Store, den der Admin selbst eingetragen hat, einer App eines anderen
  Stores eine fremde Oberfläche unterschieben. Stores trägt man also nur ein, wenn man ihnen so
  weit traut wie der Software, die man von dort installiert. Eine festere Bindung kommt mit dem
  Sicherheitsdurchgang (Plan 7).
- Welche Version einer App installiert ist, erfährt die Shell von der API. Eine übernommene API
  kann ihr daher eine ältere, aber signierte Oberfläche zeigen, nie eine, die niemand für diese
  App signiert hat. Eine Sperre gegen ältere Versionen in der Shell gibt es nicht, weil der
  Controller ein fehlgeschlagenes Update selbst auf die vorige Version zurücksetzt.
- Beim ersten Aufruf mit einem nicht vertrauenswürdigen Zertifikat kann ein Angreifer im selben
  Netz dem Browser eine eigene Shell ausliefern. Gegen sie helfen die MACs der Ersteinrichtung
  nicht, denn sie liest den Einmal-Code beim Tippen mit. Die Ersteinrichtung macht man deshalb in
  einem Netz, dem man traut, oder gleich mit einem vertrauenswürdigen Zertifikat.
- Mit Passwort-Anmeldung ohne vertrauenswürdiges Zertifikat warnt der Browser beim ersten
  Aufruf, und ein Angreifer im selben Netz könnte sich dazwischenschalten. Ein Passwort lässt
  sich außerdem leichter erraten oder abgreifen als ein Stick. Deshalb der dauerhafte Hinweis
  auf den Umstieg zu Passkeys.

### 4.6 Datenbank, Installation, Fehler, Tests (Abschnitt 5, abgestimmt)

**Datenbank.**

- **MariaDB ist der Standard**, als Galera-Cluster mit mindestens drei gleichberechtigten
  Knoten oder, bei kleinen Installationen, als einzelner Server. In der Konfiguration der API
  steht die Liste der Knoten. Die API verteilt die Last, weicht bei einem Ausfall auf einen
  anderen Knoten aus und wiederholt eine Transaktion, wenn Galera sie wegen eines
  gleichzeitigen Schreibzugriffs abweist. Mindestens drei Knoten braucht es, damit der Cluster
  bei einem Ausfall entscheiden kann, welcher Teil weiterarbeiten darf.
- Die Verbindungen laufen über TLS, die Daten liegen mit der eingebauten Verschlüsselung von
  MariaDB verschlüsselt (AES-256), Geheimnisse verschlüsselt die API zusätzlich selbst.
- Die API arbeitet mit einem Datenbankkonto, das nur Daten lesen und schreiben darf. Tabellen
  anlegen oder ändern darf nur ein eigenes Konto für Migrationen. Das benutzen nur die
  Installation über ulh und beim Update genau ein Controller mit der Rolle `db` (4.8); nur dort
  steht der Zugang in `controller.json`.
- **SQLite** gibt es an drei Stellen, jeweils mit AES-256 verschlüsselt:
  1. **Alles auf einem Pi:** Wer bei der Installation keinen Datenbankserver will, bekommt
     SQLite statt MariaDB und spart damit Ressourcen.
  2. **Beim Controller auf jedem Server:** Dort liegt alles, was der Server braucht, um ohne
     API weiterzulaufen: Kopplung, die Vertrauensliste, die installierten Apps
     und deren Einstellungen, zum Beispiel die Regeln einer Firewall.
  3. **Für Apps:** Die Dienste einer App dürfen ihre Daten in SQLite ablegen.
- **Portables SQL:** nur SQL, das alle fünf Systeme verstehen, damit Teilprojekt 4 nichts
  umbauen muss. IDs erzeugt die Anwendung, Zeitstempel sind Zahlen, SQLite-eigene Funktionen
  gibt es nicht. Änderungen am Aufbau laufen über nummerierte Migrationen.
- **Tabellen der API:** Benutzer, Rollen, Rechte, Schlüssel (Passkeys, Sticks und Passwortschlüssel), TOTP (verschlüsselt), Sitzungen,
  Apps, Dienste, Controller, Vertrauensliste (Kopie), Bestätigungsstufen und ihre Listen,
  Stores, Lizenzen, Einstellungen, Protokoll.
- **Verschlüsselte App-Daten:** Damit auch die SQLite-Dateien der Apps verschlüsselt sind, egal
  in welcher Sprache ein Dienst geschrieben ist, stellt der Controller jeder App einen eigenen
  Datenordner bereit, verschlüsselt ihn und bindet ihn in den Container der App ein.
  Vorschlag: fscrypt, die Ordnerverschlüsselung des Linux-Kernels (AES-256-XTS). Im Plan zu
  prüfen, ob das auf allen Zielsystemen geht; sonst ein verschlüsselter Container pro App.
- **Startaktion:** Eine App kann im Manifest eine Startaktion angeben. Der Controller ruft sie
  nach jedem Start selbst auf, auch ohne API. So setzt zum Beispiel eine Firewall ihre Regeln
  nach einem Neustart wieder.

**Regeln für sicheres Programmieren, verbindlich für alle vier Teile.**

- **SQL nur mit Platzhaltern.** Keine Abfrage wird aus Eingaben zusammengesetzt. Tabellen- und
  Spaltennamen stammen nur aus festen Listen.
- **Programme ohne Shell starten.** Der Controller ruft Dienste direkt mit einer Liste von
  Argumenten auf, nie über `sh -c`.
- **Jede Eingabe von außen wird geprüft,** bevor damit irgendetwas passiert: Typ, Länge,
  erlaubte Werte. Unbekannte Felder werden abgelehnt.
- **Nichts, was von außen kommt, wird als HTML eingefügt.** Genau diesen Fehler hat das
  Terminal im Prototyp, im Kern gibt es ihn nicht mehr.
- **Pakete werden sicher entpackt:** keine absoluten Pfade, kein `..`, keine symbolischen
  Links, feste Größengrenzen.
- **Überall gelten Obergrenzen:** für die Nachrichtengröße, die Zahl der Kanäle pro Sitzung und
  die Anmeldeversuche.
- **Kryptografie kommt nur aus der Standardbibliothek von Go und aus geprüften
  Fremdbibliotheken,** nichts ist selbst gebaut.
- **Geheimnisse landen nie im Log.**
- **So prüfen wir das:** Jeder Parser (Protokoll, Manifest, Katalog, Lizenz, Passkey-Daten)
  wird mit dem eingebauten Fuzzing von Go getestet, also mit Massen zufälliger und kaputter
  Eingaben. Den Fremdcode prüft `govulncheck`, ein Werkzeug des Go-Teams, auf bekannte
  Sicherheitslücken. Als Prüfliste für Reviews dient der OWASP ASVS.

**Installation über ulh.**

- Für jeden der vier Teile gibt es ulh-Skripte zum Installieren, Konfigurieren und
  Deinstallieren, dazu eines für „alles auf einem Pi". Dank `answer.yaml` laufen sie auch ohne
  Rückfragen.
- Die Skripte erledigen das Zertifikat (die drei Wege aus 4.5), den MariaDB-Galera-Cluster mit TLS
  und Verschlüsselung, die Lastverteilung in Apache, Podman auf den Servern mit Diensten, das
  Schlüsselpaar der Installation und die Kopplung von Controllern und weiteren API-Servern. Am Ende zeigen sie den Einmal-Code für den ersten Admin an.
- Programmdateien lädt ein Skript nur herunter, wenn ihre Signatur stimmt.
- Die Skripte liegen direkt im öffentlichen ulh-Repo (`scripts/ngc-*.sh`, Einträge in dessen
  `config.yaml`), auch solange NGC privat ist (Entscheidung vom 21.09.2026).
- Updates spielt danach der Controller ein (4.8). ulh bleibt der Notweg, falls NGC selbst nicht
  mehr startet.

**Fehlerbehandlung.**

- Ein Fehler läuft Station für Station zurück: vom Exit-Code des Dienstes über Controller, API
  und Shell bis zur App. Die App bekommt eine klare Meldung, zum Beispiel „keine
  Berechtigung", „Dienst nicht erreichbar" oder „Dienst mit Code 2 beendet".
- Ist ein Controller nicht erreichbar, zeigt die Shell seine Apps gleich als „nicht
  erreichbar" an.
- Bricht eine Verbindung ab, bauen Shell und Controller sie selbst wieder auf, mit wachsenden
  Pausen zwischen den Versuchen. Offene Kanäle gehen dabei verloren, ein Terminal muss man neu
  öffnen.
- Jeder Auftrag hat ein Zeitlimit, standardmäßig 30 Sekunden, im Manifest änderbar. Für offene
  Datenströme wie ein Terminal gilt es nicht.
- Die API startet nur, wenn Datenbank und Schlüssel in Ordnung sind. Sonst bricht sie mit einer
  klaren Meldung ab, statt nur halb zu laufen.

**Tests.**

- Go-Tests für Protokoll, Rechteprüfung, Kryptografie (Sitzungszertifikate,
  Root-Unterschriften, Tunnel, Ende zu Ende, Vertrauensliste, Kauf-Apps) und Datenbank. Alle Datenbanktests laufen gegen
  MariaDB und gegen SQLite.
- Ein Gesamttest startet API, Controller und einen Test-Dienst und spielt das Erfolgskriterium
  durch (4.1). Er enthält auch Angriffe: eingeschleustes SQL, gefälschte Unterschriften, wieder
  eingespielte alte Nachrichten, eine gekaufte App, die eine fremde Installation öffnen will, eine
  untergeschobene Oberfläche, ein Mann in der Mitte zwischen Apache und API bei der
  Ersteinrichtung (fremdes Angebot, fremde Fassung 1) und ein Update, das mehr Rechte verlangt, als
  die App beim Aktivieren bekam.
- Der Gesamttest läuft einmal verteilt auf sieben Servern und einmal komplett auf einem Pi.
- Ausfall: Eine API und ein Datenbankknoten werden im laufenden Betrieb abgeschaltet.
- Deinstallieren: Mit „Daten aufheben" und späterer Neuinstallation sind die Daten wieder da,
  mit „Daten löschen" ist der Speicher frei.
- Updates: automatisches Update, Zurücknehmen eines kaputten Updates, Ablehnen einer älteren
  Version und die Reihenfolge über alle Server.
- Ende zu Ende und Container: Ein Mitschnitt auf dem API-Server zeigt nur verschlüsselte Daten,
  eine manipulierte oder ältere Vertrauensliste wird abgelehnt, und ein Container kommt weder
  ins Netz noch an Dateien außerhalb seines Ordners.
- Passkeys testen wir mit einem simulierten Passkey, dafür braucht es keinen echten Stick.
- Zusätzlich prüft der Gesamttest die Anmeldung mit echten Sticks: YubiKey 5 NFC und
  Nitrokey 3 NFC, jeweils mit PIN, per USB und per NFC.
- Passwort-Anmeldung: mit Passwort und TOTP anmelden, dann auf Passkey umstellen. Ein
  Mitschnitt zeigt, dass das Passwort den Browser nie verlässt.
- Das Webinterface prüfen wir automatisch mit Playwright in den Engines von Chrome, Firefox und
  Safari. Playwright kommt per npm, aber streng abgegrenzt: nur im Ordner `apps/web/tests/`, mit
  festgeschriebenen Versionen, nur auf Test-Servern und nie nötig, um NGC zu bauen oder
  laufen zu lassen. Thomas: „zum Testen unserer Software darfst du bauen was du willst. solange
  der core davon nicht betroffen wird".

### 4.7 Arbeitsteilung (Abschnitt 6, abgestimmt)

**Rollen.** Ich (die Sitzung auf Thomas' Rechner) bin der **Koordinator**. Vier
Claude-Sitzungen, per Remote Control verbunden, arbeiten je an einem Teil. Jede sitzt auf einem
Server ihres Teils und betreut alle Server, die dazugehören:

| Server | Teil | Ordner | Was dort gebaut wird |
|---|---|---|---|
| Web | Webinterface | `apps/web/` | Desktop, Anmeldung mit Passwort, TOTP oder Passkey, Verschlüsselung im Browser bis zum Dienst, Prüfung der Vertrauensliste, App-Fenster und `ngc.js`, die Oberflächen von App-Store, Benutzer & Rechte, Rechner und Terminal, Anzeige und Einstellung der Updates, dazu der PHP-Teil mit der Liste der API-Server |
| API 1 und 2 | API | `apps/server-api/`, `apps/cli/` | Anmeldung mit Passwortschlüssel, TOTP und Passkeys, Sitzungen und Zertifikate, Zusammenspiel mehrerer APIs mit gemeinsamen Schlüsseln, Rechte, Weiterleitung, Tunnel zu den Controllern, Apps, Stores, Lizenzen und Schlüssel der Kauf-Apps, Protokoll, Verteilung der Vertrauensliste und der Updates, das Werkzeug `ngc-store` |
| Dienste | Controller | `apps/server-controller/` | Der Controller, der auf allen sieben Servern läuft: Tunnel zu jeder API, Lastverteilung in Apache auf dem Webserver einrichten, Kopplung, Dienste in Podman-Containern starten, Ende-zu-Ende-Verschlüsselung auf Serverseite, Vertrauensliste, Root-Unterschriften und Dauerfreigaben prüfen, Kauf-Apps entschlüsseln, lokaler Zustand in SQLite, verschlüsselte App-Datenordner, Startaktionen, Updates einspielen und zurücknehmen, dazu der Dienst „virtuelles Terminal" |
| DB 1 bis 3 | Datenbank | `apps/server-db/` | MariaDB-Galera-Cluster auf DB 1 bis 3 einrichten und absichern, Datenzugriff für API und Controller (MariaDB und SQLite), Migrationen, die auch mit der vorigen API-Version funktionieren, Verschlüsselung der Geheimnisse |

Jeder Teil schreibt auch sein eigenes ulh-Skript, im ulh-Repo unter `scripts/ngc-<teil>.sh`.

**Zuerst die Verträge.** Bevor jemand baut, legt der Koordinator die Schnittstellen schriftlich
in `shared/` fest, und Thomas gibt sie frei:

1. Webinterface ↔ API
2. API ↔ Controller
3. Controller ↔ Dienst im Container
4. API und Controller ↔ Datenbank, als Go-Schnittstelle
5. App-Paket, Store-Katalog und Lizenz, samt Verschlüsselung der Kauf-Apps, dazu NGC-Updates
   und Dauerfreigaben
6. ulh-Konventionen und Konfiguration: Pfade, Dienste, Ports, `answer.yaml`, die Listen der
   API-Server und Datenbankknoten
7. Vertrauensliste und Ende-zu-Ende-Verschlüsselung zwischen Browser und Controller

Zu jedem Vertrag gibt es Testdaten, zum Beispiel Beispielnachrichten und Beispielunterschriften.
Beide Seiten testen unabhängig dagegen, so muss niemand auf den anderen warten.

**Eigene Anweisungen.** Jeder Ordner bekommt eine eigene `CLAUDE.md`: die Aufgabe, welche Ordner
dem Teil gehören, welche Verträge gelten, die Sicherheitsregeln aus 4.6, wie getestet wird,
wann der Teil fertig ist und was tabu ist, also fremde Ordner und das Ändern von Verträgen.

**Abstimmung.**

- Die Sitzungen schicken sich gegenseitig Nachrichten. Fragen an einen Nachbarn gehen direkt
  an dessen Sitzung, Fragen zu einem Vertrag an den Koordinator.
- Einen Vertrag ändert nur der Koordinator, und alle vier erfahren es sofort.
- Was nicht im Repo steht, gilt nicht. Jede Entscheidung kommt ins Entscheidungsprotokoll.
- Jeder Teil arbeitet auf einem eigenen Branch und liefert per Pull Request. Der Koordinator
  prüft jeden Pull Request auf Vertrag und Sicherheit und führt ihn dann zusammen.

**Ablauf.**

1. Der Koordinator schreibt Verträge, Anweisungen und das Grundgerüst, Thomas gibt frei.
2. Die vier Teile werden parallel gebaut, jeder gegen die Testdaten.
3. Alle vier Teile laufen zusammen auf den sieben Servern, dann folgt der verteilte Gesamttest.
4. Alles läuft auf einem einzigen Pi mit SQLite, dort folgt der zweite Gesamttest.
5. Zum Schluss ein Sicherheitsdurchgang: Fuzzing, Angriffe und ein Review nach dem OWASP ASVS.

**Voraussetzungen von Thomas, wenn es losgeht:** sieben Server mit Debian oder Ubuntu, VMs
genügen, etwa auf Proxmox: Web, API 1, API 2, DB 1, DB 2, DB 3 und Dienste. Sie müssen sich
gegenseitig im Netz erreichen, und jeder braucht Zugriff auf das Repo. Die vier Sitzungen
laufen auf Web, API 1, DB 1 und Dienste und sind per Remote Control verbunden. Für Schritt 4
kommt ein Pi dazu, und für den Test je ein YubiKey 5 NFC und ein Nitrokey 3 NFC.

### 4.8 Updates (am 19.09.2026 abgestimmt)

**Vorgabe von Thomas:** Alle Teile unterstützen Autoupdate: Webinterface, API,
Server-Controller, Datenbank und Apps.

| Teil | Wer aktualisiert | Woher kommt das Update |
|---|---|---|
| Webinterface, API, Controller | der Controller auf dem jeweiligen Server | NGC-Updates, signiert mit dem Release-Schlüssel von NGC |
| Datenbank | Die Migrationen gehören zur jeweiligen API-Version. Der Controller spielt sie ein, bevor die neue API startet. | kommt mit dem API-Update |
| Apps (Oberfläche und Dienste) | API und Controller | aus dem jeweiligen Store, vom Store signiert |
| Apache, PHP, MariaDB und das übrige System | der Controller über apt | Paketquellen von Debian und Ubuntu, dort signiert |

Vorschlag, im Plan festzulegen: NGC-Updates kommen über den globalen Store als eigener,
signierter Katalog.

**Drei Modi, pro Teil und pro App einstellbar:**

- **Automatisch (Standard):** Das Update wird heruntergeladen, geprüft und selbst installiert,
  nachts in einem festen Wartungsfenster.
- **Halbautomatisch:** Das Update wird vorab heruntergeladen und geprüft, installiert wird es
  per Klick.
- **Nur Benachrichtigung:** NGC meldet nur, dass es ein Update gibt. Ein Klick lädt es
  herunter und installiert es.

**Dauerfreigabe.** Ein Update ist eine Root-Aufgabe, und nachts ist niemand da, der
unterschreiben könnte. Deshalb unterschreibt der Admin einmal eine Dauerfreigabe, wenn er
„automatisch" einstellt: „Updates für diesen Teil, die mit diesem Schlüssel signiert sind,
dürfen selbst installiert werden." Die Freigabe liegt beim Controller. Er installiert nur
Updates, die mit genau diesem Schlüssel signiert und neuer als die installierte Version sind.
Die API allein kann also weiterhin nichts unterschieben. Bei der Ersteinrichtung unterschreibt
der erste Admin diese Freigaben für NGC selbst, deshalb ist „automatisch" von Anfang an aktiv.
Neue Rechte deckt die Freigabe nicht: Verlangt eine neue Version mehr (`needs`), spielt der
Controller sie weder automatisch noch per Klick ein; der Admin aktiviert sie neu.

**Sicher zurück.** Bei jedem Update bleibt die vorige Version erhalten. Startet der
aktualisierte Teil nicht sauber, stellt der Controller selbst die alte Version wieder her. Eine
ältere Version als die installierte wird sonst nie eingespielt, damit niemand eine Version mit
bekannter Sicherheitslücke zurückbringen kann.

**Reihenfolge.** Die API verteilt ein NGC-Update Schritt für Schritt: erst die Datenbank, dann
die APIs, dann die Controller, zuletzt das Webinterface. Der nächste Schritt beginnt erst, wenn
der vorige läuft. APIs und Datenbankknoten kommen nacheinander dran, damit immer einer läuft;
so bleibt NGC während des Updates erreichbar. Dafür versteht jeder Teil auch die vorige
Version der Verträge, und jede Migration lässt die vorige API-Version weiterlaufen.
Den Ablauf steuert immer nur eine API; welche, regelt eine
Sperre in der Datenbank.

**Folgen.**

- Auf jedem Server läuft ein Controller, im Kern also sieben.
- Der Release-Schlüssel, mit dem NGC-Updates signiert werden, ist der wichtigste Schlüssel
  überhaupt: Wer ihn hat, kann Code auf alle Installationen mit Autoupdate bringen. Er liegt
  deshalb auf einem Hardware-Stick und nie auf einem Server.
- ulh bleibt für Installation, Konfiguration und Deinstallation zuständig und ist der Notweg,
  falls NGC selbst nicht mehr startet.

## 5. Offene Fragen

**Für den Kern:** alle fünf entschieden.

1. **Lizenz von NGC:** entschieden am 19.09.2026, siehe Kapitel 2.
2. **App-Updates:** entschieden am 19.09.2026, Autoupdate für alle Teile (4.8).
3. **Erneute Bestätigung:** entschieden am 19.09.2026, einstellbar in vier Stufen mit
   eigenen Listen (4.5).
4. **Die zwei Restrisiken:** entschieden am 19.09.2026, Variante B und
   Ende-zu-Ende-Verschlüsselung (4.5).
5. **Testwerkzeuge:** entschieden am 19.09.2026. Für Tests ist alles erlaubt, auch npm,
   solange NGC selbst nicht davon abhängt (4.6).

**Für spätere Teilprojekte:**

6. **Mehrere APIs:** entschieden am 19.09.2026, Lastverteilung mit Ausfallsicherheit, schon im
   Kern (4.2).
7. Teilprojekt 5: Ist mit „Controller für Tablet und Handy" eine zweite Oberfläche gemeint? Das
   ist bisher nur angenommen.
8. Den privaten Schlüssel der Installation im TPM-Chip ablegen, wo einer vorhanden ist, damit
   er sich nicht kopieren lässt (4.4).
9. Teilprojekt 7: Thomas' Projekt `ulh` auch als Grundlage für Dienste nutzen, die Server
   einrichten (Skripte für alle Distributionen, `answer.yaml` für Läufe ohne Rückfragen)?
10. LXC als zweite Container-Laufzeit einbauen, sobald eine App ein komplettes System braucht,
    etwa der AD-Server (4.3).
11. Teilprojekt 2: Let's Encrypt über eine DNS-Prüfung für Installationen mit eigener Domain,
    die von außen nicht erreichbar sind, und ein eigener Namensdienst von NGC wie `synology.me`
    bei Synology.

## 6. Entscheidungsprotokoll

| Datum | Entscheidung |
|---|---|
| 18.09.2026 | Repo `sorglos-it/nextgencloud`, privat. Prototyp `W:\ngc\index.html` unverändert als `prototype/index.html`. |
| 18.09.2026 | Zielbild (Kapitel 1) und Zerlegung in Teilprojekte (Kapitel 3) |
| 18.09.2026 | App-Modell: Die API führt nie App-Code aus. Grundsatz „wir bauen nur die Pipeline". |
| 18.09.2026 | Webinterface im Webserver, API und Controller als eigene, dauerhaft laufende Dienste |
| 18.09.2026 | Go für API und Controller |
| 18.09.2026 | PHP nur für das Webinterface selbst, App-Oberflächen nur HTML, CSS, JS |
| 18.09.2026 | Abschnitt 1 (Überblick) und Abschnitt 2 (Pipeline) abgestimmt |
| 18.09.2026 | App-Store als erste App mit mehreren Quellen und globalem Store. Die Serverseite wird Teilprojekt 5. Abschnitt 3 abgestimmt. |
| 18.09.2026 | Sicherheit: AES-256, Passkeys und YubiKey Pflicht, signierte Root-Aufgaben, Sitzungszertifikate. Der private Client-Schlüssel entsteht im Browser. Abschnitt 4 abgestimmt. |
| 18.09.2026 | NGC-Zertifizierungsstelle als eigene App (Teilprojekt 6). USB-/NFC-Sticks: FIDO2 im Kern, PIV mit der Zertifizierungsstelle. |
| 19.09.2026 | Vorgabe Thomas: nur Debian und Ubuntu, jeweils amd64 und arm64 (damit auch auf dem Pi). Windows höchstens vorsehen, auf keinen Fall umsetzen. |
| 19.09.2026 | Vorgabe Thomas: „sehr, sehr viel Sicherheit" — SQL-Injection und Ähnliches von Anfang an verhindern. |
| 19.09.2026 | Vorgabe Thomas: MariaDB ist die Standard-Datenbank (lokal oder auf einem anderen Server). SQLite steht Apps zur Verfügung, liegt beim Controller für alles, was ein Server ohne API zum Weiterlaufen braucht (Beispiel Firewall), und ersetzt MariaDB, wenn alles auf einem Pi laufen soll. |
| 19.09.2026 | Vorgabe Thomas: Webinterface, API, Server-Controller und Datenbank werden eigene Arbeitspakete mit eigenen Anweisungen. Vier Claude-Sitzungen bauen sie gleichzeitig auf vier Servern (Remote Control) und stimmen sich ab, damit die Teile wirklich getrennt funktionieren. |
| 19.09.2026 | Vorgabe Thomas: Installation, Konfiguration und Deinstallation laufen über ulh. |
| 19.09.2026 | Abschnitt 5 abgestimmt: Plattformen, Datenbank, Regeln für sicheres Programmieren, Installation über ulh (Skripte im NGC-Repo unter `ulh/`, solange es privat ist), Fehlerbehandlung, Tests |
| 19.09.2026 | Abschnitt 6 abgestimmt: Koordinator und vier Sitzungen, Verträge zuerst, eigene `CLAUDE.md` je Teil, Branch und Pull Request je Teil. Der Kern läuft damit von Anfang an verteilt, Teilprojekt 2 umfasst nur noch mehrere APIs, Datenbanken und Controller. |
| 19.09.2026 | Beim Ausformulieren ergänzt und von Thomas bestätigt: Erfolgskriterium 7 (Server läuft ohne API weiter), TLS mit AES-256 zwischen Apache und API, verschlüsselter Datenordner pro App, Startaktion im Manifest |
| 19.09.2026 | Idee Thomas, aufgenommen: Kauf-Apps werden für die kaufende Installation verschlüsselt. Nur deren API und Controller können sie öffnen (4.4). |
| 19.09.2026 | Lizenz: AGPL-3.0 für NGC, MIT für `ngc.js` und die Verträge (offene Frage 1) |
| 19.09.2026 | Vorgabe Thomas: Alle Teile unterstützen Autoupdate — Webinterface, API, Server-Controller, Datenbank und Apps. Modi: automatisch (Standard), halbautomatisch (per Klick) oder nur Benachrichtigung und dann Klick. |
| 19.09.2026 | Update-Konzept abgestimmt (4.8): Controller auf jedem Server spielt Updates ein, Dauerfreigabe des Admins für den Modus „automatisch", Rücknahme bei Fehlschlag, keine älteren Versionen, Reihenfolge Datenbank → API → Controller → Webinterface, Release-Schlüssel auf einem Hardware-Stick |
| 19.09.2026 | Erneute Passkey-Bestätigung einstellbar (offene Frage 3): Stufen Aus, Kritisch (Standard), Root und Hardcore pro Rolle, dazu eigene Black- und Whitelist; Änderungen daran verlangen immer den Passkey (4.5) |
| 19.09.2026 | Offene Frage 4, Variante B: In der Stufe Hardcore prüft der Controller jede Aufgabe selbst, nicht nur Root-Aufgaben. |
| 19.09.2026 | Vorgabe Thomas: Verschlüsselung vom Browser bis direkt zum Dienst kommt in den Kern, „nicht später. jetzt." Ausgearbeitet in 4.5. |
| 19.09.2026 | Vorgabe Thomas: Die Dienste der Apps laufen als einzelne Container („docker/lxc"). Ausgearbeitet in 4.3. |
| 19.09.2026 | Container mit Podman, jeder unter dem Systembenutzer seiner App. LXC bleibt vorbehalten und lässt sich jederzeit als zweite Laufzeit einbauen. Die Vertrauensliste, von einem Admin mit Passkey unterschrieben, ist der gemeinsame Anker für Browser und Controller. |
| 19.09.2026 | Offene Frage 5: Für Tests ist alles erlaubt, auch npm (Playwright in `tests/browser/`), solange NGC selbst nicht davon abhängt. |
| 19.09.2026 | Vorgabe Thomas zu 4.2: In der PHP-Konfiguration des Webinterface stehen mehrere Server (IP-Adressen), auf denen die API läuft, mit Lastverteilung über diese Server. Dasselbe für die Datenbank. Ausgearbeitet in 4.2 und 4.6. |
| 19.09.2026 | Lastverteilung für API und Datenbank schon im Kern (Variante A). Der verteilte Test läuft auf sieben Servern: Web, API 1 und 2, DB 1 bis 3 (MariaDB Galera) und Dienste. Es bleibt bei vier Claude-Sitzungen. |
| 19.09.2026 | Vorgabe Thomas zu 4.4: Apps lassen sich deinstallieren, um Speicher freizugeben. Dabei wählt man „Daten löschen" oder „Daten aufheben" für eine spätere Neuinstallation. |
| 19.09.2026 | Vorgabe Thomas zu 4.5: YubiKey 5 NFC und Nitrokey 3 NFC müssen als Anmeldung mit PIN funktionieren. Die PIN ist immer Pflicht, der Gesamttest prüft beide mit echten Sticks. |
| 19.09.2026 | Vorgabe Thomas: Im Heimnetz ohne vertrauenswürdiges Zertifikat beginnt NGC mit Benutzer und Passwort, TOTP lässt sich dazuschalten, und man stellt auf Passkeys um, sobald es geht. Eine Zertifizierungsstellen-App ist ein Grund, NGC überhaupt zu bauen. Ausgearbeitet in 4.5. |
| 19.09.2026 | Anmeldung in zwei Stufen abgestimmt: Benutzer und Passwort mit Passwortschlüssel im Browser und zuschaltbarem TOTP, Umstieg auf Passkeys pro Benutzer oder Rolle. Die Zertifizierungsstellen-App rückt direkt hinter den Kern (Teilprojekt 2, vorher 6), die übrigen Teilprojekte rücken eins nach. |
| 19.09.2026 | Entwurf von Thomas freigegeben, einschließlich der sechs beim Ausformulieren ergänzten Stellen: Fassungsnummern der Vertrauensliste, zweiter eigener Schlüssel ohne Admin, Prüfung der Benutzersitzung bei jedem Kanal, gemeinsame Schlüssel für neue API-Server nur mit Admin-Unterschrift, eine API steuert die Update-Reihenfolge über eine Sperre in der Datenbank, „App samt Daten löschen" in der Stufe Kritisch. |
| 19.09.2026 | Beim Schreiben von Plan 0 geklärt: Der Anker der Vertrauensliste ist Nummer und SHA-256 einer Fassung (damit auch der Admin-Schlüssel), nicht der Schlüssel allein. |
| 19.09.2026 | Beim Schreiben von Plan 0 geklärt: Ein Container je App, also ein Image je App und Architektur (`services/<arch>.tar`); die Dienste sind Programme darin. |
| 19.09.2026 | Beim Schreiben von Plan 0 geklärt: Kern-Apps (`ngc.desktop`, `ngc.users`, `ngc.store`) bedient die API selbst; ihre Kanäle haben keinen eigenen Ende-zu-Ende-Schlüsselaustausch, weil die Sitzung zur API schon so verschlüsselt ist. |
| 19.09.2026 | Beim Schreiben von Plan 0 geklärt: Update-Dateien kommen über Kanäle (`stage`, `install`) zum Controller, nicht über Steuernachrichten; über den Steuerkanal läuft nur der Fortschritt. |
| 19.09.2026 | Beim Schreiben von Plan 0 geklärt: Die Wiederherstellung (Codes, Zurücksetzen durch einen Admin, Notfallbefehl) ist noch kein Vertrag; sie kommt als Vertragsänderung vor Plan 1 des Webinterface. |
| 20.09.2026 | Beim Ausführen von Plan 0 geklärt: Bei der Anmeldung unterschreibt der Benutzer mit Passkey oder Passwortschlüssel das Client-Zertifikat, das der Browser gebaut hat (`sign.PurposeClientCert`); einen eigenen Zweck nur für die Anmeldung gibt es nicht. |
| 20.09.2026 | Beim Ausführen von Plan 0 geklärt: `session.VerifyClient` liefert eine geprüfte Sitzung (`session.Verified`), die nur durch diese Prüfung entsteht. Aufgaben (`task.Input`) nehmen nur eine solche Sitzung an, damit keine ungeprüfte bis zum Controller kommt. |
| 21.09.2026 | Vorgabe Thomas: Die ulh-Skripte für NGC kommen direkt ins öffentliche ulh (`scripts/ngc-*.sh`, Einträge in dessen `config.yaml`), auch solange NGC privat ist. Kein Unterordner in NGC, kein zweites Repo, ulh bleibt unverändert. Ersetzt die Regel „Skripte im NGC-Repo unter `ulh/`" aus Abschnitt 5. |
| 21.09.2026 | Nach der Abschlussprüfung von Plan 0: Ende zu Ende bindet der Prolog zusätzlich SHA-256 der Parameter und SHA-256 der Aufgabe, jeweils genau so, wie sie im OPEN stehen; so kann die API weder Parameter ändern noch eine andere gültige Aufgabe unterschieben. Noise-Nachricht 1 und 2 tragen in allen drei Verwendungen keine Nutzlast (je 48 Byte). |
| 21.09.2026 | Der Controller startet einen Dienst erst, wenn die erste Ende-zu-Ende-Nachricht der Shell entschlüsselt ist; die Shell schickt sie immer, notfalls leer. Ein wieder eingespieltes OPEN bewirkt so nichts. |
| 21.09.2026 | Ende-zu-Ende-Daten laufen in inneren Rahmen (Daten oder Ende); das Ende des Controllers trägt das Ergebnis. Fehlt das Ende, ist der Datenstrom abgeschnitten und das ist ein Fehler; EOF und CLOSE auf der Leitung sind nur Transport. |
| 21.09.2026 | Die API gibt das OPEN des Browsers Byte für Byte an den Controller weiter (`tunnel.Open`, Feld `open`). Ein eigenes Feld für den Benutzer gibt es nicht mehr; der Benutzer eines Kanals kommt nur aus dem geprüften Client-Zertifikat. |
| 21.09.2026 | Die Shell zeigt einen Kopplungscode (und einen Beitrittscode für weitere API-Server) erst, wenn er zu ihrer geprüften Vertrauensliste passt: Anker = ihre neueste Fassung, Schlüssel und Adresse = ein API-Server der Liste. ulh zeigt den Fingerabdruck des Ankers zum Vergleich. |
| 21.09.2026 | Ob eine Aufgabe root braucht oder kritisch ist, entscheidet der Prüfende nur aus geprüften Daten: Kernliste, Manifest und die Parameter der unterschriebenen Aufgabe, nie aus dem OPEN. Ein OPEN mit Aufgabe hat keine eigenen Parameter. |
| 21.09.2026 | Kanäle zu einer App sind Root-Aufgaben, wenn ihr Manifest `root` verlangt. Aktivieren gewährt immer alle `needs` des Manifests oder wird abgelehnt. |
| 21.09.2026 | Die API prüft eine Aufgabe, die sie an einen Controller weiterleitet, vorher selbst, mit dem Ziel-Controller als Node. |
| 21.09.2026 | Neue Fassungen der Vertrauensliste bekommt ein Controller über den Kanal `ngc.users/controller/trust` (ohne Benutzer), der Browser per HTTP und seitenweise; die Steuernachricht `trust` ist nur noch ein Hinweis. |
| 21.09.2026 | Der Browser kennt den Controller, auf dem die Dienste einer App laufen (`DesktopApp.node`), weil Ende zu Ende ihn braucht; die Oberfläche zeigt ihn normalen Benutzern nicht. Ersetzt in 4.3 „Der Browser erfährt nie, auf welchem Server ein Dienst läuft“. |
| 21.09.2026 | Freigabe je Strecke und Richtung, gezählt in Bytes der DATA-Nutzlast, additiv, höchstens 1 MiB offen. Höchstens 64 Kanalnummern je Sitzung und 1024 je Tunnel; eine Nummer zählt, bis sie wieder frei ist, auch beim Schließen. Wer darüber hinausgeht, beendet die Verbindung; ist nur der Tunnel hinter der API voll, antwortet sie dem Browser mit CLOSE E_LIMIT. |
| 21.09.2026 | Das Ergebnis eines Aufrufs sind alle Daten bis zum Ende, höchstens 8 MiB, für alle Apps; prüfen muss das der Empfänger, bei Ende-zu-Ende-Kanälen also die Shell; was der Öffner schicken darf, steht je Kern-Aktion fest. |
| 21.09.2026 | Ohne Benutzer öffnet die API nur drei Kern-Aktionen: `ngc.users/controller/trust`, `ngc.store/controller/stage` und `install`. `install` führt der Controller nur mit Dauerfreigabe aus und nur für eine neuere Version; Ausnahme sind Systempakete (Ziel `system`), die keine NGC-Version tragen. |
| 21.09.2026 | Der globale Store hat die ID `global`, `local` steht für hochgeladene Apps. Die API gibt den globalen Store bei der Ersteinrichtung mit, damit er in Fassung 1 der Vertrauensliste steht. |
| 21.09.2026 | Migrationen spielt beim Update genau ein Controller mit der Rolle `db` ein; nur dort steht der Zugang des Kontos `ngc_migrate` in `controller.json`. Ändert 4.6 („das benutzt nur die Installation über ulh“). |
| 21.09.2026 | Begriffe: Rollen eines Servers (`web`, `api`, `db`, `services`) und Softwareteile (`ngc-web`, `ngc-api`, `ngc-controller`). Release-Dateien und Update-Ziele benennen Softwareteile. |
| 21.09.2026 | JSON von außen ist strikt: keine doppelten oder anders geschriebenen Feldnamen (Go: `encoding/json/v2`, im Browser genauso). Jede Grenze aus den Go-Verträgen gilt auch im JavaScript. |
| 21.09.2026 | Verlangt eine Rolle zusätzlich das Passwort, unterschreibt ein Passwortschlüssel desselben Benutzers dieselben Zertifikat-Bytes (Zweck `clientcert/v1`). „Nur Passkey“ und „Passwort zusätzlich“ zusammen sind unzulässig. |
| 21.09.2026 | Weitere API-Server treten mit einem Beitrittscode `ngc-join:…` bei. Die gemeinsamen Schlüssel übergibt die API erst nach der Unterschrift eines Admins; wie, regelt die API selbst. |
| 21.09.2026 | ulh prüft Programmdateien mit `openssl` (Ed25519 über `ngc/release/v1`, Null-Byte, Nutzlast) und `sha256sum`; dafür installiert es `openssl` und `jq` aus den Paketquellen. |
| 21.09.2026 | Das Protokoll blättert mit Zeit und ID (Hex aus Kleinbuchstaben), damit bei gleicher Zeit kein Eintrag verloren geht. |
| 21.09.2026 | Aus der Prüfung von Plan 0, noch nicht im Protokoll: Der Passwortschlüssel entsteht nur mit Argon2id (kein PBKDF2-Ausweg). Die Grenzen für Passkey-Beweise (`MaxPublic`, `MaxSig`, `MaxRSABits`, `MaxAuthData`, `MaxClientData`) gelten auch im Browser. |
| 21.09.2026 | Kanalnummern: Ein CLOSE wird immer mit CLOSE beantwortet; eine Nummer ist erst wieder frei, wenn CLOSE gesendet und empfangen wurde. Was nach dem eigenen CLOSE für den Kanal noch ankommt, wird verworfen. |
| 21.09.2026 | „Passwort zusätzlich“ prüft die API bei der Anmeldung. Controller verlassen sich auf den Passkey-Beweis im Client-Zertifikat; ein Passwortbeweis geht nicht an die Controller. |
| 21.09.2026 | Vertragsnummern: Zwischen Browser und API steht die Nummer im Pfad (`/api/v1/…`). Neue oder geänderte Felder gibt es nur mit neuer Nummer; die API bedient die aktuelle und die vorige Nummer nebeneinander. Im Tunnel nennt `Hello.Contract` die Nummer, die neuere Seite spricht die Sprache der älteren. |
| 21.09.2026 | `ngc-store` packt und prüft Releases, signiert aber nur Test-Releases mit einer Schlüsseldatei. Der echte Release-Schlüssel bleibt auf dem Hardware-Stick; mit welchem Werkzeug der Stick signiert, wird vor dem ersten Release entschieden. |
| 22.09.2026 | Leere Listen als Ergebnis sendet die API als `[]`, nie als `null`. |
| 22.09.2026 | `hello`, `PairRequest`, `JoinRequest` und die Kopplungs- und Beitrittscodes bleiben über Vertragsnummern hinweg gleich; eine Änderung bekommt einen neuen Nachrichtentyp oder ein neues Präfix. |
| 22.09.2026 | Migrationen: Die API mit der Update-Sperre wählt unter den verbundenen Controllern mit Rolle `db` den mit der kleinsten Node-ID; fällt er aus, den nächsten. |
| 22.09.2026 | ulh installiert nie ein älteres Release als das installierte; dasselbe darf zur Reparatur erneut installiert werden. Das Prüfskript prüft genau die gelisteten Dateien seines Teils und installiert nur diese; eine leere Liste ist ein Fehler. |
| 22.09.2026 | Oberflächen-Pakete (`ui/`) liest die Shell nur in der kanonischen Form, die NGCs Packer schreibt: USTAR, nur Dateien und Ordner, PAX nur mit `path`, feste Tiefen- und Anzahlgrenzen. Alles andere wird abgelehnt, damit Browser und Go genau dasselbe sehen. |
| 22.09.2026 | Kataloge: Das Budget begrenzt auch die Summe der Versionen über alle Apps, damit ein signierter Katalog wenig Speicher kostet. |
| 22.09.2026 | Vorgabe Thomas: Das Repo folgt dem Projektstruktur-Standard. Die Teile liegen unter `apps/`: `web`, `server-api`, `server-controller`, `server-db` und `cli` (das Werkzeug `ngc-store`, gehört zur API). Die Verträge liegen in `shared/` (vorher `contracts/`, Importpfad `github.com/sorglos-it/nextgencloud/shared/…`), Skripte in `tools/` (vorher `scripts/`), der Prototyp in `docs/prototype.html`. Ein Go-Teil hat direkt nur `main.go`, `VERSION` und `CLAUDE.md`, die Logik liegt in `classes/`. Go bleibt ein Modul im Stamm, deshalb bleibt `vendor/` dort: Go verlangt es neben `go.mod` (einzige Ausnahme vom Standard). Webroot ist nur `apps/web/public/`, Fremdcode für den Browser liegt in `apps/web/public/assets/vendor/`, Browser-Tests in `apps/web/tests/`, der Gesamttest kommt als Skript nach `tools/`. Die mitgelieferten Apps haben keinen eigenen Ordner: ihre Oberflächen baut das Webinterface, den Terminal-Dienst der Controller. |
| 22.09.2026 | Nach der zweiten Abschlussprüfung von Plan 0: Kanäle zu `ngc.store/controller/*` mit Aufgabe öffnet die Shell (`core.ShellOpens`), `stage` und `ngc.users/controller/trust` nur die API ohne Benutzer. Auf einem weitergeleiteten `transfer`-Kanal (`activate`) schickt die Shell nur EOF; Kopf und Datei schreibt die API auf der Tunnel-Strecke, dort gilt `MaxIn`. Die API gibt ihre erste GRANT an die Shell erst nach der des Controllers. |
| 22.09.2026 | Jeder API-Server steht mit seiner Tunnel-Adresse in `api.json` (`url`, aus ulh `hostname`). Ein neuer API-Server meldet sich mit Adresse und TLS-Fingerabdruck (`tunnel.JoinRequest`, im MAC); die Shell zeigt wartende API-Server (`ngc.users/pending-api`) mit einem Fingerabdruck über alle Werte (`tunnel.JoinFingerprint`), den der Admin mit der Ausgabe von ulh vergleicht. |
| 22.09.2026 | Ersteinrichtung: Der Einmal-Code geht nie über die Leitung. Die API schickt ihr Angebot (`session.SetupOffer`) mit einem MAC über die genauen Bytes, die Shell Fassung 1 mit einem MAC über deren genaue Bytes, beide mit dem Code als Schlüssel. |
| 22.09.2026 | Jede interne TLS-Strecke prüft ihre Gegenseite, nirgends wird die Prüfung abgeschaltet. MariaDB: eine CA je Installation (ngc-db), als PEM in `db.ca` und `migrate.ca`; ihr privater Schlüssel kommt nie in answer.yaml, Konfiguration oder Repo. API-Server: SHA-256 ihres TLS-Schlüssels (`tls`) in Vertrauensliste, Kopplungs- und Beitrittscodes, `controller.json` und `web.json`; Controller und Apache vertrauen nur ihm (`tunnel.PinnedTLS`, `config.APITrustFile`). Bevor der Controller des Webservers gekoppelt ist, kommen die Fingerabdrücke aus ulh (`api_pins`). |
| 22.09.2026 | Tunnel und Sitzungs-WebSocket: Noise prüft beide Enden; TLS darum trägt nur durch Firewalls und Proxys und wird trotzdem geprüft. „Keine Prüfung abschalten“ heißt: keine Verbindung ohne Prüfung; Go pinnt nur mit `InsecureSkipVerify` zusammen mit `VerifyConnection`, beides setzt `tunnel.PinnedTLS`. |
| 22.09.2026 | Die API gibt bei der Ersteinrichtung den Release-Schlüssel mit (`SetupInfo.releaseKey`); Fassung 1 trägt Dauerfreigaben nur für `ngc-web`, `ngc-api`, `ngc-controller` mit diesem Schlüssel und für `system`. Controller prüfen Releases nur mit ihrem eigenen Release-Schlüssel. |
| 22.09.2026 | `ngc.store/updates` hat eine Zeile je Ziel und Controller, mit der Prüfsumme der dort bereitgelegten Datei (Update per Klick) und dem Schlüssel für Dauerfreigaben. |
| 22.09.2026 | Was die Shell unterschreibt oder vergleicht, rechnet sie selbst aus: Prüfsumme, Manifest und UI-Prüfsumme eines hochgeladenen Pakets, Fingerabdrücke wartender Controller und API-Server. Werte der API dienen nur Anzeige und Abgleich. Zwei Ausnahmen: Die Prüfsumme eines Updates per Klick darf aus der Zeile der API kommen, weil der Controller nur eine Datei installiert, die zur unterschriebenen Aufgabe und zum signierten Release oder Katalogeintrag passt; ein neuer API-Server wird mit den Werten aus `ngc.users/pending-api` eingetragen, weil der Admin den Fingerabdruck, den die Shell über genau diese Werte rechnet, mit der Ausgabe von ulh vergleicht. Schlüssel für Dauerfreigaben kommen nie von der API, sondern aus der geprüften Vertrauensliste. |
| 22.09.2026 | Jede Oberfläche hat eine signierte Prüfsumme über ihr kanonisches ui-tar (`store.PackageUI`): im Katalog je Version (Store) und in `activate` (Admin). Der Controller vergleicht sie beim Aktivieren und Installieren, die Shell prüft das ui-tar vor dem Ausführen gegen den Beweis aus `ngc.desktop/ui-proof` (bei Store-Apps der ganze Katalog, je Store und Fassung zwischengespeichert). So kann eine übernommene API keine Oberfläche unterschieben (4.5: „kann sich in keiner Stufe als Benutzer ausgeben“). Hochgeladene Apps brauchen nach dem Zurücksetzen oder Wechsel des freigebenden Admin-Schlüssels eine neue Aktivierung. |
| 22.09.2026 | HTTP vor dem WebSocket: Methoden und Statuscodes je Pfad, Fehler als CLOSE-JSON (`wire.Error`, `session.HTTPStatus`), neue Codes `E_LOGIN` und `E_LOCKED`; Cookie `ngc_session` mit `Path=/api/; Secure; HttpOnly; SameSite=Strict`, ohne Domain und Max-Age; `GET /api/v1/trust` nur mit Sitzung, 204, wenn nichts Neueres da ist. |
| 22.09.2026 | Signierte Dokumente, die bleiben: Release, Katalog und Lizenz bekommen ein neues Feld nur mit neuem Zweck (…/v2), in der Übergangszeit signiert der Herausgeber beide Fassungen, Leser nehmen die, die sie kennen. Manifeste: neue Felder nur mit `minNgc`. Vertrauensliste: neues Format nur mit neuer Vertragsnummer, wenn alle Controller sie sprechen. |
| 22.09.2026 | ngc-all koppelt seinen Controller nach der Ersteinrichtung mit einem Kopplungscode aus dem Browser (`configure`, `pair_code`). Ersetzt in 4.5 „koppelt das ulh-Skript automatisch“. |
| 22.09.2026 | Ein Ergebnis gilt nur, wenn sein Kanal mit Erfolg endet (CLOSE `ok`, bei Ende zu Ende das Ende des Controllers mit `ok`). |
| 22.09.2026 | Texte für Menschen prüft überall dieselbe Regel (`wire.ValidText`), auch die Meldung eines CLOSE. |
| 22.09.2026 | Einmalwerte in einer geteilten Datenbank räumt die API erst `MaxSkew` nach ihrem Ablauf weg. |
| 22.09.2026 | Die Wiederherstellung (Codes, Zurücksetzen durch einen Admin, Notfallbefehl) kommt wie entschieden nach der Freigabe der Verträge, aber vor den Plänen 1 bis 4. Sie braucht eine eigene Schlüsselart oder eine Ausnahme für Rollen nur mit Passkey, weil die Vertrauensliste dort Passwortschlüssel ablehnt. |
| 22.09.2026 | Kataloge dürfen zusammen höchstens 30.000 Versionen führen (`store.MaxCatalogVersions`, vorher 50.000): Mit der UI-Prüfsumme je Version kostet ein Katalog sonst beim Lesen über 32 MiB. Ein gültiger Katalog passt ohnehin nur mit rund 28.900 Versionen in `sign.MaxPayload`. |
| 22.09.2026 | Welche Zeichen in Texten für Menschen gelten, legen die Unicode-Tabellen von Go fest (Unicode 17.0 mit Go 1.27); die Shell prüft dieselbe Regel Codepunkt für Codepunkt, unbekannte Zeichen gelten als nicht druckbar. Ein Go-Update mit neuen Unicode-Tabellen ist eine Vertragsänderung; `TestValidText` hält es an, bis Doku und Testdaten nachgezogen sind. |
| 22.09.2026 | Die Oberfläche einer Store-App belegt jeder Store der Vertrauensliste, dessen Katalog App, Version und UI-Prüfsumme führt. Eine feste Bindung an den Store über die Aktivierung des Admins ist verworfen: Nach jedem Wechsel des Admin-Schlüssels (etwa ein neues Passwort) müssten sonst alle Apps neu aktiviert werden. Die Grenze steht in 4.5, eine bessere Bindung kommt mit Plan 7. |
| 22.09.2026 | Welche signierte Version einer Oberfläche die Shell ausführt, entscheidet die API; eine unsignierte kann sie nicht unterschieben. Eine Sperre gegen ältere Versionen in der Shell ist verworfen: Sie würde die Version aussperren, auf die der Controller nach einem fehlgeschlagenen Update zurücksetzt. |
| 22.09.2026 | Updates: Gespeichert wird je Ziel nur die Einstellung (`update.<ziel>`: Modus und Zeitfenster); den Stand je Server rechnet die API aus Releases, Katalogen und den Meldungen der Controller aus. |
| 22.09.2026 | Das TLS-Zertifikat einer API ist selbst signiert und 100 Jahre gültig; es trägt kein Vertrauen, das trägt der gepinnte Schlüssel. Erneuert wird es nur mit demselben Schlüssel, ein neuer Schlüssel kommt nur mit neuem Fingerabdruck in der Vertrauensliste. Der Controller mit Rolle `web` vergleicht bei jedem Tunnelaufbau und täglich das Zertifikat, das er über `tunnel.PinnedTLS` bekommt, mit dem in `config.APITrustFile`; weicht es ab (der Schlüssel passt, das prüft der Pin), schreibt er die Datei neu und lädt Apache neu. Für die Installation der API gibt es `store_key` neben `store_url`, beide vorbelegt mit dem offiziellen NGC-Store. |
| 22.09.2026 | Nach der Prüfung von Fixplan 2: Hosts und Adressen haben eine Grammatik und genau eine Schreibweise (`trust.ValidHost`, `trust.ValidHostPort`, `trust.ValidTunnelURL`, `trust.ValidWebURL`, `trust.ValidOrigin`, `trust.ValidStoreURL`): Tunnel `wss://<host>:<port>/tunnel`, hinter Apache `https://<host>:<port>` (abgeleitet mit `trust.WebURL`), Datenbankknoten `<host>:<port>`, Stores `https://<host>[:<port>][/<pfad>]`, Passkey-Herkunft `https://<host>[:<port>]` ohne den Standard-Port 443 (den schreibt kein Browser mit), dazu Paket-, Release- und Shop-URLs des Katalogs `https://<host>[:<port>][/<pfad>][?<abfrage>]` (signierte Download-Links brauchen die Abfrage). Host als DNS-Name in Kleinbuchstaben, IPv4 oder IPv6 nach RFC 5952; alles ASCII, höchstens 256 Byte, bei den Katalog-URLs 512; `trust.ValidHostPort` trägt keine eigene Grenze (bis 259 Byte), deshalb begrenzt `config.MaxField` einen Datenbankknoten. Sie gilt in Vertrauensliste, Codes, Beitrittsanfragen, allen Konfigurationen und den ulh-Skripten (`testdata/ulh/host.sh`, dort ohne IPv6: ein IPv6-Netz erreicht man über den Hostnamen). Grund: Die Werte landen in Konfigurationen anderer Programme. API-Knoten heißen wie ihre Knoten-ID, auch in den Testdaten. |
| 22.09.2026 | `core.DesktopApp.entry` nennt die Seite, mit der die Shell eine App-Oberfläche startet (aus `ui.entry` des Manifests). Sie ist nicht signiert und wählt nur unter den Dateien des geprüften ui-tar. |
| 22.09.2026 | Lizenzen prüfen die Controller selbst (`store.VerifyLicenseFor`: Unterschrift des Stores der App, Installation, App, Form); nur die API prüft den Schlüssel der Installation und die Zahl der Benutzer. Erneuerte Lizenzen bringt die API ohne Benutzer über `ngc.store/controller/license`; ohne Benutzer öffnet sie damit vier Kern-Aktionen (ersetzt „nur drei“ vom 21.09.2026). Nach Ablauf plus Übergangsfrist stoppt der Controller die Dienste, startet keine, lehnt Kanäle mit `E_LICENSE` ab und meldet `unlicensed`; mit einer gültigen Lizenz startet er sie wieder. |
| 22.09.2026 | Ein Update bekommt nie mehr Rechte als beim Aktivieren (`store.Granted` mit `dbapi.App.Grants`) und kommt nur aus dem Katalog des Stores, aus dem die App aktiviert wurde (`dbapi.App.Store`). Eine Zeile mit mehr Rechten steht auf `needsgrants` (`store.UpdateNeedsGrants`) und wird weder per Klick noch automatisch installiert; der Admin aktiviert die neue Version mit den neuen Rechten. Aktivieren einer installierten App ersetzt sie durch dieselbe oder eine neuere Version, nie eine ältere, und behält die Daten. |
| 22.09.2026 | `minNgc` vergleicht `store.SupportsMinNGC`; eine NGC-Version, die keine Release-Version ist (etwa `0.0.0-dev`), gilt als älter als jedes `minNgc`. |
| 22.09.2026 | `ngc.users/nodes` listet die Controller der Vertrauensliste mit Rollen, Online-Zustand, NGC-Version und Architektur aus ihrem `hello` (nicht signiert), damit der Admin das Ziel einer Aktivierung wählt; eine App mit Diensten aktiviert der Controller nur mit Rolle `services`. |
| 22.09.2026 | Apaches Lastverteilung steht in `config.BalancerFile` (`/etc/ngc/apache-apis.conf`); sie schreiben nur ngc-web bei der Installation und danach der Controller mit Rolle `web`, beide aus `web.json`. Die Form legt der Plan des Webinterface als Vorlage in `shared/ulh.md` fest. |
| 22.09.2026 | ngc-api zeigt beim Beitritt den Anker des Beitrittscodes wie ngc-controller beim Koppeln. Nach der Ersteinrichtung koppelt man zuerst den Controller des Webservers: Erst er schreibt `trust-anchor.json`. |
| 22.09.2026 | Vorgabe Thomas: Die Grenze der Ersteinrichtung (Angreifer im selben Netz beim ersten Aufruf ohne vertrauenswürdiges Zertifikat) wird dokumentiert, nicht technisch erzwungen. Empfehlung im Entwurf: in einem Netz einrichten, dem man traut, oder gleich mit einem vertrauenswürdigen Zertifikat (ngc-web bietet Let's Encrypt an). |
| 22.09.2026 | Die Module aus Kapitel 7 sind übernommen. Mit allem, was sie mitbringen, liegen 21 Module und rund 20 MB in `vendor/`; jede Lizenz ist mit der AGPL-3.0 vereinbar, `govulncheck` meldet keine erreichbare Lücke. |
| 23.09.2026 | Vorgabe Thomas: Der Recovery-Modus steht in `docs/recovery-modus.md` und ist verbindlich: Root-Sticks mit Quorum, TPM, verschlüsseltes Datenbank-Volume (LUKS2), signierte Admin-Datensätze, Stufen enterprise, home und test. Er ersetzt den geplanten Vertrag für die Wiederherstellung und die Entscheidung vom 22.09.2026 über einen Notfall-Code aus der Ersteinrichtung. |
| 23.09.2026 | Freigabe Thomas: Vertragsänderung V1.1 — `session.LoginChallenge` nennt je Passkey `credId` **und** `kid`. Ohne die Schlüsselkennung kann ein Browser, der die Vertrauensliste noch nie gesehen hat, den Nachweis nicht bauen; Rollen mit `passkeyOnly` wären ausgesperrt. Gefunden beim Schreiben von Plan 1. |
| 23.09.2026 | Nach einem Neustart startet **der Controller** die Container, nicht systemd. Entwurf 4.3 sagte das Gegenteil; die Verträge kennen keine systemd-Einheit je App, und `tunnel.Status` erwartet den Zustand vom Controller. Der Entwurfstext ist überholt. |
| 23.09.2026 | Entwurf 4.6 zählt Rollen, Rechte, Schlüssel, Vertrauensliste, Stores, Lizenzen und Einstellungen als eigene Tabellen auf; im Vertrag V4 sind das Dokumente (`BlobRights`, `BlobTrust`, `BlobCatalog`, `BlobLicense`, `BlobSetting`). Es gilt der Vertrag. |
| 23.09.2026 | Entwurf 4.8 „NGC-Updates als eigener signierter Katalog“ ist überholt: dafür gibt es `store.Release` mit eigenem Zweck. Es gilt der Vertrag. |
| 23.09.2026 | Offen bis zur nächsten Vertragsänderung: Entwurf 4.2 verspricht, ein neuer API-Server bekomme die gemeinsamen Schlüssel beim Koppeln von einer laufenden API; die Verträge geben dafür weder Prolog in `noisep` noch Zweck in `sign` noch ein `config.SaveAPI` her. Solange das offen ist, wird nur **ein** API-Server gebaut. |
| 23.09.2026 | Offen für den Sicherheitsdurchgang: App-Fenster laufen auf demselben Ursprung wie die Shell; der Plan des Webinterface bettet App-Dateien als `data:`-Adressen ein und muss dafür `script-src 'self' data:` erlauben. Ein zweiter Name (`apps.<hostname>`) wäre sauberer, berührt aber `ngc-web.sh` und `config.Web`. |
| 23.09.2026 | Offen aus Plan 4: Wer spielt bei der Installation die erste Migration ein, und wie startet der Controller die Migrationen eines Updates? Beides berührt `shared/ulh.md` und den Teil API und wird mit der nächsten Vertragsänderung entschieden. |
| 23.09.2026 | Vorgabe Thomas: Der Recovery-Modus läuft als eigener Strang parallel zu den vier Teilen — eigene Sitzung, Branch `recovery`, Start mit Arbeitspaket 0 in Stufe test (QEMU, swtpm, SoftHSM2, keine Hardware nötig). |
| 23.09.2026 | Offen an der Anbindung: NGC ist in Go, deshalb kommt `AdminVerify` nach Go und `server-recovery` als eigenes Teilprogramm in Rust; `lib/` als zentrale Bibliothek gibt es in NGC noch nicht. |
| 22.09.2026 | Solange kein Teil die neuen Module einbindet, hält `tools/modules.go` (Bau-Markierung `modules`) sie in `go.mod` und `vendor/`; `tools/check.sh` baut die Datei für beide Ziele mit. Jede Zeile wandert in den Teil, der das Modul zuerst benutzt, die Datei verschwindet mit der letzten Zeile. |
| 23.09.2026 | Die API baut die Anmelde-Antwort nie selbst, sondern mit `session.Challenge` aus der Vertrauensliste (ohne Datenbank; nur `totp` reicht sie hinein). Für einen Namen, den die Liste nicht kennt, erfindet dieselbe Funktion eine Antwort, die von einer echten nicht zu unterscheiden ist: Form, Zahl der Passkeys, Längen der Credential-IDs und `kdf`-Parameter zieht sie aus der Liste, `totp` aus dem Anteil, den der Aufrufer nennt. Sonst sagt schon `login/start`, welche Benutzer es gibt – ohne Anmeldung und beliebig oft abfragbar. Beide Wege tun dieselbe Arbeit, weil sonst Laufzeit und Speicher verraten, was die Antwort verschweigt. |
| 23.09.2026 | Dafür gibt es `config.API.ChallengeKey` (32 Byte, auf allen API-Knoten gleich) und nicht `SecretKey`. Nicht wegen der Kryptografie – das Label trennt sauber –, sondern wegen des Lebenszyklus: Ein Wechsel des Datenbankschlüssels würfelte jede erfundene Antwort neu, während echte gleich blieben; wer davor und danach fragt, hätte alle Namen sortiert. |
| 23.09.2026 | Die Regel „neue oder geänderte Felder nur mit einer neuen Vertragsnummer" gilt ab der ersten Veröffentlichung einer Nummer. Solange eine Fassung nirgends läuft, ändert eine Vertragsänderung sie an Ort und Stelle; deshalb bleibt V1.1 unter `/api/v1/…`. |

## 7. Fremdcode-Kandidaten

Vor der Übernahme prüfen: Lizenz, Pflege, Größe. Jede Lizenz muss mit der AGPL-3.0 vereinbar
sein. Übernahme per `go mod vendor`, Herkunft in `THIRD-PARTY.md`.

| Zweck | Kandidat | Stand |
|---|---|---|
| SQLite ohne cgo, Verschlüsselung AES-256-XTS | `github.com/ncruces/go-sqlite3` mit `vfs/xts` | Übernommen am 22.09.2026: v0.35.5, MIT, reines Go, AES-256 bei 64-Byte-Schlüssel. Bringt SQLite selbst als WebAssembly (MIT-0) und julianday (MIT) mit. |
| MariaDB | `github.com/go-sql-driver/mysql` | Übernommen am 22.09.2026: v1.10.1, MPL-2.0. Keine seiner Dateien trägt den Vermerk „Incompatible With Secondary Licenses“, deshalb mit der AGPL-3.0 vereinbar. |
| WebSocket | `github.com/coder/websocket` | Übernommen am 22.09.2026: v1.8.15, ISC, ohne weitere Module. |
| Argon2id | `golang.org/x/crypto/argon2` | Übernommen in Plan 0: v0.57.0, BSD-3-Clause, in `shared/proof` |
| Passwortschlüssel im Browser | Argon2id aus einer kopierten, geprüften JavaScript-Umsetzung (Kandidat: `@noble/hashes`); kein PBKDF2-Ausweg (21.09.2026) | zu prüfen |
| TOTP | `github.com/pquerna/otp` | Übernommen am 22.09.2026: v1.5.0, Apache-2.0; bringt barcode (MIT) für den QR-Code der Einrichtung mit, zusammen 0,15 MB. Eigene Umsetzung nach RFC 6238 damit nicht nötig. |
| WebAuthn (Passkeys, FIDO2) | `github.com/go-webauthn/webauthn` | Übernommen am 22.09.2026: v0.18.2, BSD-3-Clause; bringt acht weitere Module mit (CBOR, TPM, JWT, Metadaten), alle vereinbar. Siehe `THIRD-PARTY.md`. |
| Noise-Tunnel | `github.com/flynn/noise` | Übernommen in Plan 0: v1.1.0, BSD-3-Clause, in `shared/noisep` |
| Browser-Tests (nur Tests, nicht Teil von NGC) | Playwright per npm in `apps/web/tests/` | erlaubt seit 19.09.2026 |
| Noise im Browser (Ende zu Ende) | JavaScript-Umsetzung ohne npm | zu prüfen; findet sich keine geprüfte, dann eigene Umsetzung des Ablaufs nach der Spezifikation, getestet mit den offiziellen Testvektoren. Die Kryptografie selbst kommt aus der Web Crypto API des Browsers. |
