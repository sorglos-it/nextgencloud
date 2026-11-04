# Wie eine NGC-App gebaut sein muss

Für den, der eine App für NGC schreibt. Nicht der Weg ins Paket und in den Laden — der steht in
[`app-weg.md`](app-weg.md) —, sondern **was NGC einer App zusagt und was es ihr verbietet**.

Jeder Satz hier ist am Code oder an der laufenden Anlage gemessen, und wo er es nicht ist, steht
es dabei. Der Abschnitt **„Nicht gemessen"** am Ende ist keine Höflichkeit: wer eine App auf eine
Zusage baut, die nie geprüft wurde, baut auf nichts.

---

## 0. Drei Gestalten — und was von ihnen wirklich geht

Drei Fälle sind genannt worden. Sie sind nicht drei Bauweisen, sondern **zwei Bausteine in drei
Mischungen**, und eine der drei geht anders, als man denkt.

| Gestalt | Was im Paket liegt | Geht das? |
|---|---|---|
| **Arbeits-App** — rechnet auf dem Server, zeigt das Ergebnis | `ui/` **und** `services/` | ja, der Normalfall (Vorbild: die Uhr) |
| **Reiner Dienst** — kein Fenster, arbeitet im Hintergrund | nur `services/` | ja |
| **Reine Oberfläche** — steuert einen **vorhandenen** Server | nur `ui/` | **so nicht.** Siehe unten |

### Die reine Oberfläche kann kein Netz

Eine App ohne `services/` ist erlaubt (`Manifest.Validate`), aber ihr Fenster **kann keinen
Server erreichen** — auch keinen, der nebendran im gleichen Haus steht. Gemessen am Code der
Shell (`apps/web/public/assets/js/shell/appframe.js`):

- Der Rahmen hat `sandbox="allow-scripts"`.
- Sein Regelsatz lautet `default-src 'none'; script-src data:; style-src data: 'unsafe-inline';
  img-src data:; font-src data:; media-src data:; connect-src 'none'; form-action 'none';
  base-uri 'none'` — und derselbe Satz steht auch im Kopf der Antwort (`Page::FRAME_CSP`), also
  zweimal, und der strengere gilt.
- `connect-src 'none'` heißt: kein `fetch`, kein `XMLHttpRequest`, kein WebSocket, zu
  **keinem** Ziel. `form-action 'none'` schließt den Umweg über ein Formular.

**Wer einen vorhandenen Server steuern will, baut es so:**

```
ui/         die Oberfläche
services/   ein kleiner Dienst mit "needs": ["network"], der mit jenem Server spricht
```

Die Oberfläche ruft ihren **eigenen** Dienst (`ngc.call`), der Dienst spricht mit dem fremden
Server. Das ist nicht ein Umweg, den man in Kauf nimmt — das ist die Stelle, an der die Anlage
überhaupt sehen kann, wohin eine App telefoniert: `needs: ["network"]` steht im Manifest, der
Verwalter erlaubt es beim Einrichten, und ohne die Erlaubnis läuft der Behälter mit
`--network none`.

**Abgewogen, nicht gemessen:** eine App, die nur eine Oberfläche ist und trotzdem ans Netz will,
gibt es in NGC bewusst nicht. Sie wäre eine Webseite, die im Schreibtisch hängt, und niemand
könnte ihr ansehen, mit wem sie redet.

---

## 1. Was im Paket liegen darf

Ein Paket (`*.ngc`) ist ein tar, und es nimmt **genau** diese Namen (`shared/store/pack.go`,
`store.Walk`):

```
manifest.json          zuerst im Archiv, immer
ui/…                   die Oberfläche, eine Datei oder viele
services/amd64.tar     das Abbild des Behälters, je Architektur
services/arm64.tar
```

Alles andere ist `store: invalid app package`. Links, Geräte und Sparse-Dateien fliegen heraus.
Eines von `ui/` und `services/` muss da sein, beides muss nicht.

---

## 2. Das Manifest ist streng

`manifest.json`, höchstens 64 KiB, **strenges** JSON: ein unbekanntes, doppeltes oder anders
geschriebenes Feld ist ein **Fehler**, kein Hinweis (`enc.Budget`, `store.ParseManifest`). Ein
Tippfehler in einem Feldnamen wird nicht überlesen.

Die Felder stehen vollständig in [`app-weg.md`](app-weg.md). Für das Bauen sind vier wichtig:

- **`id`** — darf **nicht mit `ngc.` anfangen.** Gesperrt ist der ganze Namensraum, nicht eine
  Liste von Namen: `wire.CoreApp` ist `strings.HasPrefix(app, "ngc.")`. (`app-weg.md` nennt drei
  Namen — das sind Beispiele, nicht die Regel.)
- **`ui.entry`** — ein Pfad **unter `ui/`**, ohne `ui/` davor. `ui.width`/`height` je 0 oder
  200–4000.
- **`services[].command[0]`** — ein **absoluter** Pfad im Behälter.
- **`actions[].service`** — muss einen Dienst **dieses** Manifests nennen.

---

## 3. Die Oberfläche: drei Dinge, die man einmal falsch macht

Die Oberfläche wird **nicht in den Rahmen geladen, sie wird hineingeschrieben.** Die Shell holt
das `ui`-tar, prüft seine Prüfsumme gegen einen unterschriebenen Beleg, und baut das Dokument aus
diesen Dateien und aus keinen anderen: `<link>` und `<script>` werden durch die Bytes dahinter
ersetzt.

**1. Klassische Skripte, keine Module.** Der Rahmen macht keine Verbindung, und ein Modul wird
geholt. `<script type="module">` läuft dort **nie**; die Shell sagt „Diese App braucht klassische
Skripte" und zeigt die App nicht.

**2. `ngc.js` kommt von der Shell.** Sie schreibt die Bytes als erstes Skript in das Dokument.
Die App lädt sie nicht und **darf sie nicht mitliefern**.

**3. Relative, flache Adressen.** `href="app.css"`, `src="app.js"` — Dateien im `ui/`-Ordner.
`store.ValidPath` erlaubt keine `..`, keine führenden Schrägstriche, keine versteckten Namen.

### Wie die App mit ihrem Dienst spricht

Nur über `ngc.js`, und nur mit ihren **eigenen** Diensten:

```js
const bytes = await ngc.call("uhr", "zeit");   // Promise<Uint8Array>
const strom = ngc.open("uhr", "shell");        // bleibt offen: write, end, onData, onClose
```

`ngc.call` liefert ein **`Uint8Array`** — die rohen Bytes von `stdout` des Dienstes, unverändert.
Das Umwandeln ist Sache der App (`new TextDecoder().decode(bytes)`) — **und das Misstrauen auch:
alles davon gehört auf `textContent`, nie auf `innerHTML`.**

Eine Aktion, die das Manifest nicht trägt, lehnt die Shell ab, **bevor** sie jemanden fragt
(`appframe.doCall`, `CODES.FORBIDDEN`). Ein Tippfehler im Aktionsnamen ist eine Ablehnung, kein
Aufruf.

**Was der Rahmen nicht hat:** keine Verbindung, kein `<base>`, und keinen Speicher, auf den sich
eine App verlassen darf. Was eine App behalten will, behält ihr **Dienst** in `/data`.

---

## 4. Der Dienst ist ein Programm, das gerufen wird — kein Server, der lauscht

Das ist der Satz, an dem die meisten Erwartungen brechen. Eine Aktion läuft als `podman exec` mit
`stdin` und `stdout` (`runtime/podman.go`, `execArgs`). **Es gibt keinen Port, keine Adresse,
nichts zu lauschen.** NGC veröffentlicht keinen Port des Behälters; ohne `needs: ["network"]`
läuft er mit `--network none`.

Also:

- **Eine Aktion ist ein Aufruf.** Sie bekommt `stdin`, schreibt nach `stdout`, endet. Vorgabe
  30 Sekunden, bis 3600 (`timeout`).
- **Ein `stream` bleibt offen** und hat dann **kein** Zeitlimit — `timeout` **muss** 0 sein. Das
  ist der Weg für alles Lange: ein Protokoll mitlesen, eine Verbindung halten, ein Terminal.
- **`start`** ist eine Aktion, die der Controller nach jedem Start fährt. Sie darf kein `stream`
  sein.

Wer einen vorhandenen Dienst im Behälter mitbringt, der auf einem Port lauscht, muss ihn selbst
anrufen: der eigene `command` startet ihn, und eine Aktion redet **im Behälter** mit ihm. Nach
außen ist der Port nicht da.

### Was der Dienst vorfindet

| | |
|---|---|
| `/data` | der Datenordner der App, `rw,nosuid,nodev`. **Überlebt eine neue Fassung** (zweimal gemessen) |
| eigener Benutzer | je App ein eigener Systembenutzer außerhalb des Behälters; `0` **im** Behälter ist dieser Benutzer |
| `/ngc/ngc-hold` | NGCs eigenes Programm, das den Behälter offen hält, `ro,nosuid,nodev` |
| Netz | **keins**, außer `needs: ["network"]` |
| Root | **keins**, außer `needs: ["root"]` — und das ist Root **im** Behälter, nicht auf der Maschine |

**`/data` ist nicht verschlüsselt** (gemessen). Was eine App dort ablegt, liegt lesbar auf dem
Datenträger des Dienstknotens.

---

## 5. Die Schranken: was wirklich hält

Das Manifest nennt `cpu` (Tausendstel eines Kerns, 100–64000, Vorgabe 1000) und `memory` (MiB,
16–65536, Vorgabe 256). **Bis zum 3.11.2026 hielt keine der beiden** — auf drei gemessenen Knoten
hatte kein App-Behälter eine cgroup für sich, und eine Probe startete 600 Aufgaben in einem
Behälter, dessen Grenze 512 sagt. Seit PR 155 legt der Controller selbst eine cgroup an und
schreibt die Zahlen hinein.

Was davon **gemessen** hält (svc-1, 30.10.2026, je mit Gegenprobe):

```
40 Aufgaben bei pids.max 20   ->  "Cannot fork"       | ohne Schranke: 40 von 40
128 MiB bei memory.max 64     ->  Killed, oom_kill 1  | ohne Schranke: gehalten
memory.peak                   ->  genau 67108864
systemctl daemon-reload       ->  die cgroup überlebt
```

Drei Dinge, die eine App wissen muss:

1. **Die `cpu`-Schranke kann fehlen.** Gibt der Kern den cpu-Regler nicht herunter, läuft die App
   trotzdem — ein Anteil an einem Kern ist Nachbarschaftlichkeit, keine Wand. `memory` und `pids`
   fehlen nie: fehlen sie, **startet die App nicht** (`ErrCgroupDropped`). Eine Schranke, an die
   man glaubt und die nicht da ist, ist schlimmer als eine, von der man weiß, dass sie fehlt.
2. **Die Decke gilt der ganzen App.** Der Haltevorgang, `conmon` und **jeder Dienst jeder
   Aktion** zählen zusammen. 512 Aufgaben sind die Decke für alle, nicht für jeden.
3. **Nichts im Behälter kann sie heben.** Die Zahlen stehen in Dateien, die nur Root schreiben
   darf.

---

## 6. Rechte: zwei verschiedene Dinge

**`needs`** — was der **Behälter** darf. Genau zwei Werte: `root` und `network`. Der Verwalter
erlaubt sie beim **Einrichten**, einmal.

**`actions`** — was ein **Nutzer** darf. Jede Aktion wird ein Recht `<app>/<aktion>`. Dazu kommt
eines, das in keinem Manifest steht: **`<app>/use`**. Ohne das sieht ein Nutzer die App überhaupt
nicht.

**Fragt eine neue Fassung mehr `needs` an als erlaubt wurde, wird sie nicht eingespielt.** Sie
bekommt den Zustand `needsgrants`, und die Oberfläche bietet das Einrichten an, statt einen
Fehler zu zeigen (`store.Granted`, `store.UpdateNeedsGrants`). Für den, der eine App pflegt,
heißt das: **ein neues `needs` ist ein Bruch**, auch wenn die Fassungsnummer klein aussieht.

Die Uhr fragt **nichts** an. Jedes Recht, das eine App nicht braucht, ist eines, das der Weg auch
nicht braucht.

---

## 7. Worauf eine App sich nicht verlassen darf

- **Nicht auf einen Dienstknoten.** Wo eine App liegt, liest man nach dem Einrichten nach, statt
  es aus einem Namen zu schließen. Es gab einen Stand, auf dem `db-1` einen Controller trug und
  `store` gar kein NGC.
- **Nicht auf Laufzeit.** Die Anlage schaltet derzeit **jede Nacht gegen 03:45 UTC** alle
  Maschinen sauber aus und wieder an (gemessen). Was eine App im Arbeitsspeicher hält, ist
  morgens weg; was in `/data` liegt, ist da.
- **Nicht auf einen überlebenden Prozess.** Stirbt der Controller-Dienst, wird die App beendet
  und neu gestartet. Ein `start`-Lauf muss also **wiederholbar** sein und darf nicht annehmen,
  dass er der erste ist.
- **Nicht auf `lxc`.** `runtime` kennt heute nur `podman`; `lxc` ist reserviert.
- **Nicht auf eine Fassung von NGC**, ohne sie zu nennen: dafür ist `minNgc` da.

---

## 8. Die kürzeste App, die etwas tut

Vorbild ist `apps/uhr/` im Repo (`org.sorglos.uhr`) — mit Absicht winzig: eine Uhr. Ihr Manifest
steht vollständig in [`app-weg.md`](app-weg.md). Wer eine App baut, fängt dort an und tauscht das
Programm aus.

```
manifest.json
ui/index.html          klassisches <script src="app.js">, kein type="module"
ui/app.js              ngc.call(...), Ergebnis auf textContent
services/amd64.tar     ein Abbild mit einem Programm unter einem absoluten Pfad
```

---

## 9. Nicht gemessen

- **Eine App mit `needs: ["network"]`, die einen fremden Server steuert**, ist den Weg nie
  gegangen. Abschnitt 0 sagt, wie er aussehen **muss**; dass er geht, ist aus dem Code
  geschlossen und nicht gelaufen. Wer ihn zuerst geht, trägt ihn hier ein.
- **Gegen den Code gelesen, nicht übernommen:** beim Schreiben dieses Blattes stand in
  `app-weg.md`, die `id` dürfe nicht `ngc.desktop`, `ngc.users` oder `ngc.store` heißen.
  `wire.CoreApp` ist aber `strings.HasPrefix(app, "ngc.")` — gesperrt ist der ganze Namensraum.
  Beide Blätter sind berichtigt. Wer hier etwas liest und darauf baut, soll die genannte
  Codestelle aufmachen; dieses Blatt nennt sie zu jedem Satz genau deswegen.
- **Mehrere Dienste in einem Manifest** (bis 16 erlaubt) sind nie zu mehr als einem gefahren.
- **`stream`** ist am Terminal gemessen, einer Kern-App — nicht an einer App aus einem Laden.
- **Die arm64-Seite** ist im Paketformat da; ob eine App auf arm64 wirklich läuft, hat niemand
  geprüft.
- **Zwei Fassungen auf einem Datenordner** sind der bekannte Waisen-Fall und am 3.11.2026 behoben
  worden. Was eine App vorfindet, wenn ihr `/data` von einer **älteren** Fassung beschrieben
  wurde, ist ihre eigene Sache — NGC wandelt nichts.
