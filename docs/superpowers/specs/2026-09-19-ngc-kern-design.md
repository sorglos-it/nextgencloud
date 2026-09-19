# NGC – Entwurf für den Kern

**Stand:** 19.09.2026 · **Status:** Abschnitte 1–4 mit Thomas abgestimmt, Abschnitt 5 offen
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
- NGC hat vier Teile: Webinterface, API, Datenbank (MariaDB, MySQL, MSSQL, Oracle, SQLite) und
  Server-Controller. Alle vier können auf einer Maschine laufen, müssen aber nicht. Das
  Webinterface spricht mit x APIs, die mit y Datenbanken arbeiten; die APIs können z
  Server-Controller haben.
- Endausbau, nicht der Anfang: Webinterface auf einem Webserver, API auf einem
  Application-Server, Datenbank auf einem Datenbankserver, auf jedem läuft ein Controller.
  Ein vierter Server bekommt die App LDAP/Active Directory zugewiesen, ein weiterer wird
  Fileserver und so weiter.

## 2. Grundsätze

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
  spricht (Abschnitt 4.3).
- Beispiele von Thomas: Beim Mail-Client läuft die Oberfläche im Browser. Erst beim Senden
  geht die Mail über die API zum Controller des Mailservers, der sie verschickt. Beim Terminal
  geht `cp -r` über API und Controller an den Dienst „virtuelles Terminal", die Ausgabe kommt
  denselben Weg zurück.

### Rollen der vier Teile

| Teil | Rolle |
|---|---|
| Webinterface | Liegt auf jedem Webserver, zum Beispiel Standard-Apache. HTML, CSS, JS und PHP; PHP nur für das Webinterface selbst. |
| API | Eigener, dauerhaft laufender Dienst. Vermittler mit zwei Seiten (Webinterface und Controller) und „die Firewall zwischen GUI und Server". Macht Anmeldung, Rollen und Rechte und die Weiterleitung. Führt nie App-Code aus. Mehrere APIs sind möglich, zum Beispiel für die Performance. |
| Server-Controller | Eigener, dauerhaft laufender Dienst auf jedem Server. Redet nur mit der API und tut, was sie sagt. Root-Aufgaben führt er nur mit Unterschrift des Benutzers aus (4.5). |
| Datenbank | Gehört zur API. |

### Apps laufen auf dem Client

App-Oberflächen bestehen nur aus HTML, CSS und JavaScript und laufen nur im Browser. Kein Code
aus einem Store läuft auf dem Webserver oder in der API. Was auf einem Server passieren muss,
erledigt ein Dienst der App über den Controller.

### Keine Abhängigkeiten

API, Controller und Werkzeuge sind je eine einzelne Go-Datei ohne Laufzeitumgebung. Fremdcode
wird ins Repo kopiert (`vendor/`), seine Herkunft steht in `THIRD-PARTY.md`. Das Webinterface
kommt ohne Framework und ohne Build-Schritt aus.

### Sprachen

API, Controller und Werkzeuge in **Go**. Webinterface in **HTML, CSS, JS und PHP**.

## 3. Teilprojekte

| # | Teilprojekt | Inhalt |
|---|---|---|
| 1 | **Kern** | Die Pipeline durchgängig auf einer Maschine: Desktop, API, SQLite, Controller, App-Modell, App-Store mit mehreren Quellen, Benutzer & Rechte, Sicherheitsmodell, Beispiel-Apps |
| 2 | **Verteilung** | Weitere Server mit ihren Controllern koppeln (Kopplungscode). Webinterface, API und Datenbank auf getrennten Servern. x APIs, y Datenbanken, z Controller. Dienste an Server zuweisen. |
| 3 | **Datenbanken** | MariaDB, MySQL, MSSQL und Oracle hinter derselben Schnittstelle wie SQLite |
| 4 | **Handy und Tablet** | Eine zweite Oberfläche für Touch, dieselben Apps im Vollbild |
| 5 | **Store-Server** | Serverseite des globalen Stores: Entwickler veröffentlichen Apps, Kunden bezahlen, der Server stellt Lizenzen aus |
| 6 | **NGC-Zertifizierungsstelle** | Eigene App: Zertifikate für Server und Geräte, PIV-Zertifikate für USB-/NFC-Sticks |
| 7 | **Apps** | Endausbau: LDAP/AD, Fileserver, Firewall … |

Der Kern kommt zuerst, weil er die Schnittstellen festlegt, an die alles andere andockt. Die
Reihenfolge von 2 bis 7 können wir noch ändern. Jedes Teilprojekt bekommt einen eigenen
Entwurf, einen Plan und wird dann gebaut.

## 4. Der Kern

### 4.1 Erfolgskriterium

Der Kern ist fertig, wenn Folgendes geht:

1. NGC installieren und als erster Admin mit Passkey anmelden.
2. Im App-Store die Terminal-App aktivieren. Das Aktivieren ist eine Root-Aufgabe und gelingt
   nur mit der Unterschrift des Admins, die der Controller prüft.
3. Im Terminal-Fenster `cp -r` eintippen. Der Befehl läuft über API und Controller zum Dienst
   „virtuelles Terminal", die Ausgabe kommt zurück.
4. Ein Benutzer ohne das Recht sieht die Terminal-App gar nicht.
5. Eine App aus einem Test-Store installieren und eine Kauf-App mit einer Test-Lizenz
   freischalten.

### 4.2 Überblick (Abschnitt 1, abgestimmt)

```
Browser ──HTTPS──► Apache: web/  (HTML, CSS, JS, PHP)
   │
   └── WebSocket über /api/ ──► ngc-api ◄── WebSocket ── ngc-controller ── stdin/stdout ──► Dienst
                                  │                      (baut die Verbindung
                                SQLite                    selbst auf)
```

Apache reicht `/api/` an die API weiter. Der Browser kennt damit nur den Webserver, und die
API muss nicht offen im Netz stehen.

| Teil | Ort im Repo | Aufgabe |
|---|---|---|
| Webinterface | `web/` | Desktop aus dem Prototyp (Fenster, Taskleiste, Startmenü). Jede App läuft in einem abgeschotteten Fenster. PHP liefert die Desktop-Seite aus und verwaltet die Liste der APIs. |
| API | `cmd/ngc-api` | Anmeldung, Sitzungen und Sitzungszertifikate, Rollen und Rechte, Weiterleitung zum Controller, Apps, Stores, Lizenzen, Protokoll |
| Controller | `cmd/ngc-controller` | Startet Dienste, reicht Daten durch, prüft Root-Aufgaben |
| Store-Werkzeug | `cmd/ngc-store` | Packt Apps, baut und signiert Store-Kataloge |
| Apps | `apps/` | Fest dabei: **App-Store** und **Benutzer & Rechte**. Zum Aktivieren: **Rechner** aus dem Prototyp (nur Oberfläche) und **Terminal** (Oberfläche und Dienst). |
| Fremdcode | `vendor/`, `THIRD-PARTY.md` | Kopiert und mit Herkunft notiert, Kandidaten in Kapitel 7 |
| Prototyp | `prototype/` | Unverändert als Vorlage für den Desktop |

**Regel für mehrere APIs:** Sitzungen und alles Wichtige liegen in der Datenbank, nicht im
Speicher einer einzelnen API. Jede API muss jede andere ersetzen können. Gebaut wird das erst
im Teilprojekt Verteilung, aber der Kern darf es nicht verhindern.

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
4. Der Controller startet den Dienst.

Kanalnummern gelten immer nur für eine Verbindung. Der Browser erfährt nie, auf welchem Server
ein Dienst läuft, und der Dienst bekommt die Sitzung des Browsers nie zu sehen.

**Schnittstelle zum Dienst.** Für jeden Kanal startet der Controller das Programm des Dienstes.

- Daten aus dem Kanal kommen auf stdin an, was das Programm auf stdout schreibt, geht zurück,
  stderr landet im Log. Exit-Code 0 heißt Erfolg.
- Wer fragt, steht in den Umgebungsvariablen `NGC_USER`, `NGC_APP` und `NGC_ACTION`.
- Das beherrscht jede Programmiersprache, auch ein Bash-Skript.
- Jeder Dienst läuft unter einem eigenen Systembenutzer. Root-Rechte bekommt er nur, wenn die
  App das im Manifest angibt und der Admin es beim Aktivieren erlaubt.
- Was in den Daten steht, regeln App und Dienst unter sich, beim Terminal zum Beispiel
  Tastendrücke und Fenstergröße.

**Übertragung.** Zwischen den Stationen läuft ein WebSocket mit binären Nachrichten: ein kurzer
Kopf mit Art und Kanalnummer, dahinter die Daten ohne Umweg über Base64. Es gibt fünf Arten:
öffnen, Daten, Ende der Eingabe, schließen (mit Erfolg oder Fehler) und Freigabe. Die Freigabe
regelt die Menge: Ein Sender schickt pro Kanal nur so viel, wie der Empfänger freigegeben hat.
So kann eine große Datei die API nicht überlasten. Wie die Leitungen zusätzlich verschlüsselt
sind, steht in 4.5.

**Nicht im Kern:** Dienste, die dauerhaft laufen und über einen Socket angebunden sind. Die
braucht man erst, wenn es zu langsam wäre, für jeden Kanal ein Programm zu starten.

### 4.4 Apps und App-Store (Abschnitt 3, abgestimmt)

**App-Paket.**

```
org.sorglos.terminal/
├── manifest.json        Name, Version, Aktionen, Dienste
├── ui/                  nur HTML, CSS, JS und Bilder
└── services/terminal/   Programm des Dienstes, je Plattform oder als Skript
```

Im Manifest meldet die App ihre **Aktionen** an, beim Terminal etwa `shell` für „Befehle auf
dem Server ausführen". Aus den Aktionen werden die Rechte, die ein Admin an Rollen vergibt.
Dazu hat jede App das Grundrecht **„benutzen"**; wer es nicht hat, sieht die App nicht. Das
Manifest sagt außerdem, welches Programm ein Dienst startet und ob er root-Rechte braucht.

**Quellen.** Der App-Store verwaltet mehrere Quellen gleichzeitig:

- **Globaler Store:** unser Store, ab Werk eingetragen, abschaltbar für Netze ohne Internet.
  Dort stellen wir Apps bereit, gratis oder zum Kauf.
- **Weitere Stores:** per Adresse eingetragen, zum Beispiel ein firmeninterner Store oder der
  Store eines anderen Anbieters.
- **Eigene Apps:** Ein Administrator lädt ein App-Paket hoch, das nur in dieser Installation
  gilt.

**Was ein Store ist.** Für Gratis-Apps reichen Dateien auf einem beliebigen Webserver: ein
Katalog mit Apps, Versionen und Preisen sowie die Pakete. Der Store signiert seinen Katalog,
und im Katalog steht zu jedem Paket eine Prüfsumme. So fällt jede Manipulation auf.

- Den Schlüssel des globalen Stores bringt NGC mit. Bei jedem weiteren Store bestätigt der
  Admin dessen Schlüssel einmal beim Eintragen.
- Eigene Apps haben keine Signatur und lassen sich nur mit ausdrücklicher Bestätigung
  installieren.
- Mit `ngc-store` kann jeder Apps packen und einen signierten Katalog erzeugen, also einen
  eigenen Store aufsetzen.
- Wer Apps verkaufen will, braucht zusätzlich einen Store-Server (Teilprojekt 5). Der stellt
  Lizenzen aus und gibt bezahlte Pakete nur an Käufer heraus.

**Gratis und Kauf.** Im Katalog steht für jede App: gratis, Kauf oder Abo, jeweils pro Site
oder pro Benutzer. Bezahlt wird auf der Webseite des Stores, nie in NGC, deshalb sieht NGC keine
Zahlungsdaten. Danach stellt der Store eine signierte Lizenz für diese Installation aus. NGC
prüft sie selbst, auch ohne Internet:

- **Pro Benutzer:** Nur so viele Benutzer bekommen das Recht „benutzen", wie die Lizenz
  erlaubt.
- **Abo:** Die Lizenz wird regelmäßig beim Store erneuert. Ist der Store nicht erreichbar,
  läuft die App noch eine Übergangsfrist weiter. Danach wird sie deaktiviert, aber nicht
  gelöscht.

Wer NGC selbst umbaut, kann die Lizenzprüfung umgehen. Das lässt sich bei offener Software
nicht verhindern.

Bis der Store-Server fertig ist, besteht der globale Store nur aus einem signierten Katalog mit
Gratis-Apps. Kauf-Apps testet der Kern mit einem Test-Store und Test-Lizenzen.

**Aktivieren.** Die API prüft das Manifest und trägt die App in die Datenbank ein. Die
Programme der Dienste schickt sie über die Pipeline an den Controller, der für jede App einen
eigenen Systembenutzer anlegt. Weil das root-Rechte braucht, ist das Aktivieren eine
Root-Aufgabe mit Unterschrift des Admins (4.5). Rechte an einer neuen App hat zuerst nur die
Rolle Administrator, alle anderen sehen die App erst nach der Freigabe.

**Deaktivieren.** Die App verschwindet aus allen Startmenüs, ihre offenen Kanäle werden
geschlossen. Gelöscht wird nichts.

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

**Fest im Kern:** App-Store und Benutzer & Rechte. Beide sind immer aktiv und nur für
Administratoren sichtbar.

### 4.5 Sicherheit (Abschnitt 4, abgestimmt)

**Vorgaben von Thomas:** AES-256 symmetrisch beziehungsweise asymmetrisch, wo nötig. Passkeys
sind Pflicht, YubiKey ebenfalls. Root-Aufgaben aus dem Webinterface müssen vom Benutzer
signiert sein, mit einem eigenen Zertifikat pro Benutzer, das nur für die Sitzungsdauer gilt.

**Ersteinrichtung.** Der Installer zeigt einen Einmal-Code an. Nur mit diesem Code lässt sich
das erste Admin-Konto anlegen. So kann niemand im Netz eine frisch installierte Instanz
übernehmen.

**Anmeldung nur mit Passkey oder Sicherheitsschlüssel.**

- Jedes Konto braucht mindestens einen Schlüssel, besser zwei, etwa einen YubiKey und das
  Handy.
- Passkeys vom Handy, aus Windows Hello und aus Passwortmanagern funktionieren genauso wie
  Sicherheitsschlüssel.
- Pro Rolle kann zusätzlich ein Passwort verlangt werden. Gespeichert wird nur ein
  Argon2id-Hash. Nach Fehlversuchen werden weitere Versuche verzögert und das Konto kurz
  gesperrt.
- Kein TOTP: Es kann nichts unterschreiben, und das braucht das Sitzungszertifikat.
- Wer alle Schlüssel verliert: Wiederherstellungscodes, ein Admin setzt das Konto zurück, oder
  im Notfall ein Befehl direkt auf dem Server.

**USB-/NFC-Sticks mit PIN.** Thomas wollte Sticks, auf die der Admin ein Zertifikat schreibt
und die mit PIN als Single Sign-on gelten. Solche Sticks gibt es schon, zum Beispiel den
YubiKey 5 NFC oder den Nitrokey 3 NFC (Open Source), per USB und per NFC am Handy.

- **FIDO2 (im Kern):** dasselbe Verfahren wie Passkeys. Der Admin richtet den Stick für einen
  Benutzer ein und gibt ihn aus, Name und Schlüssel liegen dann auf dem Stick. Der Benutzer
  steckt ihn ein oder hält ihn ans Handy, gibt seine PIN ein und ist angemeldet, für alle Apps
  und Server von NGC.
- **PIV (mit der Zertifizierungsstelle, Teilprojekt 6):** Der Stick arbeitet als Smartcard,
  der Admin schreibt ein Zertifikat unserer Zertifizierungsstelle darauf. Das ist der Weg zu
  Single Sign-on über NGC hinaus, etwa für die Windows-Anmeldung mit der späteren
  LDAP/AD-App.

**Sitzung und Sitzungszertifikate.**

1. Der Benutzer meldet sich mit Passkey oder Stick an.
2. Der Browser erzeugt sein Schlüsselpaar, die API erzeugt ihres. Beide gelten nur für die
   Dauer der Sitzung. Den privaten Schlüssel des Browsers kann niemand auslesen, auch die Shell
   nicht.
3. Die API stellt zwei Zertifikate aus, eines für den öffentlichen Schlüssel des Clients und
   eines für ihren eigenen. Ihren öffentlichen Schlüssel schickt sie an den Client. Das
   Client-Zertifikat unterschreibt zusätzlich der Passkey. So bleibt die Kette bis zum
   Benutzer erhalten, und der Controller kann sie bei Root-Aufgaben prüfen.
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
  Sitzungsschlüssel. Dazu gehört auch das Installieren von App-Diensten.
- Der Controller prüft die ganze Kette selbst, bis zurück zum Passkey. Außerdem prüft er, ob
  der Benutzer auf diesem Server Root-Aufgaben erledigen darf.
- Diese Liste führt der Controller selbst. Nur ein Admin kann sie ändern, mit seinem Passkey.
- Ohne gültige Unterschrift läuft nichts als root. Die API allein kann Root-Aufgaben damit
  nicht fälschen.
- Jede unterschriebene Aufgabe kommt ins Protokoll. So lässt sich nachweisen, wer sie
  angeordnet hat.

**Rechte.** Die Shell blendet nur aus, was jemand nicht darf. Entscheiden tut die API, bei
jedem Kanal neu, anhand von Benutzer, Rolle, App, Aktion und Lizenz.

**Controller.** API und Controller weisen sich gegenseitig mit Schlüsseln aus. Im Kern koppelt
der Installer den lokalen Controller automatisch, weitere Server bekommen später einmalig
einen Kopplungscode. Der Controller startet nur Programme aktivierter Apps, jeweils unter dem
eigenen Systembenutzer der App. Er selbst läuft als root, weil er diese Benutzer anlegen muss.
Deshalb bleibt sein Code so klein wie möglich.

**Verschlüsselung.**

| Strecke oder Ort | Verfahren |
|---|---|
| Browser ↔ Apache | TLS 1.3, ausschließlich AES-256-GCM |
| Browser ↔ API | zusätzlich Sitzungsschlüssel und AES-256-GCM (siehe Sitzung, Schritt 4) |
| API ↔ Controller | Tunnel wie bei einem VPN nach dem Noise-Protokoll, auf dem auch WireGuard aufbaut: AES-256-GCM, beide Seiten prüfen gegenseitig ihre Schlüssel. Außen herum zusätzlich TLS, damit der Tunnel durch Firewalls und Proxys kommt. |
| Datenbank | SQLite-Datei komplett mit AES-256 verschlüsselt (XTS). Geheimnisse darin, etwa Zugangsschlüssel zu Stores, verschlüsselt die API zusätzlich mit einem eigenen Schlüssel, damit sie auch in einer anderen Datenbank oder in einer Sicherung geschützt bleiben. |
| Asymmetrisch | Passkeys, Sitzungszertifikate, Signaturen von Katalogen und Lizenzen, Schlüsselaustausch im Tunnel |

**Zertifikate für die Webseite.** Der Installer bietet drei Wege an:

- Let's Encrypt über `mod_md`, das in Apache schon eingebaut ist. Dafür braucht es einen
  öffentlichen Namen.
- Ein selbst signiertes Zertifikat. Mit Passkeys funktioniert es nur, wenn man es auf jedem
  Gerät als vertrauenswürdig einträgt, sonst verweigert der Browser den Passkey.
- Import eines Zertifikats von einer anderen Zertifizierungsstelle.

Die NGC-Zertifizierungsstelle wird eine eigene App (Teilprojekt 6).

**Folge für jede Installation:** Passkeys funktionieren nur, wenn NGC über einen Namen statt
einer IP-Adresse aufgerufen wird und der Browser dem Zertifikat vertraut. Der Installer fragt
deshalb nach dem Namen.

**Browser.** Das Webinterface läuft nur über HTTPS. Eine strenge Sicherheitsrichtlinie (CSP)
verbietet fremde Skripte und Code direkt in der HTML-Seite. Das eingebettete Skript aus dem
Prototyp kommt deshalb in eigene Dateien. Die Apps sind abgeschottet wie in 4.4.

**Protokoll.** Alle Anmeldungen und Fehlversuche, alle Änderungen an Benutzern, Rechten, Apps,
Stores und Lizenzen, jede Root-Aufgabe mit ihrer Unterschrift und jedes Öffnen eines Kanals
werden mit Zeit und Benutzer in der Datenbank festgehalten. Administratoren sehen das Protokoll
in der App Benutzer & Rechte.

**Grenzen.**

- Wer NGC selbst umbaut, kann die Lizenzprüfung umgehen (4.4).
- *Noch nicht mit Thomas besprochen:* Aufgaben ohne root laufen im Vertrauen auf die API. Wer
  die API übernimmt, kann solche Aufgaben im Namen von Benutzern auslösen und die Daten in der
  Pipeline sehen.
- *Noch nicht mit Thomas besprochen:* Wer den Webserver übernimmt, der die Shell ausliefert,
  kann einem angemeldeten Admin veränderten Code unterschieben und dessen Sitzungsschlüssel
  benutzen, solange die Sitzung offen ist. Dagegen helfen die strenge CSP, ein gehärteter
  Webserver, kurze Sitzungen und eventuell eine erneute Bestätigung per Passkey bei kritischen
  Aktionen (offene Frage 5).

### 4.6 Abschnitt 5 – noch nicht besprochen

Die folgenden Punkte sind **Vorschläge**, die Thomas noch nicht gesehen hat. In der nächsten
Sitzung werden sie als Abschnitt 5 vorgestellt und abgestimmt.

- **Datenbank:** SQLite, verschlüsselt wie in 4.5. Nur SQL, das alle fünf Systeme können,
  damit Teilprojekt 3 nichts umbauen muss. IDs erzeugt die Anwendung, Zeitstempel sind Zahlen.
  Tabellen: Benutzer, Rollen, Rechte, Schlüssel (Passkeys, Sticks), Sitzungen, Apps, Dienste,
  Controller, Root-Berechtigungen, Stores, Lizenzen, Einstellungen, Protokoll.
- **Installation:** Linux zuerst (Debian, Ubuntu, Raspberry Pi OS; amd64 und arm64), Windows
  später. Ein Installationsskript richtet ein: Apache mit PHP und den Modulen `proxy`,
  `proxy_http` und `proxy_wstunnel`, `web/` im Webroot, `ngc-api` und `ngc-controller` als
  systemd-Dienste, Schlüssel, Kopplung des lokalen Controllers, Zertifikat (drei Wege aus 4.5),
  am Ende der Einmal-Code.
- **Fehlerbehandlung:** Fehler laufen Station für Station zurück: Exit-Code des Dienstes →
  Controller → API → Shell → App. Ist ein Controller nicht erreichbar, zeigt die Shell die App
  als „nicht erreichbar". Bricht eine Verbindung ab, wird sie automatisch neu aufgebaut, offene
  Kanäle enden dabei. Jede Aktion hat ein Zeitlimit.
- **Tests:** Go-Tests für Protokoll, Rechte, Kryptografie und Datenbank. Ein Durchstich-Test
  startet API, Controller und einen Test-Dienst im selben Testlauf. Das Webinterface wird im
  Browser getestet. Offen ist, ob Testwerkzeuge wie Playwright per npm erlaubt sind (offene
  Frage 7).

## 5. Offene Fragen

1. Abschnitt 5 abstimmen (4.6).
2. Lizenz von NGC: MIT, AGPL oder etwas anderes? Wichtig für die Monetarisierung. `ngc.js`
   muss so lizenziert sein, dass es auch in bezahlten, geschlossenen Apps stecken darf.
3. Zielplattformen des Kerns: Vorschlag Linux zuerst, Windows später.
4. App-Updates aus den Stores schon im Kern oder später?
5. Kritische Aktionen zusätzlich mit einer erneuten Passkey-Bestätigung absichern?
6. Die zwei unbesprochenen Grenzen in 4.5: so akzeptieren oder weiter absichern?
7. Dürfen reine Entwicklungswerkzeuge (Testrunner, Browser-Tests) per Paketmanager kommen?
   Diese Frage ist aus der Regel „Software ohne Abhängigkeiten" noch offen.
8. Für Teilprojekt 2: Was heißt „mehrere APIs" genau: Lastverteilung, Ausfallsicherheit,
   mehrere Standorte?
9. Für Teilprojekt 4: Ist mit „Controller für Tablet und Handy" eine zweite Oberfläche gemeint?
   Das ist bisher nur angenommen.
10. Idee: Thomas' Projekt `ulh` (Skripte für alle Linux-Distributionen, `answer.yaml` für
    Läufe ohne Rückfragen) als Grundlage für Dienste, die Server einrichten. Prüfen in den
    Teilprojekten 2 und 7.

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

## 7. Fremdcode-Kandidaten

Vor der Übernahme prüfen: Lizenz, Pflege, Größe. Übernahme per `go mod vendor`, Herkunft in
`THIRD-PARTY.md`.

| Zweck | Kandidat | Stand |
|---|---|---|
| SQLite ohne cgo, Verschlüsselung AES-256-XTS | `github.com/ncruces/go-sqlite3` mit `vfs/xts` | Am 18.09.2026 geprüft: v0.35.5 vom 16.09.2026, AES-256 bei 64-Byte-Schlüssel, reines Go. Lizenz noch prüfen. |
| WebSocket | `github.com/coder/websocket` | zu prüfen |
| Argon2id | `golang.org/x/crypto/argon2` | zu prüfen |
| WebAuthn (Passkeys, FIDO2) | `github.com/go-webauthn/webauthn` | zu prüfen |
| Noise-Tunnel | `github.com/flynn/noise` | zu prüfen |
