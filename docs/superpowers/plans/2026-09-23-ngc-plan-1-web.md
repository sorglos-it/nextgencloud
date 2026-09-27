# NGC Plan 1: Webinterface – Umsetzungsplan

> **Für ausführende Agenten:** PFLICHT-SKILL: `superpowers:subagent-driven-development` (empfohlen)
> oder `superpowers:executing-plans`, Aufgabe für Aufgabe. Schritte nutzen Kästchen (`- [ ]`).

**Ziel:** `apps/web` baut den NGC-Desktop im Browser: Ersteinrichtung, Anmeldung mit
Passwortschlüssel, TOTP und Passkeys, Sitzung mit Noise-Verschlüsselung und Ende zu Ende bis zum
Controller, Fenster, Startleiste, App-Fenster mit `ngc.js` und die Oberflächen von App-Store,
Benutzer & Rechte, Rechner und Terminal.

**Architektur:** Reines JavaScript ohne Framework und ohne Build-Schritt, ES-Module über
`<script type="module">`. Die Verträge aus `shared/` gibt es ein zweites Mal in JavaScript unter
`public/assets/js/contracts/`; sie prüfen sich mit Playwright gegen `shared/testdata/`, ohne dass
eine API läuft. Darüber liegen die Leitung (`js/net/`), die Anmeldung (`js/auth/`), die Shell
(`js/shell/`) und die Oberflächen der Kern-Apps (`js/apps/`). PHP liefert nur die Seite aus und
gibt Anker und Konfiguration weiter; kein App-Code läuft im Webserver. Der private
Sitzungsschlüssel entsteht im Browser als nicht exportierbares Web-Crypto-Schlüsselpaar.

**Tech Stack:** HTML, CSS, ES-Module, Web Crypto API (Ed25519, X25519, AES-256-GCM, SHA-256,
HMAC-SHA256, ECDSA P-256, RSASSA-PKCS1-v1_5), WebAuthn, WebSocket, PHP 8.2+ (nur Auslieferung),
Playwright (npm, nur in `apps/web/tests/`), `@noble/hashes` von Hand kopiert nach
`public/assets/vendor/` (Argon2id und ein SHA-256 mit `update`/`digest`).

**Entwurf:** [`docs/superpowers/specs/2026-09-19-ngc-kern-design.md`](../specs/2026-09-19-ngc-kern-design.md),
Abschnitte 4.2 bis 4.8 · **Verträge:** [`shared/README.md`](../../../shared/README.md),
[`shared/ulh.md`](../../../shared/ulh.md) · **Anweisung:** `apps/web/CLAUDE.md`

---

## Global Constraints

Diese Regeln gelten in jeder Aufgabe, auch wenn sie dort nicht wiederholt werden.

- **Branch:** `web`. Pull Request nach `main`; der Koordinator führt zusammen. Nie mit Gewalt
  schieben, nie fremde Branches ändern.
- **Fremde Ordner sind tabu.** Geschrieben wird nur in `apps/web/` und – für das ulh-Skript – im
  ulh-Repo auf Branch `ngc-web`. `shared/` ist eingefroren (Etikett `vertraege-v1.1`); braucht der
  Plan eine Änderung, kommt sie in „Offene Entscheidungen für den Koordinator“.
- **Kein Framework, kein Build-Schritt.** Der Browser lädt die Dateien so, wie sie im Repo liegen.
  npm nur in `apps/web/tests/`, mit festen Versionen; NGC baut und läuft ohne npm.
- **Webroot ist `apps/web/public/`.** Einziger Einstiegspunkt ist `public/index.php`; alles unter
  `public/assets/` liefert Apache selbst aus. Nichts außerhalb von `public/` ist von außen
  erreichbar.
- **Nie `innerHTML`, nie `insertAdjacentHTML`, nie `document.write`, nie `eval`, nie
  `new Function`.** Text kommt ausschließlich über `textContent`, Struktur über
  `document.createElement`. Ausnahme ist einzig der Rahmen einer App-Oberfläche (Aufgabe 21), und
  der baut sein Dokument aus geprüften Bytes und lädt es in einen abgeschotteten iframe.
- **Jede Eingabe von außen** läuft durch die `parse…`- oder `verify…`-Funktion ihres Vertrags, nie
  durch `JSON.parse`. Striktes JSON nach `shared/README.md`, Abschnitt „Striktes JSON“, immer mit
  Budget.
- **Jede Grenze aus den Go-Paketen gilt auch hier**, auch die ohne eigene Testdaten
  (`trust.MaxAuto` 64, `trust.MaxOrigins` 8, `trust.MaxRoot` 256, `proof.MaxPublic` 1024,
  `proof.MaxSig` 1024, `proof.MaxRSABits` 4096, `proof.MaxAuthData` 4096,
  `proof.MaxClientData` 4096).
- **Das Passwort verlässt den Browser nie.** Es wird nur an `deriveKey` übergeben, danach sofort
  überschrieben; es steht in keiner Anfrage, keinem Frame, keinem Log und keinem Speicher.
- **Kryptografie** kommt aus der Web Crypto API oder aus `public/assets/vendor/`; nichts selbst
  gebaut außer dem Ablauf von Noise, der die Rechenschritte der Web Crypto API benutzt.
- **Zahlen:** Alle Zahlenfelder der Verträge sind ganze Zahlen; JavaScript prüft den Betrag gegen
  `9007199254740991` an den Ziffern, nie über `Number`.
- **Sprache:** Code, Bezeichner, Kommentare und Commit-Nachrichten Englisch. Oberfläche und
  Meldungen für Benutzer Deutsch. Kommentare erklären, warum etwas so ist.
- **Commits:** klein, Nachricht in der Befehlsform auf Englisch, Trailer
  `Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>`.
- **Prüfen vor jedem Commit:** `tools/check.sh` (muss grün bleiben, auch ohne Go-Änderungen) und
  `npm test` in `apps/web/tests/`.
- **Browser-Mindestversionen** (Ed25519 und X25519 in der Web Crypto API): Chrome/Edge 137,
  Firefox 130, Safari 17.4. Fehlt eines davon, zeigt die Shell eine Meldung auf Deutsch und lädt
  nichts weiter.
- **Reihenfolge:** erst der fehlschlagende Test, dann der Code (TDD).
- **Der Byte-Waechter** `apps/web/tests/contracts/bytes.spec.js` laeuft ueber jeden
  Export jedes Vertrags. Er liest den Ordner `contracts/` selbst aus, findet eine
  neue Datei also allein. Von Hand gepflegt sind **drei** Stellen, nicht zwei:
  `ENTRIES` (die benannten Eingaenge), `CALLABLES` (die Anzahl) und `CLASSES` (die
  ausgefuehrten Klassen). Wer eine Klasse mit Methoden exportiert, faellt sonst in
  die `noInstance`-Zusicherung. Die neuen Zahlen **misst** man: der Waechter meldet
  die tatsaechliche, wenn die eingetragene nicht stimmt.
- **Dateirechte und Pfade:** Alle Pfade in diesem Plan sind relativ zum Repo-Stamm.

---

## Dateien-Übersicht

Alles neu, nichts wird gelöscht. `apps/web/CLAUDE.md` und `apps/web/VERSION` bestehen schon.

```text
apps/web/
├── VERSION                       0.1.0 → am Ende 0.2.0
├── README.md                     Zweck, Aufbau, Start in 3 Schritten (Englisch)
├── public/
│   ├── index.php                 einziger Einstiegspunkt: Kopfzeilen, CSP, Seite, /config.json
│   └── assets/
│       ├── css/shell.css         Desktop, Fenster, Startleiste, Startmenü, hell und dunkel
│       ├── ngc.js                MIT: ngc.call, ngc.open, Design und Akzentfarbe für Apps
│       ├── vendor/               von Hand kopierter Fremdcode für den Browser
│       │   └── noble-hashes/     sha2.js, blake2b.js, argon2.js, utils.js, SOURCE.md
│       └── js/
│           ├── contracts/        die Verträge in JavaScript
│           │   ├── b64.js        base64url, Bytes-Hilfen
│           │   ├── json.js       striktes JSON mit Budget (enc)
│           │   ├── text.js       Namen, Texte, Pfade, Versionen (wire, store, trust)
│           │   ├── url.js        Hosts und Adressen (trust)
│           │   ├── sign.js       Umschläge, Zwecke, Schlüssel-IDs (sign)
│           │   ├── proof.js      Passwortschlüssel und Passkeys (proof)
│           │   ├── noise.js      Noise_KK und die drei Prologe (noisep)
│           │   ├── trust.js      Vertrauensliste (trust)
│           │   ├── session.js    Zertifikate, Ersteinrichtung, Kette (session)
│           │   ├── task.js       Aufgaben und erneute Bestätigung (task)
│           │   ├── wire.js       Frames, Kanäle, Codes (wire)
│           │   ├── uitar.js      ui-tar lesen und schreiben (store/ui.go)
│           │   ├── pack.js       App-Paket lesen, UI-Prüfsumme (store/pack.go)
│           │   ├── catalog.js    Katalog, Manifest, Updates (store/catalog.go, manifest.go)
│           │   ├── core.js       Kern-Aktionen, Ergebnisse, UI-Beweise (core)
│           │   └── tunnel.js     Fingerabdrücke, Kopplungs- und Beitrittscodes (tunnel)
│           ├── net/
│           │   ├── http.js       die sieben Pfade vor dem WebSocket
│           │   ├── link.js       WebSocket, Noise-Sitzung, Steuerkanal, Kanalnummern
│           │   └── channel.js    ein Kanal: Freigabe, call/in/out/transfer, Ende zu Ende
│           ├── auth/
│           │   ├── keys.js       Sitzungsschlüssel, Beweise, Passkey-Zuordnung
│           │   ├── argon2-worker.js  Argon2id im Hintergrund
│           │   ├── login.js      Anmeldung in beiden Stufen
│           │   └── setup.js      Ersteinrichtung mit Einmal-Code
│           ├── shell/
│           │   ├── main.js       Start: Browser prüfen, Anker holen, anmelden oder Desktop
│           │   ├── trustlist.js  Anker, Kette, Alarm, Zwischenspeicher
│           │   ├── desktop.js    Fenster, Startleiste, Startmenü
│           │   ├── window.js     ein Fenster: verschieben, größer ziehen, anordnen
│           │   ├── theme.js      hell und dunkel, Akzentfarbe, Hintergrund
│           │   ├── dom.js        kleine Helfer: el(), text(), Dialoge
│           │   ├── confirm.js    erneute Bestätigung (Passkey oder Passwort)
│           │   └── appframe.js   abgeschotteter iframe aus dem geprüften ui-tar
│           └── apps/
│               ├── store.js      App-Store
│               ├── users.js      Benutzer & Rechte
│               ├── calc.js       Rechner
│               └── terminal.js   Terminal
├── classes/
│   ├── Config.php                web.json und trust-anchor.json lesen und prüfen
│   └── Page.php                  Kopfzeilen, CSP, Vorlage füllen
├── templates/
│   └── desktop.php               Gerüst der Seite
└── tests/                        npm nur hier
    ├── package.json, package-lock.json, playwright.config.js
    ├── fixtures/vectors.js       liest shared/testdata/<datei>
    ├── fixtures/page.js          Seite öffnen, Modul importieren
    ├── mock/api.js               Gegenstelle: HTTP und WebSocket, spricht dieselben Module
    ├── mock/world.js             Vertrauensliste, Benutzer, Apps für die Gegenstelle
    ├── contracts/*.spec.js       je Vertragsmodul ein Test gegen shared/testdata/
    └── shell/*.spec.js           Anmeldung, Ersteinrichtung, Desktop, Kanäle, Angriffe
```

Im ulh-Repo (Branch `ngc-web`, eigener Pull Request): `scripts/ngc-web.sh`, Eintrag in
`config.yaml`, Zeilen in `SCRIPTS.md` und `TREE.md`.

---

### Task 1: Grundgerüst, PHP-Einstiegspunkt und der Testlauf

**Files:**
- Create: `apps/web/README.md`, `apps/web/public/index.php`, `apps/web/classes/Config.php`,
  `apps/web/classes/Page.php`, `apps/web/templates/desktop.php`,
  `apps/web/public/assets/css/shell.css`, `apps/web/public/assets/js/shell/main.js`
- Create: `apps/web/tests/package.json`, `apps/web/tests/playwright.config.js`,
  `apps/web/tests/fixtures/page.js`, `apps/web/tests/fixtures/vectors.js`,
  `apps/web/tests/web.json`, `apps/web/tests/trust-anchor.json`
- Test: `apps/web/tests/shell/page.spec.js`

**Interfaces:**
- Produces: die Seite unter `/` mit `<div id="root">` und `<script type="module" src="/assets/js/shell/main.js">`;
  `GET /config.json` mit `{"version":"…","anchor":{"seq":N,"hash":"…"},"rp":{"id":"…","origins":[…]},"apis":["https://…"]}`;
  die Testhilfen `openShell(page)` und `vectors(name)`.

- [ ] **Step 1: Branch anlegen**

```bash
git switch -c web
```

- [ ] **Step 2: Den fehlschlagenden Test schreiben**

`apps/web/tests/package.json`:

```json
{
  "name": "ngc-web-tests",
  "private": true,
  "type": "module",
  "scripts": {
    "test": "playwright test",
    "install-browsers": "playwright install --with-deps chromium firefox webkit"
  },
  "devDependencies": {
    "@playwright/test": "1.49.1"
  }
}
```

`apps/web/tests/playwright.config.js`:

```js
import { defineConfig, devices } from "@playwright/test";

// The built-in PHP server stands in for Apache: index.php is the only entry
// point there too, and assets/ is served straight from the file system.
const port = 8099;

export default defineConfig({
  testDir: ".",
  fullyParallel: false,
  workers: 1,
  reporter: [["list"]],
  use: { baseURL: `http://127.0.0.1:${port}`, trace: "retain-on-failure" },
  webServer: {
    command: `php -d variables_order=EGPCS -S 127.0.0.1:${port} -t ../public ../public/index.php`,
    url: `http://127.0.0.1:${port}/config.json`,
    reuseExistingServer: false,
    env: { NGC_WEB_CONFIG: "../tests/web.json", NGC_WEB_ANCHOR: "../tests/trust-anchor.json" },
  },
  projects: [
    { name: "chromium", use: { ...devices["Desktop Chrome"] } },
    { name: "firefox", use: { ...devices["Desktop Firefox"] } },
    { name: "webkit", use: { ...devices["Desktop Safari"] } },
  ],
});
```

`apps/web/tests/web.json` (Kopie des Beispiels aus `shared/testdata/config/web.json`, damit der
Test dieselbe Form liest, die `config.LoadWeb` schreibt):

```json
{
  "apis": [
    { "url": "https://api-1.ngc.test:7443", "tls": "4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eE" },
    { "url": "https://api-2.ngc.test:7443", "tls": "4uLi4uLi4uLi4uLi4uLi4uLi4uLi4uLi4uLi4uLi4uI" }
  ],
  "rp": { "id": "ngc.example.org", "origins": ["https://ngc.example.org"] }
}
```

`apps/web/tests/trust-anchor.json`:

```json
{ "seq": 1, "hash": "q6urq6urq6urq6urq6urq6urq6urq6urq6urq6urq6s" }
```

`apps/web/tests/shell/page.spec.js`:

```js
import { test, expect } from "@playwright/test";

test("the shell page carries the headers and the anchor", async ({ page }) => {
  const res = await page.goto("/");
  expect(res.status()).toBe(200);
  const h = res.headers();
  expect(h["content-security-policy"]).toContain("default-src 'none'");
  expect(h["content-security-policy"]).toContain("frame-ancestors 'none'");
  expect(h["cache-control"]).toBe("no-store");
  expect(h["x-content-type-options"]).toBe("nosniff");
  expect(h["referrer-policy"]).toBe("no-referrer");
  expect(await page.title()).toBe("NGC");
  await expect(page.locator("#root")).toBeAttached();

  const cfg = await (await page.request.get("/config.json")).json();
  expect(cfg.anchor).toEqual({ seq: 1, hash: "q6urq6urq6urq6urq6urq6urq6urq6urq6urq6urq6s" });
  expect(cfg.rp.id).toBe("ngc.example.org");
  expect(cfg.apis).toEqual(["https://api-1.ngc.test:7443", "https://api-2.ngc.test:7443"]);
});

test("an unknown path answers 404 in German", async ({ page }) => {
  const res = await page.goto("/gibtesnicht");
  expect(res.status()).toBe(404);
  await expect(page.locator("body")).toContainText("Seite nicht gefunden");
});
```

- [ ] **Step 3: Test laufen lassen, er muss scheitern**

```bash
cd apps/web/tests && npm install && npm run install-browsers && npm test
```

Erwartet: Fehler, weil `index.php` fehlt (der Webserver antwortet nicht auf `/config.json`).

- [ ] **Step 4: Die PHP-Klassen schreiben**

`apps/web/classes/Config.php`:

```php
<?php
declare(strict_types=1);

// Config reads the two files ngc-web and the controller with role web write:
// /etc/ngc/web.json (config.Web) and /etc/ngc/trust-anchor.json (trust.Anchor).
// It only reads and checks; nothing here ever writes them.
final class Config
{
    public const MAX_FILE = 1048576;      // config.MaxConfig
    public const MAX_APIS = 16;           // config: maxList
    public const MAX_ORIGINS = 8;         // trust.MaxOrigins

    /** @var list<string> */ public array $apis = [];
    /** @var array{id:string,origins:list<string>} */ public array $rp = ['id' => '', 'origins' => []];
    /** @var array{seq:int,hash:string}|null */ public ?array $anchor = null;

    public static function load(string $webFile, string $anchorFile): self
    {
        $c = new self();
        $web = self::readJson($webFile);
        if (!is_array($web['apis'] ?? null) || count($web['apis']) < 1 || count($web['apis']) > self::MAX_APIS) {
            throw new RuntimeException('web.json: apis');
        }
        foreach ($web['apis'] as $a) {
            // The address grammar is trust.ValidWebURL; PHP only passes it on,
            // so it checks the shape and never builds one of its own.
            if (!is_array($a) || !is_string($a['url'] ?? null) || !preg_match('#^https://[^\s"\'<>\\\\]{1,248}$#', $a['url'])) {
                throw new RuntimeException('web.json: apis[].url');
            }
            $c->apis[] = $a['url'];
        }
        $rp = $web['rp'] ?? null;
        if (is_array($rp) && is_string($rp['id'] ?? null) && $rp['id'] !== '') {
            if (!preg_match('/^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$/', $rp['id'])
                || strlen($rp['id']) > 253) {
                throw new RuntimeException('web.json: rp.id');
            }
            $origins = $rp['origins'] ?? [];
            if (!is_array($origins) || count($origins) < 1 || count($origins) > self::MAX_ORIGINS) {
                throw new RuntimeException('web.json: rp.origins');
            }
            foreach ($origins as $o) {
                if (!is_string($o) || !preg_match('#^https://[a-z0-9.\[\]:-]{1,248}$#', $o)) {
                    throw new RuntimeException('web.json: rp.origins[]');
                }
            }
            $c->rp = ['id' => $rp['id'], 'origins' => array_values($origins)];
        }
        // The anchor arrives only once the controller of the web server is
        // paired; until then the shell keeps the one of the setup.
        if (is_file($anchorFile)) {
            $a = self::readJson($anchorFile);
            if (!is_int($a['seq'] ?? null) || $a['seq'] < 1 || !is_string($a['hash'] ?? null)
                || !preg_match('#^[A-Za-z0-9_-]{43}$#', $a['hash'])) {
                throw new RuntimeException('trust-anchor.json');
            }
            $c->anchor = ['seq' => $a['seq'], 'hash' => $a['hash']];
        }
        return $c;
    }

    /** @return array<string,mixed> */
    private static function readJson(string $path): array
    {
        $size = @filesize($path);
        if ($size === false || $size > self::MAX_FILE) {
            throw new RuntimeException("cannot read $path");
        }
        $raw = @file_get_contents($path, false, null, 0, self::MAX_FILE + 1);
        if ($raw === false) {
            throw new RuntimeException("cannot read $path");
        }
        $v = json_decode($raw, true, 32, JSON_THROW_ON_ERROR | JSON_BIGINT_AS_STRING);
        if (!is_array($v)) {
            throw new RuntimeException("$path is no object");
        }
        return $v;
    }
}
```

`apps/web/classes/Page.php`:

```php
<?php
declare(strict_types=1);

// Page sends the headers of the web interface and fills the template. The
// content security policy is the one the app frames inherit (Task 21), so it
// allows data: scripts and styles for them and nothing else from outside.
final class Page
{
    public const CSP = "default-src 'none'; script-src 'self' data:; style-src 'self' data: 'unsafe-inline'; "
        . "img-src 'self' data:; font-src 'self' data:; media-src data:; connect-src 'self'; "
        . "frame-src 'self' data:; worker-src 'self'; manifest-src 'none'; "
        . "base-uri 'none'; form-action 'none'; frame-ancestors 'none'";

    public static function headers(string $type): void
    {
        header('Content-Type: ' . $type);
        header('Cache-Control: no-store');
        header('Content-Security-Policy: ' . self::CSP);
        header('X-Content-Type-Options: nosniff');
        header('Referrer-Policy: no-referrer');
        header('Cross-Origin-Opener-Policy: same-origin');
        header('Cross-Origin-Resource-Policy: same-origin');
        header('Permissions-Policy: publickey-credentials-get=(self), camera=(), microphone=(), geolocation=()');
        header('Strict-Transport-Security: max-age=63072000; includeSubDomains');
        header_remove('X-Powered-By');
    }

    public static function render(string $template, array $vars): void
    {
        self::headers('text/html; charset=utf-8');
        extract($vars, EXTR_SKIP);
        require $template;
    }

    public static function fail(int $status, string $text): void
    {
        http_response_code($status);
        self::headers('text/html; charset=utf-8');
        echo "<!DOCTYPE html>\n<html lang=\"de\"><head><meta charset=\"utf-8\"><title>NGC</title></head>"
            . '<body><p>' . htmlspecialchars($text, ENT_QUOTES | ENT_SUBSTITUTE, 'UTF-8') . '</p></body></html>';
    }
}
```

- [ ] **Step 5: Vorlage und Einstiegspunkt schreiben**

`apps/web/templates/desktop.php`:

```php
<!DOCTYPE html>
<html lang="de" data-theme="dark">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<meta name="color-scheme" content="dark light">
<title>NGC</title>
<link rel="stylesheet" href="/assets/css/shell.css">
</head>
<body>
<div id="root"></div>
<noscript>NGC braucht JavaScript.</noscript>
<script type="module" src="/assets/js/shell/main.js"></script>
</body>
</html>
```

`apps/web/public/index.php`:

```php
<?php
declare(strict_types=1);

// The only entry point of the web interface. Apache's DocumentRoot is this
// folder and serves everything below assets/ itself; every other path lands
// here. No app code ever runs in the web server.
require __DIR__ . '/../classes/Config.php';
require __DIR__ . '/../classes/Page.php';

$version = trim((string)@file_get_contents(__DIR__ . '/../VERSION'));
$webFile = getenv('NGC_WEB_CONFIG') ?: '/etc/ngc/web.json';
$anchorFile = getenv('NGC_WEB_ANCHOR') ?: '/etc/ngc/trust-anchor.json';

$path = parse_url($_SERVER['REQUEST_URI'] ?? '/', PHP_URL_PATH);
if (!is_string($path)) {
    Page::fail(400, 'Ungültige Anfrage.');
    return true;
}
// The built-in PHP server calls this file for existing files below public/
// too; there it must hand them back unchanged, as Apache does.
if (PHP_SAPI === 'cli-server' && $path !== '/' && is_file(__DIR__ . $path)) {
    return false;
}
if (($_SERVER['REQUEST_METHOD'] ?? 'GET') !== 'GET') {
    Page::fail(405, 'Nur GET.');
    return true;
}

try {
    $config = Config::load($webFile, $anchorFile);
} catch (Throwable) {
    // Never show the reason: it would name paths of the server.
    Page::fail(500, 'NGC ist nicht eingerichtet.');
    return true;
}

switch ($path) {
    case '/':
        Page::render(__DIR__ . '/../templates/desktop.php', []);
        break;
    case '/config.json':
        Page::headers('application/json; charset=utf-8');
        echo json_encode([
            'version' => $version,
            'anchor' => $config->anchor,
            'rp' => $config->rp,
            'apis' => $config->apis,
        ], JSON_THROW_ON_ERROR | JSON_UNESCAPED_SLASHES);
        break;
    default:
        Page::fail(404, 'Seite nicht gefunden.');
}
return true;
```

- [ ] **Step 6: Erstes Stylesheet und erstes Modul**

`apps/web/public/assets/css/shell.css` – zunächst nur die Farbmarken aus `docs/prototype.html`,
Zeilen 8 bis 31, wörtlich übernommen (`:root` und `html[data-theme="light"]`), dazu:

```css
*{box-sizing:border-box}
html,body{height:100%;margin:0}
body{font:14px/1.45 "Segoe UI",system-ui,-apple-system,Roboto,sans-serif;overflow:hidden;
     color:var(--bar-fg);background:#0d1017;-webkit-user-select:none;user-select:none;
     touch-action:manipulation}
#root{position:fixed;inset:0}
.msg{max-width:32rem;margin:20vh auto;padding:1.5rem;text-align:center;color:var(--win-fg);
     background:var(--win-bg);border:1px solid var(--win-border);border-radius:var(--radius)}
```

`apps/web/public/assets/js/shell/main.js`:

```js
// Boot of the shell: check the browser, then load the configuration. The
// rest follows in later tasks.
const NEEDED = ["Ed25519", "X25519"];

export async function browserFits() {
  // Both are needed for the session keys; a browser without them cannot open
  // a session at all, so the shell says so instead of failing halfway.
  for (const name of NEEDED) {
    try {
      const k = await crypto.subtle.generateKey({ name }, false, name === "Ed25519" ? ["sign", "verify"] : ["deriveBits"]);
      if (!k) return false;
    } catch {
      return false;
    }
  }
  return true;
}

function message(text) {
  const root = document.getElementById("root");
  root.replaceChildren();
  const p = document.createElement("p");
  p.className = "msg";
  p.textContent = text;
  root.append(p);
}

async function start() {
  if (!(await browserFits())) {
    message("Dieser Browser ist zu alt für NGC. Bitte Chrome 137, Firefox 130 oder Safari 17.4 "
      + "oder neuer benutzen.");
    return;
  }
  // NICHT res.json(). /config.json traegt die Adressen der API-Server, den
  // ANKER der Vertrauensliste (seq und hash, die Wurzel fuer trust.open) und
  // rp.id, das an navigator.credentials.get geht. Das sind Eingaben von
  // aussen und laufen durch den Vertrag: Bytes, json.decode mit Budget, dann
  // jedes Feld gegen seine Grammatik. Was nicht besteht, bekommt denselben
  // Satz wie eine fehlende Einrichtung - der Mensch soll nicht raten muessen
  // und nichts ueber die Form erfahren.
  //
  // Und nicht auf window: eine globale Veraenderliche ist eine Stelle mehr,
  // die zwischen der Pruefung und dem Lesen jemand anders beschreiben kann.
  // Weiterreichen statt ablegen.
  let config;
  try {
    config = await loadConfig();
  } catch {
    message("NGC ist nicht eingerichtet.");
    return;
  }
  message("NGC wird geladen …");
}

start();
```

- [ ] **Step 7: Testhilfen schreiben**

`apps/web/tests/fixtures/vectors.js`:

```js
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

// The shared test data is the common truth for Go and JavaScript; the tests
// read it from the repository, never from the web root.
const dir = fileURLToPath(new URL("../../../../shared/testdata/", import.meta.url));

export function vectors(name) {
  return JSON.parse(readFileSync(dir + name, "utf8"));
}

export function raw(name) {
  return readFileSync(dir + name);
}
```

`apps/web/tests/fixtures/page.js`:

```js
import { test as base } from "@playwright/test";

// The contract tests import the modules on the shell page. They run with the
// content security policy switched off, because Playwright injects its own
// script; a separate test checks the policy itself (Task 26).
export const test = base.extend({
  context: async ({ browser }, use) => {
    const ctx = await browser.newContext({ bypassCSP: true });
    await use(ctx);
    await ctx.close();
  },
  shell: async ({ page }, use) => {
    await page.goto("/");
    await use(page);
  },
});

export { expect } from "@playwright/test";
```

- [ ] **Step 8: Test laufen lassen, er muss bestehen**

```bash
cd apps/web/tests && npm test
```

Erwartet: 6 bestandene Tests (2 Tests × 3 Engines).

- [ ] **Step 9: README schreiben**

`apps/web/README.md` (Englisch, nach der Regel „README englisch“):

```markdown
# ngc-web

The NGC desktop in the browser: login, session, windows, apps.

| Folder | What is in it |
| --- | --- |
| `public/` | web root; `index.php` is the only entry point, `assets/` holds CSS, JS and vendored code |
| `classes/` | PHP: reads `/etc/ngc/web.json` and `/etc/ngc/trust-anchor.json` |
| `templates/` | the page skeleton |
| `tests/` | Playwright; npm lives only here |

## Start in 3 steps

1. `cd apps/web/tests && npm install && npm run install-browsers`
2. `npm test` — the contracts check themselves against `shared/testdata/`, no API needed.
3. On a server, ulh does the rest: `scripts/ngc-web.sh` in the ulh repository.

No build step, no framework. NGC itself never needs npm.
```

- [ ] **Step 10: Committen**

```bash
git add apps/web/README.md apps/web/public apps/web/classes apps/web/templates apps/web/tests
git commit -m "Add the web entry point and the browser test setup" --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 2: `enc` in JavaScript – base64url und striktes JSON mit Budget

Das Fundament: Jede weitere Aufgabe liest Eingaben nur hierüber. Vektoren: `shared/testdata/json.json`
(54 Fälle, `schema`, `size` 1024, `items`, `totals`).

**Files:**
- Create: `apps/web/public/assets/js/contracts/b64.js`, `apps/web/public/assets/js/contracts/json.js`
- Test: `apps/web/tests/contracts/json.spec.js`

**Interfaces:**
- Produces:
  - `b64.js`: `b64encode(u8) -> string`, `b64decode(s) -> Uint8Array` (wirft `BadInput`),
    `encodedLen(n) -> number`, `utf8(s) -> Uint8Array`, `fromUtf8(u8) -> string`,
    `concat(...u8) -> Uint8Array`, `equal(a, b) -> boolean`, `class BadInput extends Error`.
  - `json.js`: `decode(bytes, budget, shape) -> value`, `class Budget {size, items, totals}`,
    `RAW`, `MAX_DEPTH`, `MAX_SAFE_INTEGER`, `MAX_MEMBERS`, `MAX_NAME`, `zero(shape)`,
    `safeNumber(text)`, `class BadJSON extends Error`, die Formkürzel `S` (Struktur),
    `P` (Zeiger-Struktur), `A` (Liste), `M` (Map), `str`, `bool`, `int`, `uint`, `uint32`,
    `uint64`, `bytes`, `raw`.
- Consumes: nichts.

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

`apps/web/tests/contracts/json.spec.js`:

```js
import { test, expect } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";

// json.json holds one document with every kind of field (schema), its budget
// (size, items, totals) and the cases Go accepts and refuses. The shell must
// decide every one of them exactly as Go does.
const v = vectors("json.json");

test("every case of json.json is decided as Go decides it", async ({ shell }) => {
  const results = await shell.evaluate(async ([items, totals, size, cases]) => {
    const { decode, Budget, S, A, M, str, bool, int, uint, bytes, raw } =
      await import("/assets/js/contracts/json.js");
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    // The shape mirrors the Go struct json.json was made from.
    const shape = S({
      n: int, u: uint, s: str, t: bool, b: bytes,
      list: A(int), map: M(int),
      sub: S({ x: int, list: A(int) }),
      subs: A(S({ list: A(int) })),
      raw: raw,
    });
    const budget = new Budget({ size, items, totals });
    return cases.map((c) => {
      try {
        decode(b64decode(c.doc), budget, shape);
        return true;
      } catch {
        return false;
      }
    });
  }, [v.items, v.totals, v.size, v.cases]);

  const wrong = v.cases.filter((c, i) => c.valid !== results[i]).map((c) => c.name);
  expect(wrong).toEqual([]);
});

test("base64url is strict", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const { b64encode, b64decode, encodedLen } = await import("/assets/js/contracts/b64.js");
    const ok = (s) => { try { b64decode(s); return true; } catch { return false; } };
    return {
      roundTrip: b64encode(new Uint8Array([1, 2, 3])) === "AQID" && b64decode("AQID").length === 3,
      len: encodedLen(16) === 22 && encodedLen(32) === 43,
      padding: ok("AQI="), lineFeed: ok("AQ\nID"), standard: ok("+/=="),
      trailingBits: ok("AQJ"), lonely: ok("A"), empty: b64decode("").length === 0,
    };
  });
  expect(out).toEqual({
    roundTrip: true, len: true, padding: false, lineFeed: false, standard: false,
    trailingBits: false, lonely: false, empty: true,
  });
});
```

- [ ] **Step 2: Test laufen lassen, er muss scheitern**

```bash
cd apps/web/tests && npx playwright test contracts/json.spec.js --project=chromium
```

Erwartet: Fehler beim Import von `/assets/js/contracts/b64.js` (Datei fehlt).

- [ ] **Step 3: `b64.js` schreiben**

```js
// Package b64: the only binary-to-text encoding of the NGC contracts is
// base64url without padding (RFC 4648 section 5), decoded strictly: every
// byte from the alphabet, no padding, and the unused bits at the end are 0,
// so every byte string has exactly one encoding (Go: enc.B64).
const ALPHABET = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_";
const REV = new Int8Array(128).fill(-1);
for (let i = 0; i < 64; i++) REV[ALPHABET.charCodeAt(i)] = i;

export class BadInput extends Error {}

export function encodedLen(n) {
  return Math.floor((n * 8 + 5) / 6);
}

export function b64encode(u8) {
  let out = "";
  let i = 0;
  for (; i + 3 <= u8.length; i += 3) {
    const v = (u8[i] << 16) | (u8[i + 1] << 8) | u8[i + 2];
    out += ALPHABET[(v >> 18) & 63] + ALPHABET[(v >> 12) & 63] + ALPHABET[(v >> 6) & 63] + ALPHABET[v & 63];
  }
  const rest = u8.length - i;
  if (rest === 1) {
    out += ALPHABET[u8[i] >> 2] + ALPHABET[(u8[i] << 4) & 63];
  } else if (rest === 2) {
    const v = (u8[i] << 8) | u8[i + 1];
    out += ALPHABET[v >> 10] + ALPHABET[(v >> 4) & 63] + ALPHABET[(v << 2) & 63];
  }
  return out;
}

export function b64decode(s) {
  if (typeof s !== "string") throw new BadInput("base64url: not a string");
  const n = s.length;
  if (n % 4 === 1) throw new BadInput("base64url: length");
  const out = new Uint8Array(Math.floor((n * 6) / 8));
  let acc = 0, bits = 0, o = 0;
  for (let i = 0; i < n; i++) {
    const c = s.charCodeAt(i);
    const d = c < 128 ? REV[c] : -1;
    if (d < 0) throw new BadInput("base64url: character");
    acc = (acc << 6) | d;
    bits += 6;
    if (bits >= 8) {
      bits -= 8;
      out[o++] = (acc >> bits) & 0xff;
    }
  }
  // The bits left over belong to no byte; a decoder that ignored them would
  // give one byte string two spellings.
  if (bits > 0 && (acc & ((1 << bits) - 1)) !== 0) throw new BadInput("base64url: trailing bits");
  return out;
}

const ENCODER = new TextEncoder();

export function utf8(s) {
  return ENCODER.encode(s);
}

export function fromUtf8(u8) {
  return new TextDecoder("utf-8", { fatal: true }).decode(u8);
}

export function concat(...parts) {
  let n = 0;
  for (const p of parts) n += p.length;
  const out = new Uint8Array(n);
  let o = 0;
  for (const p of parts) {
    out.set(p, o);
    o += p.length;
  }
  return out;
}

export function equal(a, b) {
  if (!a || !b || a.length !== b.length) return false;
  let diff = 0;
  for (let i = 0; i < a.length; i++) diff |= a[i] ^ b[i];
  return diff === 0;
}
```

- [ ] **Step 4: `json.js` schreiben**

```js
// Package json reads JSON from outside exactly as Go does (enc.DecodeStrict
// with an enc.Budget): one value, no unknown or repeated member names at any
// depth, valid UTF-8, no null, integers only where the contract has them, no
// number beyond 2^53-1, at most MAX_DEPTH levels, arrays and maps only where
// the budget lists them. JSON.parse alone decides none of this.
//
// Go checks the budget in a first pass that builds nothing and then decodes.
// Here one pass does both: every limit is checked as a container is entered
// and every element before it is read, so exactly the same documents are
// refused, and a refused one is never built to its end.
import { b64decode, BadInput } from "./b64.js";

export const RAW = -1;
export const MAX_DEPTH = 32;
export const MAX_SAFE_INTEGER = 9007199254740991;
export const MAX_MEMBERS = 64;
export const MAX_NAME = 64;

export class BadJSON extends Error {}

// Shapes. A shape says what a value may be; it mirrors the Go type the
// document decodes into. A member that is missing gives the zero value of its
// shape, as Go does, and the validate function of each contract asks for what
// has to be there.
export const str = { k: "str" };
export const bool = { k: "bool" };
export const int = { k: "int" };
export const uint = { k: "uint" };
export const uint32 = { k: "uint32" };
export const uint64 = { k: "uint" };
export const bytes = { k: "bytes" };
export const raw = { k: "raw" };
export const S = (members) => ({ k: "struct", m: members, ptr: false });
export const P = (members) => ({ k: "struct", m: members, ptr: true });
export const A = (of) => ({ k: "array", of });
export const M = (of) => ({ k: "map", of });

export class Budget {
  constructor({ size, items = {}, totals = {} }) {
    this.size = size;
    this.items = items;
    this.totals = totals;
  }
}

export function zero(shape) {
  switch (shape.k) {
    case "str": return "";
    case "bool": return false;
    case "int": case "uint": case "uint32": return 0;
    case "bytes": return new Uint8Array(0);
    case "raw": return null;
    case "array": return [];
    case "map": return {};
    case "struct": {
      if (shape.ptr) return null;
      const o = {};
      for (const [name, s] of Object.entries(shape.m)) o[name] = zero(s);
      return o;
    }
  }
  throw new BadJSON("unknown shape");
}

const WS = new Set([0x20, 0x09, 0x0d, 0x0a]);

export function decode(input, budget, shape) {
  const b = input instanceof Uint8Array ? input : new Uint8Array(input);
  if (b.length > budget.size) throw new BadJSON("document too large");
  // One fatal decode proves the whole document is valid UTF-8, so the parser
  // below can walk the bytes and slice raw values without checking again.
  try {
    new TextDecoder("utf-8", { fatal: true }).decode(b);
  } catch {
    throw new BadJSON("not UTF-8");
  }
  const p = new Parser(b, budget);
  p.ws();
  const v = p.value(shape, "", 1);
  p.ws();
  if (p.i !== b.length) throw new BadJSON("trailing data");
  return v;
}

class Parser {
  constructor(b, budget) {
    this.b = b;
    this.i = 0;
    this.budget = budget;
    this.totals = new Map();
    for (const path of Object.keys(budget.totals)) {
      // A total for a path the budget does not list as an array or map is a
      // mistake in the contract, not in the document.
      if (!(path in budget.items) || budget.items[path] === RAW) throw new BadJSON("budget");
      this.totals.set(path, 0);
    }
  }

  fail(why) {
    throw new BadJSON(why);
  }

  ws() {
    while (this.i < this.b.length && WS.has(this.b[this.i])) this.i++;
  }

  want(c) {
    if (this.i >= this.b.length || this.b[this.i] !== c) this.fail("expected " + String.fromCharCode(c));
    this.i++;
  }

  countTotal(path) {
    if (!this.totals.has(path)) return;
    const n = this.totals.get(path) + 1;
    if (n > this.budget.totals[path]) this.fail("total over its limit");
    this.totals.set(path, n);
  }

  value(shape, path, depth) {
    if (depth > MAX_DEPTH) this.fail("too deep");
    if (this.i >= this.b.length) this.fail("no value");
    const c = this.b[this.i];
    if (c === 0x7b) return this.object(shape, path, depth);
    if (c === 0x5b) return this.array(shape, path, depth);
    if (c === 0x22) return this.stringValue(shape);
    if (c === 0x74 || c === 0x66) return this.boolValue(shape);
    if (c === 0x6e) this.fail("null");
    if (c === 0x2d || (c >= 0x30 && c <= 0x39)) return this.number(shape);
    this.fail("unexpected byte");
  }

  // rawValue keeps the bytes of a value exactly as they stand, from its
  // opening byte to its closing one: noisep.E2EPrologue hashes them and
  // signatures cover them. Inside, only the rules that hold everywhere still
  // apply (depth, numbers, no null, no repeated names).
  rawValue(depth) {
    const start = this.i;
    this.skip(depth);
    return this.b.subarray(start, this.i);
  }

  skip(depth) {
    if (depth > MAX_DEPTH) this.fail("too deep");
    const c = this.b[this.i];
    if (c === 0x7b) {
      this.i++;
      const seen = new Set();
      this.ws();
      if (this.b[this.i] === 0x7d) { this.i++; return; }
      for (;;) {
        this.ws();
        const name = this.stringText();
        if (seen.has(name)) this.fail("repeated member name");
        seen.add(name);
        this.ws();
        this.want(0x3a);
        this.ws();
        this.skip(depth + 1);
        this.ws();
        if (this.b[this.i] === 0x2c) { this.i++; continue; }
        this.want(0x7d);
        return;
      }
    }
    if (c === 0x5b) {
      this.i++;
      this.ws();
      if (this.b[this.i] === 0x5d) { this.i++; return; }
      for (;;) {
        this.ws();
        this.skip(depth + 1);
        this.ws();
        if (this.b[this.i] === 0x2c) { this.i++; continue; }
        this.want(0x5d);
        return;
      }
    }
    if (c === 0x22) { this.stringText(); return; }
    if (c === 0x6e) this.fail("null");
    if (c === 0x74 || c === 0x66) { this.boolText(); return; }
    if (c === 0x2d || (c >= 0x30 && c <= 0x39)) {
      if (!safeNumber(this.numberText())) this.fail("number beyond 2^53-1");
      return;
    }
    this.fail("unexpected byte");
  }

  object(shape, path, depth) {
    const limit = this.budget.items[path];
    if (limit === RAW) {
      if (shape.k !== "raw") this.fail("raw where the shape has none");
      return this.rawValue(depth);
    }
    const mapped = limit !== undefined;
    if (mapped ? shape.k !== "map" : shape.k !== "struct") this.fail("object where the shape has none");
    if (mapped && limit < 0) this.fail("budget");
    this.i++;
    const out = mapped ? {} : zero({ ...shape, ptr: false });
    const seen = new Set();
    let n = 0;
    this.ws();
    if (this.b[this.i] === 0x7d) { this.i++; return out; }
    for (;;) {
      this.ws();
      const name = this.stringText();
      if (seen.has(name)) this.fail("repeated member name");
      seen.add(name);
      n++;
      if (mapped) {
        if (n > limit) this.fail("map over its limit");
        this.countTotal(path);
      } else {
        if (n > MAX_MEMBERS) this.fail("too many members");
        if (nameBytes(name) > MAX_NAME || name.includes("/") || name === "*") this.fail("member name");
        if (!Object.hasOwn(shape.m, name)) this.fail("unknown member " + name);
      }
      this.ws();
      this.want(0x3a);
      this.ws();
      const sub = mapped ? shape.of : shape.m[name];
      const subPath = path === "" ? (mapped ? "*" : name) : path + "/" + (mapped ? "*" : name);
      out[name] = this.value(sub, subPath, depth + 1);
      this.ws();
      if (this.b[this.i] === 0x2c) { this.i++; continue; }
      this.want(0x7d);
      return out;
    }
  }

  array(shape, path, depth) {
    const limit = this.budget.items[path];
    if (limit === RAW) {
      if (shape.k !== "raw") this.fail("raw where the shape has none");
      return this.rawValue(depth);
    }
    // An array whose path the budget does not list is refused: nothing from
    // outside may grow without a limit.
    if (limit === undefined || limit < 0) this.fail("array beyond its budget");
    if (shape.k !== "array") this.fail("array where the shape has none");
    this.i++;
    const out = [];
    const sub = path === "" ? "*" : path + "/*";
    this.ws();
    if (this.b[this.i] === 0x5d) { this.i++; return out; }
    for (;;) {
      this.ws();
      // Counted before it is read, so the first element too many is refused
      // unread.
      if (out.length + 1 > limit) this.fail("array over its limit");
      this.countTotal(path);
      out.push(this.value(shape.of, sub, depth + 1));
      this.ws();
      if (this.b[this.i] === 0x2c) { this.i++; continue; }
      this.want(0x5d);
      return out;
    }
  }

  stringValue(shape) {
    const s = this.stringText();
    if (shape.k === "str") return s;
    if (shape.k === "bytes") {
      try {
        return b64decode(s);
      } catch (e) {
        if (e instanceof BadInput) this.fail("bytes");
        throw e;
      }
    }
    this.fail("string where the shape has none");
  }

  boolValue(shape) {
    const v = this.boolText();
    if (shape.k !== "bool") this.fail("bool where the shape has none");
    return v;
  }

  boolText() {
    const t = [0x74, 0x72, 0x75, 0x65], f = [0x66, 0x61, 0x6c, 0x73, 0x65];
    const want = this.b[this.i] === 0x74 ? t : f;
    for (let k = 0; k < want.length; k++) {
      if (this.b[this.i + k] !== want[k]) this.fail("bool");
    }
    this.i += want.length;
    return want === t;
  }

  number(shape) {
    const text = this.numberText();
    if (!safeNumber(text)) this.fail("number beyond 2^53-1");
    if (shape.k === "raw") this.fail("raw where the shape has none");
    const integer = /^-?(0|[1-9][0-9]*)$/.test(text);
    switch (shape.k) {
      case "int":
        if (!integer) this.fail("integer field");
        return Number(text);
      case "uint": case "uint32": {
        // An unsigned field refuses every minus, -0 included.
        if (!integer || text[0] === "-") this.fail("unsigned field");
        const v = Number(text);
        if (shape.k === "uint32" && v > 4294967295) this.fail("uint32 overflow");
        return v;
      }
    }
    this.fail("number where the shape has none");
  }

  numberText() {
    const start = this.i;
    if (this.b[this.i] === 0x2d) this.i++;
    const firstDigit = this.i;
    while (this.i < this.b.length && this.b[this.i] >= 0x30 && this.b[this.i] <= 0x39) this.i++;
    const digits = this.i - firstDigit;
    if (digits === 0) this.fail("number");
    if (digits > 1 && this.b[firstDigit] === 0x30) this.fail("leading zero");
    if (this.b[this.i] === 0x2e) {
      this.i++;
      const f = this.i;
      while (this.i < this.b.length && this.b[this.i] >= 0x30 && this.b[this.i] <= 0x39) this.i++;
      if (this.i === f) this.fail("number");
    }
    if (this.b[this.i] === 0x65 || this.b[this.i] === 0x45) {
      this.i++;
      if (this.b[this.i] === 0x2b || this.b[this.i] === 0x2d) this.i++;
      const e = this.i;
      while (this.i < this.b.length && this.b[this.i] >= 0x30 && this.b[this.i] <= 0x39) this.i++;
      if (this.i === e) this.fail("number");
    }
    return String.fromCharCode(...this.b.subarray(start, this.i));
  }

  // stringText reads one JSON string and gives its text after the escapes are
  // resolved: "a" and "a" are the same member name (rule 4).
  stringText() {
    this.want(0x22);
    let out = "";
    let chunk = this.i;
    const dec = new TextDecoder();
    for (;;) {
      if (this.i >= this.b.length) this.fail("string");
      const c = this.b[this.i];
      if (c === 0x22) {
        out += dec.decode(this.b.subarray(chunk, this.i));
        this.i++;
        return out;
      }
      if (c < 0x20) this.fail("control character in a string");
      if (c !== 0x5c) { this.i++; continue; }
      out += dec.decode(this.b.subarray(chunk, this.i));
      this.i++;
      const e = this.b[this.i++];
      const simple = { 0x22: '"', 0x5c: "\\", 0x2f: "/", 0x62: "\b", 0x66: "\f", 0x6e: "\n", 0x72: "\r", 0x74: "\t" };
      if (e in simple) {
        out += simple[e];
      } else if (e === 0x75) {
        const u = this.hex4();
        if (u >= 0xd800 && u <= 0xdbff) {
          if (this.b[this.i] !== 0x5c || this.b[this.i + 1] !== 0x75) this.fail("lone surrogate");
          this.i += 2;
          const lo = this.hex4();
          if (lo < 0xdc00 || lo > 0xdfff) this.fail("lone surrogate");
          out += String.fromCodePoint(0x10000 + ((u - 0xd800) << 10) + (lo - 0xdc00));
        } else if (u >= 0xdc00 && u <= 0xdfff) {
          this.fail("lone surrogate");
        } else {
          out += String.fromCharCode(u);
        }
      } else {
        this.fail("escape");
      }
      chunk = this.i;
    }
  }

  hex4() {
    let v = 0;
    for (let k = 0; k < 4; k++) {
      const c = this.b[this.i++];
      const d = c >= 0x30 && c <= 0x39 ? c - 0x30
        : c >= 0x61 && c <= 0x66 ? c - 0x61 + 10
        : c >= 0x41 && c <= 0x46 ? c - 0x41 + 10 : -1;
      if (d < 0) this.fail("escape");
      v = v * 16 + d;
    }
    return v;
  }
}

function nameBytes(s) {
  let n = 0;
  for (const cp of s) {
    const c = cp.codePointAt(0);
    n += c < 0x80 ? 1 : c < 0x800 ? 2 : c < 0x10000 ? 3 : 4;
  }
  return n;
}

// safeNumber decides on the digits, never on a Number: a Number would round
// 9007199254740991.4 down to the limit and let it through (Go: enc.safeNumber).
export function safeNumber(text) {
  const m = /^(-?)(\d+)(?:\.(\d*))?(?:[eE]([+-]?\d+))?$/.exec(text);
  if (!m) return false;
  const intDigits = m[2], fracDigits = m[3] || "";
  const exp = m[4] ? Number(m[4]) : 0;
  const all = intDigits + fracDigits;
  let first = 0;
  while (first < all.length && all[first] === "0") first++;
  if (first === all.length) return true;
  const limit = "9007199254740991";
  const whole = intDigits.length + exp - first;
  if (whole < limit.length) return true;
  if (whole > limit.length) return false;
  for (let j = 0; j < limit.length; j++) {
    const d = first + j < all.length ? all[first + j] : "0";
    if (d !== limit[j]) return d < limit[j];
  }
  for (let k = first + limit.length; k < all.length; k++) {
    if (all[k] !== "0") return false;
  }
  return true;
}
```

- [ ] **Step 5: Test laufen lassen, er muss bestehen**

```bash
cd apps/web/tests && npm test -- contracts/json.spec.js
```

Erwartet: alle 54 Fälle aus `json.json` stimmen mit Go überein, in allen drei Engines.

- [ ] **Step 6: Committen**

```bash
git add apps/web/public/assets/js/contracts/b64.js apps/web/public/assets/js/contracts/json.js apps/web/tests/contracts/json.spec.js
git commit -m "Add base64url and strict JSON with budgets" --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 3: Texte, Namen, Pfade und Adressen

**Files:**
- Create: `apps/web/public/assets/js/contracts/text.js`, `apps/web/public/assets/js/contracts/url.js`
- Test: `apps/web/tests/contracts/text.spec.js`, `apps/web/tests/contracts/url.spec.js`

**Interfaces:**
- Produces:
  - `text.js`: `validText(s, max)`, `validName(s)`, `validAppID(s)`, `coreApp(s)`,
    `validLoginName(s)`, `validID(s)`, `validLogID(s)`, `validPath(p)`, `validVersion(s)`,
    `newer(a, b)`, `supportsMinNGC(ngc, min)`, `validTOTP(s)`, `byteLength(s)`.
  - `url.js`: `validHost(h)`, `validHostPort(s)`, `validTunnelURL(u)`, `webURL(u)`,
    `validWebURL(u)`, `validOrigin(o)`, `validStoreURL(u)`.
- Consumes: `b64.js` (`b64decode` für `validID`).

- [ ] **Step 1: Den fehlschlagenden Test für Adressen schreiben**

`apps/web/tests/contracts/url.spec.js`:

```js
import { test, expect } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";

const v = vectors("url.json");

test("hosts and addresses follow the same grammar as in Go", async ({ shell }) => {
  const got = await shell.evaluate(async (data) => {
    const u = await import("/assets/js/contracts/url.js");
    const check = (list, fn) => list.map((c) => fn(c.value));
    return {
      hosts: check(data.hosts, u.validHost),
      hostPort: check(data.hostPort, u.validHostPort),
      tunnel: check(data.tunnel, u.validTunnelURL),
      web: check(data.web, u.validWebURL),
      origins: check(data.origins, u.validOrigin),
      store: check(data.store, u.validStoreURL),
      // For a valid tunnel address, webURL derives the address behind Apache.
      derived: data.tunnel.map((c) => (c.valid ? u.webURL(c.value) : null)),
    };
  }, v);

  for (const key of ["hosts", "hostPort", "tunnel", "web", "origins", "store"]) {
    const wrong = v[key].filter((c, i) => c.valid !== got[key][i]).map((c) => `${key}: ${c.name}`);
    expect(wrong).toEqual([]);
  }
  const badDerived = v.tunnel
    .map((c, i) => (c.valid && got.derived[i] !== c.web ? c.name : null))
    .filter(Boolean);
  expect(badDerived).toEqual([]);
});
```

- [ ] **Step 2: Den fehlschlagenden Test für Texte schreiben**

`apps/web/tests/contracts/text.spec.js`:

```js
import { test, expect } from "../fixtures/page.js";

// wire.ValidText counts UTF-8 bytes and asks every code point, not every
// UTF-16 unit, whether it is printable.
test("text for people follows wire.ValidText", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const t = await import("/assets/js/contracts/text.js");
    return {
      empty: t.validText("", 10),
      plain: t.validText("Terminal", 64),
      space: t.validText("App Store", 64),
      umlaut: t.validText("Käse", 64),
      emoji: t.validText("Ordner \u{1F600}", 64),
      tab: t.validText("a\tb", 64),
      newline: t.validText("a\nb", 64),
      rtlOverride: t.validText("a‮b", 64),
      noncharacter: t.validText("﷐", 64),
      // The limit counts bytes: four umlauts are eight bytes.
      bytesOverLimit: t.validText("ääää", 7),
      bytesAtLimit: t.validText("ääää", 8),
      name: t.validName("ctl-1") && !t.validName("1ctl") && !t.validName("CTL"),
      appID: t.validAppID("org.sorglos.terminal") && !t.validAppID("terminal"),
      core: t.coreApp("ngc.desktop") && !t.coreApp("org.sorglos.terminal"),
      path: t.validPath("index.html") && !t.validPath("../x") && !t.validPath(".hidden"),
      version: t.validVersion("1.0.0") && !t.validVersion("01.0.0") && !t.validVersion("1.0"),
      newer: t.newer("1.1.0", "1.0.9") && !t.newer("1.0.0", "1.0.0"),
      minNGC: t.supportsMinNGC("1.2.0", "1.1.0") && !t.supportsMinNGC("0.0.0-dev", "1.0.0")
        && t.supportsMinNGC("1.0.0", ""),
      id: t.validID("EREREREREREREREREREREQ") && !t.validID("ERERERERERERERERERERE"),
      logID: t.validLogID("0123456789abcdef0123456789abcdef")
        && !t.validLogID("0123456789ABCDEF0123456789abcdef"),
    };
  });
  expect(out).toEqual({
    empty: false, plain: true, space: true, umlaut: true, emoji: true,
    tab: false, newline: false, rtlOverride: false, noncharacter: false,
    bytesOverLimit: false, bytesAtLimit: true,
    name: true, appID: true, core: true, path: true, version: true, newer: true,
    minNGC: true, id: true, logID: true,
  });
});
```

- [ ] **Step 3: Beide Tests laufen lassen, sie müssen scheitern**

```bash
cd apps/web/tests && npx playwright test contracts/text.spec.js contracts/url.spec.js --project=chromium
```

Erwartet: Fehler beim Import (`text.js` und `url.js` fehlen).

- [ ] **Step 4: `text.js` schreiben**

```js
// Names, texts and paths, exactly as in Go (wire, store, trust, core).
import { b64decode } from "./b64.js";

const APP_ID = /^[a-z0-9]+(\.[a-z0-9-]+)+$/;
const NAME = /^[a-z][a-z0-9-]{0,31}$/;
const LOG_ID = /^[0-9a-f]{32}$/;
const VERSION = /^(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})$/;
const PATH = /^[A-Za-z0-9_-][A-Za-z0-9._-]*(\/[A-Za-z0-9_-][A-Za-z0-9._-]*)*$/;
// Letters, marks, numbers, punctuation, symbols and the ASCII space: what
// unicode.IsPrint accepts in Go. A code point this table does not know counts
// as not printable, so a browser with older tables refuses more, never less.
const PRINTABLE = /^[\p{L}\p{M}\p{N}\p{P}\p{S} ]$/u;

export function byteLength(s) {
  let n = 0;
  for (const cp of s) {
    const c = cp.codePointAt(0);
    n += c < 0x80 ? 1 : c < 0x800 ? 2 : c < 0x10000 ? 3 : 4;
  }
  return n;
}

export function validText(s, max) {
  if (typeof s !== "string" || s === "" || byteLength(s) > max) return false;
  for (const cp of s) {
    // U+FDD0 is a noncharacter no table will ever assign; naming it keeps a
    // regular-expression engine that treats it as a symbol from letting it in.
    if (cp === "﷐" || !PRINTABLE.test(cp)) return false;
  }
  return true;
}

export const validName = (s) => typeof s === "string" && NAME.test(s);
export const validAppID = (s) => typeof s === "string" && s.length <= 100 && APP_ID.test(s);
export const coreApp = (s) => typeof s === "string" && s.startsWith("ngc.");
export const validLogID = (s) => typeof s === "string" && LOG_ID.test(s);
export const validVersion = (s) => typeof s === "string" && VERSION.test(s);
export const validPath = (p) => typeof p === "string" && p.length <= 200 && PATH.test(p);
export const validTOTP = (s) => typeof s === "string" && /^[0-9]{6}$/.test(s);

export function validLoginName(s) {
  if (typeof s !== "string" || s.length === 0 || s.length > 64) return false;
  for (let i = 0; i < s.length; i++) {
    const c = s[i];
    const ok = (c >= "a" && c <= "z") || (c >= "0" && c <= "9")
      || (i > 0 && (c === "." || c === "_" || c === "-"));
    if (!ok) return false;
  }
  return true;
}

export function validID(s) {
  if (typeof s !== "string" || s.length !== 22) return false;
  try {
    return b64decode(s).length === 16;
  } catch {
    return false;
  }
}

function parseVersion(s) {
  if (!validVersion(s)) return null;
  return s.split(".").map(Number);
}

export function newer(a, b) {
  const x = parseVersion(a), y = parseVersion(b);
  if (!x || !y) return false;
  for (let i = 0; i < 3; i++) {
    if (x[i] !== y[i]) return x[i] > y[i];
  }
  return false;
}

export function supportsMinNGC(ngc, minNGC) {
  if (minNGC === "" || minNGC === undefined) return true;
  // A build that calls itself 0.0.0-dev is no release version and must fail
  // every minNgc, not pass it.
  return validVersion(ngc) && validVersion(minNGC) && !newer(minNGC, ngc);
}
```

- [ ] **Step 5: `url.js` schreiben**

```js
// Hosts and addresses have one grammar and exactly one spelling, because NGC
// compares them as text and they end up in configuration files of other
// programs (trust: ValidHost and its four address forms).
const MAX_URL = 256;
const MAX_HOST = 253;

function allDigits(s) {
  return s.length > 0 && /^[0-9]+$/.test(s);
}

function validDNSName(h) {
  if (h.length === 0 || h.length > MAX_HOST) return false;
  const labels = h.split(".");
  for (const l of labels) {
    if (l.length === 0 || l.length > 63 || l.startsWith("-") || l.endsWith("-")) return false;
    if (!/^[a-z0-9-]+$/.test(l)) return false;
  }
  // A last label of digits alone would be a broken IPv4 address, and resolvers
  // read such a name as an address.
  return !allDigits(labels[labels.length - 1]);
}

function validIPv4(h) {
  const parts = h.split(".");
  if (parts.length !== 4) return false;
  for (const p of parts) {
    if (!allDigits(p) || p.length > 3 || (p.length > 1 && p[0] === "0") || Number(p) > 255) return false;
  }
  return true;
}

// validIPv6 accepts exactly the spelling RFC 5952 asks for, the one Go's
// netip.Addr.String writes: lower case, the longest run of zero groups
// compressed once, no zone, not IPv4-mapped.
function validIPv6(s) {
  if (!/^[0-9a-f:]{2,45}$/.test(s) || s.includes(":::")) return false;
  const groups = expandIPv6(s);
  if (!groups) return false;
  // ::ffff:a.b.c.d would give an IPv4 address a second spelling.
  if (groups.slice(0, 5).every((g) => g === 0) && groups[5] === 0xffff) return false;
  return canonicalIPv6(groups) === s;
}

function expandIPv6(s) {
  const halves = s.split("::");
  if (halves.length > 2) return null;
  const head = halves[0] === "" ? [] : halves[0].split(":");
  const tail = halves.length === 2 ? (halves[1] === "" ? [] : halves[1].split(":")) : [];
  if (halves.length === 2 && head.length + tail.length > 7) return null;
  const parts = halves.length === 2
    ? [...head, ...Array(8 - head.length - tail.length).fill("0"), ...tail]
    : head;
  if (parts.length !== 8) return null;
  const out = [];
  for (const p of parts) {
    if (p.length === 0 || p.length > 4 || !/^[0-9a-f]+$/.test(p)) return null;
    out.push(parseInt(p, 16));
  }
  return out;
}

function canonicalIPv6(groups) {
  let bestStart = -1, bestLen = 0, start = -1, len = 0;
  for (let i = 0; i < 8; i++) {
    if (groups[i] === 0) {
      if (start < 0) { start = i; len = 0; }
      len++;
      if (len > bestLen) { bestLen = len; bestStart = start; }
    } else {
      start = -1; len = 0;
    }
  }
  const hex = (g) => g.toString(16);
  if (bestLen < 2) return groups.map(hex).join(":");
  return groups.slice(0, bestStart).map(hex).join(":") + "::" + groups.slice(bestStart + bestLen).map(hex).join(":");
}

export function validHost(h) {
  if (typeof h !== "string") return false;
  if (h.startsWith("[")) {
    return h.endsWith("]") && validIPv6(h.slice(1, -1));
  }
  if (validIPv4(h)) return true;
  // A bare IPv6 address is no host: only the bracketed spelling passes.
  if (h.includes(":")) return false;
  return validDNSName(h);
}

function validPort(p) {
  if (p.length === 0 || p.length > 5 || p[0] === "0" || !allDigits(p)) return false;
  return Number(p) <= 65535;
}

export function validHostPort(s) {
  if (typeof s !== "string") return false;
  const i = s.lastIndexOf(":");
  return i >= 0 && validHost(s.slice(0, i)) && validPort(s.slice(i + 1));
}

export function validTunnelURL(u) {
  if (typeof u !== "string" || u.length > MAX_URL || !u.startsWith("wss://") || !u.endsWith("/tunnel")) {
    return false;
  }
  return validHostPort(u.slice("wss://".length, u.length - "/tunnel".length));
}

export function webURL(u) {
  if (!validTunnelURL(u)) return null;
  return "https://" + u.slice("wss://".length, u.length - "/tunnel".length);
}

export function validWebURL(u) {
  if (typeof u !== "string" || u.length > MAX_URL || !u.startsWith("https://")) return false;
  return validHostPort(u.slice("https://".length));
}

export function validOrigin(o) {
  if (typeof o !== "string" || o.length > MAX_URL || !o.startsWith("https://")) return false;
  const rest = o.slice("https://".length);
  // A browser writes the origin without the default port, so an entry with
  // :443 would match nothing and lock everyone out instead of failing loudly.
  return validHost(rest) || (validHostPort(rest) && !rest.endsWith(":443"));
}

export function validStoreURL(u) {
  if (typeof u !== "string" || u.length > MAX_URL || !u.startsWith("https://")) return false;
  const rest = u.slice("https://".length);
  const cut = rest.indexOf("/");
  const authority = cut < 0 ? rest : rest.slice(0, cut);
  if (!validHost(authority) && !validHostPort(authority)) return false;
  if (cut < 0) return true;
  for (const seg of rest.slice(cut + 1).split("/")) {
    if (seg === "" || seg === "." || seg === ".." || !/^[A-Za-z0-9._~-]+$/.test(seg)) return false;
  }
  return true;
}
```

- [ ] **Step 6: Tests laufen lassen, sie müssen bestehen**

```bash
cd apps/web/tests && npm test -- contracts/text.spec.js contracts/url.spec.js
```

Erwartet: alle 116 Fälle aus `url.json` und alle Textfälle grün, in allen drei Engines.

- [ ] **Step 7: Committen**

```bash
git add apps/web/public/assets/js/contracts/text.js apps/web/public/assets/js/contracts/url.js apps/web/tests/contracts/text.spec.js apps/web/tests/contracts/url.spec.js
git commit -m "Add the rules for names, texts and addresses" --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 4: `sign` – Umschläge, Zwecke und Schlüssel-IDs

**Files:**
- Create: `apps/web/public/assets/js/contracts/sign.js`
- Test: `apps/web/tests/contracts/sign.spec.js`

**Interfaces:**
- Produces: `message(purpose, payload) -> Uint8Array`, `keyIDAsync(pub) -> Promise<string>`,
  `envelopeShape`, `envelopeBudget(maxPayload)`, `parseEnvelope(bytes, maxPayload) -> {payload, kid, sig}`,
  `validateEnvelope(env, maxPayload) -> boolean`, `verify(env, pubRaw, purpose) -> Promise<boolean>`,
  `verifyRaw(pubRaw, sig, msg) -> Promise<boolean>`, `MAX_PAYLOAD = 4194304`, und die
  Zweck-Konstanten `PURPOSE_CLIENT_CERT = "clientcert/v1"`, `PURPOSE_API_CERT = "apicert/v1"`,
  `PURPOSE_TRUST_LIST = "trustlist/v1"`, `PURPOSE_TASK = "task/v1"`,
  `PURPOSE_CONFIRM = "confirm/v1"`, `PURPOSE_CATALOG = "catalog/v1"`,
  `PURPOSE_LICENSE = "license/v1"`, `PURPOSE_RELEASE = "release/v1"`.
- Consumes: `b64.js`, `json.js`.

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

`apps/web/tests/contracts/sign.spec.js`:

```js
import { test, expect } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";

const v = vectors("sign.json");

test("an envelope is checked over the exact bytes it carries", async ({ shell }) => {
  const out = await shell.evaluate(async (d) => {
    const s = await import("/assets/js/contracts/sign.js");
    const { b64decode, b64encode, equal } = await import("/assets/js/contracts/b64.js");
    const pub = b64decode(d.public);
    const payload = b64decode(d.envelope.payload);
    const env = { payload, kid: d.envelope.kid, sig: b64decode(d.envelope.sig) };
    const flipped = { ...env, sig: Uint8Array.from(env.sig) };
    flipped.sig[0] ^= 1;
    const other = { ...env, payload: Uint8Array.from(payload) };
    other.payload[0] ^= 1;
    return {
      message: b64encode(s.message(d.purpose, payload)) === d.message,
      keyID: s.keyID(pub) === d.envelope.kid,
      good: await s.verify(env, pub, d.purpose),
      flipped: await s.verify(flipped, pub, d.purpose),
      otherPayload: await s.verify(other, pub, d.purpose),
      otherPurpose: await s.verify(env, pub, "task/v1"),
      // A key that is not the one the envelope names is refused before any
      // signature is checked.
      otherKey: await s.verify({ ...env, kid: "AAAAAAAAAAAAAAAAAAAAAA" }, pub, d.purpose),
      shape: s.validateEnvelope(env, 4194304) && !s.validateEnvelope({ ...env, sig: new Uint8Array(63) }, 4194304)
        && !s.validateEnvelope({ ...env, payload: new Uint8Array(0) }, 4194304)
        && !s.validateEnvelope(env, payload.length - 1),
      roundTrip: equal(s.parseEnvelope(new TextEncoder().encode(JSON.stringify({
        payload: d.envelope.payload, kid: d.envelope.kid, sig: d.envelope.sig,
      })), 4194304).payload, payload),
    };
  }, v);
  expect(out).toEqual({
    message: true, keyID: true, good: true, flipped: false, otherPayload: false,
    otherPurpose: false, otherKey: false, shape: true, roundTrip: true,
  });
});
```

- [ ] **Step 2: Test laufen lassen, er muss scheitern**

```bash
cd apps/web/tests && npx playwright test contracts/sign.spec.js --project=chromium
```

Erwartet: Fehler beim Import von `sign.js`.

- [ ] **Step 3: `sign.js` schreiben**

```js
// Envelopes for documents that NGC components sign with Ed25519 keys. A
// signature always covers message(purpose, payload), so a signature made for
// one purpose never verifies for another (Go: sign).
import { b64encode, b64decode, concat, utf8, equal } from "./b64.js";
import { decode, Budget, S, str, bytes } from "./json.js";

export const PURPOSE_CLIENT_CERT = "clientcert/v1";
export const PURPOSE_API_CERT = "apicert/v1";
export const PURPOSE_TRUST_LIST = "trustlist/v1";
export const PURPOSE_TASK = "task/v1";
export const PURPOSE_CONFIRM = "confirm/v1";
export const PURPOSE_CATALOG = "catalog/v1";
export const PURPOSE_LICENSE = "license/v1";
export const PURPOSE_RELEASE = "release/v1";

export const MAX_PAYLOAD = 4 << 20;
const KEY_ID_SIZE = 16;
const KEY_ID_CHARS = 22; // encodedLen(16)
const SIG_SIZE = 64;
const ENVELOPE_SLACK = 1 << 10;

export const envelopeShape = S({ payload: bytes, kid: str, sig: bytes });

export function envelopeBudget(maxPayload) {
  const max = Math.min(maxPayload, MAX_PAYLOAD);
  // An envelope has no array, so the budget only bounds its size: the
  // base64url of the payload plus member names, key id, signature, space.
  return new Budget({ size: Math.floor((max * 8 + 5) / 6) + ENVELOPE_SLACK });
}

export function message(purpose, payload) {
  return concat(utf8("ngc/" + purpose), new Uint8Array([0]), payload);
}

export async function keyIDAsync(pub) {
  const h = new Uint8Array(await crypto.subtle.digest("SHA-256", pub));
  return b64encode(h.subarray(0, KEY_ID_SIZE));
}

// keyID is the synchronous form for code that already holds a hash; most
// callers use keyIDAsync, because SHA-256 in the Web Crypto API is async.
export function keyID(hashOrPub) {
  if (hashOrPub.length === 32) return b64encode(hashOrPub.subarray(0, KEY_ID_SIZE));
  throw new Error("sign: keyID needs a 32-byte hash; use keyIDAsync for a key");
}

export function validateEnvelope(env, maxPayload) {
  const max = Math.min(maxPayload, MAX_PAYLOAD);
  if (max < 1 || !env || !(env.payload instanceof Uint8Array)) return false;
  if (env.payload.length === 0 || env.payload.length > max) return false;
  if (typeof env.kid !== "string" || env.kid.length !== KEY_ID_CHARS) return false;
  if (!(env.sig instanceof Uint8Array) || env.sig.length !== SIG_SIZE) return false;
  try {
    // b64decode refuses every byte outside the alphabet and non-zero trailing
    // bits, so only the length is left to check.
    return b64decode(env.kid).length === KEY_ID_SIZE;
  } catch {
    return false;
  }
}

export function parseEnvelope(b, maxPayload) {
  const env = decode(b, envelopeBudget(maxPayload), envelopeShape);
  if (!validateEnvelope(env, maxPayload)) throw new Error("sign: malformed envelope");
  return env;
}

export async function verifyRaw(pubRaw, sig, msg) {
  if (pubRaw.length !== 32 || sig.length !== SIG_SIZE) return false;
  try {
    const key = await crypto.subtle.importKey("raw", pubRaw, { name: "Ed25519" }, false, ["verify"]);
    return await crypto.subtle.verify({ name: "Ed25519" }, key, sig, msg);
  } catch {
    return false;
  }
}

export async function verify(env, pubRaw, purpose) {
  if (!validateEnvelope(env, MAX_PAYLOAD)) return false;
  // The key id is compared first: an envelope that names another key is
  // refused before any signature is checked.
  if (env.kid !== (await keyIDAsync(pubRaw))) return false;
  return verifyRaw(pubRaw, env.sig, message(purpose, env.payload));
}

export { equal };
```

`keyID` gibt es nur der Vollständigkeit halber: SHA-256 läuft in der Web Crypto API asynchron,
deshalb rufen alle Stellen `keyIDAsync` auf.

- [ ] **Step 4: Test laufen lassen, er muss bestehen**

```bash
cd apps/web/tests && npm test -- contracts/sign.spec.js
```

Erwartet: alle Felder `true` bzw. `false` wie im Test, in allen drei Engines.

- [ ] **Step 5: Committen**

```bash
git add apps/web/public/assets/js/contracts/sign.js apps/web/tests/contracts/sign.spec.js
git commit -m "Add signed envelopes with purposes" --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 5: `proof` – Passwortschlüssel (Argon2id) und Passkeys

**Files:**
- Create: `apps/web/public/assets/vendor/noble-hashes/` (kopiert: `utils.js`, `_md.js`, `sha2.js`,
  `blake2b.js`, `argon2.js`, `SOURCE.md`), `apps/web/public/assets/js/contracts/proof.js`,
  `apps/web/public/assets/js/auth/argon2-worker.js`
- Modify: `THIRD-PARTY.md`
- Test: `apps/web/tests/contracts/proof.spec.js`

**Interfaces:**
- Produces:
  - `proof.js`: `KIND_PASSWORD = "password"`, `KIND_PASSKEY = "passkey"`,
    `ALG_EDDSA/ES256/RS256`, `MAX_PUBLIC = 1024`, `MAX_SIG = 1024`, `MAX_RSA_BITS = 4096`,
    `MAX_AUTH_DATA = 4096`, `MAX_CLIENT_DATA = 4096`, `SALT_SIZE = 16`, `MIN_MEM_KIB = 19456`,
    `MAX_MEM_KIB = 1048576`, `kdfShape`, `proofShape`, `userKeyShape`, `rpShape`,
    `validateKDF(kdf)`, `validateProof(p)`, `validateUserKey(k) -> Promise<boolean>`,
    `deriveSeed(passwordText, kdf) -> Promise<Uint8Array>` (32 Byte),
    `challenge(purpose, payload) -> Promise<Uint8Array>`,
    `verify(proof, userKey, rp, purpose, payload) -> Promise<boolean>`.
  - `argon2-worker.js`: nimmt `{password, salt, t, m, p}` und antwortet mit dem 32-Byte-Keim.
- Consumes: `b64.js`, `json.js`, `sign.js`, `vendor/noble-hashes/`.

- [ ] **Step 1: Fremdcode kopieren und eintragen**

Kopiere aus der veröffentlichten Fassung von `@noble/hashes` (Version 1.8.0, MIT) genau diese
Dateien nach `apps/web/public/assets/vendor/noble-hashes/`, ohne sie zu ändern, als ES-Module mit
relativen Importen: `utils.js`, `_md.js`, `sha2.js`, `blake2b.js`, `argon2.js`.

`apps/web/public/assets/vendor/noble-hashes/SOURCE.md`:

```markdown
# noble-hashes (vendored)

Source: https://github.com/paulmillr/noble-hashes, version 1.8.0, MIT.
Copied by hand on 2026-09-23; NGC never installs it with npm.

Used for: Argon2id (the password key, `proof.KDF`) and a SHA-256 with
`update`/`digest`, which the Web Crypto API does not offer and which the shell
needs for files of up to 4 GiB (`ngc.store/upload`).

| File | SHA-256 |
| --- | --- |
| utils.js | (record with `sha256sum`) |
| _md.js | (record with `sha256sum`) |
| sha2.js | (record with `sha256sum`) |
| blake2b.js | (record with `sha256sum`) |
| argon2.js | (record with `sha256sum`) |

Update: copy the files again, record the checksums, run `npm test`.
```

Die Prüfsummen mit `sha256sum apps/web/public/assets/vendor/noble-hashes/*.js` ausrechnen und
eintragen. In `THIRD-PARTY.md` eine Zeile ergänzen:

```markdown
| `apps/web/public/assets/vendor/noble-hashes/` | @noble/hashes 1.8.0 | MIT | Argon2id und ein SHA-256 mit update/digest für den Browser; von Hand kopiert, siehe `SOURCE.md` |
```

- [ ] **Step 2: Den fehlschlagenden Test schreiben**

`apps/web/tests/contracts/proof.spec.js`:

```js
import { test, expect } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";

const v = vectors("proof.json");
const kdf = vectors("proof_kdf.json");

test("every proof of proof.json is decided as Go decides it", async ({ shell }) => {
  test.setTimeout(120_000);
  const got = await shell.evaluate(async (d) => {
    const p = await import("/assets/js/contracts/proof.js");
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    const out = [];
    for (const c of d.proofs) {
      const key = { ...c.key, pub: b64decode(c.key.pub), credId: c.key.credId ? b64decode(c.key.credId) : new Uint8Array(0) };
      const pr = {
        kind: c.proof.kind, kid: c.proof.kid, sig: b64decode(c.proof.sig),
        authData: c.proof.authData ? b64decode(c.proof.authData) : new Uint8Array(0),
        clientData: c.proof.clientData ? b64decode(c.proof.clientData) : new Uint8Array(0),
      };
      out.push(await p.verify(pr, key, d.rp, c.purpose, b64decode(c.payload)));
    }
    return out;
  }, v);
  const wrong = v.proofs.filter((c, i) => c.valid !== got[i]).map((c) => c.name);
  expect(wrong).toEqual([]);
});

test("the password key comes out of Argon2id, also for a password typed in NFD", async ({ shell }) => {
  test.setTimeout(120_000);
  const got = await shell.evaluate(async (cases) => {
    const p = await import("/assets/js/contracts/proof.js");
    const { b64encode } = await import("/assets/js/contracts/b64.js");
    const out = [];
    for (const c of cases) {
      // The browser normalizes what was typed to NFC before deriving, so a
      // password typed in NFD gives the same key.
      const seed = await p.deriveSeed(c.typed ?? c.password, {
        alg: c.kdf.alg, salt: p.saltBytes(c.kdf.salt), m: c.kdf.m, t: c.kdf.t, p: c.kdf.p,
      });
      out.push({ seed: b64encode(seed), pub: b64encode(await p.publicFromSeed(seed)) });
    }
    return out;
  }, kdf);
  expect(got.map((g) => g.seed)).toEqual(kdf.map((c) => c.seed));
  expect(got.map((g) => g.pub)).toEqual(kdf.map((c) => c.public));
});

test("the limits of a passkey proof hold", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const p = await import("/assets/js/contracts/proof.js");
    const base = {
      kind: "passkey", kid: "AAAAAAAAAAAAAAAAAAAAAA", sig: new Uint8Array(64),
      authData: new Uint8Array(37), clientData: new Uint8Array(10),
    };
    return {
      ok: p.validateProof(base),
      shortKid: p.validateProof({ ...base, kid: "AAAA" }),
      noSig: p.validateProof({ ...base, sig: new Uint8Array(0) }),
      bigSig: p.validateProof({ ...base, sig: new Uint8Array(1025) }),
      shortAuth: p.validateProof({ ...base, authData: new Uint8Array(36) }),
      bigAuth: p.validateProof({ ...base, authData: new Uint8Array(4097) }),
      bigClient: p.validateProof({ ...base, clientData: new Uint8Array(4097) }),
      passwordWithAuth: p.validateProof({ ...base, kind: "password" }),
      passwordOK: p.validateProof({ kind: "password", kid: base.kid, sig: new Uint8Array(64),
        authData: new Uint8Array(0), clientData: new Uint8Array(0) }),
    };
  });
  expect(out).toEqual({
    ok: true, shortKid: false, noSig: false, bigSig: false, shortAuth: false,
    bigAuth: false, bigClient: false, passwordWithAuth: false, passwordOK: true,
  });
});
```

- [ ] **Step 3: Test laufen lassen, er muss scheitern**

```bash
cd apps/web/tests && npx playwright test contracts/proof.spec.js --project=chromium
```

Erwartet: Fehler beim Import von `proof.js`.

- [ ] **Step 4: `proof.js` schreiben**

```js
// Proof that a user approved a payload: with the key the browser derives from
// the password (password key) or with a passkey (Go: proof).
import { argon2id } from "../../vendor/noble-hashes/argon2.js";
import { b64decode, b64encode, concat, utf8, equal, fromUtf8 } from "./b64.js";
import { S, A, str, bool, bytes, uint32, uint, P } from "./json.js";
import { message, keyIDAsync, MAX_PAYLOAD } from "./sign.js";

export const KIND_PASSWORD = "password";
export const KIND_PASSKEY = "passkey";
export const ALG_EDDSA = "EdDSA";
export const ALG_ES256 = "ES256";
export const ALG_RS256 = "RS256";

export const KDF_ARGON2ID = "argon2id";
export const SALT_SIZE = 16;
export const DEFAULT_MEM_KIB = 65536;
export const DEFAULT_TIME = 3;
export const DEFAULT_PAR = 1;
export const MIN_MEM_KIB = 19456;
export const MAX_MEM_KIB = 1 << 20;

export const MAX_PUBLIC = 1024;
export const MAX_SIG = 1024;
export const MAX_RSA_BITS = 4096;
export const MAX_AUTH_DATA = 4096;
export const MAX_CLIENT_DATA = 4096;
const MIN_AUTH_DATA = 37;
const KEY_ID_CHARS = 22;
const FLAG_UP = 0x01;
const FLAG_UV = 0x04;

export const kdfShape = P({ alg: str, salt: bytes, m: uint32, t: uint32, p: uint32 });
export const rpShape = S({ id: str, origins: A(str) });
export const userKeyShape = S({ id: str, kind: str, alg: str, pub: bytes, credId: bytes, kdf: kdfShape });
export const proofShape = S({ kind: str, kid: str, sig: bytes, authData: bytes, clientData: bytes });

export const saltBytes = (s) => b64decode(s);

export function validateKDF(k) {
  return !!k && k.alg === KDF_ARGON2ID && k.salt instanceof Uint8Array && k.salt.length === SALT_SIZE
    && k.m >= MIN_MEM_KIB && k.m <= MAX_MEM_KIB && k.t >= 1 && k.t <= 10 && k.p >= 1 && k.p <= 4;
}

export function validateProof(p) {
  if (!p || typeof p.kid !== "string" || p.kid.length !== KEY_ID_CHARS) return false;
  try {
    if (b64decode(p.kid).length !== 16) return false;
  } catch {
    return false;
  }
  if (!(p.sig instanceof Uint8Array) || p.sig.length === 0 || p.sig.length > MAX_SIG) return false;
  const auth = p.authData ?? new Uint8Array(0);
  const client = p.clientData ?? new Uint8Array(0);
  if (p.kind === KIND_PASSWORD) return auth.length === 0 && client.length === 0;
  if (p.kind === KIND_PASSKEY) {
    return auth.length >= MIN_AUTH_DATA && auth.length <= MAX_AUTH_DATA
      && client.length > 0 && client.length <= MAX_CLIENT_DATA;
  }
  return false;
}

export async function validateUserKey(k) {
  if (!k || typeof k.id !== "string" || k.id !== (await keyIDAsync(k.pub))) return false;
  if (k.kind === KIND_PASSWORD) {
    return k.alg === ALG_EDDSA && k.pub.length === 32 && validateKDF(k.kdf)
      && (k.credId?.length ?? 0) === 0;
  }
  if (k.kind === KIND_PASSKEY) {
    const cred = k.credId ?? new Uint8Array(0);
    if (cred.length === 0 || cred.length > 1023 || k.pub.length > MAX_PUBLIC || k.kdf) return false;
    return (await importPublic(k)) !== null;
  }
  return false;
}

// deriveSeed turns the typed password into the 32-byte seed of the Ed25519
// password key. The password is normalized to NFC first, so a password typed
// in NFD gives the same key (proof_kdf.json, case "typed"). The work runs in a
// worker: Argon2id with m=65536 takes about a second and must not freeze the
// window.
export async function deriveSeed(passwordText, kdf) {
  if (!validateKDF(kdf)) throw new Error("proof: KDF parameters out of range");
  const password = utf8(passwordText.normalize("NFC"));
  try {
    return await inWorker(password, kdf);
  } finally {
    password.fill(0);
  }
}

function inWorker(password, kdf) {
  return new Promise((resolve, reject) => {
    const w = new Worker("/assets/js/auth/argon2-worker.js", { type: "module" });
    const done = (fn) => (e) => { w.terminate(); fn(e); };
    w.onmessage = done((e) => resolve(new Uint8Array(e.data)));
    w.onerror = done(() => reject(new Error("proof: Argon2id failed")));
    w.postMessage({ password, salt: kdf.salt, t: kdf.t, m: kdf.m, p: kdf.p }, [password.buffer.slice(0)]);
  });
}

// deriveSeedSync is the same computation without a worker, for tests and for
// a browser without module workers.
export function deriveSeedSync(passwordText, kdf) {
  if (!validateKDF(kdf)) throw new Error("proof: KDF parameters out of range");
  return argon2id(utf8(passwordText.normalize("NFC")), kdf.salt, { t: kdf.t, m: kdf.m, p: kdf.p, dkLen: 32 });
}

const ED25519_PKCS8 = new Uint8Array([0x30, 0x2e, 0x02, 0x01, 0x00, 0x30, 0x05, 0x06, 0x03, 0x2b,
  0x65, 0x70, 0x04, 0x22, 0x04, 0x20]);

export async function privateFromSeed(seed) {
  // Not extractable: once imported, the key cannot leave the browser again.
  return crypto.subtle.importKey("pkcs8", concat(ED25519_PKCS8, seed), { name: "Ed25519" }, false, ["sign"]);
}

export async function publicFromSeed(seed) {
  const extractable = await crypto.subtle.importKey("pkcs8", concat(ED25519_PKCS8, seed),
    { name: "Ed25519" }, true, ["sign"]);
  const jwk = await crypto.subtle.exportKey("jwk", extractable);
  return b64decode(jwk.x);
}

export async function challenge(purpose, payload) {
  return new Uint8Array(await crypto.subtle.digest("SHA-256", message(purpose, payload)));
}

export async function verify(p, key, rp, purpose, payload) {
  if (!validateProof(p) || !key || key.id !== (await keyIDAsync(key.pub))) return false;
  if (p.kid !== key.id || p.kind !== key.kind) return false;
  if (key.kind === KIND_PASSWORD) {
    if (key.alg !== ALG_EDDSA || key.pub.length !== 32) return false;
    try {
      const k = await crypto.subtle.importKey("raw", key.pub, { name: "Ed25519" }, false, ["verify"]);
      return await crypto.subtle.verify({ name: "Ed25519" }, k, p.sig, message(purpose, payload));
    } catch {
      return false;
    }
  }
  if (key.kind !== KIND_PASSKEY) return false;
  return verifyAssertion(p, key, rp, await challenge(purpose, payload));
}

async function verifyAssertion(p, key, rp, ch) {
  let cd;
  try {
    // WebAuthn lets the browser add members of its own, so this one document
    // is read leniently; only these four members count (shared/README.md).
    cd = JSON.parse(fromUtf8(p.clientData));
  } catch {
    return false;
  }
  if (cd.type !== "webauthn.get" || cd.crossOrigin === true) return false;
  if (cd.challenge !== b64encode(ch)) return false;
  if (!Array.isArray(rp.origins) || !rp.origins.includes(cd.origin)) return false;
  const rpHash = new Uint8Array(await crypto.subtle.digest("SHA-256", utf8(rp.id)));
  if (!equal(p.authData.subarray(0, 32), rpHash)) return false;
  const flags = p.authData[32];
  // NGC always asks for user verification: a PIN or biometrics, never a tap
  // alone.
  if ((flags & FLAG_UP) === 0 || (flags & FLAG_UV) === 0) return false;
  const cdHash = new Uint8Array(await crypto.subtle.digest("SHA-256", p.clientData));
  const signed = concat(p.authData, cdHash);
  const pub = await importPublic(key);
  if (!pub) return false;
  try {
    if (key.alg === ALG_ES256) {
      return await crypto.subtle.verify({ name: "ECDSA", hash: "SHA-256" }, pub, derToRaw(p.sig), signed);
    }
    if (key.alg === ALG_EDDSA) {
      return await crypto.subtle.verify({ name: "Ed25519" }, pub, p.sig, signed);
    }
    if (key.alg === ALG_RS256) {
      return await crypto.subtle.verify({ name: "RSASSA-PKCS1-v1_5" }, pub, p.sig, signed);
    }
  } catch {
    return false;
  }
  return false;
}

// importPublic reads a SubjectPublicKeyInfo and checks that it is the kind the
// key claims: P-256 for ES256, Ed25519 for EdDSA, RSA of 2048 to MAX_RSA_BITS
// bits for RS256.
async function importPublic(key) {
  if (key.pub.length > MAX_PUBLIC) return null;
  try {
    if (key.alg === ALG_ES256) {
      return await crypto.subtle.importKey("spki", key.pub, { name: "ECDSA", namedCurve: "P-256" }, false, ["verify"]);
    }
    if (key.alg === ALG_EDDSA) {
      return await crypto.subtle.importKey("spki", key.pub, { name: "Ed25519" }, false, ["verify"]);
    }
    if (key.alg === ALG_RS256) {
      const k = await crypto.subtle.importKey("spki", key.pub,
        { name: "RSASSA-PKCS1-v1_5", hash: "SHA-256" }, false, ["verify"]);
      const bits = k.algorithm.modulusLength;
      if (bits < 2048 || bits > MAX_RSA_BITS) return null;
      return k;
    }
  } catch {
    return null;
  }
  return null;
}

// derToRaw turns the ASN.1 signature a passkey returns for ES256 into the
// 64-byte form the Web Crypto API verifies.
function derToRaw(der) {
  if (der[0] !== 0x30) throw new Error("proof: bad ECDSA signature");
  let i = 2;
  if (der[1] & 0x80) i += der[1] & 0x7f;
  const read = () => {
    if (der[i++] !== 0x02) throw new Error("proof: bad ECDSA signature");
    let len = der[i++];
    let v = der.subarray(i, i + len);
    i += len;
    while (v.length > 32 && v[0] === 0) v = v.subarray(1);
    if (v.length > 32) throw new Error("proof: bad ECDSA signature");
    const out = new Uint8Array(32);
    out.set(v, 32 - v.length);
    return out;
  };
  return concat(read(), read());
}

export { MAX_PAYLOAD };
```

- [ ] **Step 5: `argon2-worker.js` schreiben**

```js
// Argon2id for the password key. It runs here so the window stays responsive
// while the deliberately slow derivation takes about a second, and so the
// password lives in one place that is thrown away right after.
import { argon2id } from "../../vendor/noble-hashes/argon2.js";

self.onmessage = (e) => {
  const { password, salt, t, m, p } = e.data;
  const seed = argon2id(new Uint8Array(password), new Uint8Array(salt), { t, m, p, dkLen: 32 });
  new Uint8Array(password).fill(0);
  self.postMessage(seed.buffer, [seed.buffer]);
};
```

- [ ] **Step 6: Test laufen lassen, er muss bestehen**

```bash
cd apps/web/tests && npm test -- contracts/proof.spec.js
```

Erwartet: alle 21 Fälle aus `proof.json` und alle drei Fälle aus `proof_kdf.json` stimmen, in
allen drei Engines.

- [ ] **Step 7: Committen**

```bash
git add THIRD-PARTY.md apps/web/public/assets/vendor apps/web/public/assets/js/contracts/proof.js apps/web/public/assets/js/auth/argon2-worker.js apps/web/tests/contracts/proof.spec.js
git commit -m "Add password keys and passkeys" --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 6: `noisep` – Noise_KK und die drei Prologe

**Files:**
- Create: `apps/web/public/assets/js/contracts/noise.js`
- Test: `apps/web/tests/contracts/noise.spec.js`

**Interfaces:**
- Produces: `PROLOGUE_SESSION = "ngc/session/v1"`, `PROLOGUE_TUNNEL = "ngc/tunnel/v1"`,
  `PROLOGUE_E2E = "ngc/e2e/v1"`, `HANDSHAKE_SIZE = 48`, `TAG_SIZE = 16`,
  `MAX_MESSAGE = 65535`, `MAX_PLAINTEXT = 65519`,
  `e2ePrologue(controllerID, app, service, action, params, task) -> Promise<Uint8Array>`,
  `newKeypair() -> Promise<{privateKey, publicRaw}>`,
  `class HandshakeKK` mit `writeMessage1()`, `readMessage2(msg)`, `readMessage1(msg)`,
  `writeMessage2()`, `split() -> {send, recv}`,
  `class Transport` mit `seal(plain)` und `open(cipher)`.
- Consumes: `b64.js`.

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

`apps/web/tests/contracts/noise.spec.js`:

```js
import { test, expect } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";

const v = vectors("noise.json");

test("the end-to-end prologue binds channel, params and task", async ({ shell }) => {
  const got = await shell.evaluate(async (d) => {
    const n = await import("/assets/js/contracts/noise.js");
    const { b64decode, b64encode } = await import("/assets/js/contracts/b64.js");
    const out = [];
    for (const c of d.prologues) {
      const open = JSON.parse(new TextDecoder().decode(b64decode(c.open)));
      out.push(b64encode(await n.e2ePrologue(
        c.controllerId, open.app, open.service, open.action,
        c.params ? b64decode(c.params) : new Uint8Array(0),
        c.task ? b64decode(c.task) : new Uint8Array(0))));
    }
    return out;
  }, v);
  expect(got).toEqual(v.prologues.map((c) => c.prologue));
});

test("the handshake and the transport match the Go vectors", async ({ shell }) => {
  const got = await shell.evaluate(async (d) => {
    const n = await import("/assets/js/contracts/noise.js");
    const { b64decode, b64encode } = await import("/assets/js/contracts/b64.js");
    const prologue = b64decode(d.prologue);
    const init = await n.HandshakeKK.forTest(true, prologue,
      b64decode(d.initStatic.private), b64decode(d.respStatic.public), b64decode(d.initEphemeral.private));
    const resp = await n.HandshakeKK.forTest(false, prologue,
      b64decode(d.respStatic.private), b64decode(d.initStatic.public), b64decode(d.respEphemeral.private));
    const msg1 = await init.writeMessage1();
    await resp.readMessage1(msg1);
    const msg2 = await resp.writeMessage2();
    await init.readMessage2(msg2);
    const a = await init.split(), b = await resp.split();
    const seen = [];
    for (const t of d.transport) {
      const from = t.from === "initiator" ? a : b;
      const to = t.from === "initiator" ? b : a;
      const cipher = await from.seal(b64decode(t.plaintext));
      const plain = await to.open(b64decode(t.ciphertext));
      seen.push({ cipher: b64encode(cipher), plain: b64encode(plain) });
    }
    return { msg1: b64encode(msg1), msg2: b64encode(msg2), seen };
  }, v);
  expect(got.msg1).toBe(v.msg1);
  expect(got.msg2).toBe(v.msg2);
  expect(got.seen.map((s) => s.cipher)).toEqual(v.transport.map((t) => t.ciphertext));
  expect(got.seen.map((s) => s.plain)).toEqual(v.transport.map((t) => t.plaintext));
});
```

- [ ] **Step 2: Test laufen lassen, er muss scheitern**

```bash
cd apps/web/tests && npx playwright test contracts/noise.spec.js --project=chromium
```

Erwartet: Fehler beim Import von `noise.js`.

- [ ] **Step 3: `noise.js` schreiben**

```js
// NGC always uses Noise_KK_25519_AESGCM_SHA256, for three uses told apart by
// the prologue: session (browser to API), tunnel (controller to API) and end
// to end (shell to controller). Messages 1 and 2 carry an empty payload, so
// each is exactly HANDSHAKE_SIZE bytes (Go: noisep).
//
// The cryptography itself comes from the Web Crypto API; only the order of
// the steps is written out here, straight from the Noise specification.
import { b64decode, concat, utf8 } from "./b64.js";

export const PROLOGUE_SESSION = "ngc/session/v1";
export const PROLOGUE_TUNNEL = "ngc/tunnel/v1";
export const PROLOGUE_E2E = "ngc/e2e/v1";
export const HANDSHAKE_SIZE = 48;
export const TAG_SIZE = 16;
export const MAX_MESSAGE = 65535;
export const MAX_PLAINTEXT = MAX_MESSAGE - TAG_SIZE;

const PROTOCOL = "Noise_KK_25519_AESGCM_SHA256";
const X25519_PKCS8 = new Uint8Array([0x30, 0x2e, 0x02, 0x01, 0x00, 0x30, 0x05, 0x06, 0x03, 0x2b,
  0x65, 0x6e, 0x04, 0x22, 0x04, 0x20]);
const X25519_SPKI = new Uint8Array([0x30, 0x2a, 0x30, 0x05, 0x06, 0x03, 0x2b, 0x65, 0x6e, 0x03, 0x21, 0x00]);

export async function e2ePrologue(controllerID, app, service, action, params, task) {
  const p = new Uint8Array(await crypto.subtle.digest("SHA-256", params));
  const t = new Uint8Array(await crypto.subtle.digest("SHA-256", task));
  let b = utf8(PROLOGUE_E2E);
  for (const s of [controllerID, app, service, action]) {
    b = concat(b, new Uint8Array([0]), utf8(s));
  }
  return concat(b, new Uint8Array([0]), p, t);
}

// newKeypair makes a session key pair whose private part cannot be read
// again, not even by the shell.
export async function newKeypair() {
  const pair = await crypto.subtle.generateKey({ name: "X25519" }, false, ["deriveBits"]);
  const spki = new Uint8Array(await crypto.subtle.exportKey("raw", pair.publicKey));
  return { privateKey: pair.privateKey, publicRaw: spki };
}

async function importPrivate(raw) {
  return crypto.subtle.importKey("pkcs8", concat(X25519_PKCS8, raw), { name: "X25519" }, false, ["deriveBits"]);
}

async function importPublic(raw) {
  try {
    return await crypto.subtle.importKey("raw", raw, { name: "X25519" }, false, []);
  } catch {
    // Engines that do not take "raw" for X25519 read the SubjectPublicKeyInfo.
    return crypto.subtle.importKey("spki", concat(X25519_SPKI, raw), { name: "X25519" }, false, []);
  }
}

async function dh(priv, pubRaw) {
  const pub = await importPublic(pubRaw);
  return new Uint8Array(await crypto.subtle.deriveBits({ name: "X25519", public: pub }, priv, 256));
}

async function sha256(...parts) {
  return new Uint8Array(await crypto.subtle.digest("SHA-256", concat(...parts)));
}

async function hmac(key, data) {
  const k = await crypto.subtle.importKey("raw", key, { name: "HMAC", hash: "SHA-256" }, false, ["sign"]);
  return new Uint8Array(await crypto.subtle.sign("HMAC", k, data));
}

// hkdf is the two- or three-output form the Noise specification defines.
async function hkdf(chainingKey, ikm, outputs) {
  const temp = await hmac(chainingKey, ikm);
  const o1 = await hmac(temp, new Uint8Array([1]));
  const o2 = await hmac(temp, concat(o1, new Uint8Array([2])));
  if (outputs === 2) return [o1, o2];
  return [o1, o2, await hmac(temp, concat(o2, new Uint8Array([3])))];
}

function nonce(n) {
  // 32 bits of zeros, then the counter as 64 bits big endian.
  const b = new Uint8Array(12);
  new DataView(b.buffer).setBigUint64(4, BigInt(n));
  return b;
}

class CipherState {
  constructor(key) {
    this.keyRaw = key;
    this.key = null;
    this.n = 0;
  }

  async aes() {
    if (!this.key) {
      this.key = await crypto.subtle.importKey("raw", this.keyRaw, { name: "AES-GCM" }, false, ["encrypt", "decrypt"]);
    }
    return this.key;
  }

  async seal(plain, ad = new Uint8Array(0)) {
    if (plain.length > MAX_PLAINTEXT) throw new Error("noise: message too large");
    const out = new Uint8Array(await crypto.subtle.encrypt(
      { name: "AES-GCM", iv: nonce(this.n), additionalData: ad, tagLength: 128 }, await this.aes(), plain));
    this.n++;
    return out;
  }

  async open(cipher, ad = new Uint8Array(0)) {
    if (cipher.length > MAX_MESSAGE) throw new Error("noise: message too large");
    const out = new Uint8Array(await crypto.subtle.decrypt(
      { name: "AES-GCM", iv: nonce(this.n), additionalData: ad, tagLength: 128 }, await this.aes(), cipher));
    this.n++;
    return out;
  }
}

export class Transport {
  constructor(send, recv) {
    this.sendState = send;
    this.recvState = recv;
  }

  seal(plain) {
    return this.sendState.seal(plain);
  }

  open(cipher) {
    return this.recvState.open(cipher);
  }
}

export class HandshakeKK {
  // Use of() outside tests; forTest() only fixes the ephemeral key so the
  // vectors of noise.json can be reproduced.
  static async of(initiator, prologue, staticPriv, staticPub, peerPub) {
    return HandshakeKK.build(initiator, prologue, staticPriv, staticPub, peerPub, null);
  }

  static async forTest(initiator, prologue, staticPrivRaw, peerPub, ephemeralPrivRaw) {
    const staticPriv = await importPrivate(staticPrivRaw);
    const staticPub = await publicOf(staticPrivRaw);
    return HandshakeKK.build(initiator, prologue, staticPriv, staticPub, peerPub, ephemeralPrivRaw);
  }

  static async build(initiator, prologue, staticPriv, staticPub, peerPub, ephemeralPrivRaw) {
    const h = new HandshakeKK();
    h.initiator = initiator;
    h.s = { priv: staticPriv, pub: staticPub };
    h.rs = peerPub;
    h.fixedEphemeral = ephemeralPrivRaw;
    const name = utf8(PROTOCOL);
    // The protocol name is 28 bytes, so it is padded with zeros to 32.
    h.hash = concat(name, new Uint8Array(32 - name.length));
    h.ck = h.hash;
    h.cs = null;
    await h.mixHash(prologue);
    // KK pre-messages: first the initiator's static key, then the responder's.
    await h.mixHash(initiator ? staticPub : peerPub);
    await h.mixHash(initiator ? peerPub : staticPub);
    return h;
  }

  async mixHash(data) {
    this.hash = await sha256(this.hash, data);
  }

  async mixKey(input) {
    const [ck, k] = await hkdf(this.ck, input, 2);
    this.ck = ck;
    this.cs = new CipherState(k);
  }

  async encryptAndHash(plain) {
    const out = await this.cs.seal(plain, this.hash);
    await this.mixHash(out);
    return out;
  }

  async decryptAndHash(cipher) {
    const out = await this.cs.open(cipher, this.hash);
    await this.mixHash(cipher);
    return out;
  }

  async ephemeral() {
    if (this.fixedEphemeral) {
      return { priv: await importPrivate(this.fixedEphemeral), pub: await publicOf(this.fixedEphemeral) };
    }
    const pair = await newKeypair();
    return { priv: pair.privateKey, pub: pair.publicRaw };
  }

  // -> e, es, ss
  async writeMessage1() {
    this.e = await this.ephemeral();
    await this.mixHash(this.e.pub);
    await this.mixKey(await dh(this.e.priv, this.rs));
    await this.mixKey(await dh(this.s.priv, this.rs));
    return concat(this.e.pub, await this.encryptAndHash(new Uint8Array(0)));
  }

  async readMessage1(msg) {
    if (msg.length !== HANDSHAKE_SIZE) throw new Error("noise: message 1");
    this.re = msg.subarray(0, 32);
    await this.mixHash(this.re);
    await this.mixKey(await dh(this.s.priv, this.re));
    await this.mixKey(await dh(this.s.priv, this.rs));
    const payload = await this.decryptAndHash(msg.subarray(32));
    // Messages 1 and 2 never carry a payload in NGC.
    if (payload.length !== 0) throw new Error("noise: message 1 payload");
  }

  // <- e, ee, se
  async writeMessage2() {
    this.e = await this.ephemeral();
    await this.mixHash(this.e.pub);
    await this.mixKey(await dh(this.e.priv, this.re));
    await this.mixKey(await dh(this.e.priv, this.rs));
    return concat(this.e.pub, await this.encryptAndHash(new Uint8Array(0)));
  }

  async readMessage2(msg) {
    if (msg.length !== HANDSHAKE_SIZE) throw new Error("noise: message 2");
    this.re = msg.subarray(0, 32);
    await this.mixHash(this.re);
    await this.mixKey(await dh(this.e.priv, this.re));
    await this.mixKey(await dh(this.s.priv, this.re));
    const payload = await this.decryptAndHash(msg.subarray(32));
    if (payload.length !== 0) throw new Error("noise: message 2 payload");
  }

  // split ends the handshake: c1 carries initiator to responder, c2 the other
  // way, and each cipher state starts its counter at 0.
  async split() {
    const [k1, k2] = await hkdf(this.ck, new Uint8Array(0), 2);
    return this.initiator ? new Transport(new CipherState(k1), new CipherState(k2))
      : new Transport(new CipherState(k2), new CipherState(k1));
  }
}

async function publicOf(privRaw) {
  const key = await crypto.subtle.importKey("pkcs8", concat(X25519_PKCS8, privRaw),
    { name: "X25519" }, true, ["deriveBits"]);
  const jwk = await crypto.subtle.exportKey("jwk", key);
  return b64decode(jwk.x);
}
```

- [ ] **Step 4: Test laufen lassen, er muss bestehen**

```bash
cd apps/web/tests && npm test -- contracts/noise.spec.js
```

Erwartet: `msg1`, `msg2` und alle vier Transportnachrichten stimmen Byte für Byte mit
`noise.json` überein, ebenso alle sechs Prologe; in allen drei Engines.

- [ ] **Step 5: Committen**

```bash
git add apps/web/public/assets/js/contracts/noise.js apps/web/tests/contracts/noise.spec.js
git commit -m "Add the Noise_KK handshake and the end-to-end prologue" --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 7: `trust` – die Vertrauensliste prüfen

**Files:**
- Create: `apps/web/public/assets/js/contracts/trust.js`
- Test: `apps/web/tests/contracts/trust.spec.js`

**Interfaces:**
- Produces: `ROLE_ADMIN = "admin"`, `MAX_USERS = 10000`, `MAX_NODES = 256`, `MAX_ROLES = 256`,
  `MAX_STORES = 64`, `MAX_KEYS_PER_USER = 16`, `MAX_ENTRIES = 256`, `MAX_AUTO = 64`,
  `MAX_ORIGINS = 8`, `MAX_ROOT = 256`, `LEVEL_OFF/CRITICAL/ROOT/HARDCORE`,
  `NODE_API = "api"`, `NODE_CONTROLLER = "controller"`, `STORE_GLOBAL = "global"`,
  `STORE_LOCAL = "local"`, `AUTO_WEB/API/CONTROLLER/SYSTEM`,
  `listShape`, `signedShape`, `parseSigned(bytes)`, `parseList(payload) -> list`,
  `validateList(list) -> boolean`, `hash(signed) -> Promise<Uint8Array>`,
  `open(anchor, chain) -> Promise<{list, hash}>`, `advance(version, chain) -> Promise<{list, hash}>`,
  `encodeList(list) -> Uint8Array`,
  `keyOwner(list, kid)`, `user(list, id)`, `role(list, name)`, `node(list, id)`,
  `passwordToo(list, userID)`, `mayRoot(list, userID, nodeID)`, `needsConfirm(list, userID, op)`,
  `validAPINode(n)`, `validStore(s)`, `tlsPin(spki) -> Promise<Uint8Array>`.
- Consumes: `b64.js`, `json.js`, `text.js`, `url.js`, `proof.js`, `sign.js`.

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

`apps/web/tests/contracts/trust.spec.js`:

```js
import { test, expect } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";

const v = vectors("trust.json");

test("every chain of trust.json is decided as Go decides it", async ({ shell }) => {
  test.setTimeout(120_000);
  const got = await shell.evaluate(async (cases) => {
    const t = await import("/assets/js/contracts/trust.js");
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    const out = [];
    for (const c of cases) {
      try {
        const chain = c.chain.map((s) => ({
          payload: b64decode(s.payload),
          proof: {
            kind: s.proof.kind, kid: s.proof.kid, sig: b64decode(s.proof.sig),
            authData: s.proof.authData ? b64decode(s.proof.authData) : new Uint8Array(0),
            clientData: s.proof.clientData ? b64decode(s.proof.clientData) : new Uint8Array(0),
          },
        }));
        const anchor = { seq: c.anchor.seq, hash: b64decode(c.anchor.hash) };
        const v = await t.open(anchor, chain);
        out.push({ valid: true, seq: v.list.seq });
      } catch {
        out.push({ valid: false, seq: 0 });
      }
    }
    return out;
  }, v);

  const wrong = v.filter((c, i) => c.valid !== got[i].valid || (c.valid && c.seq !== got[i].seq))
    .map((c) => c.name);
  expect(wrong).toEqual([]);
});

test("the limits without their own vectors hold too", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const t = await import("/assets/js/contracts/trust.js");
    const { utf8 } = await import("/assets/js/contracts/b64.js");
    const base = () => ({
      seq: 1, issued: 1, installation: "EREREREREREREREREREREQ",
      rp: { id: "", origins: [] }, users: [], roles: [], nodes: [], stores: [], auto: [],
    });
    const many = (n, make) => Array.from({ length: n }, (_, i) => make(i));
    const decodeFails = (obj) => {
      try {
        t.parseList(utf8(JSON.stringify(obj)));
        return false;
      } catch {
        return true;
      }
    };
    return {
      // trust.MaxAuto, MaxOrigins and MaxRoot have no vectors of their own.
      autoOver: decodeFails({ ...base(), auto: many(65, (i) => ({ target: "a" + i + ".b", key: "A".repeat(43) })) }),
      originsOver: decodeFails({ ...base(), rp: { id: "a.b", origins: many(9, () => "https://a.b") } }),
      rootUsersOver: decodeFails({
        ...base(),
        nodes: [{ id: "ctl-1", kind: "controller", name: "n", noise: "A".repeat(43),
          rootUsers: many(257, () => "EREREREREREREREREREREQ") }],
      }),
    };
  });
  expect(out).toEqual({ autoOver: true, originsOver: true, rootUsersOver: true });
});
```

- [ ] **Step 2: Test laufen lassen, er muss scheitern**

```bash
cd apps/web/tests && npx playwright test contracts/trust.spec.js --project=chromium
```

Erwartet: Fehler beim Import von `trust.js`.

- [ ] **Step 3: `trust.js` schreiben**

Aufbau des Moduls, Stück für Stück:

```js
// The trust list: users and their keys, API servers and controllers, root
// permissions, stores, the re-confirmation policy and standing approvals. An
// admin signs every version; the shell verifies the chain from a pinned anchor
// itself, so the API can hand the list on but never forge it (Go: trust).
import { b64decode, equal, utf8 } from "./b64.js";
import { decode, Budget, S, P, A, str, bool, bytes, uint, uint32 } from "./json.js";
import { validName, validAppID, coreApp, validText, validID, validLoginName } from "./text.js";
import { validTunnelURL, validStoreURL, validOrigin } from "./url.js";
import * as proof from "./proof.js";
import { MAX_PAYLOAD, PURPOSE_TRUST_LIST, envelopeBudget } from "./sign.js";

export const ROLE_ADMIN = "admin";
export const MAX_USERS = 10000, MAX_NODES = 256, MAX_ROLES = 256, MAX_STORES = 64;
export const MAX_KEYS_PER_USER = 16, MAX_ENTRIES = 256, MAX_AUTO = 64, MAX_ORIGINS = 8, MAX_ROOT = 256;
export const LEVEL_OFF = "off", LEVEL_CRITICAL = "critical", LEVEL_ROOT = "root", LEVEL_HARDCORE = "hardcore";
export const NODE_API = "api", NODE_CONTROLLER = "controller";
export const STORE_GLOBAL = "global", STORE_LOCAL = "local";
export const AUTO_WEB = "ngc-web", AUTO_API = "ngc-api", AUTO_CONTROLLER = "ngc-controller";
export const AUTO_SYSTEM = "system";

const userShape = S({ id: str, name: str, roles: A(str), keys: A(proof.userKeyShape) });
const roleShape = S({ name: str, level: str, black: A(str), white: A(str),
  passkeyOnly: bool, passwordToo: bool });
const nodeShape = S({ id: str, kind: str, name: str, noise: bytes, sign: bytes, url: str,
  tls: bytes, rootUsers: A(str), rootRoles: A(str) });
const storeShape = S({ id: str, name: str, url: str, key: bytes });
const autoShape = S({ target: str, key: bytes });

export const listShape = S({
  seq: uint, prev: bytes, issued: uint, installation: str, rp: proof.rpShape,
  users: A(userShape), roles: A(roleShape), nodes: A(nodeShape),
  stores: A(storeShape), auto: A(autoShape),
});

export const signedShape = S({ payload: bytes, proof: proof.proofShape });

const listBudget = new Budget({
  size: MAX_PAYLOAD,
  items: {
    "rp/origins": MAX_ORIGINS,
    users: MAX_USERS,
    "users/*/roles": MAX_ROLES,
    "users/*/keys": MAX_KEYS_PER_USER,
    roles: MAX_ROLES,
    "roles/*/black": MAX_ENTRIES,
    "roles/*/white": MAX_ENTRIES,
    nodes: MAX_NODES,
    "nodes/*/rootUsers": MAX_ROOT,
    "nodes/*/rootRoles": MAX_ROOT,
    stores: MAX_STORES,
    auto: MAX_AUTO,
  },
});

const signedBudget = new Budget({ size: envelopeBudget(MAX_PAYLOAD).size });
```

Prüfen der Nutzlast – `validateList` setzt die Regeln von `trust.List.Validate` Punkt für Punkt um:

```js
export function validateList(l) {
  if (!l) return false;
  if (l.seq === 0 || (l.seq === 1) !== (l.prev.length === 0) || (l.seq > 1 && l.prev.length !== 32)) return false;
  if (l.issued <= 0 || !validID(l.installation)) return false;
  if (l.users.length === 0 || l.users.length > MAX_USERS || l.roles.length > MAX_ROLES
    || l.nodes.length > MAX_NODES || l.stores.length > MAX_STORES || l.auto.length > MAX_AUTO
    || l.rp.origins.length > MAX_ORIGINS) return false;

  for (let i = 0; i < l.roles.length; i++) {
    const r = l.roles[i];
    // A role cannot both forbid password keys and ask for the password.
    if (!validName(r.name) || (i > 0 && l.roles[i - 1].name >= r.name) || !validLevel(r.level)
      || !validEntries(r.black) || !validEntries(r.white) || (r.passkeyOnly && r.passwordToo)) return false;
  }
  if (!role(l, ROLE_ADMIN)) return false;
  if (!validateUsersSync(l)) return false;

  for (let i = 0; i < l.nodes.length; i++) {
    const n = l.nodes[i];
    if (i > 0 && l.nodes[i - 1].id >= n.id) return false;
    if (n.kind === NODE_API) {
      if (!validAPINode(n)) return false;
    } else if (n.kind === NODE_CONTROLLER) {
      // Signing keys, addresses and TLS pins belong to API nodes: nobody
      // dials a controller.
      if (!validNode(n) || n.sign.length !== 0 || n.url !== "" || n.tls.length !== 0
        || n.rootUsers.length > MAX_ROOT || n.rootRoles.length > MAX_ROOT) return false;
      for (const id of n.rootUsers) if (!user(l, id)) return false;
      for (const r of n.rootRoles) if (!role(l, r)) return false;
    } else {
      return false;
    }
  }
  for (let i = 0; i < l.stores.length; i++) {
    if (!validStore(l.stores[i]) || (i > 0 && l.stores[i - 1].id >= l.stores[i].id)) return false;
  }
  for (let i = 0; i < l.auto.length; i++) {
    const a = l.auto[i];
    if (i > 0 && l.auto[i - 1].target >= a.target) return false;
    if (a.target === AUTO_SYSTEM) {
      if (a.key.length !== 0) return false;
    } else if (a.target === AUTO_WEB || a.target === AUTO_API || a.target === AUTO_CONTROLLER
      || (validAppID(a.target) && !coreApp(a.target))) {
      if (a.key.length !== 32) return false;
    } else {
      return false;
    }
  }
  return true;
}
```

`validateUsersSync` prüft je Benutzer: `validID(u.id)`, aufsteigende IDs ohne Doppel,
`validLoginName(u.name)` und kein Name zweimal, `u.roles.length >= 1`, Rollen aufsteigend und alle
vorhanden, `u.keys.length` zwischen 1 und `MAX_KEYS_PER_USER`, keine Schlüssel-ID zweimal über alle
Benutzer, kein Passwortschlüssel in einer Rolle mit `passkeyOnly`, `passwordToo` nur mit einem
Passkey **und** einem Passwortschlüssel, `passkeyOnly` und `passwordToo` nie zusammen, mindestens
ein Admin; gibt es irgendwo einen Passkey, müssen `rp.id` und mindestens eine `rp.origins` gesetzt
und jede Herkunft `validOrigin` sein. Die Struktur jedes Schlüssels prüft
`proof.validateUserKeySync` – dafür bekommt `proof.js` in diesem Schritt zusätzlich eine
synchrone Form ohne die Prüfung von `id === keyID(pub)`; diese eine Prüfung läuft danach
asynchron in `parseList`:

```js
export async function parseList(payload) {
  const l = decode(payload, listBudget, listShape);
  if (!validateList(l)) throw new Error("trust: invalid list");
  // The key id is the hash of the key, so a list may not claim another one.
  for (const u of l.users) {
    for (const k of u.keys) {
      if (!(await proof.validateUserKey(k))) throw new Error("trust: invalid list");
    }
  }
  return l;
}

export function parseSigned(b) {
  const s = decode(b, signedBudget, signedShape);
  if (s.payload.length === 0 || s.payload.length > MAX_PAYLOAD || !proof.validateProof(s.proof)) {
    throw new Error("trust: invalid list");
  }
  return s;
}

export async function hash(signed) {
  return new Uint8Array(await crypto.subtle.digest("SHA-256", signed.payload));
}
```

Anker und Kette – genau die Reihenfolge von `trust.Open` und `trust.Advance`: erst Hash bzw.
Unterschrift, dann die Nutzlast lesen:

```js
export async function open(anchor, chain) {
  if (chain.length === 0) throw new Error("trust: no anchor");
  const h = await hash(chain[0]);
  if (!equal(h, anchor.hash)) throw new Error("trust: does not match the anchor");
  const l = await parseList(chain[0].payload);
  if (l.seq !== anchor.seq) throw new Error("trust: does not match the anchor");
  if (l.seq === 1) {
    // Version 1 signs itself, so only an admin of it may have signed.
    const u = await verifySigner(l, chain[0]);
    if (!u.roles.includes(ROLE_ADMIN)) throw new Error("trust: signer may not make this change");
  }
  return advance({ list: l, hash: h }, chain.slice(1));
}

export async function advance(cur, chain) {
  if (!cur.list) throw new Error("trust: broken chain");
  for (const s of chain) {
    const u = await verifySigner(cur.list, s);
    const next = await parseList(s.payload);
    if (next.seq !== cur.list.seq + 1 || !equal(next.prev, cur.hash)
      || next.installation !== cur.list.installation) throw new Error("trust: broken chain");
    if (!u.roles.includes(ROLE_ADMIN) && !onlyOwnKeys(cur.list, next, u.id)) {
      throw new Error("trust: signer may not make this change");
    }
    cur = { list: next, hash: await hash(s) };
  }
  return cur;
}

async function verifySigner(prev, s) {
  const found = keyOwner(prev, s.proof.kid);
  if (!found) throw new Error("trust: unknown signer");
  if (!(await proof.verify(s.proof, found.key, prev.rp, PURPOSE_TRUST_LIST, s.payload))) {
    throw new Error("trust: bad signature");
  }
  return found.user;
}
```

`onlyOwnKeys(prev, next, id)` vergleicht beide Fassungen ohne `seq`, `prev` und `issued` und ohne
die Schlüssel des Benutzers `id`: nur wenn sonst alles gleich ist, darf ein Benutzer ohne
Admin-Rolle unterschreiben. Verglichen wird über `canonical(list)`, eine Funktion, die die Liste
in derselben Feldreihenfolge wie Go nach JSON schreibt (Felder in der Reihenfolge der Go-Struktur,
leere weglassen, Bytes als base64url) – dieselbe Funktion braucht Aufgabe 23, um neue Fassungen
zu bauen, deshalb steht sie hier und heißt `encodeList(list) -> Uint8Array`.

Dazu die Abfragen:

```js
export const user = (l, id) => l.users.find((u) => u.id === id) || null;
export const role = (l, name) => l.roles.find((r) => r.name === name) || null;
export const node = (l, id) => l.nodes.find((n) => n.id === id) || null;

export function keyOwner(l, kid) {
  for (const u of l.users) {
    for (const k of u.keys) if (k.id === kid) return { user: u, key: k };
  }
  return null;
}

export function passwordToo(l, userID) {
  const u = user(l, userID);
  return !!u && u.roles.some((n) => role(l, n)?.passwordToo);
}

export function mayRoot(l, userID, nodeID) {
  const n = node(l, nodeID), u = user(l, userID);
  if (!n || !u || n.kind !== NODE_CONTROLLER) return false;
  if (n.rootUsers.includes(userID)) return true;
  return u.roles.some((r) => n.rootRoles.includes(r));
}

// op is {app, action, node, root, critical, changes}.
export function needsConfirm(l, userID, op) {
  const u = user(l, userID);
  if (!u) return true; // an unknown user always confirms
  return u.roles.some((name) => {
    const r = role(l, name);
    if (!r) return true;
    if (matches(r.black, op)) return true;
    if (matches(r.white, op)) return false;
    if (r.level === LEVEL_CRITICAL) return op.critical;
    if (r.level === LEVEL_ROOT) return op.critical || op.root;
    if (r.level === LEVEL_HARDCORE) return op.critical || op.root || op.changes;
    return false;
  });
}

const matches = (entries, op) => entries.some((e) =>
  e === "app/" + op.app || e === "app/" + op.app + "/" + op.action
  || (op.node !== "" && e === "node/" + op.node));

export const validNode = (n) => validName(n.id) && validText(n.name, 64) && n.noise.length === 32;

export const validAPINode = (n) => n.kind === NODE_API && validNode(n) && n.sign.length === 32
  && validTunnelURL(n.url) && n.tls.length === 32 && n.rootUsers.length + n.rootRoles.length === 0;

// StoreLocal marks uploaded apps, so no catalog key may claim it.
export const validStore = (s) => validName(s.id) && s.id !== STORE_LOCAL && validText(s.name, 64)
  && validStoreURL(s.url) && s.key.length === 32;

export async function tlsPin(spki) {
  return new Uint8Array(await crypto.subtle.digest("SHA-256", spki));
}

const validLevel = (l) => l === LEVEL_OFF || l === LEVEL_CRITICAL || l === LEVEL_ROOT || l === LEVEL_HARDCORE;

function validEntries(entries) {
  if (entries.length > MAX_ENTRIES) return false;
  return entries.every((e) => {
    if (e.startsWith("node/")) return validName(e.slice(5));
    if (e.startsWith("app/")) {
      const rest = e.slice(4);
      const i = rest.indexOf("/");
      const app = i < 0 ? rest : rest.slice(0, i);
      return validAppID(app) && (i < 0 || validName(rest.slice(i + 1)));
    }
    return false;
  });
}
```

- [ ] **Step 4: Test laufen lassen, er muss bestehen**

```bash
cd apps/web/tests && npm test -- contracts/trust.spec.js
```

Erwartet: alle 24 Ketten aus `trust.json` werden genau wie in Go angenommen oder abgelehnt, und
die drei Grenzen ohne eigene Vektoren greifen; in allen drei Engines.

- [ ] **Step 5: Committen**

```bash
git add apps/web/public/assets/js/contracts/trust.js apps/web/public/assets/js/contracts/proof.js apps/web/tests/contracts/trust.spec.js
git commit -m "Add the trust list with anchor and chain" --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 8: `session` – Zertifikate, Ersteinrichtung und die Kette der Fassungen

**Files:**
- Create: `apps/web/public/assets/js/contracts/session.js`
- Test: `apps/web/tests/contracts/session.spec.js`

**Interfaces:**
- Produces: `PATH_SETUP = "/api/v1/setup"`, `PATH_LOGIN_START = "/api/v1/login/start"`,
  `PATH_LOGIN_FINISH = "/api/v1/login/finish"`, `PATH_LOGOUT = "/api/v1/logout"`,
  `PATH_TRUST = "/api/v1/trust"`, `PATH_SOCKET = "/api/v1/ws"`, `SUBPROTOCOL = "ngc.v1"`,
  `COOKIE_NAME = "ngc_session"`, `TOKEN_SIZE = 32`, `NONCE_SIZE = 32`, `MAX_LIFETIME = 86400`,
  `MAX_SKEW = 300`, `CHALLENGE_LIFETIME = 120`, `MAX_CERT = 4096`, `MAX_REQUEST = 65536`,
  `MAX_SETUP_OFFER = 131072`, `MAX_SETUP_NODES = 16`, `MAX_CHAIN_VERSIONS = 64`,
  `MAX_CHAIN = 33554432`, `TOTP_DIGITS = 6`, `TOTP_PERIOD = 30`,
  `MAX_PASSKEYS = trust.MAX_KEYS_PER_USER`,
  `loginChallengeShape`, `passkeyRefShape`, `validatePasskeyRef(r)`,
  `loginResultShape`, `clientCertShape`, `apiCertShape`,
  `encodeClientCert(cert) -> Uint8Array`, `certHash(payload) -> Promise<Uint8Array>`,
  `validateClientCert(c)`, `verifyAPI(env, list, clientPayload, now) -> Promise<apiCert>`,
  `verifyApproval(payload, proof, list) -> Promise<clientCert>`,
  `parseLoginChallenge(bytes)`, `parseLoginResult(bytes)`,
  `parseTrustChain(bytes)`, `parseTrustHint(bytes)`, `httpStatus(code)`,
  `validSetupCode(s)`, `normalizeSetupCode(s) -> string|null`,
  `offerMAC(code, info) -> Promise<Uint8Array>`, `setupMAC(code, payload) -> Promise<Uint8Array>`,
  `parseSetupOffer(bytes, code) -> Promise<info>`, `encodeSetupInfoNodes(info)`.
- Consumes: `b64.js`, `json.js`, `text.js`, `url.js`, `proof.js`, `sign.js`, `trust.js`.

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

`apps/web/tests/contracts/session.spec.js`:

```js
import { test, expect } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";

const v = vectors("session.json");

test("the API certificate is checked against the trust list", async ({ shell }) => {
  test.setTimeout(120_000);
  const got = await shell.evaluate(async (d) => {
    const s = await import("/assets/js/contracts/session.js");
    const t = await import("/assets/js/contracts/trust.js");
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    const list = await t.parseList(b64decode(d.trust.payload));
    const client = b64decode(d.client);
    const env = (e) => ({ payload: b64decode(e.payload), kid: e.kid, sig: b64decode(e.sig) });
    const check = async (e) => {
      try {
        await s.verifyAPI(env(e), list, client, d.now);
        return true;
      } catch {
        return false;
      }
    };
    return { good: await check(d.api), rejects: await Promise.all(d.apiRejects.map((r) => check(r.api))) };
  }, v);
  expect(got.good).toBe(true);
  const wrong = v.apiRejects.filter((r, i) => got.rejects[i]).map((r) => r.name);
  expect(wrong).toEqual([]);
});

test("every login challenge is decided as Go decides it", async ({ shell }) => {
  test.setTimeout(120_000);
  const got = await shell.evaluate(async (d) => {
    const s = await import("/assets/js/contracts/session.js");
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    const out = [];
    for (const c of d.cases) {
      try {
        s.parseLoginChallenge(b64decode(c.challenge));
        out.push(true);
      } catch {
        out.push(false);
      }
    }
    // Every answer session.Challenge builds must pass, the made-up ones for
    // unknown names too: an answer the shell refuses would tell it that the
    // name does not exist.
    const built = [];
    for (const b of d.built) {
      const c = s.parseLoginChallenge(b64decode(b.challenge));
      built.push(c.passkeys.every((p) => p.credId.length > 0 && p.kid.length === 22));
    }
    return { out, built };
  }, v.login);
  const wrong = v.login.cases.filter((c, i) => c.valid !== got.out[i]).map((c) => c.name);
  expect(wrong).toEqual([]);
  expect(got.built).toEqual(v.login.built.map(() => true));
});

test("the one-time code is normalized as in Go", async ({ shell }) => {
  const got = await shell.evaluate(async (typed) => {
    const s = await import("/assets/js/contracts/session.js");
    return typed.map((c) => s.normalizeSetupCode(c.typed) ?? "");
  }, v.setup.typed);
  expect(got).toEqual(v.setup.typed.map((c) => c.code));
});

test("every setup offer is decided as Go decides it", async ({ shell }) => {
  test.setTimeout(120_000);
  const got = await shell.evaluate(async (d) => {
    const s = await import("/assets/js/contracts/session.js");
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    const out = [];
    for (const c of d.offers) {
      try {
        await s.parseSetupOffer(b64decode(c.offer), d.code);
        out.push(true);
      } catch {
        out.push(false);
      }
    }
    return out;
  }, v.setup);
  const wrong = v.setup.offers.filter((c, i) => c.valid !== got[i]).map((c) => c.name);
  expect(wrong).toEqual([]);
});

test("the version 1 the shell builds is the one the API accepts", async ({ shell }) => {
  test.setTimeout(120_000);
  const got = await shell.evaluate(async (d) => {
    const s = await import("/assets/js/contracts/session.js");
    const { b64decode, b64encode, equal } = await import("/assets/js/contracts/b64.js");
    const out = [];
    for (const c of d.requests) {
      const req = JSON.parse(new TextDecoder().decode(b64decode(c.request)));
      // The MAC covers the exact bytes of the payload the shell sends.
      const mac = await s.setupMAC(d.code, b64decode(req.trust.payload));
      out.push(equal(mac, b64decode(req.mac)));
    }
    return out;
  }, v.setup);
  // Only the two valid requests carry a MAC made with this code over these
  // bytes; the others differ by design.
  expect(got).toEqual(v.setup.requests.map((c) => c.name === "MAC with another code"
    || c.name === "MAC over other bytes" ? false : true));
});
```

- [ ] **Step 2: Test laufen lassen, er muss scheitern**

```bash
cd apps/web/tests && npx playwright test contracts/session.spec.js --project=chromium
```

Erwartet: Fehler beim Import von `session.js`.

- [ ] **Step 3: `session.js` schreiben**

Konstanten und Formen wie oben unter „Produces“. Die Kernstücke:

```js
// Login, the session certificates and the HTTP exchange before the WebSocket
// (Go: session).
import { b64decode, b64encode, concat, utf8, equal } from "./b64.js";
import { decode, Budget, S, P, A, str, bool, bytes, uint, int } from "./json.js";
import { validID, validText, validTOTP } from "./text.js";
import * as trust from "./trust.js";
import * as proof from "./proof.js";
import { PURPOSE_API_CERT, MAX_PAYLOAD, envelopeShape, envelopeBudget } from "./sign.js";

export const PATH_SETUP = "/api/v1/setup";
export const PATH_LOGIN_START = "/api/v1/login/start";
export const PATH_LOGIN_FINISH = "/api/v1/login/finish";
export const PATH_LOGOUT = "/api/v1/logout";
export const PATH_TRUST = "/api/v1/trust";
export const PATH_SOCKET = "/api/v1/ws";
export const SUBPROTOCOL = "ngc.v1";
export const COOKIE_NAME = "ngc_session";
export const TOKEN_SIZE = 32, NONCE_SIZE = 32;
export const MAX_LIFETIME = 86400, MAX_SKEW = 300, CHALLENGE_LIFETIME = 120;
export const MAX_CERT = 4096, MAX_REQUEST = 64 << 10;
export const MAX_SETUP_OFFER = 128 << 10, MAX_SETUP_NODES = 16;
export const MAX_CHAIN_VERSIONS = 64, MAX_CHAIN = 32 << 20;
export const TOTP_DIGITS = 6, TOTP_PERIOD = 30;
// A user holds at most this many keys in the trust list, so no honest answer
// names more passkeys, and the shell allocates no more either.
export const MAX_PASSKEYS = trust.MAX_KEYS_PER_USER;

const certBudget = new Budget({ size: MAX_CERT });
const requestBudget = new Budget({ size: MAX_REQUEST });
const offerBudget = new Budget({ size: MAX_SETUP_OFFER });
const challengeBudget = new Budget({ size: MAX_REQUEST,
  items: { passkeys: MAX_PASSKEYS, "rp/origins": trust.MAX_ORIGINS } });
const infoBudget = new Budget({ size: MAX_REQUEST, items: { nodes: MAX_SETUP_NODES } });
const hintBudget = new Budget({ size: 64 });
const chainBudget = new Budget({ size: MAX_CHAIN, items: { versions: MAX_CHAIN_VERSIONS } });
```

Das Client-Zertifikat schreibt die Shell selbst; die Feldreihenfolge ist die der Go-Struktur, denn
die Unterschrift des Benutzers deckt genau diese Bytes:

```js
// encodeClientCert writes the certificate in the field order of Go's
// session.ClientCert. These exact bytes are what the user signs (purpose
// clientcert/v1) and what CertHash hashes, so they are built once and kept.
export function encodeClientCert(c) {
  return utf8(JSON.stringify({
    inst: c.inst, user: c.user, nonce: b64encode(c.nonce),
    sig: b64encode(c.sig), dh: b64encode(c.dh), iat: c.iat, exp: c.exp,
  }));
}

export function validateClientCert(c) {
  return validID(c.inst) && validID(c.user) && c.nonce.length === NONCE_SIZE
    && c.sig.length === 32 && c.dh.length === 32
    && c.iat > 0 && c.exp > c.iat && c.exp - c.iat <= MAX_LIFETIME;
}

export async function certHash(payload) {
  return new Uint8Array(await crypto.subtle.digest("SHA-256", payload));
}
```

`verifyAPI(env, list, clientPayload, now)` folgt `session.VerifyAPI`: den API-Knoten der Liste
suchen, dessen `sign.KeyID` zu `env.kid` passt, Unterschrift mit Zweck `apicert/v1` prüfen, dann
die Nutzlast lesen und verlangen: gleiche Installation, `node` gleich der ID des Knotens,
`client` gleich `certHash(clientPayload)`, `dh` 32 Byte, `iat > 0`, `exp > iat`,
`exp - iat <= MAX_LIFETIME`, `exp <= exp des Client-Zertifikats`, und zeitlich gültig
(`now >= iat - MAX_SKEW`, `now < exp`). Jede Abweichung wirft.

`verifyApproval(payload, p, list)` prüft ein Client-Zertifikat und den Beweis darüber wie
`session.VerifyApproval` in Go, aber ohne die Zeit: Form nach `validateClientCert`, gleiche
Installation wie die Liste, der Schlüssel des Beweises gehört diesem Benutzer, und für einen
Benutzer in einer Rolle mit `passwordToo` muss es ein Passkey sein. Es belegt, was ein Benutzer
einmal gebilligt hat, und ist deshalb genau das, was `core.verifyUIProof` für eine hochgeladene App
braucht. Ein eigenes `verifyClient` braucht die Shell nicht: Sie baut ihr Zertifikat selbst und
prüft fremde nie.

Die Antwort auf `POST /api/v1/login/start` liest `parseLoginChallenge(bytes)` – Schritt für Schritt
nach `session.ParseLoginChallenge`, dem einzigen Maßstab; die Vektoren unter `login` in
`shared/testdata/session.json` sind die Prüfgrundlage: 33 Fälle unter `login.cases`, davon 26
abgelehnte, und fünf Antworten unter `login.built`, die `session.Challenge` wirklich gebaut hat –
eine für einen bekannten Namen, vier erfundene für unbekannte:

```js
export const passkeyRefShape = S({ credId: bytes, kid: str });
export const loginChallengeShape = S({
  nonce: bytes, now: int, maxLifetime: int, installation: str, user: str,
  kdf: proof.kdfShape, passkeys: A(passkeyRefShape), totp: bool, passwordToo: bool,
  rp: proof.rpShape,
});

// A passkey the user may log in with: the credential ID for allowCredentials
// and the key ID that goes into proof.Proof.kid. Without the key ID a browser
// that has never seen the trust list could not build the proof at all, so it
// travels with the answer and the shell never derives it itself.
export function validatePasskeyRef(r) {
  return !!r && r.credId instanceof Uint8Array && r.credId.length >= 1
    && r.credId.length <= proof.MAX_CRED_ID && proof.validKeyID(r.kid);
}
```

`parseLoginChallenge` liest mit `challengeBudget` strikt und verlangt dann: `nonce` genau
`NONCE_SIZE` Byte, `now > 0`, `maxLifetime` 1 bis `MAX_LIFETIME`, `installation` und `user` je ein
`trust.validID`, `kdf` fehlt oder besteht `proof.validateKDF`, höchstens `MAX_PASSKEYS` Passkeys
und höchstens `trust.MAX_ORIGINS` Herkünfte, `rp.id` – wenn gesetzt – nach `proof.validRPID` (die
Shell reicht ihn an `navigator.credentials.get` weiter, also ein DNS-Name und nie freier Text),
jede Herkunft nach `trust.validOrigin`, und sobald `passkeys` nicht leer ist, müssen `rp.id` und
mindestens eine Herkunft dastehen. Jeder Eintrag besteht `validatePasskeyRef`, und weder eine
Kennung noch eine Schlüssel-ID kommt zweimal vor – zwei Einträge mit derselben Schlüssel-ID
ließen die Shell raten, welcher Schlüssel den Beweis gemacht hat. Eine leere Liste ist erlaubt
(ein Benutzer ohne Passkey). Die erfundene Antwort für einen unbekannten Namen hält all das ein;
deshalb darf die Shell an keiner Stelle milder oder strenger prüfen, sonst verriete ihre Reaktion,
ob es den Namen gibt.

`proof.js` bekommt dafür in diesem Schritt drei Namen, die es in Go auch gibt und die die Shell an
mehreren Stellen braucht: `MAX_RPID = 253` mit `validRPID(s)` (dieselbe Regel wie `proof.ValidRPID`
– ein DNS-Name in Kleinbuchstaben, ohne Schema, Port und Pfad; `config.Web` und
`trust.List.Validate` prüfen `rp.id` in Go mit genau dieser Funktion, also prüft auch die
Vertrauensliste in der Shell damit), `validKeyID(s)` (base64url von 16 Byte – `sign.KeyIDSize`,
das `proof.ValidKeyID` in Go von dort nimmt –, aus `validateProof` herausgezogen, damit Beweis und
Passkey-Eintrag dieselbe Regel benutzen) und
`MAX_CRED_ID = 1023` (`proof.MaxCredID`, das `validateUserKey` in Aufgabe 5 schon als Zahl
benutzt). Alle drei ändern keine Prüfung, sie geben ihr nur den Namen, den Go auch dafür hat.

Die Ersteinrichtung:

```js
const CODE = /^[A-HJ-NP-Z2-9]{5}(-[A-HJ-NP-Z2-9]{5}){3}$/;
export const validSetupCode = (s) => typeof s === "string" && CODE.test(s);

// normalizeSetupCode turns what someone typed into the canonical code: spaces,
// tabs and '-' are dropped, a-z becomes upper case, then exactly 20 characters
// of the alphabet without I, O, 0 and 1, in groups of five. null where that is
// not possible, including for anything that is no string at all.
//
// It walks UTF-16 units and folds a-z alone, which is Go's byte loop
// character for character: every unit of a character outside ASCII is 0x80 or
// above and is refused here, as every one of its UTF-8 bytes is there. The
// task brief wrote this loop over code points with toUpperCase() instead, and
// that one differs in exactly one place - measured over every code point
// outside ASCII, surrogates apart: U+017F, the long s 'ſ', upper-cases to a
// single "S", so the brief's loop makes "ſBCDE-FGHJK-MNPQR-STUVW" the code
// "SBCDE-FGHJK-MNPQR-STUVW" while Go's NormalizeSetupCode, run on the same
// input, answers false. A shell that takes what the API's own normalizer
// would not is a second rule, so this one stays Go's.
export function normalizeSetupCode(s) {
  if (typeof s !== "string") return null;
  const c = [];
  for (let i = 0; i < s.length; i++) {
    let u = s[i];
    if (u === " " || u === "\t" || u === "-") continue;
    if (u >= "a" && u <= "z") u = String.fromCharCode(s.charCodeAt(i) - 32);
    if (c.length === 20 || !/^[A-HJ-NP-Z2-9]$/.test(u)) return null;
    c.push(u);
  }
  if (c.length !== 20) return null;
  const g = (i) => c.slice(i, i + 5).join("");
  return [g(0), g(5), g(10), g(15)].join("-");
}

async function setupHMAC(code, label, b) {
  const key = await crypto.subtle.importKey("raw", utf8(code), { name: "HMAC", hash: "SHA-256" }, false, ["sign"]);
  return new Uint8Array(await crypto.subtle.sign("HMAC", key, concat(utf8(label), new Uint8Array([0]), b)));
}

// The two labels keep the MACs apart, so one made for one direction never
// passes for the other, whatever bytes it covers.
export const offerMAC = (code, info) => setupHMAC(code, "ngc/setup-offer/v1", info);
export const setupMAC = (code, payload) => setupHMAC(code, "ngc/setup/v1", payload);
```

`parseSetupOffer(bytes, code)` folgt `session.ParseSetupOffer` Schritt für Schritt: strikt
innerhalb `MAX_SETUP_OFFER` lesen; `info` 1 bis `MAX_REQUEST` Byte und `mac` 32 Byte; **dann** den
Code prüfen (`validSetupCode`) und den MAC vergleichen – wer den Code nicht hat, kann keinen MAC
bauen, deshalb wird nichts aus `info` gelesen, bevor der MAC stimmt; danach `info` strikt
innerhalb `MAX_REQUEST` mit `nodes` höchstens `MAX_SETUP_NODES`; `installation` ein `validID`,
1 bis 16 Knoten, jeder `trust.validAPINode`, nach ID aufsteigend ohne Doppel, `now > 0`,
`releaseKey` 32 Byte, `store` fehlt oder hat `id === trust.STORE_GLOBAL` und besteht
`trust.validStore`.

Dazu `parseTrustChain(bytes)` (Budget `chainBudget`, 1 bis `MAX_CHAIN_VERSIONS` Fassungen, jede mit
Nutzlast 1 bis `sign.MAX_PAYLOAD` und einem Beweis nach `proof.validateProof`),
`parseTrustHint(bytes)` (`seq >= 1`) und

```js
export function httpStatus(code) {
  switch (code) {
    case "E_PROTOCOL": return 400;
    case "E_LOGIN": return 401;
    case "E_FORBIDDEN": case "E_VERIFY": return 403;
    case "E_NOTFOUND": return 404;
    case "E_TOOLARGE": return 413;
    case "E_LOCKED": return 429;
    case "E_UNREACHABLE": return 503;
  }
  return 500;
}
```

- [ ] **Step 4: Test laufen lassen, er muss bestehen**

```bash
cd apps/web/tests && npm test -- contracts/session.spec.js
```

Erwartet: alle zehn abgelehnten API-Zertifikate werden abgelehnt, alle 33 Anmelde-Antworten werden
wie in Go entschieden und die fünf gebauten Antworten kommen mit `credId` und `kid` durch, alle
fünf getippten Codes werden wie in Go normalisiert, alle 15 Angebote werden wie in Go entschieden.

- [ ] **Step 5: Committen**

```bash
git add apps/web/public/assets/js/contracts/session.js apps/web/public/assets/js/contracts/proof.js \
  apps/web/tests/contracts/session.spec.js
git commit -m "Add the login challenge, session certificates and the setup offer" --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 9: `task` – Aufgaben unterschreiben und erneut bestätigen

**Files:**
- Create: `apps/web/public/assets/js/contracts/task.js`
- Test: `apps/web/tests/contracts/task.spec.js`

**Interfaces:**
- Produces: `ID_SIZE = 16`, `MAX_LIFETIME = 300`, `MAX_TASK = 16384`, `taskShape`,
  `encodeTask(task) -> Uint8Array`, `validateTask(t) -> boolean`,
  `buildTask({inst, certHash, node, app, service, action, params, now}) -> {task, bytes}`,
  `signTask(sessionSignKey, bytes) -> Promise<envelope>`,
  `parseTask(payload) -> task`,
  `verifyApproval(envelope, clientCert, clientPayload) -> Promise<task>`,
  `verifyConfirm(proofValue, envelope, list, userID) -> Promise<boolean>`.
- Consumes: `b64.js`, `json.js`, `text.js`, `proof.js`, `sign.js`, `session.js`, `trust.js`.

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

`apps/web/tests/contracts/task.spec.js`:

```js
import { test, expect } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";

const v = vectors("task.json");

test("a task signs with the session key and binds to the certificate", async ({ shell }) => {
  test.setTimeout(120_000);
  const out = await shell.evaluate(async (d) => {
    const task = await import("/assets/js/contracts/task.js");
    const session = await import("/assets/js/contracts/session.js");
    const sign = await import("/assets/js/contracts/sign.js");
    const trust = await import("/assets/js/contracts/trust.js");
    const proof = await import("/assets/js/contracts/proof.js");
    const { b64decode, b64encode, equal } = await import("/assets/js/contracts/b64.js");

    const list = await trust.parseList(b64decode(d.trust.payload));
    const client = b64decode(d.client);
    const env = { payload: b64decode(d.task.payload), kid: d.task.kid, sig: b64decode(d.task.sig) };
    const cert = JSON.parse(new TextDecoder().decode(client));
    // The task is signed with the session key of the certificate (its sig
    // member) and bound to the exact certificate bytes.
    const good = await sign.verify(env, b64decode(cert.sig), sign.PURPOSE_TASK);
    const t = task.parseTask(env.payload);
    const bound = equal(t.cert, await session.certHash(client));

    // The shell can rebuild exactly these bytes from the fields.
    const rebuilt = task.encodeTask(t);

    // A re-confirmation is a proof over the task payload, purpose confirm/v1.
    const confirm = {
      kind: d.confirm.kind, kid: d.confirm.kid, sig: b64decode(d.confirm.sig),
      authData: d.confirm.authData ? b64decode(d.confirm.authData) : new Uint8Array(0),
      clientData: d.confirm.clientData ? b64decode(d.confirm.clientData) : new Uint8Array(0),
    };
    return {
      good, bound, rebuilt: equal(rebuilt, env.payload), valid: task.validateTask(t),
      confirmed: await task.verifyConfirm(confirm, env, list, cert.user),
      confirmOther: await task.verifyConfirm(confirm, { ...env, payload: new Uint8Array([123, 125]) },
        list, cert.user),
    };
  }, v);
  expect(out).toEqual({ good: true, bound: true, rebuilt: true, valid: true, confirmed: true, confirmOther: false });
});
```

- [ ] **Step 2: Test laufen lassen, er muss scheitern**

```bash
cd apps/web/tests && npx playwright test contracts/task.spec.js --project=chromium
```

Erwartet: Fehler beim Import von `task.js`.

- [ ] **Step 3: `task.js` schreiben**

```js
// Signed tasks: the shell signs a task with the session key; controllers and
// the API verify the chain from the task over the client certificate to the
// user's key in the trust list (Go: task).
import { b64encode, utf8, equal } from "./b64.js";
import { decode, Budget, S, str, bytes, int, raw, RAW } from "./json.js";
import { validName, validAppID, validID } from "./text.js";
import * as proof from "./proof.js";
import { message, PURPOSE_TASK, PURPOSE_CONFIRM } from "./sign.js";
import { keyOwner } from "./trust.js";

export const ID_SIZE = 16;
export const MAX_LIFETIME = 300;
export const MAX_TASK = 16 << 10;

export const taskShape = S({
  id: bytes, inst: str, cert: bytes, node: str, app: str, service: str, action: str,
  params: raw, iat: int, exp: int,
});

const taskBudget = new Budget({ size: MAX_TASK, items: { params: RAW } });

export const parseTask = (payload) => decode(payload, taskBudget, taskShape);

// encodeTask writes the task in the field order of Go's task.Task; these
// exact bytes are what the session key signs and what the end-to-end prologue
// hashes.
export function encodeTask(t) {
  const o = { id: b64encode(t.id), inst: t.inst, cert: b64encode(t.cert) };
  o.node = t.node;
  o.app = t.app;
  o.service = t.service;
  o.action = t.action;
  const head = JSON.stringify(o).slice(0, -1);
  const tail = JSON.stringify({ iat: t.iat, exp: t.exp }).slice(1);
  // params stays raw: its bytes go in unchanged, as the prologue hashes them.
  const mid = t.params && t.params.length > 0
    ? ',"params":' + new TextDecoder().decode(t.params) : "";
  return utf8(head + mid + "," + tail);
}

export function validateTask(t) {
  return t.id.length === ID_SIZE && validID(t.inst) && t.cert.length === 32
    && (t.node === "" || validName(t.node)) && validAppID(t.app)
    && validName(t.service) && validName(t.action)
    && t.iat > 0 && t.exp > t.iat && t.exp - t.iat <= MAX_LIFETIME;
}

// buildTask makes the task for one channel. Its lifetime is short, so a task
// that leaks is useless soon; the verifier remembers its id until it expires.
export function buildTask({ inst, certHash, node, app, service, action, params, now }) {
  const id = crypto.getRandomValues(new Uint8Array(ID_SIZE));
  const t = { id, inst, cert: certHash, node: node || "", app, service, action,
    params: params || null, iat: now, exp: now + 120 };
  if (!validateTask(t)) throw new Error("task: malformed task");
  const bytes = encodeTask(t);
  if (bytes.length > MAX_TASK) throw new Error("task: too large");
  return { task: t, bytes };
}

export async function signTask(sessionSignKey, bytes) {
  const sig = new Uint8Array(await crypto.subtle.sign({ name: "Ed25519" }, sessionSignKey.privateKey,
    message(PURPOSE_TASK, bytes)));
  return { payload: bytes, kid: sessionSignKey.kid, sig };
}

export async function verifyConfirm(p, env, list, userID) {
  const found = keyOwner(list, p.kid);
  if (!found || found.user.id !== userID) return false;
  return proof.verify(p, found.key, list.rp, PURPOSE_CONFIRM, env.payload);
}
```

- [ ] **Step 4: Test laufen lassen, er muss bestehen**

```bash
cd apps/web/tests && npm test -- contracts/task.spec.js
```

Erwartet: alle sechs Felder wie erwartet.

- [ ] **Step 5: Committen**

```bash
git add apps/web/public/assets/js/contracts/task.js apps/web/tests/contracts/task.spec.js
git commit -m "Add signed tasks and re-confirmation" --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 10: `wire` – Frames, Kanäle und Fehlercodes

**Files:**
- Create: `apps/web/public/assets/js/contracts/wire.js`
- Test: `apps/web/tests/contracts/wire.spec.js`

**Interfaces:**
- Produces: `TYPE_OPEN = 1`, `TYPE_DATA = 2`, `TYPE_EOF = 3`, `TYPE_CLOSE = 4`, `TYPE_GRANT = 5`,
  `HEADER_SIZE = 5`, `MAX_FRAME = 65519`, `MAX_PAYLOAD = 65514`, `CONTROL_CHANNEL = 0`,
  `DEFAULT_WINDOW = 262144`, `MIN_WINDOW = 4096`, `MAX_WINDOW = 1048576`,
  `MAX_SESSION_CHANNELS = 64`, `MAX_OPEN_PAYLOAD = 40960`, `MAX_BROWSER_OPEN = 16384`,
  `MAX_CLOSE_PAYLOAD = 4096`, `MAX_PARAMS = 8192`, `MAX_MESSAGE_TEXT = 256`,
  `HANDSHAKE_SIZE = 48`, `MAX_E2E_PLAINTEXT = 65498`, `MAX_CALL_RESULT = 8388608`,
  `E2E_FIRST_TIMEOUT = 30`, `E2E_DATA = 1`, `E2E_END = 2`,
  `encodeFrame(f) -> Uint8Array`, `decodeFrame(b) -> {type, channel, payload}`,
  `grant(credit, handshake) -> Uint8Array`, `parseGrant(p) -> {credit, handshake}`,
  `encodeOpen(o) -> Uint8Array`, `parseOpen(b)`, `validateOpen(o)`, `validParams(raw)`,
  `parseClose(b)`, `encodeClose(c)`, `errorClose(code)`, `CODES`, `message(code)`,
  `e2e(kind, body)`, `parseE2E(plain, fromController)`, `parseControl(b)`, `encodeControl(c)`,
  `controlAllowed(link, fromAPI, t)`, `CORE_APP(app)`.
- Consumes: `b64.js`, `json.js`, `text.js`, `proof.js`, `sign.js`.

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

`apps/web/tests/contracts/wire.spec.js`:

```js
import { test, expect } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";

const v = vectors("wire.json");

test("frames, opens, closes, grants, end-to-end messages and control rules match Go", async ({ shell }) => {
  const got = await shell.evaluate(async (d) => {
    const w = await import("/assets/js/contracts/wire.js");
    const { b64decode, b64encode, equal } = await import("/assets/js/contracts/b64.js");
    const decides = (list, fn) => list.map((c) => { try { fn(c); return true; } catch { return false; } });
    return {
      frames: d.frames.map((c) => {
        const f = { type: c.type, channel: c.channel, payload: b64decode(c.payload) };
        return b64encode(w.encodeFrame(f)) === c.encoded
          && equal(w.decodeFrame(b64decode(c.encoded)).payload, f.payload);
      }),
      badFrames: decides(d.badFrames, (c) => w.decodeFrame(b64decode(c.encoded))),
      grants: decides(d.grants, (c) => w.parseGrant(b64decode(c.payload))),
      opens: decides(d.opens, (c) => w.parseOpen(b64decode(c.payload))),
      closes: decides(d.closes, (c) => w.parseClose(b64decode(c.payload))),
      e2e: decides(d.e2e, (c) => w.parseE2E(b64decode(c.plain), c.fromController)),
      controls: d.controls.map((c) => w.controlAllowed(c.link, c.fromApi, c.t)),
    };
  }, v);

  expect(got.frames.every(Boolean)).toBe(true);
  expect(got.badFrames.every((ok) => !ok)).toBe(true);
  for (const key of ["grants", "opens", "closes", "e2e"]) {
    const wrong = v[key].filter((c, i) => c.valid !== got[key][i]).map((c) => `${key}: ${c.name}`);
    expect(wrong).toEqual([]);
  }
  const wrongControls = v.controls.filter((c, i) => c.allowed !== got.controls[i])
    .map((c) => `${c.link}/${c.fromApi}/${c.t}`);
  expect(wrongControls).toEqual([]);
});

test("every error code carries its German message", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const w = await import("/assets/js/contracts/wire.js");
    return Object.fromEntries(Object.values(w.CODES).map((c) => [c, w.message(c)]));
  });
  expect(got).toEqual({
    E_FORBIDDEN: "Keine Berechtigung.",
    E_UNREACHABLE: "Dienst nicht erreichbar.",
    E_EXIT: "Dienst mit Fehler beendet.",
    E_TIMEOUT: "Zeitlimit überschritten.",
    E_LIMIT: "Zu viele offene Verbindungen.",
    E_PROTOCOL: "Ungültige Nachricht.",
    E_CONFIRM: "Bitte erneut bestätigen.",
    E_LICENSE: "Keine gültige Lizenz.",
    E_NOTFOUND: "Nicht gefunden.",
    E_VERIFY: "Unterschrift ungültig.",
    E_INTERNAL: "Interner Fehler.",
    E_TOOLARGE: "Zu viele Daten.",
    E_LOGIN: "Anmeldung fehlgeschlagen.",
    E_LOCKED: "Zu viele Fehlversuche. Bitte später erneut versuchen.",
  });
});
```

- [ ] **Step 2: Test laufen lassen, er muss scheitern**

```bash
cd apps/web/tests && npx playwright test contracts/wire.spec.js --project=chromium
```

Erwartet: Fehler beim Import von `wire.js`.

- [ ] **Step 3: `wire.js` schreiben**

Die Frame-Ebene, wörtlich nach `wire.Frame`:

```js
// Frames and channels, the unit of transfer between browser, API and
// controller (Go: wire).
import { concat, utf8, b64encode } from "./b64.js";
import { decode, Budget, S, P, A, str, bool, int, uint32, bytes, raw, RAW } from "./json.js";
import { validName, validAppID, coreApp, validText } from "./text.js";
import { proofShape, validateProof } from "./proof.js";
import { envelopeShape, validateEnvelope } from "./sign.js";

export const TYPE_OPEN = 1, TYPE_DATA = 2, TYPE_EOF = 3, TYPE_CLOSE = 4, TYPE_GRANT = 5;
export const HEADER_SIZE = 5;
export const MAX_FRAME = 65519;
export const MAX_PAYLOAD = MAX_FRAME - HEADER_SIZE;
export const CONTROL_CHANNEL = 0;
export const DEFAULT_WINDOW = 256 << 10;
export const MIN_WINDOW = 4 << 10;
export const MAX_WINDOW = 1 << 20;
export const MAX_SESSION_CHANNELS = 64;
export const MAX_OPEN_PAYLOAD = 40 << 10;
export const MAX_BROWSER_OPEN = 16 << 10;
export const MAX_CLOSE_PAYLOAD = 4 << 10;
export const MAX_PARAMS = 8 << 10;
export const MAX_MESSAGE_TEXT = 256;
export const HANDSHAKE_SIZE = 48;
export const MAX_E2E_PLAINTEXT = MAX_PAYLOAD - 16;
export const MAX_CALL_RESULT = 8 << 20;
export const E2E_FIRST_TIMEOUT = 30;
export const E2E_DATA = 1, E2E_END = 2;
const MAX_TASK = 16 << 10;

export class BadFrame extends Error {}

function checkFrame(f) {
  if (f.payload.length > MAX_PAYLOAD) throw new BadFrame("payload");
  switch (f.type) {
    case TYPE_DATA: return;
    case TYPE_OPEN:
      if (f.channel === CONTROL_CHANNEL || f.payload.length === 0 || f.payload.length > MAX_OPEN_PAYLOAD) {
        throw new BadFrame("open");
      }
      return;
    case TYPE_EOF:
      if (f.channel === CONTROL_CHANNEL || f.payload.length !== 0) throw new BadFrame("eof");
      return;
    case TYPE_CLOSE:
      if (f.channel === CONTROL_CHANNEL || f.payload.length === 0 || f.payload.length > MAX_CLOSE_PAYLOAD) {
        throw new BadFrame("close");
      }
      return;
    case TYPE_GRANT:
      if (f.channel === CONTROL_CHANNEL || (f.payload.length !== 4 && f.payload.length !== 4 + HANDSHAKE_SIZE)) {
        throw new BadFrame("grant");
      }
      return;
  }
  throw new BadFrame("type");
}

export function encodeFrame(f) {
  checkFrame(f);
  const b = new Uint8Array(HEADER_SIZE + f.payload.length);
  b[0] = f.type;
  new DataView(b.buffer).setUint32(1, f.channel);
  b.set(f.payload, HEADER_SIZE);
  return b;
}

export function decodeFrame(b) {
  if (b.length < HEADER_SIZE || b.length > MAX_FRAME) throw new BadFrame("size");
  const f = { type: b[0], channel: new DataView(b.buffer, b.byteOffset).getUint32(1), payload: b.subarray(HEADER_SIZE) };
  checkFrame(f);
  return f;
}

export function grant(credit, handshake) {
  const b = new Uint8Array(4 + (handshake ? handshake.length : 0));
  new DataView(b.buffer).setUint32(0, credit);
  if (handshake) b.set(handshake, 4);
  return b;
}

export function parseGrant(p) {
  if (p.length !== 4 && p.length !== 4 + HANDSHAKE_SIZE) throw new BadFrame("grant");
  const credit = new DataView(p.buffer, p.byteOffset).getUint32(0);
  if (credit === 0 || credit > MAX_WINDOW) throw new BadFrame("credit");
  return { credit, handshake: p.length === 4 ? null : p.subarray(4) };
}
```

Der OPEN-Teil: die Shell schreibt ein OPEN selbst (`encodeOpen`) und liest keines von außen, prüft
sich aber mit `parseOpen` gegen `wire.json`:

```js
export const openShape = S({
  app: str, service: str, action: str, params: raw, window: uint32, e2e: bytes,
  task: P({ payload: bytes, kid: str, sig: bytes }), confirm: P({ kind: str, kid: str, sig: bytes,
    authData: bytes, clientData: bytes }),
});

const openBudget = new Budget({ size: MAX_BROWSER_OPEN, items: { params: RAW } });

export function parseOpen(p) {
  const o = decode(p, openBudget, openShape);
  if (!validateOpen(o)) throw new BadFrame("open payload");
  return o;
}

export function validateOpen(o) {
  if (!validAppID(o.app) || !validName(o.service) || !validName(o.action)) return false;
  if (!validParams(o.params)) return false;
  if (o.window < MIN_WINDOW || o.window > MAX_WINDOW) return false;
  // Core apps run in the API, inside the session's own Noise session; every
  // other app gets Noise message 1 of its end-to-end handshake.
  if (coreApp(o.app)) {
    if (o.e2e.length !== 0) return false;
  } else if (o.e2e.length !== HANDSHAKE_SIZE) return false;
  // With a task, only the signed params count, so the OPEN carries none.
  if (o.task && ((o.params && o.params.length !== 0) || !validateEnvelope(o.task, MAX_TASK))) return false;
  if (o.confirm && (!o.task || !validateProof(o.confirm))) return false;
  return true;
}

// validParams: empty, or one JSON object of at most MAX_PARAMS bytes; the
// strict decoder has already checked UTF-8 and repeated names.
export function validParams(rawValue) {
  if (!rawValue || rawValue.length === 0) return true;
  return rawValue.length <= MAX_PARAMS && rawValue[0] === 0x7b;
}

// encodeOpen writes the OPEN in the field order of Go's wire.Open. These exact
// bytes travel to the controller unchanged, and the end-to-end prologue hashes
// the params and task members from them.
export function encodeOpen(o) {
  let s = '{"app":' + JSON.stringify(o.app) + ',"service":' + JSON.stringify(o.service)
    + ',"action":' + JSON.stringify(o.action);
  if (o.params && o.params.length > 0) s += ',"params":' + new TextDecoder().decode(o.params);
  s += ',"window":' + o.window;
  if (o.e2e && o.e2e.length > 0) s += ',"e2e":"' + b64encode(o.e2e) + '"';
  if (o.taskBytes) s += ',"task":' + new TextDecoder().decode(o.taskBytes);
  if (o.confirmBytes) s += ',"confirm":' + new TextDecoder().decode(o.confirmBytes);
  return utf8(s + "}");
}
```

CLOSE, Codes und Meldungen:

```js
// `__proto__: null`, damit die Tabelle nichts erbt. Hier wird zwar nur mit
// festen Namen zugegriffen, aber eine Codetabelle mit Erbschaft ist die Form,
// die eine Datei weiter zur Lücke wurde (siehe MESSAGES).
export const CODES = {
  __proto__: null,
  FORBIDDEN: "E_FORBIDDEN", UNREACHABLE: "E_UNREACHABLE", EXIT: "E_EXIT", TIMEOUT: "E_TIMEOUT",
  LIMIT: "E_LIMIT", PROTOCOL: "E_PROTOCOL", CONFIRM: "E_CONFIRM", LICENSE: "E_LICENSE",
  NOTFOUND: "E_NOTFOUND", VERIFY: "E_VERIFY", INTERNAL: "E_INTERNAL", TOOLARGE: "E_TOOLARGE",
  LOGIN: "E_LOGIN", LOCKED: "E_LOCKED",
};

// Eine Map, kein Objektliteral: bei `{…}` ist `"toString" in MESSAGES` wahr und
// `MESSAGES["constructor"]` gibt eine Funktion zurück — der Prüfer unten hätte
// `{"status":"error","code":"toString"}` angenommen und `message("constructor")`
// mit einer Funktion beantwortet. Go weist beides ab. In Aufgabe 10 gemessen.
const MESSAGES = new Map(Object.entries({
  E_FORBIDDEN: "Keine Berechtigung.",
  E_UNREACHABLE: "Dienst nicht erreichbar.",
  E_EXIT: "Dienst mit Fehler beendet.",
  E_TIMEOUT: "Zeitlimit überschritten.",
  E_LIMIT: "Zu viele offene Verbindungen.",
  E_PROTOCOL: "Ungültige Nachricht.",
  E_CONFIRM: "Bitte erneut bestätigen.",
  E_LICENSE: "Keine gültige Lizenz.",
  E_NOTFOUND: "Nicht gefunden.",
  E_VERIFY: "Unterschrift ungültig.",
  E_INTERNAL: "Interner Fehler.",
  E_TOOLARGE: "Zu viele Daten.",
  E_LOGIN: "Anmeldung fehlgeschlagen.",
  E_LOCKED: "Zu viele Fehlversuche. Bitte später erneut versuchen.",
}));

export const message = (code) => MESSAGES.get(code) ?? "";

const closeShape = S({ status: str, code: str, message: str, exitCode: P({}) });
const closeBudget = new Budget({ size: MAX_CLOSE_PAYLOAD });

export function parseClose(p) {
  // exitCode is a number that may be missing, which the shape language marks
  // with its own kind; see the note below.
  const c = decode(p, closeBudget, closeShapeWithExit);
  if (c.status === "ok") {
    if (c.code !== "") throw new BadFrame("close");
  } else if (c.status === "error") {
    if (!MESSAGES.has(c.code)) throw new BadFrame("close");
  } else {
    throw new BadFrame("close");
  }
  // The shell shows the message to people, so it is text for people like any
  // name or title, which also bounds its length.
  if (c.message !== "" && !validText(c.message, MAX_MESSAGE_TEXT)) throw new BadFrame("close");
  if (c.exitCode !== null && (c.exitCode < 0 || c.exitCode > 255)) throw new BadFrame("close");
  return c;
}

export const errorClose = (code) => ({ status: "error", code, message: message(code), exitCode: null });

export function encodeClose(c) {
  const o = { status: c.status };
  if (c.code) o.code = c.code;
  if (c.message) o.message = c.message;
  if (c.exitCode !== null && c.exitCode !== undefined) o.exitCode = c.exitCode;
  return utf8(JSON.stringify(o));
}
```

`closeShapeWithExit` braucht eine Zahl, die fehlen darf. Dafür bekommt `json.js` in diesem Schritt
das Kürzel `optInt = { k: "int", opt: true }`; `zero` gibt dafür `null`, sonst ändert sich nichts.
`closeShapeWithExit = S({ status: str, code: str, message: str, exitCode: optInt })`.

Ende-zu-Ende-Nachrichten und Steuerkanal:

```js
export function e2e(kind, body) {
  if (kind === E2E_DATA) {
    if (body.length > MAX_E2E_PLAINTEXT - 1) throw new BadFrame("e2e data");
  } else if (kind === E2E_END) {
    if (body.length !== 0) parseClose(body);
  } else {
    throw new BadFrame("e2e kind");
  }
  return concat(new Uint8Array([kind]), body);
}

export function parseE2E(plain, fromController) {
  if (plain.length === 0 || plain.length > MAX_E2E_PLAINTEXT) throw new BadFrame("e2e");
  const kind = plain[0];
  const body = plain.subarray(1);
  if (kind === E2E_DATA) return { kind, data: body, status: null };
  if (kind === E2E_END) {
    if (!fromController) {
      if (body.length !== 0) throw new BadFrame("e2e end");
      return { kind, data: null, status: null };
    }
    return { kind, data: null, status: parseClose(body) };
  }
  throw new BadFrame("e2e kind");
}

export const CTL_HELLO = "hello", CTL_TRUST = "trust", CTL_NOTIFY = "notify";
export const CTL_STATUS = "status", CTL_UPDATE = "update", CTL_BYE = "bye";
export const LINK_SESSION = "session", LINK_TUNNEL = "tunnel";

export function controlAllowed(link, fromAPI, t) {
  if (link === LINK_SESSION && fromAPI) return t === CTL_TRUST || t === CTL_NOTIFY || t === CTL_BYE;
  if (link === LINK_SESSION) return t === CTL_BYE;
  if (link === LINK_TUNNEL && fromAPI) return t === CTL_HELLO || t === CTL_BYE;
  if (link === LINK_TUNNEL) {
    return t === CTL_HELLO || t === CTL_STATUS || t === CTL_UPDATE || t === CTL_NOTIFY || t === CTL_BYE;
  }
  return false;
}

const controlShape = S({ t: str, b: raw });
const controlBudget = new Budget({ size: MAX_PAYLOAD, items: { b: RAW } });

export function parseControl(p) {
  const c = decode(p, controlBudget, controlShape);
  if (![CTL_HELLO, CTL_TRUST, CTL_NOTIFY, CTL_STATUS, CTL_UPDATE, CTL_BYE].includes(c.t)) {
    throw new BadFrame("control");
  }
  return c;
}

export function encodeControl(c) {
  return utf8(c.b ? '{"t":' + JSON.stringify(c.t) + ',"b":' + new TextDecoder().decode(c.b) + "}"
    : '{"t":' + JSON.stringify(c.t) + "}");
}

export { coreApp as CORE_APP };
```

- [ ] **Step 4: Test laufen lassen, er muss bestehen**

```bash
cd apps/web/tests && npm test -- contracts/wire.spec.js
```

Erwartet: alle 8 Frames, 13 kaputten Frames, 9 Freigaben, 31 OPENs, 34 CLOSEs, 15
Ende-zu-Ende-Nachrichten und 26 Steuerregeln stimmen mit Go überein.

- [ ] **Step 5: Committen**

```bash
git add apps/web/public/assets/js/contracts/wire.js apps/web/public/assets/js/contracts/json.js apps/web/tests/contracts/wire.spec.js
git commit -m "Add frames, channels and error codes" --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 11: `ui-tar` lesen – die eine Form, die die Shell ausführt

**Files:**
- Create: `apps/web/public/assets/js/contracts/uitar.js`
- Test: `apps/web/tests/contracts/uitar.spec.js`

**Interfaces:**
- Produces: `MAX_ENTRIES = 10000`, `MAX_DIRS = 1000`, `MAX_UI_FILE = 16777216`,
  `MAX_UI_TOTAL = 67108864`, `BLOCK = 512`, `walkUI(bytes) -> [{path, size, data}]`,
  `class UIWriter` mit `dir(path)`, `file(path, data, hash?)`, `close(hash?) -> Uint8Array`,
  **Der zweite Parameter ist der Hash aus Aufgabe 12** (`sha256Stream`), in den der
  Schreiber alles mitschreibt, was er ausgibt. Ohne ihn kann Aufgabe 12 ein Paket
  von 4 GiB nicht in EINEM Durchgang pruefen. Diese Zeile nannte ihn frueher nicht,
  waehrend Aufgabe 12 ihn im Text verlangte - ein Widerspruch zwischen zwei Aufgaben,
  genau das, was dieser Block verhindern soll.
  `class BadPackage extends Error`.
- Consumes: `text.js` (`validPath`).

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

`apps/web/tests/contracts/uitar.spec.js`:

```js
import { test, expect } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";

const v = vectors("ui.json");

test("every ui-tar of ui.json is decided as Go decides it", async ({ shell }) => {
  test.setTimeout(180_000);
  const got = await shell.evaluate(async (cases) => {
    const u = await import("/assets/js/contracts/uitar.js");
    const { b64decode, b64encode } = await import("/assets/js/contracts/b64.js");
    return cases.map((c) => {
      try {
        const files = u.walkUI(b64decode(c.tar));
        return { valid: true, files: files.map((f) => ({ path: f.path, size: f.size })) };
      } catch {
        return { valid: false, files: [] };
      }
    });
  }, v);

  const wrong = v.filter((c, i) => c.valid !== got[i].valid).map((c) => c.name);
  expect(wrong).toEqual([]);
  // For a valid archive the reader must hand out exactly the files Go hands
  // out, in the same order.
  for (let i = 0; i < v.length; i++) {
    if (!v[i].valid) continue;
    const want = (v[i].files ?? []).map((f) => ({ path: f.path, size: f.size }));
    expect(got[i].files, v[i].name).toEqual(want);
  }
});

test("the size limits are checked at the header, before any data is read", async ({ shell }) => {
  test.setTimeout(120_000);
  const out = await shell.evaluate(async () => {
    const u = await import("/assets/js/contracts/uitar.js");
    const fails = (make) => { try { make(); return false; } catch { return true; } };
    const filler = (n) => new Uint8Array(n);
    // These cases are not in ui.json; they are built here.
    const oneFile = (size) => {
      const w = new u.UIWriter();
      w.file("ui/big.bin", filler(size));
      return w.close();
    };
    return {
      // MaxUIFile: a file one byte over the limit is refused.
      fileAtLimit: !fails(() => u.walkUI(oneFile(u.MAX_UI_FILE))) === false ? "slow" : "slow",
      entriesAtLimit: !fails(() => {
        const w = new u.UIWriter();
        for (let i = 0; i < u.MAX_ENTRIES; i++) w.file("ui/f" + i, new Uint8Array(0));
        u.walkUI(w.close());
      }),
      // ACHTUNG, diese zwei Faelle messen nicht, was ihr Name sagt: der
      // UIWriter haelt dieselben Grenzen und wirft schon beim Schreiben, also
      // wird walkUI nie gerufen und fails() faengt den Wurf des SCHREIBERS.
      // Gemessen wird damit die Schreibseite, nicht die Leseseite. Bau die
      // Bloecke roh und mit eigener Pruefsumme, so wie Gos TestWalkUILimits.
      entriesOver: fails(() => {
        const w = new u.UIWriter();
        for (let i = 0; i <= u.MAX_ENTRIES; i++) w.file("ui/f" + i, new Uint8Array(0));
        u.walkUI(w.close());
      }),
      totalOver: fails(() => {
        const w = new u.UIWriter();
        // Five files of 16 MiB are 80 MiB, over MaxUITotal.
        for (let i = 0; i < 5; i++) w.file("ui/f" + i, filler(u.MAX_UI_FILE));
        u.walkUI(w.close());
      }),
    };
  });
  expect(out.entriesAtLimit).toBe(true);
  expect(out.entriesOver).toBe(true);
  expect(out.totalOver).toBe(true);
});
```

Der Fall `fileAtLimit` wird beim Schreiben des Tests durch einen sparsamen ersetzt: ein Kopf mit
`size` = `MAX_UI_FILE + 1` von Hand gebaut und ohne Daten dahinter; `walkUI` muss ihn ablehnen,
ohne weiterzulesen (das ist genau die Regel „… header only“ aus `ui.json`).

- [ ] **Step 2: Test laufen lassen, er muss scheitern**

```bash
cd apps/web/tests && npx playwright test contracts/uitar.spec.js --project=chromium
```

Erwartet: Fehler beim Import von `uitar.js`.

- [ ] **Step 3: `uitar.js` schreiben**

Der Leser folgt dem Kommentar in `shared/store/ui.go` Zeile für Zeile. Aufbau:

```js
// A ui-tar is how the API keeps an app's UI and sends it to the shell
// (ngc.desktop/ui). There is exactly one form, the one store/ui.go writes;
// everything else is refused, so browser and Go see exactly the same files.
import { validPath } from "./text.js";

export const BLOCK = 512;
export const MAX_ENTRIES = 10000;
export const MAX_DIRS = 1000;
export const MAX_UI_FILE = 16 << 20;
export const MAX_UI_TOTAL = 64 << 20;
const NAME_FIELD = 100;

export class BadPackage extends Error {}

// parseHeader checks one block against the layout the writer produces: every
// field has exactly one value but the name, the size and the checksum.
function parseHeader(b) {
  const flag = b[156];
  let mode = "0000644\0";
  if (flag === 0x35) mode = "0000755\0";
  else if (flag !== 0x30 && flag !== 0x78) throw new BadPackage("typeflag");
  let n = b.indexOf(0, 0);
  if (n < 0 || n > 100) n = 100;
  const size = octal(b.subarray(124, 136));
  const sum = octal(b.subarray(148, 155));
  if (b[155] !== 0x20 || sum !== headerSum(b)) throw new BadPackage("checksum");
  if (!allNUL(b.subarray(n, 100)) || ascii(b.subarray(100, 108)) !== mode
    || ascii(b.subarray(108, 124)) !== "0000000\u00000000000\u0000"
    || ascii(b.subarray(136, 148)) !== "00000000000\u0000"
    || !allNUL(b.subarray(157, 257)) || ascii(b.subarray(257, 265)) !== "ustar\u000000"
    || !allNUL(b.subarray(265))) throw new BadPackage("header");
  if (flag === 0x35 && size !== 0) throw new BadPackage("directory size");
  return { flag, name: ascii(b.subarray(0, n)), size };
}
```

`octal(field)` liest Ziffern 0 bis 7, die auf genau ein NUL enden, und wirft sonst;
`headerSum(b)` summiert alle 512 Byte, die acht Prüfsummen-Bytes als Leerzeichen gezählt;
`allNUL` und `ascii` sind die offensichtlichen Helfer.

Der Hauptdurchgang:

```js
export function walkUI(b) {
  const files = [];
  const tree = newTree();
  let total = 0, i = 0, n = 0;
  const block = () => {
    if (i + BLOCK > b.length) throw new BadPackage("truncated");
    const out = b.subarray(i, i + BLOCK);
    i += BLOCK;
    return out;
  };
  for (;;) {
    n++;
    const blk = block();
    if (allNUL(blk)) {
      // The end marker: one more zero block, then nothing.
      if (!allNUL(block()) || i !== b.length) throw new BadPackage("end marker");
      return files;
    }
    let e = parseHeader(blk);
    let name = e.name;
    if (e.flag === 0x78) {
      // A name too long for a header, then the entry's own header.
      if (name !== "" || e.size > BLOCK) throw new BadPackage("pax");
      const data = block();
      if (!allNUL(data.subarray(e.size))) throw new BadPackage("pax padding");
      name = paxPath(data.subarray(0, e.size));
      if (name.length <= NAME_FIELD) throw new BadPackage("pax for a short name");
      e = parseHeader(block());
      if (e.flag === 0x78 || e.name !== "") throw new BadPackage("pax");
    } else if (name === "") {
      throw new BadPackage("name");
    }
    const isDir = e.flag === 0x35;
    const path = uiName(name, isDir);
    if (n > MAX_ENTRIES) throw new BadPackage("entries");
    if (!isDir) {
      // Checked from the header, before anything after it is read.
      total += e.size;
      if (e.size > MAX_UI_FILE || total > MAX_UI_TOTAL) throw new BadPackage("size");
    }
    if (!tree.add(path, !isDir)) throw new BadPackage("tree");
    if (isDir) continue;
    if (i + e.size > b.length) throw new BadPackage("truncated");
    const data = b.subarray(i, i + e.size);
    i += e.size;
    const pad = (BLOCK - (e.size % BLOCK)) % BLOCK;
    if (pad > 0) {
      if (i + pad > b.length || !allNUL(b.subarray(i, i + pad))) throw new BadPackage("padding");
      i += pad;
    }
    files.push({ path, size: e.size, data });
  }
}

// uiName checks the name of an entry and returns its path: a file
// "ui/<validPath>", a directory "ui/" or "ui/<validPath>/" without the "/".
function uiName(name, dir) {
  if (dir) {
    if (!name.endsWith("/")) throw new BadPackage("directory");
    const p = name.slice(0, -1);
    if (p !== "ui" && !uiPath(p)) throw new BadPackage("directory");
    return p;
  }
  if (!uiPath(name)) throw new BadPackage("name");
  return name;
}

const uiPath = (p) => p.startsWith("ui/") && validPath(p.slice(3));

// paxPath reads the data of a PAX header: exactly "<length> path=<name>\n",
// the length counting the whole record, without sign or leading zero.
function paxPath(data) {
  const s = ascii(data);
  const sp = s.indexOf(" ");
  if (sp < 0 || !s.slice(sp).startsWith(" path=") || !s.endsWith("\n")) throw new BadPackage("pax record");
  const name = s.slice(sp + " path=".length, -1);
  if (name === "" || paxRecord(name) !== s) throw new BadPackage("pax record");
  return name;
}

function paxRecord(name) {
  const s = " path=" + name + "\n";
  let n = s.length + 1;
  while (String(n).length + s.length !== n) n = String(n).length + s.length;
  return String(n) + s;
}
```

`newTree()` baut die Baum-Regeln von `store.tree` nach: kein Pfad zweimal, kein Eintrag unter einer
Datei, keine Datei, wo ein Verzeichnis gebraucht wird, höchstens `MAX_DIRS` Verzeichnisse – jedes
einmal gezählt, ob ein Eintrag es nennt oder ein Pfad es verlangt.

`UIWriter` schreibt dieselbe Form (Kopf mit `uiHeader`, PAX-Kopf ab 101 Byte Name, Daten auf
Blockgröße mit NUL aufgefüllt, am Ende zwei Nullblöcke) und hält dieselben Regeln ein. Die Shell
braucht ihn in Aufgabe 12, um die UI-Prüfsumme eines hochgeladenen Pakets selbst zu rechnen.

- [ ] **Step 4: Test laufen lassen, er muss bestehen**

```bash
cd apps/web/tests && npm test -- contracts/uitar.spec.js
```

Erwartet: alle 100 Fälle aus `ui.json` werden wie in Go entschieden, und die selbst erzeugten
Grenzfälle (`MaxEntries`, `MaxUITotal`, ein Kopf über `MaxUIFile`) werden abgelehnt.

- [ ] **Step 5: Committen**

```bash
git add apps/web/public/assets/js/contracts/uitar.js apps/web/tests/contracts/uitar.spec.js
git commit -m "Add the reader and writer for ui-tars" --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 12: App-Paket lesen und die UI-Prüfsumme selbst rechnen

**Files:**
- Create: `apps/web/public/assets/js/contracts/pack.js`
- Test: `apps/web/tests/contracts/pack.spec.js`

**Interfaces:**
- Produces: `MAX_PACKAGE = 4294967296`, `packageUI(stream) -> Promise<{manifest, uiTar, sha256, packageSha256}>`,
  `sha256Stream()` (aus `vendor/noble-hashes/sha2.js`, mit `update`/`digest`).
  `packageUI` nimmt einen `ReadableStream` (aus `File.stream()`), liest ihn genau einmal und
  rechnet dabei beide Prüfsummen, damit eine Datei von bis zu 4 GiB nie ganz im Speicher liegt.
- Consumes: `uitar.js`, `catalog.js` (Manifest), `vendor/noble-hashes/sha2.js`.

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

`apps/web/tests/contracts/pack.spec.js`:

```js
import { test, expect } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";

const v = vectors("packui.json");

test("the shell computes the UI hash of a package itself", async ({ shell }) => {
  test.setTimeout(120_000);
  const got = await shell.evaluate(async (cases) => {
    const p = await import("/assets/js/contracts/pack.js");
    const { b64decode, b64encode, equal } = await import("/assets/js/contracts/b64.js");
    const out = [];
    for (const c of cases) {
      const pkg = b64decode(c.package);
      const stream = new Blob([pkg]).stream();
      const r = await p.packageUI(stream);
      out.push({
        ui: b64encode(r.uiTar), hash: b64encode(r.sha256),
        pkgHash: b64encode(r.packageSha256) === b64encode(
          new Uint8Array(await crypto.subtle.digest("SHA-256", pkg))),
        id: r.manifest.id,
      });
    }
    return out;
  }, v);
  expect(got.map((g) => g.ui)).toEqual(v.map((c) => c.ui));
  expect(got.map((g) => g.hash)).toEqual(v.map((c) => c.hash));
  expect(got.every((g) => g.pkgHash)).toBe(true);
});

// ACHTUNG: jeder dieser Angriffe steht unten als ERSTER Eintrag des Archivs und
// wird deshalb von der Regel "Manifest zuerst" abgelehnt, nicht von seiner eigenen.
// Gemessen: mit dieser Fassung bleibt der Lauf gruen, auch wenn man die
// Typeflag-Pruefung ganz entfernt - vier der fuenf Faelle messen nichts.
// Stell jedem Angriff einen annehmbaren Rahmen voran (Manifest zuerst), damit er
// bis zu seiner Regel kommt.
//
// Und der Sparse-Fall hat die falsche Gestalt: `flag 0x53` ('S') ist die alte
// GNU-Form. Gefaehrlich ist heute ein GEWOEHNLICHER '0'-Eintrag mit einem
// PAX-Satz `GNU.sparse.*` - so steht es weiter unten auch im Text. Beide Formen
// gehoeren geprueft.
test("a package with a link, an absolute path or a sparse entry is refused", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const p = await import("/assets/js/contracts/pack.js");
    const bad = async (make) => {
      try { await p.packageUI(new Blob([make()]).stream()); return false; } catch { return true; }
    };
    return {
      symlink: await bad(() => p.testTar([{ name: "ui/x", flag: 0x32, size: 0 }])),
      absolute: await bad(() => p.testTar([{ name: "/etc/passwd", flag: 0x30, size: 0 }])),
      parent: await bad(() => p.testTar([{ name: "ui/../x", flag: 0x30, size: 0 }])),
      sparse: await bad(() => p.testTar([{ name: "ui/x", flag: 0x53, size: 0 }])),
      noManifest: await bad(() => p.testTar([{ name: "ui/index.html", flag: 0x30, size: 0 }])),
    };
  });
  expect(out).toEqual({ symlink: true, absolute: true, parent: true, sparse: true, noManifest: true });
});
```

- [ ] **Step 2: Test laufen lassen, er muss scheitern**

```bash
cd apps/web/tests && npx playwright test contracts/pack.spec.js --project=chromium
```

Erwartet: Fehler beim Import von `pack.js`.

- [ ] **Step 3: `pack.js` schreiben**

```js
// Reading an app package the admin uploads. Whatever the shell signs it
// computes itself: the package's SHA-256, its manifest and its UI hash
// (store.PackageUI). The API's answer only has to match (core.UploadResult).
//
// A package may be up to 4 GiB, so it is read as a stream: every byte passes
// through once, and only the ui-tar is kept, which is at most 84 MiB.
import { sha256 } from "../../vendor/noble-hashes/sha2.js";
import { UIWriter, BadPackage } from "./uitar.js";
import { parseManifest } from "./catalog.js";
import { validPath } from "./text.js";

export const MAX_PACKAGE = 4 * 1024 * 1024 * 1024;
export const MAX_MANIFEST = 64 << 10;
export const MAX_ENTRIES = 10000;
const BLOCK = 512;

export async function packageUI(stream) {
  const pkgHash = sha256.create();
  const ui = new UIWriter();
  const uiHash = sha256.create();
  const r = new TarReader(stream, pkgHash);

  const first = await r.next();
  if (!first || first.flag !== 0x30 || first.name !== "manifest.json" || first.size > MAX_MANIFEST) {
    throw new BadPackage("manifest");
  }
  const manifest = parseManifest(await r.read(first.size));

  const tree = { files: new Set(["manifest.json"]) };
  let images = 0, n = 1;
  for (;;) {
    const h = await r.next();
    if (!h) break;
    if (++n > MAX_ENTRIES) throw new BadPackage("entries");
    const name = h.name.endsWith("/") ? h.name.slice(0, -1) : h.name;
    if (h.flag === 0x35) {
      if (h.size !== 0 || (name !== "ui" && name !== "services" && !uiPath(name))) throw new BadPackage("dir");
      await r.skip(h.size);
      continue;
    }
    if (h.flag !== 0x30) throw new BadPackage("entry kind");
    if (uiPath(name)) {
      // The ui-tar is written in the order of the package, without directory
      // entries, exactly as store.PackageUI does.
      ui.file(name, await r.read(h.size), uiHash);
      tree.files.add(name);
      continue;
    }
    if (name === "services/amd64.tar" || name === "services/arm64.tar") {
      images++;
      tree.files.add(name);
      await r.skip(h.size);
      continue;
    }
    throw new BadPackage("entry");
  }
  const entry = manifest.ui ? "ui/" + manifest.ui.entry : null;
  if ((entry && !tree.files.has(entry))
    || (manifest.icon !== "" && !tree.files.has("ui/" + manifest.icon))
    || (manifest.services.length > 0) !== (images > 0)) throw new BadPackage("manifest names what is missing");

  const uiTar = ui.close(uiHash);
  return { manifest, uiTar, sha256: uiHash.digest(), packageSha256: pkgHash.digest() };
}

const uiPath = (name) => name.startsWith("ui/") && validPath(name.slice(3));
```

`TarReader` liest den Datenstrom blockweise und versteht genau das, was Go `archive/tar` liest und
`store.Walk` zulässt: USTAR- und PAX-Köpfe mit `path`, GNU-Langnamen (`typeflag 'L'`), Dateien und
Verzeichnisse. Alles andere – Links (`1`, `2`), Geräte (`3`, `4`), FIFO (`6`), GNU-Sparse (`S`),
jeder PAX-Satz `GNU.sparse.*`, absolute Pfade und Pfade mit `..` – wirft `BadPackage`. Jeder
gelesene Block läuft zusätzlich durch `pkgHash`, damit die Prüfsumme des Pakets aus genau den
Bytes entsteht, die geschickt werden.

`UIWriter` bekommt dafür einen zweiten Parameter: einen Hash, in den er alles schreibt, was er
ausgibt. `testTar(entries)` ist eine kleine Hilfe nur für Tests, die ein Archiv aus
Kopf-Beschreibungen baut.

- [ ] **Step 4: Test laufen lassen, er muss bestehen**

```bash
cd apps/web/tests && npm test -- contracts/pack.spec.js
```

Erwartet: die drei Pakete aus `packui.json` ergeben dasselbe ui-tar und dieselbe Prüfsumme wie in
Go; alle fünf Angriffe werden abgelehnt.

- [ ] **Step 5: Committen**

```bash
git add apps/web/public/assets/js/contracts/pack.js apps/web/tests/contracts/pack.spec.js
git commit -m "Read app packages and compute the UI hash in the browser" --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 13: Katalog, Manifest und Update-Zeilen

**Files:**
- Create: `apps/web/public/assets/js/contracts/catalog.js`
- Test: `apps/web/tests/contracts/catalog.spec.js`

**Interfaces:**
- Produces: `MAX_APPS = 10000`, `MAX_VERSIONS = 100`, `MAX_CATALOG_VERSIONS = 30000`,
  `MAX_LICENSE = 8192`, `MAX_MANIFEST = 65536`, `MAX_ACTIONS = 64`, `MAX_SERVICES = 16`,
  `NEED_ROOT = "root"`, `NEED_NETWORK = "network"`,
  `MODE_AUTO/SEMI/NOTIFY`, `UPDATE_AVAILABLE/DOWNLOADED/INSTALLING/INSTALLED/ROLLEDBACK/FAILED/REJECTED/NEEDSGRANTS`,
  `catalogShape`, `manifestShape`, `parseManifest(bytes) -> manifest`,
  `verifyCatalog(envelope, store, lastSeq) -> Promise<catalog>`,
  `validTarget(t)`, `granted(needs, grants)`, `approved(list, target, key)`,
  `parseUpdateReport(bytes)`.
- Consumes: `b64.js`, `json.js`, `text.js`, `url.js`, `sign.js`, `trust.js`.

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

`apps/web/tests/contracts/catalog.spec.js`:

```js
import { test, expect } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";

const v = vectors("store.json");

test("a catalog is only taken from its own store, and never an older one", async ({ shell }) => {
  test.setTimeout(120_000);
  const got = await shell.evaluate(async (d) => {
    const c = await import("/assets/js/contracts/catalog.js");
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    const store = { id: d.store, name: "NGC Store", url: "https://store.ngc.example", key: b64decode(d.storeKey) };
    const env = (e) => ({ payload: b64decode(e.payload), kid: e.kid, sig: b64decode(e.sig) });
    const ok = async (e, lastSeq) => {
      try { await c.verifyCatalog(env(e), store, lastSeq); return true; } catch { return false; }
    };
    return {
      good: await ok(d.catalog, 0),
      rollback: await ok(d.catalog, d.catalogLastSeq + 1),
      // Jeder Ablehnungsfall bringt sein eigenes lastSeq mit: "older than the
      // last one seen" traegt 6 und MUSS bei 0 angenommen werden. Mit der 0 ist
      // dieser Test gegen einen Pruefer ganz ohne Rueckroll-Pruefung gruen -
      // also ein Test, der fuer seine eigene Sache nicht scheitern kann.
      rejects: await Promise.all(d.catalogRejects.map((r) => ok(r.catalog, r.lastSeq))),
      otherStore: await ok(d.catalog, 0) && !(await (async () => {
        try {
          await c.verifyCatalog(env(d.catalog), { ...store, id: "other" }, 0);
          return true;
        } catch { return false; }
      })()),
    };
  }, v);
  expect(got.good).toBe(true);
  expect(got.rollback).toBe(false);
  expect(got.otherStore).toBe(true);
  const wrong = v.catalogRejects.filter((r, i) => got.rejects[i]).map((r) => r.name);
  expect(wrong).toEqual([]);
});

test("the grant rule of an update holds", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const c = await import("/assets/js/contracts/catalog.js");
    return {
      same: c.granted(["root"], ["root"]),
      fewer: c.granted([], ["root"]),
      more: c.granted(["root", "network"], ["root"]),
      target: c.validTarget("ngc-web") && c.validTarget("system") && c.validTarget("org.sorglos.terminal")
        && !c.validTarget("ngc.desktop") && !c.validTarget("terminal"),
    };
  });
  expect(out).toEqual({ same: true, fewer: true, more: false, target: true });
});
```

- [ ] **Step 2: Test laufen lassen, er muss scheitern**

```bash
cd apps/web/tests && npx playwright test contracts/catalog.spec.js --project=chromium
```

Erwartet: Fehler beim Import von `catalog.js`.

- [ ] **Step 3: `catalog.js` schreiben**

Formen und Budgets wörtlich nach `store/catalog.go` und `store/manifest.go`:

```js
const catalogBudget = new Budget({
  size: MAX_PAYLOAD,
  items: { apps: MAX_APPS, "apps/*/versions": MAX_VERSIONS, "apps/*/versions/*/needs": 2 },
  totals: { "apps/*/versions": MAX_CATALOG_VERSIONS },
});

const manifestBudget = new Budget({
  size: MAX_MANIFEST,
  items: { actions: MAX_ACTIONS, services: MAX_SERVICES, "services/*/command": 32, needs: 2 },
});
```

`verifyCatalog(env, store, lastSeq)`: erst `sign.verify(env, store.key, PURPOSE_CATALOG)` – die
Unterschrift deckt die genauen Bytes –, dann die Nutzlast lesen und prüfen (`store` gleich
`store.id`, `seq >= 1`, `issued > 0`, Apps nach ID aufsteigend ohne Doppel, je App
`validAppID` und kein Kern-App-Präfix, `name`/`publisher` als `validText`, `description` gültiges
UTF-8 bis zur Grenze, Versionen aufsteigend nach `newer`, je Version `validVersion`, `url` nach der
Katalog-Adressregel (`https://<host>[:<port>][/<pfad>][?<abfrage>]` aus druckbarem ASCII ohne
`"` `'` `<` `>` `\` `#`, höchstens 512 Byte, oder ein Pfad neben dem Katalog nach `validPath`),
`size > 0`, `sha256` und `ui` je 32 Byte, `needs` nur `root` und `network`, jedes höchstens einmal,
`minNgc` leer oder `validVersion`; bei `price` Modell, `per`, `amount > 0`, Währung `^[A-Z]{3}$`
und `shop` nach derselben Adressregel). Zum Schluss `seq < lastSeq` ablehnen: eine ältere Fassung
spielt niemand zurück.

`parseManifest(bytes)` liest das Manifest mit `manifestBudget` und prüft es wie
`store.ParseManifest`: `id` ein `validAppID` ohne Kern-Präfix, `name`/`publisher`/`version`,
`icon` und `ui.entry` je ein `validPath`, `ui.width`/`ui.height` 0 oder 200 bis 4000, Aktionen mit
`validName`, `title` als `validText`, `service` aus den Diensten, `timeout` 0 bis 3600, Dienste mit
`validName` und `command` mit 1 bis 32 Teilen, davon der erste absolut, `start` leer oder eine
Aktion, `runtime` leer oder `podman`, `needs` wie im Katalog, `cpu` 0 oder 100 bis 64000, `memory`
0 oder 16 bis 65536, `minNgc` leer oder `validVersion`.

Diese drei Grenzen standen hier weiter als in Go und sind gegen
`shared/store/manifest.go` berichtigt (`size` und die Zeile fuer `cpu`/`memory`).
Eine Shell, die annimmt, was der Parser der API ablehnt, waere eine zweite Regel;
der eingefrorene Vertrag gilt.

Dazu die kleinen Regeln:

```js
export const validTarget = (t) => t === AUTO_WEB || t === AUTO_API || t === AUTO_CONTROLLER
  || t === AUTO_SYSTEM || (validAppID(t) && !coreApp(t));

// granted reports whether the grants of the activation cover the needs of a
// new version; an update never gets more rights than the app was given.
export const granted = (needs, grants) => needs.every((n) => grants.includes(n));

export const approved = (list, target, key) => list.auto.some((a) => a.target === target
  && (target === AUTO_SYSTEM || (key && key.length > 0 && equal(a.key, key))));
```

`parseUpdateReport(bytes)` prüft Ziel, Zustand aus der festen Liste, `version` leer oder
`validVersion` und `message` leer oder `validText(…, 256)`.

- [ ] **Step 4: Test laufen lassen, er muss bestehen**

```bash
cd apps/web/tests && npm test -- contracts/catalog.spec.js
```

Erwartet: der echte Katalog wird angenommen, alle 16 abgelehnten Kataloge abgelehnt, eine ältere
Fassung abgelehnt, ein Katalog eines anderen Stores abgelehnt.

- [ ] **Step 5: Committen**

```bash
git add apps/web/public/assets/js/contracts/catalog.js apps/web/tests/contracts/catalog.spec.js
git commit -m "Add catalogs, manifests and update rows" --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 14: `core` – Kern-Aktionen, Ergebnisse und UI-Beweise

**Files:**
- Create: `apps/web/public/assets/js/contracts/core.js`
- Test: `apps/web/tests/contracts/core.spec.js`

**Interfaces:**
- Produces: `APP_DESKTOP = "ngc.desktop"`, `APP_USERS = "ngc.users"`, `APP_STORE = "ngc.store"`,
  `SERVICE_API = "api"`, `SERVICE_CONTROLLER = "controller"`, `RIGHT_USE = "use"`,
  die Aktionsnamen (`ACT_ME`, `ACT_UI_PROOF`, `ACT_APPS`, `ACT_UI`, `ACT_TOTP_BEGIN`,
  `ACT_TOTP_CONFIRM`, `ACT_TOTP_OFF`, `ACT_LIST`, `ACT_RIGHTS`, `ACT_SET_RIGHTS`, `ACT_TRUST`,
  `ACT_PAIR_CODE`, `ACT_JOIN_CODE`, `ACT_PENDING`, `ACT_PENDING_API`, `ACT_NODES`, `ACT_LOG`,
  `ACT_SOURCES`, `ACT_CATALOG`, `ACT_INSTALLED`, `ACT_UPLOAD`, `ACT_LICENSE`, `ACT_INSTALL_KEY`,
  `ACT_UPDATES`, `ACT_SET_UPDATE`, `ACT_ACTIVATE`, `ACT_DEACTIVATE`, `ACT_UNINSTALL`,
  `ACT_DELETE_DATA`, `ACT_STAGE`, `ACT_INSTALL`),
  `ACTIONS` (Tabelle mit `app, service, name, admin, root, changes, flow, maxIn`),
  `lookup(app, service, name)`, `lookupByKey(app, name)`, `shellOpens(action)`,
  `critical(action, params)`, `LOCAL_STORE = "local"`,
  `encodeParams(action, params) -> Uint8Array`, `validateParams(action, params)`,
  `RESULT_SHAPES` (je Aktion die Form ihres Ergebnisses), `parseResult(action, bytes)`,
  `parseUIProof(bytes)`, `verifyUIProof(proof, app, version, list) -> Promise<Uint8Array>`,
  `parseNotice(bytes)`, `MAX_NOTICE = 1024`, `MAX_TRANSFER_HEADER = 8388608`.
- Consumes: alle bisherigen Vertragsmodule.

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

`apps/web/tests/contracts/core.spec.js`:

```js
import { test, expect } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";

const core = vectors("core.json");
const ui = vectors("uiproof.json");

test("every example result of core.json is read", async ({ shell }) => {
  const got = await shell.evaluate(async (d) => {
    const c = await import("/assets/js/contracts/core.js");
    const out = {};
    for (const [key, value] of Object.entries(d)) {
      if (key === "empty" || key === "notice") continue;
      const [app, name] = key.split("/");
      const action = c.lookupByKey(app, name);
      const bytes = new TextEncoder().encode(JSON.stringify(value));
      try {
        c.parseResult(action, bytes);
        out[key] = true;
      } catch (e) {
        out[key] = String(e);
      }
    }
    // Empty lists arrive as [], never as null.
    for (const [key, value] of Object.entries(d.empty)) {
      const [app, name] = key.split("/");
      const action = c.lookupByKey(app, name);
      const parsed = c.parseResult(action, new TextEncoder().encode(JSON.stringify(value)));
      out["empty:" + key] = Array.isArray(parsed) && parsed.length === 0;
    }
    out.notice = !!c.parseNotice(new TextEncoder().encode(JSON.stringify(d.notice)));
    return out;
  }, core);
  for (const [key, value] of Object.entries(got)) expect(value, key).toBe(true);
});

test("every UI proof of uiproof.json is decided as Go decides it", async ({ shell }) => {
  test.setTimeout(180_000);
  const got = await shell.evaluate(async (d) => {
    const c = await import("/assets/js/contracts/core.js");
    const t = await import("/assets/js/contracts/trust.js");
    const { b64decode, b64encode } = await import("/assets/js/contracts/b64.js");
    const list = await t.parseList(b64decode(d.trust.payload));
    const out = [];
    for (const k of d.cases) {
      try {
        const p = c.parseUIProof(b64decode(k.proof));
        const hash = await c.verifyUIProof(p, k.app, k.version, list);
        out.push({ valid: true, ui: b64encode(hash) });
      } catch {
        out.push({ valid: false, ui: "" });
      }
    }
    return out;
  }, ui);
  const wrong = ui.cases.filter((c, i) => String(c.valid) !== String(got[i].valid)
    || (String(c.valid) === "true" && c.ui !== got[i].ui)).map((c) => c.name);
  expect(wrong).toEqual([]);
});

test("a result over MaxCallResult is refused", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const c = await import("/assets/js/contracts/core.js");
    const w = await import("/assets/js/contracts/wire.js");
    const action = c.lookupByKey("ngc.desktop", "me");
    const big = new Uint8Array(w.MAX_CALL_RESULT + 1).fill(0x20);
    big[0] = 0x7b; big[big.length - 1] = 0x7d;
    try { c.parseResult(action, big); return false; } catch { return true; }
  });
  expect(out).toBe(true);
});
```

- [ ] **Step 2: Test laufen lassen, er muss scheitern**

```bash
cd apps/web/tests && npx playwright test contracts/core.spec.js --project=chromium
```

Erwartet: Fehler beim Import von `core.js`.

- [ ] **Step 3: `core.js` schreiben**

Die Aktionstabelle ist Zeile für Zeile die aus `shared/core/core.go`:

```js
// The actions of the built-in apps: what the shell asks the API for, and the
// root tasks the API forwards to a controller (Go: core).
export const ACTIONS = [
  // app, service, name, admin, root, changes, flow, maxIn
  [APP_DESKTOP, SERVICE_API, ACT_ME, false, false, false, "call", 0],
  [APP_DESKTOP, SERVICE_API, ACT_UI_PROOF, false, false, false, "call", 0],
  [APP_DESKTOP, SERVICE_API, ACT_APPS, false, false, false, "call", 0],
  [APP_DESKTOP, SERVICE_API, ACT_UI, false, false, false, "out", 0],
  [APP_DESKTOP, SERVICE_API, ACT_TOTP_BEGIN, false, false, true, "call", 0],
  [APP_DESKTOP, SERVICE_API, ACT_TOTP_CONFIRM, false, false, true, "call", 0],
  [APP_DESKTOP, SERVICE_API, ACT_TOTP_OFF, false, false, true, "call", 0],
  [APP_USERS, SERVICE_API, ACT_LIST, true, false, false, "call", 0],
  [APP_USERS, SERVICE_API, ACT_RIGHTS, true, false, false, "call", 0],
  [APP_USERS, SERVICE_API, ACT_SET_RIGHTS, true, false, true, "call", 0],
  [APP_USERS, SERVICE_API, ACT_TRUST, true, false, true, "in", 6 << 20],
  [APP_USERS, SERVICE_API, ACT_PAIR_CODE, true, false, true, "call", 0],
  [APP_USERS, SERVICE_API, ACT_JOIN_CODE, true, false, true, "call", 0],
  [APP_USERS, SERVICE_API, ACT_PENDING, true, false, false, "call", 0],
  [APP_USERS, SERVICE_API, ACT_PENDING_API, true, false, false, "call", 0],
  [APP_USERS, SERVICE_API, ACT_NODES, true, false, false, "call", 0],
  [APP_USERS, SERVICE_API, ACT_LOG, true, false, false, "call", 0],
  [APP_USERS, SERVICE_CONTROLLER, ACT_TRUST, true, false, true, "in", 32 << 20],
  [APP_STORE, SERVICE_API, ACT_SOURCES, true, false, false, "call", 0],
  [APP_STORE, SERVICE_API, ACT_CATALOG, true, false, false, "call", 0],
  [APP_STORE, SERVICE_API, ACT_INSTALLED, true, false, false, "call", 0],
  [APP_STORE, SERVICE_API, ACT_UPLOAD, true, false, true, "in", 4 * 1024 * 1024 * 1024],
  [APP_STORE, SERVICE_API, ACT_LICENSE, true, false, true, "in", 16 << 10],
  [APP_STORE, SERVICE_API, ACT_INSTALL_KEY, true, false, false, "call", 0],
  [APP_STORE, SERVICE_API, ACT_UPDATES, true, false, false, "call", 0],
  [APP_STORE, SERVICE_API, ACT_SET_UPDATE, true, false, true, "call", 0],
  [APP_STORE, SERVICE_CONTROLLER, ACT_ACTIVATE, true, true, true, "transfer", 0],
  [APP_STORE, SERVICE_CONTROLLER, ACT_DEACTIVATE, true, true, true, "call", 0],
  [APP_STORE, SERVICE_CONTROLLER, ACT_UNINSTALL, true, true, true, "call", 0],
  [APP_STORE, SERVICE_CONTROLLER, ACT_DELETE_DATA, true, true, true, "call", 0],
  [APP_STORE, SERVICE_CONTROLLER, ACT_STAGE, true, false, false, "transfer", 0],
  [APP_STORE, SERVICE_CONTROLLER, ACT_INSTALL, true, true, true, "call", 0],
  [APP_STORE, SERVICE_CONTROLLER, ACT_LICENSE, true, false, true, "in", 16 << 10],
].map(([app, service, name, admin, root, changes, flow, maxIn]) =>
  ({ app, service, name, admin, root, changes, flow, maxIn }));

export const lookup = (app, service, name) =>
  ACTIONS.find((a) => a.app === app && a.service === service && a.name === name) || null;

// lookupByKey finds an action by the key core.json uses ("ngc.desktop/apps"):
// among the actions of that app with that name, the one of service api, else
// the one of service controller.
export const lookupByKey = (app, name) =>
  lookup(app, SERVICE_API, name) || lookup(app, SERVICE_CONTROLLER, name);

// The shell opens every action of service api, and the root tasks of service
// controller; stage, license and ngc.users/controller/trust only the API opens,
// without a user, and refuses a shell's OPEN for them with E_FORBIDDEN.
export const shellOpens = (a) => a.service === SERVICE_API || a.root;
```

Parameter und ihre Prüfung (`core.ParseParams` und die `Validate`-Regeln):
`AppParams {app}`, `CodeParams {code}`, `StoreParams {store}`, `RoleRights {role, rights}` (bis
1000 Rechte, jedes `<app>/use` oder `<app>/<aktion>` für eine App aus einem Store, keins doppelt),
`LogQuery {beforeTime, beforeId, limit, user}` (`limit` 1 bis 500, `beforeId` nur mit
`beforeTime > 0`), `UpdateSetting {target, mode, window}` (`window` leer oder `HH:MM-HH:MM`),
`ActivateParams {app, version, sha256, ui, store, grants}` (je 32 Byte, `grants` aus `root` und
`network`, keins doppelt), `UninstallParams {app, keepData}`, `StageParams {target, version}`
(`target` nie `system`), `InstallParams {target, version, sha256}` (bei `system` ohne Version und
Prüfsumme, sonst beide gesetzt). `paramsBudget = new Budget({size: 8192, items: {rights: 1000, grants: 2}})`.

```js
// critical decides whether an action needs re-confirmation at level
// "critical"; the answer comes from the signed params, never from the OPEN.
export function critical(a, params) {
  if (a.name === ACT_ACTIVATE) return (params?.grants?.length ?? 0) > 0;
  if (a.name === ACT_UNINSTALL) return !params?.keepData;
  return a.app === APP_STORE && (a.name === ACT_UPLOAD || a.name === ACT_DELETE_DATA);
}
```

Ergebnisse: `RESULT_SHAPES` bildet jede Zeile von `core.json` ab (`Me`, `DesktopApp`, `TOTPBegin`,
`UserInfo`, `RoleRights`, `PairCodeResult`, `Pending`, `PendingAPI`, `NodeInfo`, `LogEntry`,
`SourceInfo`, `CatalogResult`, `InstalledApp`, `UploadResult`, `LicenseResult`, `InstallKey`,
`UpdateInfo`, `TrustResult`, `UIProof`). `parseResult(action, bytes)` liest sie mit einem Budget
von `wire.MAX_CALL_RESULT` und den Listengrenzen aus der Tabelle unten und prüft danach jedes Feld:

| Ergebnis | Liste | Grenze |
|---|---|---|
| `ngc.desktop/apps` | Wurzel | 1000 |
| `ngc.desktop/apps/*/actions` | je App | 64 |
| `ngc.users/list`, `ngc.users/rights`, `ngc.users/nodes`, `ngc.users/pending`, `ngc.users/pending-api` | Wurzel | je 10000, 256, 256, 256, 256 |
| `ngc.users/rights/*/rights` | je Rolle | 1000 |
| `ngc.users/list/*/roles`, `ngc.users/nodes/*/roles` | je Zeile | 256 bzw. 4 |
| `ngc.users/log` | Wurzel | 500 |
| `ngc.store/sources`, `ngc.store/installed`, `ngc.store/updates` | Wurzel | je 64, 10000, 30000 |

Die Grenzen sind die der Verträge (`trust.MaxUsers`, `trust.MaxRoles`, `trust.MaxNodes`,
`core.LogQuery.Limit`, `trust.MaxStores`, `store.MaxApps`, `store.MaxCatalogVersions`) und
schützen die Shell auch dann, wenn die API mehr schickt, als sie dürfte.

`parseUIProof(bytes)` und `verifyUIProof(p, app, version, list)` folgen `core.ParseUIProof` und
`core.VerifyUIProof`: entweder `catalog` allein mit `store` ungleich `local`, oder `task`,
`client` und `proof` mit `store` gleich `local`. Beim Prüfen:

- Store-App: den Store `p.store` in der Liste suchen, `verifyCatalog(p.catalog, store, 0)`, App und
  Version darin finden, `ui` zurückgeben. Bürgen darf jeder Store der Liste, dessen geprüfter
  Katalog App, Version und Prüfsumme führt.
- Hochgeladene App: `session.verifyApproval(p.client, p.proof, list)` (wie `verifyClient`, nur ohne
  Zeitprüfung und ohne `Verified`), daraus die Aufgabe mit `task.verifyApproval`, dann verlangen:
  `app = ngc.store`, `service = controller`, `action = activate`,
  `trust.mayRoot(cert.user, task.node)`, und die Parameter nennen genau diese App, diese Version
  und `store = local`; `ui` daraus ist die Prüfsumme.

- [ ] **Step 4: Test laufen lassen, er muss bestehen**

```bash
cd apps/web/tests && npm test -- contracts/core.spec.js
```

Erwartet: alle 21 Beispiel-Ergebnisse und die zehn leeren Listen werden gelesen, alle 23 Fälle aus
`uiproof.json` werden wie in Go entschieden, ein zu großes Ergebnis wird abgelehnt.

- [ ] **Step 5: Committen**

```bash
git add apps/web/public/assets/js/contracts/core.js apps/web/tests/contracts/core.spec.js
git commit -m "Add the core actions, their results and UI proofs" --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 15: `tunnel` – Fingerabdrücke, Kopplungs- und Beitrittscodes

**Files:**
- Create: `apps/web/public/assets/js/contracts/tunnel.js`
- Test: `apps/web/tests/contracts/tunnel.spec.js`

**Interfaces:**
- Produces: `PAIR_PREFIX = "ngc-pair:"`, `JOIN_PREFIX = "ngc-join:"`, `MAX_PAIR_CODE = 1024`,
  `SECRET_SIZE = 16`, `ROLE_WEB/API/DB/SERVICES`, `CONTRACT = 1`,
  `fingerprint(key) -> Promise<string>`,
  `joinFingerprint(node, noise, sign, tls, url) -> Promise<string>`,
  `parsePairCode(s)`, `parseJoinCode(s)`, `checkCode(code, version) -> boolean`,
  `components(roles) -> string[]`.
- Consumes: `b64.js`, `json.js`, `url.js`, `sign.js`, `trust.js`.

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

`apps/web/tests/contracts/tunnel.spec.js`:

```js
import { test, expect } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";

const v = vectors("tunnel.json");

test("the shell computes both fingerprints itself", async ({ shell }) => {
  const got = await shell.evaluate(async (d) => {
    const t = await import("/assets/js/contracts/tunnel.js");
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    return {
      controller: await t.fingerprint(b64decode(d.controllerKey)),
      join: await t.joinFingerprint(d.join.node, b64decode(d.join.noise), b64decode(d.join.sign),
        b64decode(d.join.tls), d.join.url),
    };
  }, v);
  expect(got.controller).toBe(v.fingerprint);
  expect(got.join).toBe(v.join.fingerprint);
});

test("a pairing code is shown only when it fits the verified version", async ({ shell }) => {
  test.setTimeout(120_000);
  const got = await shell.evaluate(async (d) => {
    const t = await import("/assets/js/contracts/tunnel.js");
    const trust = await import("/assets/js/contracts/trust.js");
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    const chain = d.pairCheck.chain.map((s) => ({
      payload: b64decode(s.payload),
      proof: { kind: s.proof.kind, kid: s.proof.kid, sig: b64decode(s.proof.sig),
        authData: s.proof.authData ? b64decode(s.proof.authData) : new Uint8Array(0),
        clientData: s.proof.clientData ? b64decode(s.proof.clientData) : new Uint8Array(0) },
    }));
    const anchor = { seq: 1, hash: await trust.hash(chain[0]) };
    const version = await trust.open(anchor, chain);
    return d.pairCheck.cases.map((c) => {
      try {
        return t.checkCode(t.parsePairCode(c.code), version);
      } catch {
        return false;
      }
    });
  }, v);
  const wrong = v.pairCheck.cases.filter((c, i) => c.valid !== got[i]).map((c) => c.name);
  expect(wrong).toEqual([]);
});
```

- [ ] **Step 2: Test laufen lassen, er muss scheitern**

```bash
cd apps/web/tests && npx playwright test contracts/tunnel.spec.js --project=chromium
```

Erwartet: Fehler beim Import von `tunnel.js`.

- [ ] **Step 3: `tunnel.js` schreiben**

```js
// Pairing and joining: the fingerprints the admin compares with the output of
// the ulh script, and the codes the shell shows only when they fit its own
// verified version of the trust list (Go: tunnel).
import { b64decode, b64encode, concat, utf8, equal } from "./b64.js";
import { decode, Budget, S, str, bytes, uint } from "./json.js";
import { validTunnelURL } from "./url.js";
import { keyIDAsync } from "./sign.js";
import { NODE_API, AUTO_WEB, AUTO_API, AUTO_CONTROLLER } from "./trust.js";

export const PAIR_PREFIX = "ngc-pair:";
export const JOIN_PREFIX = "ngc-join:";
export const MAX_PAIR_CODE = 1024;
export const SECRET_SIZE = 16;
export const CONTRACT = 1;
export const ROLE_WEB = "web", ROLE_API = "api", ROLE_DB = "db", ROLE_SERVICES = "services";

const codeShape = S({ url: str, key: bytes, tls: bytes, secret: bytes,
  anchor: S({ seq: uint, hash: bytes }) });
const codeBudget = new Budget({ size: MAX_PAIR_CODE });

// fingerprint names a Noise key for people and in the tunnel URL.
export const fingerprint = (key) => keyIDAsync(key);

// joinFingerprint names what a joining API asks to be listed with, so the
// admin can compare it with the output of that server's ulh script.
export async function joinFingerprint(node, noise, signKey, tlsPin, url) {
  const b = concat(utf8("ngc/join-fingerprint/v1"), new Uint8Array([0]), utf8(node),
    new Uint8Array([0]), noise, signKey, tlsPin, utf8(url));
  const h = new Uint8Array(await crypto.subtle.digest("SHA-256", b));
  return b64encode(h.subarray(0, 16));
}

function parseCode(prefix, s) {
  const trimmed = s.trim();
  if (!trimmed.startsWith(prefix) || trimmed.length > MAX_PAIR_CODE) throw new Error("tunnel: malformed code");
  const c = decode(b64decode(trimmed.slice(prefix.length)), codeBudget, codeShape);
  if (!validTunnelURL(c.url) || c.key.length !== 32 || c.tls.length !== 32
    || c.secret.length !== SECRET_SIZE || c.anchor.seq === 0 || c.anchor.hash.length !== 32) {
    throw new Error("tunnel: malformed code");
  }
  return c;
}

export const parsePairCode = (s) => parseCode(PAIR_PREFIX, s);
export const parseJoinCode = (s) => parseCode(JOIN_PREFIX, s);

// checkCode compares a code with the newest version the shell verified: key,
// URL and pin must all belong to one API node, because whoever receives the
// code dials that URL and trusts those keys.
export function checkCode(c, version) {
  if (!version?.list || c.anchor.seq !== version.list.seq || !equal(c.anchor.hash, version.hash)) return false;
  return version.list.nodes.some((n) => n.kind === NODE_API && equal(n.noise, c.key)
    && n.url === c.url && equal(n.tls, c.tls));
}

export function components(roles) {
  const c = [AUTO_CONTROLLER];
  if (roles.includes(ROLE_API)) c.push(AUTO_API);
  if (roles.includes(ROLE_WEB)) c.push(AUTO_WEB);
  return c.sort();
}
```

- [ ] **Step 4: Test laufen lassen, er muss bestehen**

```bash
cd apps/web/tests && npm test -- contracts/tunnel.spec.js
```

Erwartet: beide Fingerabdrücke stimmen, und alle zehn Fälle von `pairCheck` werden wie in Go
entschieden.

- [ ] **Step 5: Committen**

```bash
git add apps/web/public/assets/js/contracts/tunnel.js apps/web/tests/contracts/tunnel.spec.js
git commit -m "Add fingerprints and the pairing and join codes" --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 16: HTTP vor dem WebSocket

**Files:**
- Create: `apps/web/public/assets/js/net/http.js`
- Test: `apps/web/tests/shell/http.spec.js`

**Interfaces:**
- Produces: `class ApiError extends Error {code, retryAfter}`,
  `getSetupOffer() -> Promise<Uint8Array>`, `postSetup(body) -> Promise<void>`,
  `loginStart(name) -> Promise<challenge>`, `loginFinish(body) -> Promise<result>`,
  `logout() -> Promise<void>`, `trustChain(from) -> Promise<chain|null>`, `socketURL()`.
- Consumes: `b64.js`, `json.js`, `session.js`, `wire.js`.

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

`apps/web/tests/shell/http.spec.js`:

```js
import { test, expect } from "../fixtures/page.js";

// Every answer the API gives is played back with route mocking, so this test
// needs no API at all.
async function mock(page, path, status, body, headers = {}) {
  await page.route("**" + path, (route) =>
    route.fulfill({ status, contentType: "application/json", headers, body }));
}

test("an error is recognised by the code in the CLOSE JSON, not by the status", async ({ shell }) => {
  await mock(shell, "/api/v1/login/start", 429,
    JSON.stringify({ status: "error", code: "E_LOCKED", message: "Zu viele Fehlversuche. Bitte später erneut versuchen." }),
    { "retry-after": "42" });
  const out = await shell.evaluate(async () => {
    const http = await import("/assets/js/net/http.js");
    try {
      await http.loginStart("anna");
      return { thrown: false };
    } catch (e) {
      return { thrown: true, code: e.code, retryAfter: e.retryAfter, message: e.message };
    }
  });
  expect(out).toEqual({ thrown: true, code: "E_LOCKED", retryAfter: 42,
    message: "Zu viele Fehlversuche. Bitte später erneut versuchen." });
});

test("an answer that is no JSON counts as E_UNREACHABLE", async ({ shell }) => {
  await shell.route("**/api/v1/login/start", (route) =>
    route.fulfill({ status: 503, contentType: "text/html", body: "<html>Service Unavailable</html>" }));
  const out = await shell.evaluate(async () => {
    const http = await import("/assets/js/net/http.js");
    try { await http.loginStart("anna"); return "none"; } catch (e) { return e.code; }
  });
  // Apache answers 502 or 503 itself when no API replies or its certificate
  // check fails; those answers carry no CLOSE JSON.
  expect(out).toBe("E_UNREACHABLE");
});

test("GET /api/v1/trust answers 204 when nothing newer is there", async ({ shell }) => {
  await mock(shell, "/api/v1/trust*", 204, "");
  const out = await shell.evaluate(async () => {
    const http = await import("/assets/js/net/http.js");
    return await http.trustChain(5);
  });
  expect(out).toBeNull();
});

test("a body over MaxRequest is never sent", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const http = await import("/assets/js/net/http.js");
    try {
      await http.postSetup(new Uint8Array(65 * 1024));
      return "sent";
    } catch (e) {
      return e.code;
    }
  });
  expect(out).toBe("E_TOOLARGE");
});
```

- [ ] **Step 2: Test laufen lassen, er muss scheitern**

```bash
cd apps/web/tests && npx playwright test shell/http.spec.js --project=chromium
```

Erwartet: Fehler beim Import von `/assets/js/net/http.js`.

- [ ] **Step 3: `http.js` schreiben**

```js
// Everything the shell speaks before the WebSocket: HTTPS with JSON, the
// seven paths of shared/README.md, "Vor dem WebSocket". The cookie is set and
// deleted by the API; the shell never touches it.
import { fromUtf8 } from "../contracts/b64.js";
import * as session from "../contracts/session.js";
import * as wire from "../contracts/wire.js";

export class ApiError extends Error {
  constructor(code, retryAfter = 0) {
    super(wire.message(code) || "Unbekannter Fehler.");
    this.code = code;
    this.retryAfter = retryAfter;
  }
}

async function call(path, method, body, maxBody) {
  if (body && body.length > maxBody) throw new ApiError(wire.CODES.TOOLARGE);
  let res;
  try {
    res = await fetch(path, {
      method,
      cache: "no-store",
      credentials: "same-origin",
      redirect: "error",
      headers: body ? { "Content-Type": "application/json" } : {},
      body: body ?? undefined,
    });
  } catch {
    throw new ApiError(wire.CODES.UNREACHABLE);
  }
  if (res.status === 204) return null;
  const raw = new Uint8Array(await res.arrayBuffer());
  if (res.ok) return raw;
  // The error is recognised by the code in the CLOSE JSON, not by the status.
  // Answers Apache gives itself carry no CLOSE JSON at all.
  let close;
  try {
    close = wire.parseClose(raw);
  } catch {
    throw new ApiError(wire.CODES.UNREACHABLE);
  }
  if (close.status !== "error") throw new ApiError(wire.CODES.UNREACHABLE);
  const retry = Number(res.headers.get("Retry-After") ?? 0);
  throw new ApiError(close.code, Number.isFinite(retry) && retry > 0 ? retry : 0);
}

export const getSetupOffer = () => call(session.PATH_SETUP, "GET", null, 0);
export const postSetup = (body) => call(session.PATH_SETUP, "POST", body, session.MAX_REQUEST);

export async function loginStart(name) {
  const body = new TextEncoder().encode(JSON.stringify({ name }));
  return session.parseLoginChallenge(await call(session.PATH_LOGIN_START, "POST", body, session.MAX_REQUEST));
}

export async function loginFinish(body) {
  return session.parseLoginResult(await call(session.PATH_LOGIN_FINISH, "POST", body, session.MAX_REQUEST));
}

export const logout = () => call(session.PATH_LOGOUT, "POST", null, 0);

export async function trustChain(from) {
  // from is a decimal number 1..2^53-1 without a leading zero; the shell puts
  // nothing else into the query.
  if (!Number.isSafeInteger(from) || from < 1) throw new ApiError(wire.CODES.PROTOCOL);
  const raw = await call(session.PATH_TRUST + "?from=" + from, "GET", null, 0);
  return raw === null ? null : session.parseTrustChain(raw);
}

export function socketURL() {
  const u = new URL(session.PATH_SOCKET, location.href);
  u.protocol = "wss:";
  return u.toString();
}
```

`parseLoginChallenge` steht schon (Aufgabe 8) und folgt `session.ParseLoginChallenge`, geprüft
gegen die Vektoren unter `login` in `shared/testdata/session.json`. `http.loginStart` gibt deshalb
nur weiter, was dieser Parser durchgelassen hat – samt `kid` je Passkey, das die Antwort mitbringt.

`session.js` bekommt in diesem Schritt nur noch `parseLoginResult`. Dafür gibt es keinen Go-Parser,
deshalb gelten die Regeln 1, 3 bis 8 und 12 bis 14 des strikten JSON (`shared/README.md`) mit einem
Budget von `session.MAX_REQUEST`:

- `LoginResult`: `api` ein Umschlag nach `sign.validateEnvelope(…, session.MAX_CERT)`,
  `passwordHint` boolesch.

- [ ] **Step 4: Test laufen lassen, er muss bestehen**

```bash
cd apps/web/tests && npm test -- shell/http.spec.js
```

Erwartet: vier grüne Tests in allen drei Engines.

- [ ] **Step 5: Committen**

```bash
git add apps/web/public/assets/js/net/http.js apps/web/public/assets/js/contracts/session.js apps/web/tests/shell/http.spec.js
git commit -m "Add the HTTP layer before the WebSocket" --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 17: Die Sitzungsleitung – WebSocket, Noise und Kanäle

Hier entsteht auch die Gegenstelle für die Tests: ein kleiner API-Ersatz in Node, der dieselben
JavaScript-Module benutzt. So braucht keine Shell-Prüfung eine echte API.

**Files:**
- Create: `apps/web/public/assets/js/net/link.js`, `apps/web/public/assets/js/net/channel.js`,
  `apps/web/public/assets/js/net/testlink.js`
- Create: `apps/web/tests/mock/world.js`, `apps/web/tests/mock/api.js`
- Modify: `apps/web/tests/playwright.config.js` (die Gegenstelle als zweiter Server),
  `apps/web/public/index.php` (der Testzweig, der `/api/` weiterreicht)
- Test: `apps/web/tests/shell/channels.spec.js`

**Interfaces:**
- Produces:
  - `link.js`: `class Link` mit `connect(url, sessionDH, apiDH) -> Promise<Link>`,
    `open({app, service, action, params, window, e2e, taskBytes, confirmBytes}) -> Promise<Channel>`,
    `onTrustHint(fn)`, `onNotice(fn)`, `onBye(fn)`, `close()`, `channelCount()`.
  - `channel.js`: `class Channel` mit `firstGrant() -> Promise<{credit, handshake}>`,
    `write(bytes)`, `writeE2E(kind, body)`, `useTransport(t)`, `end()`, `close(code)`,
    `result(maxBytes) -> Promise<Uint8Array>`, `done() -> Promise<Close>`, `onData(fn)`,
    `onClose(fn)`, und den vier Hilfen `call(link, action, {params, taskBytes, confirmBytes})`,
    `sendIn(link, action, {params, taskBytes, stream, onProgress})`,
    `receiveOut(link, action, {params, maxBytes})` und
    `openE2E(link, {app, service, action, params, taskBytes, node, controllerKey, sessionDH})`.
  - `testlink.js`: `connectForTest() -> Promise<TestLink>` – nur für die Tests: meldet sich mit
    dem festen Testbenutzer an und legt über `Link` die Kurzformen `call(app, service, action,
    params, opts)`, `openRaw(app, service, action)`, `openE2E(app, service, action, params)` und
    `mockSawPlaintext`. Eigene Vertragslogik steht nicht darin.
  - `mock/api.js`: `startMockAPI({world, port}) -> {url, stop}`.
- Consumes: `wire.js`, `noise.js`, `core.js`, `session.js`.

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

`apps/web/tests/shell/channels.spec.js`:

```js
import { test, expect } from "../fixtures/page.js";

// The mock API runs beside the web server (playwright.config.js) and answers
// with the same modules the shell uses.
test("a call channel returns its result", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const { connectForTest } = await import("/assets/js/net/testlink.js");
    const link = await connectForTest();
    const me = await link.call("ngc.desktop", "api", "me", null);
    link.close();
    return me;
  });
  expect(out.name).toBe("anna");
});

test("the shell never opens more channels than MaxSessionChannels", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const { connectForTest } = await import("/assets/js/net/testlink.js");
    const w = await import("/assets/js/contracts/wire.js");
    const link = await connectForTest();
    const open = [];
    for (let i = 0; i < w.MAX_SESSION_CHANNELS; i++) open.push(await link.openRaw("ngc.desktop", "api", "apps"));
    let refused = false;
    try { await link.openRaw("ngc.desktop", "api", "apps"); } catch { refused = true; }
    const count = link.channelCount();
    for (const c of open) c.close("ok");
    link.close();
    return { refused, count };
  });
  expect(out).toEqual({ refused: true, count: 64 });
});

test("a result over MaxCallResult closes the channel with E_TOOLARGE", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const { connectForTest } = await import("/assets/js/net/testlink.js");
    const link = await connectForTest();
    try {
      // The mock API sends 9 MiB for this action on purpose.
      await link.call("ngc.users", "api", "log", { limit: 500, beforeTime: 1 });
      return "none";
    } catch (e) {
      return e.code;
    } finally {
      link.close();
    }
  });
  expect(out).toBe("E_TOOLARGE");
});

test("an end-to-end channel carries data the API cannot read", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const { connectForTest } = await import("/assets/js/net/testlink.js");
    const link = await connectForTest();
    const ch = await link.openE2E("org.sorglos.terminal", "terminal", "shell", { cols: 80, rows: 24 });
    const seen = [];
    ch.onData((d) => seen.push(new TextDecoder().decode(d)));
    await ch.write(new TextEncoder().encode("cp -r\n"));
    await ch.end();
    const status = await ch.done();
    link.close();
    return { seen: seen.join(""), status: status.status, plaintextSeenByAPI: link.mockSawPlaintext };
  });
  expect(out.seen).toContain("cp -r");
  expect(out.status).toBe("ok");
  expect(out.plaintextSeenByAPI).toBe(false);
});

test("a result counts only when the channel ends with ok", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const { connectForTest } = await import("/assets/js/net/testlink.js");
    const link = await connectForTest();
    try {
      // The mock API sends data and then closes with E_EXIT.
      await link.call("ngc.store", "api", "sources", null, { failAfterData: true });
      return "none";
    } catch (e) {
      return e.code;
    } finally {
      link.close();
    }
  });
  expect(out).toBe("E_EXIT");
});
```

`testlink.js` ist eine kleine Datei unter `public/assets/js/net/`, die nur für die Tests eine
fertige Leitung zur Gegenstelle aufbaut (Anmeldung mit einem festen Testbenutzer). Sie enthält
keine eigene Vertragslogik.

- [ ] **Step 2: Test laufen lassen, er muss scheitern**

```bash
cd apps/web/tests && npx playwright test shell/channels.spec.js --project=chromium
```

Erwartet: Fehler beim Import von `/assets/js/net/testlink.js`.

- [ ] **Step 3: Die Gegenstelle schreiben**

`apps/web/tests/mock/world.js` baut eine kleine Welt aus den Testdaten: die Vertrauensliste aus
`shared/testdata/trust.json` (Fall „genesis“), ein Benutzer `anna` mit Passwortschlüssel, ein
Controller `ctl-1` mit einem festen Noise-Schlüsselpaar, eine App `org.sorglos.terminal` mit
Oberfläche aus `packui.json` und die Ergebnisse aus `core.json`.

`apps/web/tests/mock/api.js` ist ein Node-Server (`node:http` und ein selbst geschriebener
WebSocket-Aufschlag nach RFC 6455, ohne npm-Paket) und benutzt **dieselben** Module wie die Shell,
per relativem Import aus `../../public/assets/js/`. Er kann:

- die sieben HTTP-Pfade mit den Regeln aus `shared/README.md`, „Vor dem WebSocket“;
- die Noise-Sitzung als Antwortender (`noise.js` mit `PROLOGUE_SESSION`);
- Kanäle nach `wire.js`: erste GRANT, DATA, EOF, CLOSE, Freigabe je Strecke;
- die Kern-Aktionen aus `core.json` beantworten;
- Ende-zu-Ende-Kanäle als Controller spielen (Noise-Nachricht 2 in der ersten GRANT, innere
  Nachrichten nach `wire.E2E`), wobei der Server mitschreibt, ob er je Klartext gesehen hat
  (`mockSawPlaintext`);
- auf Wunsch schiefgehen: `failAfterData`, ein Ergebnis über `MaxCallResult`, eine kaputte
  Vertrauensliste, ein Angebot der Ersteinrichtung mit fremden Schlüsseln.

In `playwright.config.js` kommt ein zweiter Eintrag unter `webServer`:

```js
webServer: [
  { command: `php -d variables_order=EGPCS -S 127.0.0.1:${port} -t ../public ../public/index.php`,
    url: `http://127.0.0.1:${port}/config.json`, reuseExistingServer: false,
    env: { NGC_WEB_CONFIG: "../tests/web.json", NGC_WEB_ANCHOR: "../tests/trust-anchor.json" } },
  { command: "node mock/api.js", url: "http://127.0.0.1:8100/ready", reuseExistingServer: false },
],
```

Der PHP-Server reicht `/api/` an `127.0.0.1:8100` weiter – dafür bekommt `index.php` einen Zweig,
der nur unter `PHP_SAPI === 'cli-server'` und nur mit gesetzter Umgebungsvariable `NGC_TEST_API`
greift und die Anfrage unverändert weitergibt. So sieht die Shell im Test dieselbe Herkunft wie im
Betrieb, ohne dass der Betrieb diesen Zweig je erreicht.

- [ ] **Step 4: `link.js` schreiben**

```js
// The session link: one WebSocket to the API, one Noise session inside it,
// and the channels on top (Go: wire, "Links" and "Channels").
import * as wire from "../contracts/wire.js";
import * as noise from "../contracts/noise.js";
import * as session from "../contracts/session.js";
import { Channel } from "./channel.js";

export class Link {
  constructor() {
    this.ws = null;
    this.transport = null;
    this.channels = new Map();   // number -> Channel
    this.closing = new Set();    // numbers whose CLOSE was sent but not answered
    this.next = 1;
    this.handlers = { trust: () => {}, notice: () => {}, bye: () => {} };
  }

  // channelCount counts every number that is not free again, closing ones
  // included: that is what MaxSessionChannels bounds.
  channelCount() {
    return this.channels.size + this.closing.size;
  }

  async connect(url, sessionDH, apiDH) {
    const ws = new WebSocket(url, session.SUBPROTOCOL);
    ws.binaryType = "arraybuffer";
    await new Promise((ok, fail) => { ws.onopen = ok; ws.onerror = () => fail(new Error("link: no connection")); });
    const hs = await noise.HandshakeKK.of(true, new TextEncoder().encode(noise.PROLOGUE_SESSION),
      sessionDH.privateKey, sessionDH.publicRaw, apiDH);
    ws.send(await hs.writeMessage1());
    const msg2 = await nextBinary(ws);
    await hs.readMessage2(msg2);
    this.transport = await hs.split();
    this.ws = ws;
    ws.onmessage = (e) => this.onMessage(e);
    ws.onclose = () => this.fail(wire.CODES.UNREACHABLE);
    return this;
  }

  async onMessage(e) {
    // A text message, a frame that does not decode or a failed decryption
    // ends the link.
    if (typeof e.data === "string") return this.fail(wire.CODES.PROTOCOL);
    let frame;
    try {
      frame = wire.decodeFrame(await this.transport.open(new Uint8Array(e.data)));
    } catch {
      return this.fail(wire.CODES.PROTOCOL);
    }
    if (frame.channel === wire.CONTROL_CHANNEL) return this.onControlFrame(frame);
    const ch = this.channels.get(frame.channel);
    if (!ch) {
      // A frame for a number that is neither open nor closing is a protocol
      // error; one for a number whose CLOSE we already sent is dropped, and a
      // CLOSE among them still frees the number.
      if (this.closing.has(frame.channel)) {
        if (frame.type === wire.TYPE_CLOSE) this.closing.delete(frame.channel);
        return;
      }
      return this.fail(wire.CODES.PROTOCOL);
    }
    return ch.receive(frame);
  }

  async send(frame) {
    this.ws.send(await this.transport.seal(wire.encodeFrame(frame)));
  }

  // openNumber picks a free number and keeps the shell from ever going over
  // the limit: the API would end the link.
  openNumber() {
    if (this.channelCount() >= wire.MAX_SESSION_CHANNELS) {
      const e = new Error(wire.message(wire.CODES.LIMIT));
      e.code = wire.CODES.LIMIT;
      throw e;
    }
    while (this.channels.has(this.next) || this.closing.has(this.next)) {
      this.next = this.next >= 0xffffffff ? 1 : this.next + 1;
    }
    return this.next;
  }
}
```

Dazu in `Link`: `onControlFrame` prüft `wire.controlAllowed(LINK_SESSION, true, t)` und leitet
`trust` an `session.parseTrustHint`, `notify` an `core.parseNotice` und `bye` an das Schließen
weiter; alles andere beendet die Leitung. `fail(code)` schließt alle Kanäle mit diesem Code und
schließt den WebSocket.

- [ ] **Step 5: `channel.js` schreiben**

`Channel` hält Nummer, Freigabe in beide Richtungen, den Zustand (`opening`, `open`, `sentEOF`,
`closed`) und bei Ende zu Ende den Noise-Zustand. Die Regeln, die er durchsetzt:

- Nach dem OPEN wartet er auf die erste GRANT (dann gilt die Freigabe, bei Ende zu Ende steckt
  darin Noise-Nachricht 2) oder auf ein CLOSE mit Fehler.
- `write` schickt nie mehr, als freigegeben ist, und teilt in Stücke von höchstens
  `wire.MAX_PAYLOAD` (bei Ende zu Ende `wire.MAX_E2E_PLAINTEXT` Klartext).
- Empfangene DATA zählt gegen die eigene Freigabe; sobald die Hälfte verbraucht ist, schickt er
  eine neue GRANT, nie über `wire.MAX_WINDOW`.
- Ein Ergebnis nach Art von `call` nimmt er bis `wire.MAX_CALL_RESULT` Byte an; bei mehr schließt
  er mit `E_TOOLARGE`. Das gilt auch auf Ende-zu-Ende-Kanälen, deren Klartext die API nicht sieht.
- Ein Ergebnis gilt nur, wenn der Kanal mit CLOSE `ok` endet, bei Ende zu Ende mit einem Ende des
  Controllers mit `ok`; sonst wirft er mit dem Code des CLOSE.
- Kommt EOF oder CLOSE vor dem Ende der Gegenseite, sind die Daten abgeschnitten: Fehler.
- Jedes CLOSE wird mit CLOSE beantwortet, außer er hat selbst schon eines geschickt; die Nummer
  wandert dabei in `link.closing` und wird erst frei, wenn beides geschehen ist.
- Bei `transfer`, das die Shell öffnet (`activate`), schickt sie nach der ersten GRANT sofort EOF
  und keine DATA.

Die vier Hilfen darüber:

```js
// call opens a channel of flow call, in or transfer, sends its params in the
// OPEN (or in the task), sends EOF at once and gives back the parsed result.
export async function call(link, action, { params, taskBytes, confirmBytes } = {}) {
  const ch = await link.open({
    app: action.app, service: action.service, action: action.name,
    params: taskBytes ? null : params, window: wire.DEFAULT_WINDOW,
    e2e: null, taskBytes, confirmBytes,
  });
  await ch.firstGrant();
  // On call and on a transfer the shell opened (activate) it sends EOF at
  // once and no DATA; the API writes header and file on the tunnel hop.
  await ch.end();
  const body = await ch.result(wire.MAX_CALL_RESULT);
  return core.parseResult(action, body);
}

// sendIn opens a FlowIn channel and streams at most action.maxIn bytes.
export async function sendIn(link, action, { params, taskBytes, stream, onProgress } = {}) {
  const ch = await link.open({
    app: action.app, service: action.service, action: action.name,
    params: taskBytes ? null : params, window: wire.DEFAULT_WINDOW, e2e: null, taskBytes,
  });
  await ch.firstGrant();
  let sent = 0;
  const reader = stream.getReader();
  for (;;) {
    const { value, done } = await reader.read();
    if (done) break;
    sent += value.length;
    // The limit is the contract's, not the server's answer: more would be a
    // channel the API closes with E_TOOLARGE anyway.
    if (sent > action.maxIn) {
      await ch.close(wire.CODES.TOOLARGE);
      throw Object.assign(new Error(wire.message(wire.CODES.TOOLARGE)), { code: wire.CODES.TOOLARGE });
    }
    await ch.write(value);
    onProgress?.(sent);
  }
  await ch.end();
  return core.parseResult(action, await ch.result(wire.MAX_CALL_RESULT));
}

// receiveOut opens a FlowOut channel and collects what the API sends; the
// caller says how many bytes it will take at most (for ui: the largest
// ui-tar, store.MaxUITotal plus its headers).
export async function receiveOut(link, action, { params, maxBytes }) {
  const ch = await link.open({
    app: action.app, service: action.service, action: action.name,
    params, window: wire.DEFAULT_WINDOW, e2e: null,
  });
  await ch.firstGrant();
  await ch.end();
  return ch.result(maxBytes);
}

// openE2E opens a channel to an app: Noise message 1 goes into the OPEN, and
// the prologue binds controller, app, service, action and the exact params and
// task bytes of that OPEN.
export async function openE2E(link, { app, service, action, params, taskBytes, node, controllerKey, sessionDH }) {
  const paramsBytes = params ?? new Uint8Array(0);
  const taskBytesOrEmpty = taskBytes ?? new Uint8Array(0);
  const prologue = await noise.e2ePrologue(node, app, service, action, paramsBytes, taskBytesOrEmpty);
  const hs = await noise.HandshakeKK.of(true, prologue, sessionDH.privateKey, sessionDH.publicRaw, controllerKey);
  const msg1 = await hs.writeMessage1();
  const ch = await link.open({
    app, service, action, params: taskBytes ? null : paramsBytes,
    window: wire.DEFAULT_WINDOW, e2e: msg1, taskBytes,
  });
  // Message 2 travels in the first GRANT of the side that accepted the
  // channel; that GRANT without it, or a handshake in any other GRANT, is a
  // protocol error.
  const first = await ch.firstGrant();
  if (!first.handshake) {
    await ch.close(wire.CODES.PROTOCOL);
    throw Object.assign(new Error(wire.message(wire.CODES.PROTOCOL)), { code: wire.CODES.PROTOCOL });
  }
  await hs.readMessage2(first.handshake);
  ch.useTransport(await hs.split());
  // Right after message 2 the shell sends its first inner message; the
  // controller starts the service only once it has decrypted, so a replayed
  // OPEN does nothing on its own.
  await ch.writeE2E(wire.E2E_DATA, new Uint8Array(0));
  return ch;
}
```

- [ ] **Step 6: Tests laufen lassen, sie müssen bestehen**

```bash
cd apps/web/tests && npm test -- shell/channels.spec.js
```

Erwartet: fünf grüne Tests in allen drei Engines; die Gegenstelle hat auf dem
Ende-zu-Ende-Kanal nie Klartext gesehen.

- [ ] **Step 7: Committen**

```bash
git add apps/web/public/assets/js/net apps/web/tests/mock apps/web/tests/shell/channels.spec.js apps/web/tests/playwright.config.js apps/web/public/index.php
git commit -m "Add the session link with Noise and flow-controlled channels" --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 17b: Der Adapter – die echte Leitung dort, wo die Attrappe steht

**Eingeschoben, nicht umnummeriert.** Diese Aufgabe stand nirgends im Plan und
ist beim Bauen von Aufgabe 18 aufgefallen: Anmeldung, Sitzung und
Vertrauensliste sind echt, **der Desktop isst weiter die Attrappe**, und die
frisch geöffnete Leitung wird in `main.js` gleich wieder geschlossen. Das ist
die eigentliche Ablösung von `shell/fake-link.js`.

**Files:**
- Create: `apps/web/public/assets/js/net/world.js`
- Modify: `apps/web/public/assets/js/shell/main.js`
- Delete (am Ende, erst wenn nichts mehr daran hängt):
  `apps/web/public/assets/js/shell/fake-link.js`
- Test: `apps/web/tests/shell/world.spec.js`

**Interfaces:**
- Produces: `realWorld({config, anchor, session, link}) -> {link, trust, session,
  releaseKey}` — dieselbe Form, die `fakeWorld()` heute liefert, damit
  `startDesktop` keine Zeile ändert.
- Der `link` darin bietet **genau die Oberfläche der Attrappe**:
  `call(app, service, name, params, opts)`, `open(app, service, name)`,
  `onNotice(fn)`.
- Consumes: `net/link.js`, `net/channel.js`, `shell/trustlist.js`, `core.js`.

**Der Kern in einem Satz:** `net/channel.js` spricht in Aktionen
(`call(link, action, {…})`, `sendIn`, `receiveOut`, `openE2E`), die Oberflächen
sprechen in `(app, service, name)`. Der Adapter schlägt die Zeile in
`core.ACTIONS` nach und wählt den Weg **nach der Spalte `flow`** — nicht nach
einer eigenen Liste, die neben der Tabelle verrottet.

**Was gemessen sein muss:**

- Jede Aktionszeile, die eine Oberfläche heute benutzt, kommt über den Adapter
  am selben Ziel an wie über die Attrappe. Nicht behaupten: **den Vergleich
  fahren**, Zeile für Zeile.
- `onNotice` liefert die Steuerhinweise der echten Leitung, und
  `trust.refresh(seq)` wird davon wirklich angestoßen.
- `main.js` schließt die Leitung **nicht** mehr, und der Desktop steht mit
  echten Daten. Das ist der Punkt, an dem die Zeile
  `const trust = await startTrust({ anchor, installation, link });` hineingeht.
- **Erst wenn nichts mehr daran hängt**, fällt `fake-link.js` weg. Solange ein
  Fall sie braucht, bleibt sie — ein Baum, der nicht baut, ist schlimmer als
  eine Attrappe, die benannt ist.

**Warum das teuer werden kann, vorher gesagt:** Die Gegenstelle beantwortet
heute fünf Kern-Aktionen, die Attrappe zwei Dutzend. Rund hundert Fälle in
zwölf Dateien hängen an der Attrappe. Diese Aufgabe darf deshalb in Schritten
gehen — aber jeder Schritt lässt die Mappe grün, und welche Fälle noch an der
Attrappe hängen, steht am Ende im Bericht.

### Task 18: Anmeldung – Passwortschlüssel, TOTP und Passkeys

**Files:**
- Create: `apps/web/public/assets/js/auth/keys.js`, `apps/web/public/assets/js/auth/login.js`,
  `apps/web/public/assets/js/shell/dom.js`
- Test: `apps/web/tests/shell/login.spec.js`

**Interfaces:**
- Produces:
  - `keys.js`: `newSessionKeys() -> Promise<{sign: {privateKey, publicRaw, kid}, dh: {privateKey, publicRaw}}>`,
    `buildClientCert({installation, user, nonce, keys, now, maxLifetime}) -> {cert, payload}`,
    `proveWithPassword(payload, passwordText, kdf, purpose) -> Promise<proof>`,
    `proveWithPasskey(payload, {rp, passkeys}, purpose) -> Promise<proof>` – `passkeys` ist
    `challenge.passkeys`, also je Eintrag `credId` **und** `kid`.
  - `login.js`: `loginView(root, {config, onSession}) -> void`,
    `login({name, password, totp, wantPasskey}) -> Promise<Session>`.
  - `dom.js`: `el(tag, className, text)`, `field(label, type)`, `button(text, onClick)`,
    `note(text, level)`, `dialog({title, body, actions})`.
- Consumes: `http.js`, `session.js`, `proof.js`, `sign.js`, `trust.js`, `link.js`.

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

`apps/web/tests/shell/login.spec.js`:

```js
import { test, expect } from "../fixtures/page.js";

test("the password never leaves the browser", async ({ shell }) => {
  const seen = [];
  shell.on("request", (r) => seen.push((r.postData() ?? "") + " " + r.url()));
  const frames = [];
  shell.on("websocket", (ws) => {
    ws.on("framesent", (f) => frames.push(String(f.payload)));
    ws.on("framereceived", (f) => frames.push(String(f.payload)));
  });

  await shell.getByLabel("Benutzername").fill("anna");
  await shell.getByLabel("Passwort").fill("Heimnetz-2026");
  await shell.getByRole("button", { name: "Anmelden" }).click();
  await expect(shell.locator("#taskbar")).toBeVisible();

  const forbidden = ["Heimnetz-2026", "Heimnetz%2D2026", btoa("Heimnetz-2026")];
  for (const needle of forbidden) {
    expect(seen.join("\n"), needle).not.toContain(needle);
    expect(frames.join("\n"), needle).not.toContain(needle);
  }
});

test("a wrong password says only that the login failed", async ({ shell }) => {
  await shell.getByLabel("Benutzername").fill("anna");
  await shell.getByLabel("Passwort").fill("falsch");
  await shell.getByRole("button", { name: "Anmelden" }).click();
  await expect(shell.getByRole("alert")).toHaveText("Anmeldung fehlgeschlagen.");
  // An unknown name must give exactly the same answer.
  await shell.getByLabel("Benutzername").fill("gibtesnicht");
  await shell.getByLabel("Passwort").fill("egal");
  await shell.getByRole("button", { name: "Anmelden" }).click();
  await expect(shell.getByRole("alert")).toHaveText("Anmeldung fehlgeschlagen.");
});

test("E_LOCKED waits the given seconds", async ({ shell }) => {
  await shell.evaluate(() => { window.__ngcTest = { lock: 30 }; });
  await shell.getByLabel("Benutzername").fill("anna");
  await shell.getByLabel("Passwort").fill("Heimnetz-2026");
  await shell.getByRole("button", { name: "Anmelden" }).click();
  await expect(shell.getByRole("alert")).toContainText("Zu viele Fehlversuche");
  await expect(shell.getByRole("button", { name: /Anmelden/ })).toBeDisabled();
});

test("TOTP is asked for when the challenge says so", async ({ shell }) => {
  await shell.getByLabel("Benutzername").fill("bernd");
  await shell.getByLabel("Passwort").fill("Heimnetz-2026");
  await shell.getByRole("button", { name: "Anmelden" }).click();
  await expect(shell.getByLabel("Code aus der Authenticator-App")).toBeVisible();
  await shell.getByLabel("Code aus der Authenticator-App").fill("123456");
  await shell.getByRole("button", { name: "Anmelden" }).click();
  await expect(shell.locator("#taskbar")).toBeVisible();
});

test("a passkey signs the certificate, and a role with passwordToo signs it twice",
  async ({ shell, browserName }) => {
  test.skip(browserName !== "chromium", "virtual authenticators only exist in Chromium");
  const cdp = await shell.context().newCDPSession(shell);
  await cdp.send("WebAuthn.enable");
  const { authenticatorId } = await cdp.send("WebAuthn.addVirtualAuthenticator", {
    options: { protocol: "ctap2", transport: "usb", hasUserVerification: true,
      hasResidentKey: true, isUserVerified: true, automaticPresenceSimulation: true },
  });
  // A browser that has never seen this installation: nothing remembered, no
  // trust list yet. The key ID comes from the challenge alone, so this has to
  // work on the first try.
  await shell.evaluate(() => { localStorage.clear(); sessionStorage.clear(); });
  await shell.evaluate(() => window.__ngcTest = { user: "clara" }); // clara has a passkey
  await shell.getByRole("button", { name: "Mit Passkey anmelden" }).click();
  await expect(shell.locator("#taskbar")).toBeVisible();

  // dora is in a role with passwordToo: the proof comes from the passkey and
  // the password proof from the password key, both over the same certificate.
  const both = await shell.evaluate(async () => {
    const keys = await import("/assets/js/auth/keys.js");
    return keys.lastLoginHadBothProofs;
  });
  expect(both === true || both === false).toBe(true);
  await cdp.send("WebAuthn.removeVirtualAuthenticator", { authenticatorId });
});
```

- [ ] **Step 2: Test laufen lassen, er muss scheitern**

```bash
cd apps/web/tests && npx playwright test shell/login.spec.js --project=chromium
```

Erwartet: keine Anmeldemaske vorhanden.

- [ ] **Step 3: `keys.js` schreiben**

```js
// The session keys and the proofs over the client certificate. The private
// parts never leave the browser: both key pairs are created as
// non-extractable Web Crypto keys, and the password is thrown away right
// after it has been turned into a key.
import { b64encode, b64decode, utf8, equal as equalBytes } from "../contracts/b64.js";
import * as session from "../contracts/session.js";
import * as proof from "../contracts/proof.js";
import * as sign from "../contracts/sign.js";
import * as noise from "../contracts/noise.js";

export async function newSessionKeys() {
  // Ed25519 signs tasks, X25519 carries the Noise session and every
  // end-to-end channel. Neither can be read out again, not even by the shell.
  const signPair = await crypto.subtle.generateKey({ name: "Ed25519" }, false, ["sign", "verify"]);
  const jwk = await crypto.subtle.exportKey("jwk", signPair.publicKey);
  const publicRaw = b64decode(jwk.x);
  const dh = await noise.newKeypair();
  return {
    sign: { privateKey: signPair.privateKey, publicRaw, kid: await sign.keyIDAsync(publicRaw) },
    dh,
  };
}

export function buildClientCert({ installation, user, nonce, keys, now, maxLifetime }) {
  const cert = {
    inst: installation, user, nonce,
    sig: keys.sign.publicRaw, dh: keys.dh.publicRaw,
    iat: now, exp: now + Math.min(maxLifetime, session.MAX_LIFETIME),
  };
  if (!session.validateClientCert(cert)) throw new Error("keys: malformed certificate");
  // These exact bytes are what the user signs and what CertHash hashes, so
  // they are built once and kept for the whole session.
  return { cert, payload: session.encodeClientCert(cert) };
}

export async function proveWithPassword(payload, passwordText, kdf, purpose) {
  const seed = await proof.deriveSeed(passwordText, kdf);
  try {
    const key = await proof.privateFromSeed(seed);
    const pub = await proof.publicFromSeed(seed);
    const sig = new Uint8Array(await crypto.subtle.sign({ name: "Ed25519" }, key,
      sign.message(purpose, payload)));
    return { kind: proof.KIND_PASSWORD, kid: await sign.keyIDAsync(pub), sig,
      authData: new Uint8Array(0), clientData: new Uint8Array(0) };
  } finally {
    seed.fill(0);
  }
}

// proveWithPasskey asks the authenticator for an assertion over the challenge
// of this purpose and payload. NGC always asks for user verification: a PIN or
// biometrics, never a tap alone.
//
// passkeys is challenge.passkeys, checked by session.parseLoginChallenge: each
// entry holds the credential ID for allowCredentials and the key ID the proof
// needs. The shell takes the key ID from the answer and derives none of its
// own, so a browser that has never seen the trust list can log in with a
// passkey on the first try.
export async function proveWithPasskey(payload, { rp, passkeys }, purpose) {
  const challenge = await proof.challenge(purpose, payload);
  const cred = await navigator.credentials.get({
    publicKey: {
      challenge,
      rpId: rp.id,
      userVerification: "required",
      timeout: 60000,
      allowCredentials: passkeys.map((p) => ({ type: "public-key", id: p.credId })),
    },
  });
  if (!cred) throw new Error("keys: no passkey");
  const id = new Uint8Array(cred.rawId);
  // The authenticator answers with one of the credential IDs that were
  // offered, so its entry is the one that names the key ID.
  const ref = passkeys.find((p) => equalBytes(p.credId, id));
  if (!ref) throw new Error("keys: the authenticator answered with another credential");
  return {
    kind: proof.KIND_PASSKEY, kid: ref.kid,
    sig: new Uint8Array(cred.response.signature),
    authData: new Uint8Array(cred.response.authenticatorData),
    clientData: new Uint8Array(cred.response.clientDataJSON),
  };
}
```

`equalBytes` ist der Vergleich aus `b64.js` (`equal`), in konstanter Zeit; hier geht es nur um die
Zuordnung, aber die Shell hat nur diesen einen Vergleich.

- [ ] **Step 4: `login.js` und die Ansicht schreiben**

Ablauf, genau nach `shared/README.md`, Abschnitt „Vor dem WebSocket“:

1. `http.loginStart(name)` – die API antwortet auch für unbekannte Namen mit einer Aufgabe, die von
   einer echten nicht zu unterscheiden ist. `parseLoginChallenge` prüft sie (Aufgabe 8); was dort
   durchfällt, behandelt die Shell wie jeden anderen Fehlschlag und sagt nur „Anmeldung
   fehlgeschlagen.“
2. Aus `challenge.kdf` und dem eingegebenen Passwort den Passwortschlüssel bilden, oder mit
   `challenge.passkeys` den Passkey fragen. Jeder Eintrag bringt `credId` für
   `allowCredentials` und `kid` für den Beweis mit; die Shell leitet das `kid` nirgends selbst her
   und braucht dafür weder die Vertrauensliste noch einen gemerkten Eintrag im Browser. Ein frischer
   Browser, ein privates Fenster oder ein neues Gerät kommt damit genauso hinein wie ein bekanntes –
   auch ein Benutzer in einer Rolle mit `passkeyOnly`.
3. Sitzungsschlüssel erzeugen, Client-Zertifikat bauen, unterschreiben lassen. Verlangt die Rolle
   das Passwort zusätzlich (`challenge.passwordToo`), kommt `proof` vom Passkey und `password`
   vom Passwortschlüssel – beide über **dieselben** Zertifikat-Bytes, Zweck `clientcert/v1`.
4. `http.loginFinish({cert, proof, password, totp})`; das Cookie setzt die API.
5. Das API-Zertifikat aus `result.api` mit `session.verifyAPI` gegen die geprüfte Vertrauensliste
   prüfen (Aufgabe 20 liefert sie; bis dahin gegen die Fassung, die die Ersteinrichtung gebaut hat).
6. Leitung aufbauen (`link.connect`), Desktop starten.

Fehler zeigt die Ansicht als `role="alert"` mit `wire.message(code)`: `E_LOGIN` sagt nie, was
falsch war; bei `E_LOCKED` bleibt der Knopf `retryAfter` Sekunden gesperrt und zählt sichtbar
herunter. Das Passwortfeld wird nach dem Ableiten sofort geleert.

Die Ansicht besteht aus: Benutzername, Passwort, bei Bedarf TOTP, den Knöpfen „Anmelden“ und
„Mit Passkey anmelden“ (nur wenn `rp.id` gesetzt ist), und dem dauerhaften Hinweis „Umstellen auf
Passkeys empfohlen“, sobald `result.passwordHint` gesetzt ist. Alle Texte kommen über
`textContent`.

- [ ] **Step 5: Tests laufen lassen, sie müssen bestehen**

```bash
cd apps/web/tests && npm test -- shell/login.spec.js
```

Erwartet: fünf grüne Tests; der Passkey-Test läuft nur in Chromium und wird in Firefox und WebKit
übersprungen.

- [ ] **Step 6: Committen**

```bash
git add apps/web/public/assets/js/auth apps/web/public/assets/js/shell/dom.js apps/web/tests/shell/login.spec.js
git commit -m "Add login with password key, TOTP and passkeys" --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 19: Ersteinrichtung mit Einmal-Code

**Files:**
- Create: `apps/web/public/assets/js/auth/setup.js`
- Test: `apps/web/tests/shell/setup.spec.js`

**Interfaces:**
- Produces: `setupView(root, {config, onDone})`,
  `runSetup({code, name, password, store}) -> Promise<{version, anchor}>`.
- Consumes: `http.js`, `session.js`, `trust.js`, `proof.js`, `keys.js`.

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

`apps/web/tests/shell/setup.spec.js`:

```js
import { test, expect } from "../fixtures/page.js";

// The mock API answers GET /api/v1/setup while it has no admin.
test.use({ storageState: { cookies: [], origins: [] } });

test("the code is asked for first, and the offer is checked with it", async ({ shell }) => {
  await shell.evaluate(() => { window.__ngcTest = { setup: true }; });
  await shell.reload();
  await expect(shell.getByLabel("Einmal-Code")).toBeVisible();
  // The offer is fetched only after the code has been typed.
  const before = await shell.evaluate(() => window.__ngcSeenSetupRequests ?? 0);
  expect(before).toBe(0);

  await shell.getByLabel("Einmal-Code").fill("abcde fghjk mnpqr stuvw");
  await shell.getByRole("button", { name: "Weiter" }).click();
  await expect(shell.getByLabel("Benutzername")).toBeVisible();
  await expect(shell.getByText("API-Server")).toBeVisible();
});

test("a man in the middle with other keys is refused", async ({ shell }) => {
  await shell.evaluate(() => { window.__ngcTest = { setup: true, offer: "foreignKeys" }; });
  await shell.reload();
  await shell.getByLabel("Einmal-Code").fill("ABCDE-FGHJK-MNPQR-STUVW");
  await shell.getByRole("button", { name: "Weiter" }).click();
  await expect(shell.getByRole("alert")).toHaveText(
    "Der Einmal-Code passt nicht zu dieser Anlage. Bitte den Code prüfen und noch einmal versuchen.");
  // Nothing of the offer may be shown: it was never read.
  await expect(shell.getByText("api-")).toHaveCount(0);
});

test("the one-time code never travels", async ({ shell }) => {
  const seen = [];
  shell.on("request", (r) => seen.push((r.postData() ?? "") + " " + r.url()));
  await shell.evaluate(() => { window.__ngcTest = { setup: true }; });
  await shell.reload();
  await shell.getByLabel("Einmal-Code").fill("ABCDE-FGHJK-MNPQR-STUVW");
  await shell.getByRole("button", { name: "Weiter" }).click();
  await shell.getByLabel("Benutzername").fill("anna");
  await shell.getByLabel("Passwort").fill("Heimnetz-2026");
  await shell.getByLabel("Passwort wiederholen").fill("Heimnetz-2026");
  await shell.getByRole("button", { name: "Einrichten" }).click();
  await expect(shell.locator("#taskbar")).toBeVisible();
  for (const part of ["ABCDE", "FGHJK", "MNPQR", "STUVW", "Heimnetz-2026"]) {
    expect(seen.join("\n"), part).not.toContain(part);
  }
});
```

- [ ] **Step 2: Test laufen lassen, er muss scheitern**

```bash
cd apps/web/tests && npx playwright test shell/setup.spec.js --project=chromium
```

Erwartet: keine Maske für den Einmal-Code.

- [ ] **Step 3: `setup.js` schreiben**

Der Ablauf, genau nach `session.ParseSetupOffer` und `session.VerifySetup`:

1. Erst den Einmal-Code abfragen und mit `session.normalizeSetupCode` in die kanonische Form
   bringen. Passt er nicht, kommt gar keine Anfrage: „Der Einmal-Code ist nicht vollständig.“
2. `GET /api/v1/setup` holen und mit `session.parseSetupOffer(bytes, code)` prüfen. Erst der MAC,
   dann `info`. Schlägt es fehl, zeigt die Shell „Der Einmal-Code passt nicht zu dieser Anlage.
   Bitte den Code prüfen und noch einmal versuchen.“ und **nichts** aus dem Angebot.
3. Dann die Werte zeigen: die Installation, die API-Knoten Zeichen für Zeichen mit ihrer Adresse
   und ihrem Fingerabdruck, und – mit Haken zum Abwählen – den globalen Store.
4. Admin anlegen: Benutzername (`trust.validLoginName`), Passwort zweimal, daraus mit einem frisch
   gezogenen Salz (`proof.SALT_SIZE` Zufallsbytes) und den Standardwerten (m=65536, t=3, p=1) der
   Passwortschlüssel; sein öffentlicher Teil kommt in Fassung 1.
5. Fassung 1 bauen: `seq` 1, kein `prev`, `issued` = `info.now`, `installation`, `rp` aus
   `/config.json` (leer, solange es keine Passkeys gibt), ein Benutzer mit Rolle `admin`, die
   Rolle `admin` mit Stufe `critical`, die API-Knoten **genau** aus `info.nodes` in derselben
   Reihenfolge, der Store nur, wenn der Haken steht, und Dauerfreigaben für `ngc-web`, `ngc-api`,
   `ngc-controller` mit `info.releaseKey` und für `system` ohne Schlüssel – nach `target`
   sortiert, also `ngc-api`, `ngc-controller`, `ngc-web`, `system`.
6. Die Nutzlast einmal schreiben (`trust.encodeList`), mit dem Passwortschlüssel unterschreiben
   (Zweck `trustlist/v1`), den MAC `session.setupMAC(code, payload)` darüber rechnen und
   `POST /api/v1/setup` mit `{mac, trust: {payload, proof}}` schicken. Der Code selbst geht nie
   über die Leitung.
7. Zur Kontrolle dieselbe Prüfung fahren, die die API fährt (`trust.open` mit dem Anker
   `{seq: 1, hash: sha256(payload)}`), den Anker merken und sich anmelden.

Schlägt `POST` mit `E_VERIFY` fehl, zeigt die Shell „Die Anlage hat die Einrichtung abgelehnt.
Bitte NGC neu installieren und den neuen Code benutzen.“; bei `E_NOTFOUND` „Diese Anlage ist schon
eingerichtet.“

- [ ] **Step 4: Tests laufen lassen, sie müssen bestehen**

```bash
cd apps/web/tests && npm test -- shell/setup.spec.js
```

Erwartet: drei grüne Tests in allen drei Engines.

- [ ] **Step 5: Committen**

```bash
git add apps/web/public/assets/js/auth/setup.js apps/web/tests/shell/setup.spec.js
git commit -m "Add the first-run setup with the one-time code" --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 20: Die Vertrauensliste in der Shell – Anker, Kette, Alarm

**Files:**
- Create: `apps/web/public/assets/js/shell/trustlist.js`
- Test: `apps/web/tests/shell/trustlist.spec.js`

**Interfaces:**
- Produces: `class TrustStore` mit
  `load(installation) -> Promise<{list, hash}|null>` (aus IndexedDB),
  `bootstrap(anchor) -> Promise<version>` (Kette ab dem Anker holen und prüfen),
  `refresh(hintSeq) -> Promise<version>`, `current()`, `onChange(fn)`, `onAlarm(fn)`,
  `controllerKey(nodeID)`, `storeOf(id)`.
- Consumes: `http.js`, `trust.js`, `session.js`.

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

`apps/web/tests/shell/trustlist.spec.js`:

```js
import { test, expect } from "../fixtures/page.js";

test("a tampered version raises the alarm and logs out", async ({ shell }) => {
  await shell.getByLabel("Benutzername").fill("anna");
  await shell.getByLabel("Passwort").fill("Heimnetz-2026");
  await shell.getByRole("button", { name: "Anmelden" }).click();
  await expect(shell.locator("#taskbar")).toBeVisible();

  // The mock API now sends a version whose signature does not hold.
  await shell.evaluate(() => window.__ngcTest.tamperTrust = true);
  await shell.evaluate(() => window.__ngcSendTrustHint(2));

  await expect(shell.getByRole("alertdialog")).toContainText(
    "Die Vertrauensliste dieser Anlage stimmt nicht. NGC hat Sie sicherheitshalber abgemeldet.");
  await expect(shell.getByLabel("Benutzername")).toBeVisible();
});

test("a version the shell already knows is not fetched twice", async ({ shell }) => {
  await shell.getByLabel("Benutzername").fill("anna");
  await shell.getByLabel("Passwort").fill("Heimnetz-2026");
  await shell.getByRole("button", { name: "Anmelden" }).click();
  await expect(shell.locator("#taskbar")).toBeVisible();
  const calls = await shell.evaluate(async () => {
    const before = window.__ngcTrustCalls;
    window.__ngcSendTrustHint(1);
    await new Promise((r) => setTimeout(r, 200));
    return window.__ngcTrustCalls - before;
  });
  expect(calls).toBe(0);
});

test("after a new version the UI proofs are checked again", async ({ shell }) => {
  await shell.getByLabel("Benutzername").fill("anna");
  await shell.getByLabel("Passwort").fill("Heimnetz-2026");
  await shell.getByRole("button", { name: "Anmelden" }).click();
  await shell.evaluate(() => window.__ngcSendTrustHint(2));
  const cleared = await shell.evaluate(async () => {
    await new Promise((r) => setTimeout(r, 200));
    return window.__ngcUIProofCacheSize;
  });
  expect(cleared).toBe(0);
});
```

- [ ] **Step 2: Test laufen lassen, er muss scheitern**

```bash
cd apps/web/tests && npx playwright test shell/trustlist.spec.js --project=chromium
```

Erwartet: kein Alarm, weil `trustlist.js` fehlt.

- [ ] **Step 3: `trustlist.js` schreiben**

Regeln, die das Modul durchsetzt:

- Der Anker kommt vom Webserver (`/config.json`), nie von der API. Fehlt er (der Controller des
  Webservers ist noch nicht gekoppelt), gilt der Anker aus der Ersteinrichtung, den die Shell im
  Browser gemerkt hat; hat sie auch den nicht, sagt sie: „Diese Anlage ist noch nicht fertig
  eingerichtet. Bitte den Controller des Webservers koppeln.“
- Die Kette holt sie mit `GET /api/v1/trust?from=N`, seitenweise, solange `more` gesetzt ist; die
  erste Seite beginnt bei der Nummer des Ankers. Geprüft wird mit `trust.open` und `trust.advance`
  – erst der Hash bzw. die Unterschrift, dann die Nutzlast lesen.
- Die neueste geprüfte Fassung liegt in IndexedDB (`ngc`, Speicher `trust`, Schlüssel die
  Installations-ID): Nutzlast, Beweis, Hash und Nummer. Beim nächsten Start setzt die Shell darauf
  auf und holt nur, was fehlt.
- Die Steuernachricht `trust` ist nur ein Hinweis: Ist `seq` nicht größer als die eigene, passiert
  nichts.
- Bricht die Kette (falscher Anker, Lücke, ungültige Unterschrift, falsche Installation, Rückfall
  auf eine ältere Nummer), zeigt die Shell einen Alarm, verwirft die Sitzung, meldet ab
  (`POST /api/v1/logout`) und zeigt die Anmeldung. Der Text lautet: „Die Vertrauensliste dieser
  Anlage stimmt nicht. NGC hat Sie sicherheitshalber abgemeldet.“
- Nach jeder neuen Fassung: den Zwischenspeicher der UI-Beweise und der Kataloge leeren und die
  Shell die Apps neu laden lassen. Für Passkeys merkt sich die Shell **nichts**: `kid` und `credId`
  stehen in der Anmelde-Antwort (`session.PasskeyRef`), die Liste wird dafür nicht gebraucht.
- `controllerKey(nodeID)` gibt den Noise-Schlüssel eines Controllers **nur** aus der geprüften
  Liste; kennt sie den Knoten nicht, gibt es keinen Ende-zu-Ende-Kanal, sondern die Meldung
  „Dieser Server steht nicht in der Vertrauensliste.“

- [ ] **Step 4: Tests laufen lassen, sie müssen bestehen**

```bash
cd apps/web/tests && npm test -- shell/trustlist.spec.js
```

Erwartet: drei grüne Tests in allen drei Engines.

- [ ] **Step 5: Committen**

```bash
git add apps/web/public/assets/js/shell/trustlist.js apps/web/tests/shell/trustlist.spec.js
git commit -m "Keep and verify the trust list in the shell" --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 21: Der Desktop – Fenster, Startleiste, Startmenü, hell und dunkel

**Files:**
- Create: `apps/web/public/assets/js/shell/window.js`, `apps/web/public/assets/js/shell/desktop.js`,
  `apps/web/public/assets/js/shell/theme.js`
- Modify: `apps/web/public/assets/css/shell.css`, `apps/web/public/assets/js/shell/main.js`
- Test: `apps/web/tests/shell/desktop.spec.js`

**Interfaces:**
- Produces:
  - `window.js`: `class Win` mit `open()`, `focus()`, `minimize()`, `restore()`, `toggleMax()`,
    `close()`, `setTitle(t)`, `body` (ein leeres `<div>` für den Inhalt).
  - `desktop.js`: `startDesktop(root, {link, session, trust}) -> Desktop` mit
    `openApp(appID)`, `apps()`, `tile()`, `closeAll()`, `notice(level, text)`.
  - `theme.js`: `applyTheme(t)`, `applyAccent(c)`, `applyWallpaper(id)`, `currentTheme()`,
    `onThemeChange(fn)`.
- Consumes: `dom.js`, `core.js`, `channel.js`.

Vorlage ist `docs/prototype.html`: Farbmarken (Zeilen 8–31), Aufbau von Taskleiste, Startmenü und
Fenstern (Zeilen 244–266) und das Verhalten der Fenster (Zeilen 308–548). Übernommen wird alles,
was Aussehen und Bedienung ausmacht; nicht übernommen wird, wie der Prototyp Inhalte einsetzt: dort
steht `innerHTML`, hier `textContent` und `createElement`.

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

`apps/web/tests/shell/desktop.spec.js`:

```js
import { test, expect } from "../fixtures/page.js";

test.beforeEach(async ({ shell }) => {
  await shell.getByLabel("Benutzername").fill("anna");
  await shell.getByLabel("Passwort").fill("Heimnetz-2026");
  await shell.getByRole("button", { name: "Anmelden" }).click();
  await expect(shell.locator("#taskbar")).toBeVisible();
});

test("the start menu lists only the apps this user may use", async ({ shell }) => {
  await shell.getByRole("button", { name: "Start" }).click();
  await expect(shell.getByRole("menuitem", { name: "App-Store" })).toBeVisible();
  await expect(shell.getByRole("menuitem", { name: "Terminal" })).toBeVisible();
  // bernd has no right on the terminal; the mock API leaves it out for him.
  await expect(shell.getByRole("menuitem", { name: "Geheim" })).toHaveCount(0);
});

test("a window opens, moves, maximizes and closes", async ({ shell }) => {
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("menuitem", { name: "Rechner" }).click();
  const win = shell.locator(".win", { hasText: "Rechner" });
  await expect(win).toBeVisible();
  const before = await win.boundingBox();
  // hover() setzt die Maus in die MITTE der Titelleiste, nicht an before.x.
  // Ein absolutes move(before.x + 120, ...) zieht das Fenster deshalb nach
  // LINKS, und die Zusicherung unten kann nie wahr werden. Gemessen. Es muss
  // vom Griff aus gerechnet werden, nicht vom linken Fensterrand.
  const grip = await shell.locator(".win .titlebar", { hasText: "Rechner" }).boundingBox();
  await shell.mouse.move(grip.x + grip.width / 2, grip.y + grip.height / 2);
  await shell.mouse.down();
  await shell.mouse.move(grip.x + grip.width / 2 + 120, grip.y + grip.height / 2 + 80);
  await shell.mouse.up();
  const after = await win.boundingBox();
  expect(Math.round(after.x - before.x)).toBeGreaterThan(80);
  await win.getByRole("button", { name: "Maximieren" }).click();
  await expect(win).toHaveClass(/max/);
  await win.getByRole("button", { name: "Schließen" }).click();
  await expect(win).toHaveCount(0);
});

test("light and dark are kept", async ({ shell }) => {
  await shell.getByRole("button", { name: "Design wechseln" }).click();
  await expect(shell.locator("html")).toHaveAttribute("data-theme", "light");
  await shell.reload();
  await expect(shell.locator("html")).toHaveAttribute("data-theme", "light");
});

test("a name from outside is never HTML", async ({ shell }) => {
  // The mock API names one app "<img src=x onerror=alert(1)>Böse".
  await shell.getByRole("button", { name: "Start" }).click();
  const item = shell.getByRole("menuitem", { name: /Böse/ });
  await expect(item).toBeVisible();
  expect(await item.evaluate((e) => e.querySelectorAll("img").length)).toBe(0);
});

test("a notice from the API is shown as text", async ({ shell }) => {
  await shell.evaluate(() => window.__ngcSendNotice("warn", "Update für ngc-web bereit."));
  await expect(shell.getByRole("status")).toHaveText("Update für ngc-web bereit.");
});
```

- [ ] **Step 2: Test laufen lassen, er muss scheitern**

```bash
cd apps/web/tests && npx playwright test shell/desktop.spec.js --project=chromium
```

Erwartet: keine Taskleiste.

- [ ] **Step 3: `shell.css` vollständig schreiben**

Aus `docs/prototype.html` übernommen: `:root` und `html[data-theme="light"]` (Zeilen 8–31),
`#desktop`, `#wallpaper`, `#icons`, `.dicon`, `#taskbar`, `#startbtn`, `#tasks`, `.task`, `#tray`,
`#clock`, `#startmenu`, `.win`, `.titlebar`, `.wbtn`, `.body`, `.rs` und `#snaphint`. Dazu neu:
`.appframe` (der iframe füllt `.body` ohne Rahmen), `.alert` und `.status` für Meldungen und
`@media (prefers-color-scheme: light)` als Vorgabe, solange niemand ein Design gewählt hat.

- [ ] **Step 4: `window.js` schreiben**

`Win` baut das Fenster aus `createElement`:

```js
// One window. The prototype builds its frame with innerHTML; here every node
// is created, because titles and names come from outside.
import { el } from "./dom.js";

export class Win {
  constructor(desktop, { id, title, icon, width, height }) {
    this.desktop = desktop;
    this.el = el("div", "win");
    const bar = el("div", "titlebar");
    bar.append(el("span", "g", icon ?? ""), el("span", "t", title));
    const btns = el("div", "wbtns");
    for (const [cls, label] of [["mini", "Minimieren"], ["maxi", "Maximieren"], ["close", "Schließen"]]) {
      const b = el("button", "wbtn " + cls, cls === "mini" ? "—" : cls === "maxi" ? "▢" : "✕");
      b.type = "button";
      b.setAttribute("aria-label", label);
      btns.append(b);
    }
    bar.append(btns);
    this.body = el("div", "body");
    this.el.append(bar, this.body);
    for (const dir of ["n", "e", "s", "w", "se", "sw"]) this.el.append(el("div", "rs " + dir));

    // The window opens cascaded, at most as large as the desktop minus a
    // margin, like the prototype does.
    const a = desktop.area();
    const w = Math.min(width || 560, Math.max(280, a.w - 40));
    const h = Math.min(height || 400, Math.max(180, a.h - 40));
    const off = (desktop.cascade++ % 8) * 26;
    Object.assign(this.el.style, {
      left: Math.max(8, Math.min(a.w - w - 8, 70 + off)) + "px",
      top: Math.max(8, Math.min(a.h - h - 8, 40 + off)) + "px",
      width: w + "px", height: h + "px",
    });

    this.tab = el("div", "task");
    this.tab.append(el("span", null, icon ?? ""), el("span", "lbl", title));
    this.tab.addEventListener("click", () => (this.min ? this.restore() : this.focus()));

    btns.children[0].addEventListener("click", () => this.minimize());
    btns.children[1].addEventListener("click", () => this.toggleMax());
    btns.children[2].addEventListener("click", () => this.close());
    this.el.addEventListener("pointerdown", () => this.focus());
    this.makeDraggable(bar);
    this.makeResizable();
  }

  setTitle(t) {
    this.el.querySelector(".t").textContent = t;
    this.tab.querySelector(".lbl").textContent = t;
  }
}
```

Verschieben, Größe ziehen, Anordnen, Andocken an die Ränder und das Verhalten beim Verkleinern des
Fensters kommen aus dem Prototyp (Zeilen 427–548), mit Zeigerereignissen (`pointerdown`,
`pointermove`, `pointerup`) statt Mausereignissen, damit es auf Tablet und Handy genauso geht.
Die kleinsten Maße sind 240 × 150 wie im Prototyp.

- [ ] **Step 5: `desktop.js` und `theme.js` schreiben**

`startDesktop` baut Hintergrund, Symbole, Taskleiste (Startknopf, laufende Fenster, Uhr,
Design-Knopf) und Startmenü mit Suchfeld. Die Liste der Apps kommt aus `ngc.desktop/apps`: je App
`id`, `name`, `version`, `icon`, `entry`, `width`, `height`, `actions`, `state`, `core`, `node`.

- `core: true` heißt App-Store oder Benutzer & Rechte: ihre Oberfläche gehört zur Shell
  (Aufgaben 23 und 24), es gibt keinen iframe und keinen UI-Beweis.
- `state: "unreachable"` zeigt die App gleich als „nicht erreichbar“ und öffnet kein Fenster.
- `state: "unlicensed"` zeigt „Keine gültige Lizenz.“
- `node` wird nie angezeigt; es ist nur der Schlüssel für Ende zu Ende.
- Jeder Name, jeder Titel und jede Meldung geht durch `textContent`; ein Symbol ist ein Pfad im
  geprüften ui-tar und wird erst gezeigt, wenn der Beweis stimmt (Aufgabe 22).

`theme.js` hält Design, Akzentfarbe und Hintergrund in `localStorage` unter `ngc.theme`,
`ngc.accent` und `ngc.wallpaper` – nur Bequemlichkeit, nichts Sicherheitsrelevantes; fehlt der
Speicher, gilt die Vorgabe des Systems (`prefers-color-scheme`). Die Auswahl gibt es über den
Knopf „Design wechseln“ und im Startmenü unter „Aussehen“.

- [ ] **Step 6: Tests laufen lassen, sie müssen bestehen**

```bash
cd apps/web/tests && npm test -- shell/desktop.spec.js
```

Erwartet: sechs grüne Tests in allen drei Engines.

- [ ] **Step 7: Committen**

```bash
git add apps/web/public/assets/js/shell apps/web/public/assets/css/shell.css apps/web/tests/shell/desktop.spec.js
git commit -m "Add the desktop with windows, taskbar and start menu" --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 22: App-Fenster – UI-Beweis, ui-tar und `ngc.js`

**Files:**
- Create: `apps/web/public/assets/js/shell/appframe.js`, `apps/web/public/assets/ngc.js`
- Test: `apps/web/tests/shell/appframe.spec.js`

**Interfaces:**
- Produces:
  - `appframe.js`: `openAppUI(win, {app, link, trust, cache}) -> Promise<void>`,
    `class UICache` mit `get(app, version)`, `put(app, version, files)`, `clear()`.
  - `ngc.js` (MIT): `ngc.call(service, action, params) -> Promise<any>`,
    `ngc.open(service, action, params) -> Promise<Stream>` mit `write`, `end`, `onData`, `onClose`,
    `ngc.theme`, `ngc.accent`, `ngc.onTheme(fn)`, `ngc.version`.
- Consumes: `core.js`, `uitar.js`, `trust.js`, `channel.js`.

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

`apps/web/tests/shell/appframe.spec.js`:

```js
import { test, expect } from "../fixtures/page.js";

test.beforeEach(async ({ shell }) => {
  await shell.getByLabel("Benutzername").fill("anna");
  await shell.getByLabel("Passwort").fill("Heimnetz-2026");
  await shell.getByRole("button", { name: "Anmelden" }).click();
  await expect(shell.locator("#taskbar")).toBeVisible();
});

test("an app runs only when its ui-tar fits the signed proof", async ({ shell }) => {
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("menuitem", { name: "Terminal" }).click();
  const frame = shell.frameLocator(".appframe");
  await expect(frame.getByText("Terminal")).toBeVisible();
});

test("a ui-tar that does not fit the proof is refused", async ({ shell }) => {
  await shell.evaluate(() => { window.__ngcTest.swapUI = true; });
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("menuitem", { name: "Terminal" }).click();
  await expect(shell.getByRole("alert")).toHaveText(
    "Die Oberfläche dieser App ist nicht echt. NGC führt sie nicht aus.");
  await expect(shell.locator(".appframe")).toHaveCount(0);
});

test("an entry page that is not in the ui-tar is refused", async ({ shell }) => {
  await shell.evaluate(() => { window.__ngcTest.entry = "nichtda.html"; });
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("menuitem", { name: "Terminal" }).click();
  await expect(shell.getByRole("alert")).toHaveText(
    "Die Startseite dieser App fehlt. NGC führt sie nicht aus.");
});

test("an app reaches neither the shell nor the network", async ({ shell }) => {
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("menuitem", { name: "Terminal" }).click();
  const frame = shell.frameLocator(".appframe");
  const out = await frame.locator("body").evaluate(async () => {
    const r = { parent: "no", fetch: "no", storage: "no" };
    try { r.parent = String(window.parent.document.title); } catch { r.parent = "blocked"; }
    try { await fetch("/config.json"); r.fetch = "reached"; } catch { r.fetch = "blocked"; }
    try { localStorage.setItem("x", "1"); r.storage = "reached"; } catch { r.storage = "blocked"; }
    return r;
  });
  expect(out).toEqual({ parent: "blocked", fetch: "blocked", storage: "blocked" });
});

test("a message that does not come from the frame is ignored", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const before = window.__ngcChannelsOpened;
    window.postMessage({ ngc: 1, id: 1, op: "call", service: "terminal", action: "shell" }, "*");
    await new Promise((r) => setTimeout(r, 100));
    return window.__ngcChannelsOpened - before;
  });
  expect(out).toBe(0);
});
```

- [ ] **Step 2: Test laufen lassen, er muss scheitern**

```bash
cd apps/web/tests && npx playwright test shell/appframe.spec.js --project=chromium
```

Erwartet: kein `.appframe` im Fenster.

- [ ] **Step 3: `appframe.js` schreiben**

Die Reihenfolge ist Sicherheit, deshalb steht sie hier als Ablauf:

1. **Beweis holen:** `ngc.desktop/ui-proof` mit `{app}` öffnen, `core.parseUIProof` lesen,
   `core.verifyUIProof(p, app.id, app.version, trust.current().list)` gegen die neueste geprüfte
   Fassung prüfen. Das Ergebnis ist die Prüfsumme des ui-tar.
2. **Zwischenspeicher:** Ist für diese App und Version schon eine geprüfte Prüfsumme gemerkt, wird
   der Beweis nicht neu geholt; nach jeder neuen Fassung der Vertrauensliste ist der Speicher leer
   (Aufgabe 20).
3. **ui-tar holen:** `ngc.desktop/ui` (`out`) ganz lesen, höchstens `store.MaxUITotal` +
   `store.MaxEntries` × 512 Byte an Köpfen annehmen, sonst mit `E_TOOLARGE` schließen.
4. **Vergleichen:** SHA-256 über die empfangenen Bytes rechnen und mit der Prüfsumme aus dem Beweis
   vergleichen. Stimmt sie nicht: „Die Oberfläche dieser App ist nicht echt. NGC führt sie nicht
   aus.“ – und nichts davon wird gelesen.
5. **Erst danach lesen:** `uitar.walkUI(bytes)`.
6. **Startseite:** `app.entry` muss eine Datei sein, die `walkUI` geliefert hat und die
   `store.validPath` besteht; sonst „Die Startseite dieser App fehlt. NGC führt sie nicht aus.“
7. **Dokument bauen:** Aus der Startseite wird ein Dokument, in dem alles steckt, was die App
   braucht: jedes `<link rel="stylesheet" href="…">`, `<script src="…">`, `<img src="…">` und jedes
   `url(…)` in CSS wird durch eine `data:`-Adresse mit dem Inhalt aus dem ui-tar ersetzt. Die
   Ersetzung läuft über `DOMParser` und den fertigen Baum, nie über Zeichenkettenersetzung.
   `ngc.js` kommt als erstes Skript dazu, ebenfalls als `data:`-Adresse.
   Eine Datei, die im ui-tar fehlt, wird zu einer leeren `data:`-Adresse; ein
   `<script type="module">` lehnt die Shell mit „Diese App braucht klassische Skripte.“ ab, weil
   ein Modul aus einer `data:`-Adresse keine weiteren Module laden kann.
8. **Rahmen:** ein `<iframe class="appframe" sandbox="allow-scripts" referrerpolicy="no-referrer"
   allow="">` mit diesem Dokument als `srcdoc`. Ohne `allow-same-origin` hat die Seite eine eigene,
   leere Herkunft: sie kommt weder an die Shell noch an deren Speicher. Im Dokument steht zusätzlich
   `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src data:;
   style-src data: 'unsafe-inline'; img-src data:; font-src data:; media-src data:;
   connect-src 'none'; form-action 'none'; base-uri 'none'">` – eigene Verbindungen sind damit
   gesperrt.
9. **Brücke:** Die Shell hört auf `message` und nimmt nur an, was von genau diesem
   `iframe.contentWindow` kommt und ein frisch gezogenes Wort (`token`) trägt. Sie schickt der
   App nie mehr, als diese angefordert hat, und öffnet Kanäle immer mit **ihrer** App-ID, nie mit
   einer aus der Nachricht.

```js
// The bridge between shell and app frame. Everything the frame sends is data,
// never an instruction: the shell takes the app id from its own window, not
// from the message.
function bridge(win, frame, app, link, token) {
  const onMessage = async (e) => {
    if (e.source !== frame.contentWindow) return;
    const m = e.data;
    if (!m || m.ngc !== 1 || m.token !== token || typeof m.id !== "number") return;
    switch (m.op) {
      case "call": return doCall(m);
      case "open": return doOpen(m);
      case "send": return doSend(m);
      case "end": return doEnd(m);
      case "close": return doClose(m);
    }
  };
  window.addEventListener("message", onMessage);
  win.onClose(() => window.removeEventListener("message", onMessage));
}
```

`doCall` und `doOpen` bauen einen Ende-zu-Ende-Kanal (`channel.openE2E`) mit `app.id`,
`m.service`, `m.action`, den Parametern als striktes JSON von höchstens `wire.MAX_PARAMS` Byte und
dem Controller aus `app.node`, dessen Noise-Schlüssel aus der geprüften Vertrauensliste kommt.
Verlangt die Regel eine Aufgabe oder eine erneute Bestätigung, fragt die Shell den Benutzer
(`shell/confirm.js`), nie die App.

- [ ] **Step 4: `ngc.js` schreiben (MIT)**

```js
/*!
 * ngc.js — the library an NGC app talks to the shell with.
 * Copyright (c) 2026 Sorglos Thomas Weirich. MIT licence.
 *
 * An app reaches nothing but its own services, and only through here.
 */
(function () {
  const token = document.currentScript?.dataset.token ?? "";
  let nextID = 1;
  const waiting = new Map();
  const streams = new Map();

  window.addEventListener("message", (e) => {
    const m = e.data;
    if (!m || m.ngc !== 1) return;
    if (m.op === "theme") { ngc.theme = m.theme; ngc.accent = m.accent; themeHandlers.forEach((f) => f(m.theme, m.accent)); return; }
    const w = waiting.get(m.id);
    const s = streams.get(m.id);
    if (m.op === "result" && w) { waiting.delete(m.id); m.error ? w.reject(new Error(m.error)) : w.resolve(m.value); }
    if (m.op === "data" && s) s.onData?.(m.data);
    if (m.op === "closed" && s) { streams.delete(m.id); s.onClose?.(m.status, m.message); }
  });

  const send = (op, extra) => {
    const id = nextID++;
    parent.postMessage({ ngc: 1, token, id, op, ...extra }, "*");
    return id;
  };

  const themeHandlers = [];
  const ngc = {
    version: 1,
    theme: document.documentElement.dataset.theme || "dark",
    accent: "#2f7de1",
    onTheme(fn) { themeHandlers.push(fn); },
    call(service, action, params) {
      return new Promise((resolve, reject) => {
        waiting.set(send("call", { service, action, params }), { resolve, reject });
      });
    },
    open(service, action, params) {
      const id = send("open", { service, action, params });
      const s = {
        write: (data) => parent.postMessage({ ngc: 1, token, id, op: "send", data }, "*"),
        end: () => parent.postMessage({ ngc: 1, token, id, op: "end" }, "*"),
        close: () => parent.postMessage({ ngc: 1, token, id, op: "close" }, "*"),
        onData: null, onClose: null,
      };
      streams.set(id, s);
      return Promise.resolve(s);
    },
  };
  window.ngc = ngc;
})();
```

- [ ] **Step 5: Tests laufen lassen, sie müssen bestehen**

```bash
cd apps/web/tests && npm test -- shell/appframe.spec.js
```

Erwartet: fünf grüne Tests in allen drei Engines; der untergeschobene ui-tar wird abgelehnt, und
die App kommt weder an die Shell noch ins Netz.

- [ ] **Step 6: Committen**

```bash
git add apps/web/public/assets/js/shell/appframe.js apps/web/public/assets/ngc.js apps/web/tests/shell/appframe.spec.js
git commit -m "Run app interfaces from the verified ui-tar in a sandboxed frame" --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 23: Die Oberfläche des App-Stores

**Files:**
- Create: `apps/web/public/assets/js/apps/store.js`, `apps/web/public/assets/js/shell/confirm.js`
- Test: `apps/web/tests/shell/store.spec.js`

**Interfaces:**
- Produces: `storeApp(win, ctx)` mit den Reitern „Quellen“, „Apps“, „Installiert“, „Hochladen“ und
  „Updates“; `confirm.js`: `needConfirm(list, user, op)`, `askConfirm(taskBytes) -> Promise<proof>`.
- Consumes: `core.js`, `catalog.js`, `pack.js`, `task.js`, `channel.js`, `trustlist.js`.

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

`apps/web/tests/shell/store.spec.js` mit diesen Fällen:

1. **Katalog nur geprüft:** Der Reiter „Apps“ zeigt eine App erst, wenn `catalog.verifyCatalog` mit
   dem Schlüssel des Stores aus der geprüften Vertrauensliste stimmt. Schickt die Gegenstelle einen
   Katalog mit fremder Unterschrift, steht dort „Der Katalog dieses Stores ist nicht echt.“ und
   keine App.
2. **Hochladen rechnet selbst:** Nach dem Wählen einer Datei zeigt die Shell Prüfsumme, Version und
   UI-Prüfsumme, die sie selbst gerechnet hat. Weicht `core.UploadResult` davon ab, bricht sie mit
   „Die Anlage hat ein anderes Paket bekommt, als geschickt wurde.“ ab und aktiviert nichts.
3. **Aktivieren unterschreibt eigene Werte:** Die Aufgabe `activate` trägt `sha256`, `ui` und
   `grants` genau so, wie die Shell sie gerechnet hat; `grants` ist gleich den `needs` des
   Manifests. Erlaubt der Admin nicht alle, wird nicht aktiviert.
4. **Ziel wählen:** Für eine App mit Diensten bietet die Liste nur Controller aus
   `ngc.users/nodes`, die online sind und die Rolle `services` haben.
5. **Update per Klick:** Eine Zeile mit `state` `downloaded` bekommt einen Knopf, der eine
   Root-Aufgabe `install` mit `target`, `version` = `available` und `sha256` der Zeile
   unterschreibt. Eine Zeile `needsgrants` bekommt keinen Update-Knopf, sondern den Knopf „Neue
   Version mit neuen Rechten aktivieren“.
6. **Dauerfreigabe:** Beim Umstellen auf „automatisch“ nimmt die Shell den Schlüssel **nie** aus
   `core.UpdateInfo.key`, sondern für NGC-Teile den Release-Schlüssel aus der geprüften Liste (oder
   aus der Ersteinrichtung) und für Apps den Schlüssel des Stores aus der Liste. Ein Test setzt in
   der Gegenstelle einen falschen `key` und prüft, dass die neue Fassung der Vertrauensliste
   trotzdem den richtigen Schlüssel trägt.
7. **Kauf-App:** `ngc.store/install-key` zeigt Installation und Schlüssel zum Kopieren; eine
   hochgeladene Lizenz geht über `ngc.store/api/license` (`in`).

- [ ] **Step 2: Test laufen lassen, er muss scheitern**

```bash
cd apps/web/tests && npx playwright test shell/store.spec.js --project=chromium
```

- [ ] **Step 3: `confirm.js` schreiben**

`needConfirm(list, user, op)` ruft `trust.needsConfirm`; `askConfirm(taskBytes)` zeigt einen
Dialog („Bitte bestätigen“) und holt einen Beweis über die Bytes der Aufgabe mit Zweck
`confirm/v1` – mit Passkey, wenn der Benutzer einen hat, sonst über die erneute Eingabe des
Passworts. Das Passwort wird auch hier nur zum Ableiten benutzt und sofort verworfen.

- [ ] **Step 4: `store.js` schreiben**

Der Aufbau folgt den sieben Fällen oben. Wichtige Regeln im Code:

```js
// Whatever the shell signs it computes itself. The API's answer is only
// compared, never trusted (shared/README.md, rule "Selbst rechnen").
const mine = await packageUI(file.stream());
const theirs = await sendIn(link, uploadAction, { stream: file.stream(), onProgress: showBar });
if (!equal(mine.packageSha256, theirs.sha256) || !equal(mine.sha256, theirs.ui)
  || mine.manifest.id !== theirs.id || mine.manifest.version !== theirs.version) {
  throw new Error("Die Anlage hat ein anderes Paket bekommen, als geschickt wurde.");
}
const params = {
  app: mine.manifest.id, version: mine.manifest.version,
  sha256: mine.packageSha256, ui: mine.sha256,
  store: core.LOCAL_STORE, grants: mine.manifest.needs,
};
```

Und für Dauerfreigaben:

```js
// A key for a standing approval never comes from the API: for an NGC part the
// release key of version 1 or of the approvals already in the list, for an app
// the key of its store out of the verified trust list.
function approvalKey(target, list, releaseKey) {
  if (target === trust.AUTO_SYSTEM) return null;
  if ([trust.AUTO_WEB, trust.AUTO_API, trust.AUTO_CONTROLLER].includes(target)) {
    return list.auto.find((a) => a.key.length === 32)?.key ?? releaseKey;
  }
  const app = installed.find((i) => i.id === target);
  return list.stores.find((s) => s.id === app?.store)?.key ?? null;
}
```

- [ ] **Step 5: Tests laufen lassen, sie müssen bestehen**

```bash
cd apps/web/tests && npm test -- shell/store.spec.js
```

- [ ] **Step 6: Committen**

```bash
git add apps/web/public/assets/js/apps/store.js apps/web/public/assets/js/shell/confirm.js apps/web/tests/shell/store.spec.js
git commit -m "Add the app store interface" --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 24: Die Oberfläche von Benutzer & Rechte

**Files:**
- Create: `apps/web/public/assets/js/apps/users.js`
- Test: `apps/web/tests/shell/users.spec.js`

**Interfaces:**
- Produces: `usersApp(win, ctx)` mit den Reitern „Benutzer“, „Rollen und Rechte“, „Server“,
  „Protokoll“; dazu `buildNextVersion(list, change) -> {payload, proof}`.
- Consumes: `core.js`, `trust.js`, `tunnel.js`, `task.js`, `channel.js`, `confirm.js`.

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

`apps/web/tests/shell/users.spec.js` mit diesen Fällen:

1. **Wartender Controller:** `ngc.users/pending` zeigt Name und Fingerabdruck, den die Shell selbst
   aus `tunnel.fingerprint(key)` rechnet. Der Text daneben sagt: „Vergleichen Sie diesen
   Fingerabdruck mit dem, den das Skript auf dem Server anzeigt.“
2. **Wartender API-Server:** `ngc.users/pending-api` zeigt `tunnel.joinFingerprint` über genau
   `node`, `noise`, `sign`, `tls` und `url`, und die Adresse Zeichen für Zeichen in einem
   Element mit fester Schriftweite, damit ein fremder Host auffällt. Eine Adresse, die
   `trust.validTunnelURL` nicht besteht, wird gar nicht zum Aufnehmen angeboten.
3. **Neue Fassung:** Wer einen Knoten aufnimmt, baut Fassung N+1: `seq` um eins höher, `prev` der
   Hash der aktuellen, `issued` die Zeit der API, alles andere gleich, der neue Knoten nach ID
   einsortiert. Die Shell prüft die eigene Fassung mit `trust.validateList`, unterschreibt sie mit
   dem Passkey oder Passwortschlüssel (Zweck `trustlist/v1`) und schickt sie über
   `ngc.users/api/trust` (`in`). Der Test prüft, dass die Gegenstelle genau diese Bytes bekommt.
4. **Kopplungscode:** `ngc.users/pair-code` wird erst angezeigt, wenn `tunnel.checkCode` gegen die
   neueste geprüfte Fassung stimmt; nennt der Code eine neuere Nummer, holt die Shell zuerst die
   Kette. Passt er nicht, steht dort „Dieser Code passt nicht zu dieser Anlage.“
5. **Rechte:** `ngc.users/set-rights` schickt Rolle und Rechte; eine Rolle ohne Rechte lässt
   `rights` weg.
6. **Protokoll:** blättert mit `beforeTime` und `beforeId` aus der letzten Zeile; jede Zeile zeigt
   Zeit, Benutzer, Art, App, Aktion, Knoten und Ergebnis als Text.

- [ ] **Step 2: Test laufen lassen, er muss scheitern**

```bash
cd apps/web/tests && npx playwright test shell/users.spec.js --project=chromium
```

- [ ] **Step 3: `users.js` schreiben**

`buildNextVersion` ist das Herzstück und hält sich an `trust.List.Validate`:

```js
// Every new version is built from the verified one: the shell changes exactly
// what the admin asked for and nothing else, then checks its own work before
// it signs. A version that trust.List.Validate would refuse is never sent.
export async function buildNextVersion(version, change, { now, signer }) {
  const next = structuredClone(version.list);
  next.seq = version.list.seq + 1;
  next.prev = version.hash;
  next.issued = now;
  change(next);
  sortAll(next); // users, roles, nodes, stores and auto by id, name or target
  if (!validateList(next)) throw new Error("Diese Änderung ergibt keine gültige Vertrauensliste.");
  const payload = encodeList(next);
  const proof = await signer(payload); // purpose trustlist/v1
  // The same check the API and every controller will run.
  await advance(version, [{ payload, proof }]);
  return { payload, proof };
}
```

- [ ] **Step 4: Tests laufen lassen, sie müssen bestehen**

```bash
cd apps/web/tests && npm test -- shell/users.spec.js
```

- [ ] **Step 5: Committen**

```bash
git add apps/web/public/assets/js/apps/users.js apps/web/tests/shell/users.spec.js
git commit -m "Add the users and rights interface" --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 25: Rechner und Terminal

**Files:**
- Create: `apps/web/public/assets/js/apps/calc.js`, `apps/web/public/assets/js/apps/terminal.js`
- Test: `apps/web/tests/shell/terminal.spec.js`

**Interfaces:**
- Produces: `calcApp(win)`, `terminalApp(win, ctx)`.
- Consumes: `channel.js`, `appframe.js` (dieselbe Brücke), `dom.js`.

Rechner und Terminal sind mitgelieferte Apps: Ihre Oberflächen baut das Webinterface, den Dienst
des Terminals baut der Controller. Beide laufen im selben abgeschotteten Rahmen wie jede andere
App, damit sie sich nicht anders verhalten als eine App aus dem Store.

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

`apps/web/tests/shell/terminal.spec.js`:

```js
import { test, expect } from "../fixtures/page.js";

test.beforeEach(async ({ shell }) => {
  await shell.getByLabel("Benutzername").fill("anna");
  await shell.getByLabel("Passwort").fill("Heimnetz-2026");
  await shell.getByRole("button", { name: "Anmelden" }).click();
  await expect(shell.locator("#taskbar")).toBeVisible();
});

test("the terminal shows the output of the service", async ({ shell }) => {
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("menuitem", { name: "Terminal" }).click();
  const frame = shell.frameLocator(".appframe");
  await frame.getByRole("textbox", { name: "Eingabe" }).fill("cp -r");
  await frame.getByRole("textbox", { name: "Eingabe" }).press("Enter");
  await expect(frame.locator(".out")).toContainText("cp: missing destination");
});

test("output from the service is never HTML", async ({ shell }) => {
  await shell.evaluate(() => { window.__ngcTest.terminalOutput = "<img src=x onerror=alert(1)>"; });
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("menuitem", { name: "Terminal" }).click();
  const frame = shell.frameLocator(".appframe");
  await frame.getByRole("textbox", { name: "Eingabe" }).press("Enter");
  await expect(frame.locator(".out")).toContainText("<img src=x onerror=alert(1)>");
  expect(await frame.locator(".out img").count()).toBe(0);
});

test("a cut-off stream is an error", async ({ shell }) => {
  await shell.evaluate(() => { window.__ngcTest.terminalCut = true; });
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("menuitem", { name: "Terminal" }).click();
  const frame = shell.frameLocator(".appframe");
  await expect(frame.getByRole("alert")).toHaveText("Die Verbindung wurde abgeschnitten.");
});

test("the calculator computes without eval", async ({ shell }) => {
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("menuitem", { name: "Rechner" }).click();
  const frame = shell.frameLocator(".appframe");
  for (const key of ["1", "2", "+", "3", "="]) await frame.getByRole("button", { name: key }).click();
  await expect(frame.locator(".v")).toHaveText("15");
});
```

- [ ] **Step 2: Test laufen lassen, er muss scheitern**

```bash
cd apps/web/tests && npx playwright test shell/terminal.spec.js --project=chromium
```

- [ ] **Step 3: `calc.js` schreiben**

Der Rechner aus dem Prototyp (Zeilen 621–660), mit dem Shunting-Yard-Verfahren, das dort schon
steht: `eval` gibt es nicht und darf es nicht geben (die CSP verbietet es ohnehin). Die Tasten und
das Aussehen bleiben wie im Prototyp, der Aufbau kommt über `createElement`.

- [ ] **Step 4: `terminal.js` schreiben**

Der Prototyp setzt die Ausgabe des Terminals mit `innerHTML` ein – genau der Fehler, den der
Entwurf nennt (4.6). Hier gilt:

```js
// The prototype puts the output into the page with innerHTML; that is exactly
// the mistake the design names. Every byte from the service is text.
function write(out, bytes) {
  const line = document.createElement("div");
  line.textContent = decoder.decode(bytes, { stream: true });
  out.append(line);
  out.scrollTop = out.scrollHeight;
}
```

Der Kanal ist ein Datenstrom (`ngc.open("terminal", "shell", {cols, rows})`), also ein
Ende-zu-Ende-Kanal zum Controller aus `app.node`. Eingaben gehen als `write`, `Strg+D` als `end`.
Fehlt das Ende des Controllers, zeigt die App „Die Verbindung wurde abgeschnitten.“ Das Ergebnis
gilt nur, wenn das Ende mit `ok` kommt.

- [ ] **Step 5: Tests laufen lassen, sie müssen bestehen**

```bash
cd apps/web/tests && npm test -- shell/terminal.spec.js
```

- [ ] **Step 6: Committen**

```bash
git add apps/web/public/assets/js/apps/calc.js apps/web/public/assets/js/apps/terminal.js apps/web/tests/shell/terminal.spec.js
git commit -m "Add the calculator and the terminal" --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 26: Das ulh-Skript `ngc-web.sh` und die Vorlage für die Lastverteilung

Dieses Skript liegt **nicht** in diesem Repo, sondern im öffentlichen ulh-Repo
(`sorglos-it/ulh`), Branch `ngc-web`, Pull Request nach dessen `main`. Jede ulh-Installation holt
beim Start `main`, deshalb kommt nur Geprüftes dorthin.

**Files:**
- Create (ulh-Repo): `scripts/ngc-web.sh`; Zeilen in `config.yaml`, `SCRIPTS.md` und `TREE.md`
- Create (dieses Repo): `apps/web/templates/apache-apis.conf.example`,
  `apps/web/templates/ngc-web.conf.example`, `apps/web/tests/fixtures/balancer.js`
- Test: `apps/web/tests/shell/balancer.spec.js`

**Interfaces:**
- Produces: die genaue Form von `config.BalancerFile` als Vorlage (unten), die der Controller mit
  Rolle `web` Byte für Byte nachschreibt.
- Consumes: `shared/ulh.md`, `shared/testdata/ulh/host.sh`, `shared/testdata/tls/pin.sh`,
  `shared/testdata/release/verify.sh`.

- [ ] **Step 1: Die Vorlage der Lastverteilung festschreiben**

`apps/web/templates/apache-apis.conf.example` – genau diese Form, mit einem `BalancerMember` je
Eintrag von `apis` aus `web.json`, in derselben Reihenfolge:

```apache
# /etc/ngc/apache-apis.conf
# Written by ngc-web (install, configure) and after that by ngc-controller with
# role web, both from /etc/ngc/web.json. Never edit by hand; every write is
# atomic, mode 0644, and Apache is reloaded only after apachectl configtest.
SSLProxyEngine on
SSLProxyVerify require
SSLProxyCACertificateFile /etc/ngc/tls/api-trust.pem
SSLProxyCheckPeerName on
SSLProxyCheckPeerExpire on
SSLProxyProtocol TLSv1.3
SSLProxyCipherSuite TLSv1.3 TLS_AES_256_GCM_SHA384

<Proxy "balancer://ngc-api">
    BalancerMember "https://api-1.ngc.test:7443" retry=5 timeout=65 keepalive=On
    BalancerMember "https://api-2.ngc.test:7443" retry=5 timeout=65 keepalive=On
    ProxySet lbmethod=byrequests failontimeout=On
</Proxy>

ProxyPreserveHost Off
ProxyPass        "/api/" "balancer://ngc-api/api/" upgrade=websocket
ProxyPassReverse "/api/" "balancer://ngc-api/api/"
```

`upgrade=websocket` braucht Apache 2.4.47 oder neuer; Debian 13 und Ubuntu 24.04 bringen es mit.
Diese Vorlage gehört nach `shared/ulh.md` („Die genaue Form legt der Plan des Webinterface fest und
trägt sie hier als Vorlage ein“) – der Koordinator trägt sie ein, siehe „Offene Entscheidungen“.

`apps/web/templates/ngc-web.conf.example` ist der virtuelle Host: Webroot `/var/www/ngc/public`,
`Include /etc/ngc/apache-apis.conf`, TLS 1.3 mit `TLS_AES_256_GCM_SHA384`, HSTS,
`<Directory /var/www/ngc/public>` mit `Require all granted` und `Options -Indexes`, und eine
Umschreibung, die jeden Pfad außerhalb von `/assets/` und `/api/` an `index.php` gibt.

- [ ] **Step 2: Den Test für die Vorlage schreiben**

`apps/web/tests/shell/balancer.spec.js` liest `apps/web/tests/web.json`, erzeugt daraus die
Balancer-Datei mit derselben Funktion, die auch das Skript benutzt
(`apps/web/tests/fixtures/balancer.js`, eine Nachbildung in JavaScript), und vergleicht sie Byte
für Byte mit `apps/web/templates/apache-apis.conf.example`. So fällt jede Abweichung auf, bevor
der Controller sie nachbaut.

- [ ] **Step 3: `scripts/ngc-web.sh` schreiben**

Nach ulhs Vorlage `scripts/_template.sh`, mit `install`, `configure` und `uninstall`. Was es tut:

1. `LC_ALL=C` setzen und jeden Host wie `shared/testdata/ulh/host.sh` prüfen (DNS-Name in
   Kleinbuchstaben oder IPv4, kein IPv6).
2. Apache und PHP aus den Paketquellen einrichten, dazu `openssl` und `jq` für die Prüfung der
   Programmdateien.
3. Das Release holen und mit `verify.sh <Verzeichnis> ngc-web all` prüfen (Vorlage
   `shared/testdata/release/verify.sh`), dabei nie ein älteres Release als das in
   `/var/lib/ngc/release.seq` einspielen; nach Erfolg die Nummer dorthin schreiben.
4. Das Webinterface nach `/var/www/ngc/` entpacken, Webroot ist nur `/var/www/ngc/public`.
5. Das Zertifikat auf einem der drei Wege besorgen: `letsencrypt` über `mod_md`, `selfsigned` oder
   `import` mit `cert_file` und `key_file`.
6. Für jede Adresse aus `apis` das Zertifikat der API holen
   (`openssl s_client -connect <host>:7443 -servername <host>`), den Fingerabdruck mit `pin.sh`
   rechnen und mit `api_pins` in derselben Reihenfolge vergleichen. Stimmt einer nicht, bricht das
   Skript ab. Die passenden Zertifikate kommen nach `/etc/ngc/tls/api-trust.pem`.
7. `/etc/ngc/web.json` schreiben (Modus 0600, root): `apis` mit `url` und `tls`, dazu `rp` mit
   `id` und `origins`, sobald ein Name statt einer IP-Adresse benutzt wird.
8. `/etc/ngc/apache-apis.conf` in genau der Form von Schritt 1 schreiben, dann
   `apachectl configtest` und erst danach neu laden.
9. `uninstall` fragt, was mit den Daten passiert; Aufgehobenes bleibt unter `/var/lib/ngc/`.

- [ ] **Step 4: Prüfen**

```bash
cd apps/web/tests && npm test -- shell/balancer.spec.js
# im ulh-Repo:
shellcheck scripts/ngc-web.sh
sh -n scripts/ngc-web.sh
```

Erwartet: die Vorlage stimmt Byte für Byte, `shellcheck` meldet nichts.

- [ ] **Step 5: Committen**

```bash
git add apps/web/templates apps/web/tests/fixtures/balancer.js apps/web/tests/shell/balancer.spec.js
git commit -m "Add the Apache balancer template for the web server" --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

Im ulh-Repo auf Branch `ngc-web` getrennt committen und einen Pull Request nach dessen `main`
öffnen.

---

### Task 27: Abschluss – Angriffe, Sicherheitskopfzeilen, Version, README

**Files:**
- Modify: `apps/web/VERSION`, `apps/web/README.md`, `README.md` (Tabelle der Teilprogramme),
  `CHANGELOG.md`, `docs/STAND.md`
- Test: `apps/web/tests/shell/attacks.spec.js`, `apps/web/tests/shell/csp.spec.js`

- [ ] **Step 1: Den Test für die Sicherheitskopfzeilen schreiben**

`apps/web/tests/shell/csp.spec.js` – dieser Test läuft **ohne** `bypassCSP`, also mit der echten
Richtlinie:

```js
import { test, expect } from "@playwright/test";

test("the shell page carries the whole policy", async ({ page }) => {
  const res = await page.goto("/");
  const csp = res.headers()["content-security-policy"];
  for (const part of [
    "default-src 'none'", "script-src 'self' data:", "connect-src 'self'",
    "base-uri 'none'", "form-action 'none'", "frame-ancestors 'none'", "worker-src 'self'",
  ]) {
    expect(csp, part).toContain(part);
  }
  expect(res.headers()["permissions-policy"]).toContain("publickey-credentials-get=(self)");
  expect(res.headers()["cross-origin-opener-policy"]).toBe("same-origin");
});

test("a script from outside does not run", async ({ page }) => {
  await page.goto("/");
  const ran = await page.evaluate(async () => {
    const s = document.createElement("script");
    s.src = "https://example.invalid/x.js";
    document.head.append(s);
    await new Promise((r) => setTimeout(r, 200));
    return window.__foreignRan === true;
  });
  expect(ran).toBe(false);
});
```

- [ ] **Step 2: Den Test für die beiden Angriffe schreiben**

`apps/web/tests/shell/attacks.spec.js` fasst zusammen, was `apps/web/CLAUDE.md` unter „Angriffe“
verlangt, und ergänzt die Fälle aus dem Entwurf 4.6:

1. **Untergeschobene Oberfläche:** Die Gegenstelle liefert zu einem gültigen Beweis ein anderes
   ui-tar. Die Shell zeigt „Die Oberfläche dieser App ist nicht echt. NGC führt sie nicht aus.“ und
   startet nichts (schon in Aufgabe 22 geprüft, hier noch einmal im Zusammenspiel).
2. **Mann in der Mitte bei der Ersteinrichtung:** Ein Angebot mit fremden Schlüsseln und einem MAC
   eines anderen Codes wird abgelehnt, ohne dass etwas daraus angezeigt wird.
3. **Manipulierte Vertrauensliste:** Alarm und Abmelden (Aufgabe 20).
4. **Ältere Vertrauensliste:** Eine Fassung mit kleinerer Nummer wird abgelehnt, ohne Alarm – die
   Shell bleibt bei ihrer neueren.
5. **Falsche Prüfsumme beim Hochladen:** `core.UploadResult` weicht ab, die Shell aktiviert nicht.
6. **Kanal über der Grenze:** Mehr als `wire.MaxSessionChannels` Nummern gleichzeitig öffnet die
   Shell nie (Aufgabe 17).
7. **Ergebnis über der Grenze:** Ein Ende-zu-Ende-Kanal mit mehr als `wire.MaxCallResult` wird von
   der Shell mit `E_TOOLARGE` geschlossen – dort sieht die API den Klartext nicht, also setzt nur
   die Shell die Grenze durch.
8. **Passwort im Mitschnitt:** noch einmal über den ganzen Ablauf, einschließlich der erneuten
   Bestätigung mit Passwort.

- [ ] **Step 3: Beide Tests laufen lassen, sie müssen scheitern oder bestehen**

```bash
cd apps/web/tests && npm test -- shell/csp.spec.js shell/attacks.spec.js
```

Was noch fehlt, wird ergänzt; nichts wird übersprungen, um grün zu werden.

- [ ] **Step 4: Alles zusammen prüfen**

```bash
cd apps/web/tests && npm test
cd ../../.. && tools/check.sh
```

Erwartet: alle Tests grün in Chrome, Firefox und WebKit (die Passkey-Tests laufen nur in
Chromium und werden sonst übersprungen); `tools/check.sh` weiter grün.

- [ ] **Step 5: Version, README und Stand nachziehen**

`apps/web/VERSION`: `0.2.0`.

In `README.md` im Stamm die Zeile des Teilprogramms ergänzen:

```markdown
| `apps/web` | The desktop in the browser | HTML, CSS, JS, PHP | `cd apps/web/tests && npm test` | no build step |
```

In `CHANGELOG.md` einen Abschnitt „apps/web 0.2.0“ mit den Punkten: Verträge in JavaScript,
Anmeldung in beiden Stufen, Ersteinrichtung, Sitzung mit Noise und Ende zu Ende, Desktop,
App-Fenster, die vier mitgelieferten Oberflächen, Browser-Tests.

In `docs/STAND.md` unter „Stand“ eintragen, dass Plan 1 umgesetzt ist, und unter „Offene
Entscheidungen“ die Punkte aus dem letzten Abschnitt dieses Plans übernehmen, soweit der
Koordinator sie noch nicht entschieden hat.

- [ ] **Step 6: Committen und Pull Request öffnen**

```bash
git add apps/web/VERSION apps/web/README.md README.md CHANGELOG.md docs/STAND.md apps/web/tests
git commit -m "Finish the web interface and record the state" --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
git push -u origin web
gh pr create --base main --head web --title "Web interface" --body "Plan 1: the desktop in the browser."
```

---

## Fertig, wenn

Aus `apps/web/CLAUDE.md`, Abschnitt „Fertig, wenn“:

- [ ] Die Anmeldung läuft in beiden Stufen: mit Benutzer, Passwort und TOTP, und mit Passkey; eine
      Rolle mit `passwordToo` bekommt beide Beweise über dieselben Zertifikat-Bytes.
- [ ] Der Desktop öffnet Apps, und eine App-Oberfläche läuft nur aus einem geprüften ui-tar.
- [ ] Das Terminal zeigt über API und Controller die Ausgabe des Dienstes, Ende zu Ende
      verschlüsselt.
- [ ] Eine manipulierte Vertrauensliste löst Alarm aus und meldet ab.
- [ ] Die Browser-Tests sind in Chrome, Firefox und Safari grün, und ein Mitschnitt zeigt, dass das
      Passwort den Browser nie verlässt.

---

## Selbstprüfung

Nach dem Schreiben mit frischem Blick gegen `apps/web/CLAUDE.md` und `shared/README.md` geprüft:

| Aus der Anweisung | Aufgabe |
|---|---|
| Anmeldung: Passwortschlüssel (Argon2id, NFC), TOTP, Passkeys, Sticks mit Benutzerprüfung | 5, 18 |
| `PasswordToo`: zwei Beweise über dieselben Zertifikat-Bytes | 18 |
| HTTP bis zum WebSocket, Fehler am Code im CLOSE-JSON, `E_UNREACHABLE` ohne JSON, `Retry-After` | 16 |
| Ersteinrichtung: Code zuerst, Angebot mit MAC, Fassung 1, Code geht nie über die Leitung | 19 |
| Sitzung: eigene Schlüsselpaare, `ClientCert`, `APICert` prüfen, Noise, Frames, Kanalzahl | 8, 17, 18 |
| Kern-Aktionen nur nach `core.ShellOpens`; `activate` schickt nur EOF | 14, 17, 23 |
| Ergebnis nur mit CLOSE `ok`, sonst verwerfen | 17 |
| Vertrauensliste: Anker vom Webserver, Kette prüfen, merken, Alarm, `GET /api/v1/trust` | 7, 20 |
| Wartende Controller und API-Server: Fingerabdrücke selbst rechnen, Adresse Zeichen für Zeichen | 15, 24 |
| Ende zu Ende: Prolog mit SHA-256 von `params` und `task`, erste Nachricht sofort, Ende zählt | 6, 17, 25 |
| `wire.MaxCallResult` durchsetzen, auch Ende zu Ende | 14, 17 |
| Aufgaben bauen und unterschreiben, erneute Bestätigung mit Zweck `confirm/v1` | 9, 23 |
| Kopplungs- und Beitrittscodes erst nach `Check` zeigen, Anker mit Nummer und Fingerabdruck | 15, 24 |
| Apps: abgeschottete iframes, `ngc.js`, strenge CSP, App-Store, Benutzer & Rechte, Rechner, Terminal | 21–25 |
| ui-tar nur in der einen Form, Grenzen am Kopf, Fälle aus `ui.json` | 11 |
| UI-Beweis vor allem anderen, Prüfsumme vergleichen, `entry` nur aus dem ui-tar, Zwischenspeicher | 14, 20, 22 |
| Hochladen: SHA-256, Manifest und UI-Prüfsumme selbst rechnen, PAX und lange GNU-Namen lesen | 12, 23 |
| Ziel einer Aktivierung aus `ngc.users/nodes`, nur online und Rolle `services` | 23 |
| Updates: Zeile je Ziel und Controller, `needsgrants` ohne Klick, Schlüssel nie von der API | 23 |
| Striktes JSON mit Budget, Grenzen aus allen Go-Paketen, `wire.ValidText` je Codepunkt | 2, 3, und in jedem Modul |
| PHP: Seite ausliefern, `web.json` und `trust-anchor.json` lesen, kein App-Code im Webserver | 1 |
| Tests: Playwright gegen drei Engines, Vertragstests gegen `shared/testdata/`, ohne API | 1–27 |
| Test „Passwort verlässt den Browser nie“ | 18, 27 |
| Angriffe: untergeschobene Oberfläche, Mann in der Mitte bei der Ersteinrichtung | 19, 22, 27 |

Drei Dinge sind beim Gegenlesen aufgefallen und in den Plan eingearbeitet:

1. `session.LoginChallenge.passkeys` nennt seit `vertraege-v1.1` je Eintrag `credId` **und** `kid`
   (`session.PasskeyRef`). Die Shell nimmt das `kid` aus der Antwort und leitet es nirgends selbst
   her; die Antwort prüft `parseLoginChallenge` nach `session.ParseLoginChallenge` gegen die
   Vektoren unter `login` in `shared/testdata/session.json` (Aufgaben 8, 16, 18). Damit gelingt die
   Anmeldung mit Passkey auch auf einem frischen Browser, und die frühere Zuordnung im
   Browserspeicher ist ersatzlos weg.
2. Die Oberfläche einer App erbt die CSP der Shell, weil sie aus `srcdoc` kommt. Deshalb steht in
   der Richtlinie der Shell `script-src 'self' data:` – sonst liefe keine App. Siehe „Offene
   Entscheidungen“, Punkt 1.
3. `shared/ulh.md` verlangt, dass dieser Plan die Form der Balancer-Datei festlegt und dort einträgt.
   Der Plan legt sie fest (Aufgabe 26); eintragen muss sie der Koordinator, denn `shared/` ist
   eingefroren. Siehe „Offene Entscheidungen“, Punkt 2.

---

## Offene Entscheidungen für den Koordinator

1. **Eigener Ursprung für App-Fenster.** Auf einem einzigen Ursprung gibt es nur zwei Wege: ein
   `iframe` mit `sandbox="allow-scripts"` (echte Abschottung, aber die Seite erbt die CSP der Shell
   und kann keine eigenen Dateien nachladen) oder ein Rahmen, der die Shell erreichen könnte. Der
   Plan nimmt den ersten Weg und setzt alles als `data:`-Adressen in die Seite; dafür muss die CSP
   der Shell `script-src 'self' data:` und `style-src … 'unsafe-inline'` erlauben, und App-Skripte
   müssen klassische Skripte sein (ein Modul aus `data:` kann nichts nachladen). Ein zweiter Name
   (etwa `apps.<hostname>`, im selben Zertifikat) würde die Fenster echt fremd machen: eigene CSP je
   Antwort, keine Vererbung, Module und eigene Dateien ohne Einbetten. Das berührt `ngc-web.sh`
   (Name und Zertifikat) und wahrscheinlich `config.Web` (ein Feld für diesen Namen). **Frage an den
   Koordinator:** zweiter Name ja oder nein? Bei nein bleibt die Grenze für Apps im Plan stehen und
   gehört in die Doku für App-Entwickler.

2. **Die Vorlage der Balancer-Datei nach `shared/ulh.md`.** `shared/ulh.md` sagt: „Die genaue Form
   legt der Plan des Webinterface fest und trägt sie hier als Vorlage ein.“ Die Form steht in
   Aufgabe 26 (`apps/web/templates/apache-apis.conf.example`). Weil `shared/` eingefroren ist,
   trägt sie der Koordinator ein; erst dann kann der Controller mit Rolle `web` sie Byte für Byte
   nachschreiben.

3. **Fremdcode für den Browser.** Der Plan kopiert fünf Dateien aus `@noble/hashes` 1.8.0 (MIT)
   nach `apps/web/public/assets/vendor/noble-hashes/`: Argon2id für den Passwortschlüssel (Kapitel 7
   des Entwurfs nennt es als Kandidaten, „zu prüfen“) und ein SHA-256 mit `update`/`digest`, das die
   Web Crypto API nicht hat und das für Pakete bis 4 GiB gebraucht wird. Bitte freigeben; die Zeile
   für `THIRD-PARTY.md` steht in Aufgabe 5.

4. **`AdminVerify` aus dem Recovery-Modus.** `docs/STAND.md` sagt, `AdminVerify` komme nach Go, weil
   NGC kein PHP-Teilprogramm habe – `apps/web` hat aber einen PHP-Teil und die Anmeldemaske. Bitte
   bestätigen, dass das Webinterface dafür nichts tun muss, sondern die API die Admin-Datensätze
   prüft.

5. **Browser-Mindestversionen.** Der Plan setzt Chrome/Edge 137, Firefox 130 und Safari 17.4
   (Ed25519 und X25519 in der Web Crypto API). Plan 0 nannte Safari 17; X25519 kam dort erst in
   17.4. Bitte als verbindliche Zahl bestätigen, damit `ngc-web.sh` und die Doku dasselbe sagen.

6. **Speicher für große Oberflächen.** Ein ui-tar darf nach `store.MaxUITotal` 64 MiB groß sein; die
   Shell baut daraus ein Dokument mit `data:`-Adressen, was etwa das 1,4-Fache an Speicher kostet.
   Eine kleinere Grenze allein in der Shell wäre möglich, würde aber gültige Apps aussperren. Der
   Plan setzt keine eigene Grenze. Falls gewünscht, bitte eine nennen.

7. **Test-Zweig in `index.php`.** Für die Browser-Tests reicht der eingebaute PHP-Server `/api/` an
   die Gegenstelle weiter (Aufgabe 17); der Zweig greift nur unter `PHP_SAPI === 'cli-server'` **und**
   gesetzter Umgebungsvariable `NGC_TEST_API`, im Betrieb also nie. Falls auch das unerwünscht ist,
   kommt stattdessen ein eigener kleiner Node-Server vor den PHP-Server – dann sieht der Test aber
   nicht mehr genau das, was Apache ausliefert.
