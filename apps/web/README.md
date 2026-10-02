# ngc-web

[![Version](https://img.shields.io/badge/version-0.2.0-brightgreen.svg)](VERSION)
[![License: AGPL-3.0](https://img.shields.io/badge/license-AGPL--3.0-blue.svg)](../../LICENSE)
[![No build step](https://img.shields.io/badge/build-none-lightgrey.svg)](#start-in-3-steps)
[![Browsers](https://img.shields.io/badge/browsers-Chrome%20%7C%20Firefox%20%7C%20Safari-informational.svg)](tests/playwright.config.js)

The NGC desktop in the browser: login, session, windows, apps. HTML, CSS, JavaScript and PHP — no
framework, no build step, and npm only under `tests/`.

| Folder | What is in it |
| --- | --- |
| `public/` | web root; `index.php` is the only entry point, `assets/` holds CSS, JS and vendored code |
| `classes/` | PHP: reads `/etc/ngc/web.json` and `/etc/ngc/trust-anchor.json`, sends the headers |
| `templates/` | the page skeletons (the desktop, an app window), plus `apache-vhost.conf` — the web server, copy it and fill in one name — and `apache-apis.conf.example`, the form `ngc-web` writes for the API servers |
| `tests/` | Playwright; npm lives only here. `tests/php/` checks the PHP side and needs no npm |

## Start in 3 steps

1. `cd apps/web/tests && npm install && npm run install-browsers`
2. `npm test`, then `php php/config-test.php` — the contracts check themselves against
   `shared/testdata/`, no API needed. Is php installed but not on the PATH? Then write
   `NGC_PHP=<path to php>` into `tools/local.env` (`tools/local.env.example` shows how);
   `npm test` and `tools/check.sh` both read it. Two runs at once: set `NGC_WEB_TEST_PORT`.
3. On a server, ulh does the rest: `scripts/ngc-web.sh` in the ulh repository. By hand instead:
   `templates/apache-vhost.conf` carries six commands at the top, in order, with one name to fill
   in. Apache's DocumentRoot is `public/` and nothing else is reachable from outside.

## What it holds to

- **Nothing from outside becomes HTML.** `textContent` everywhere; the one exception is the
  bootstrap of an app window (`public/assets/js/shell/frame-boot.js`), which writes only bytes a
  signature vouches for and only in a document with an origin of its own.
- **One entry point.** `public/index.php` answers `/`, `/config.json` and `/frame.html`; everything
  else is a 404, and only GET is answered.
- **The policy is measured, not promised.** `tests/shell/csp.spec.js` checks every directive of
  `classes/Page.php` and then makes the browser refuse what it forbids.
- **The files have walls too, and not the page's.** Apache serves everything below
  `public/assets/`, so `classes/Page.php` never sees those answers; `templates/apache-vhost.conf`
  gives them `nosniff`, `Cross-Origin-Resource-Policy` and a policy written for a file instead of
  for a document. `tests/mock/api.js` — the harness in front of the PHP test server, not PHP itself
  — reads the values out of that file, so one case measures the template and the browser at once.
- **The password never leaves the browser**, at the login and at a second confirmation alike
  (`tests/shell/login.spec.js`, `tests/shell/attacks.spec.js`).

## What no test here can measure: the web server itself

There is no Apache in this repository, so `templates/apache-vhost.conf` is the one thing that is
reasoned out and not measured. Nine commands close that gap, and they take about two minutes on the
first machine that has an Apache. `HOST` is the name you filled in; `-k` is for the self-signed
certificate of step 2. The template carries the same list, so whichever you read says the same
thing.

| Run this | Expected | What it decides |
| --- | --- | --- |
| `apachectl configtest` | `Syntax OK` | Apache accepts the file at all — syntax, `Redirect 308`, `FallbackResource`, every `Header` line |
| `curl -sI http://HOST/irgendwas` | `308`, `Location: https://HOST/irgendwas` | the redirect works and keeps the path |
| `curl -sI http://HOST/.well-known/acme-challenge/x` | **not** a 308 (a 404 from `/var/www/html`) | the one path Let's Encrypt needs stays free, so renewal keeps working |
| `curl -skD - -o /dev/null https://HOST/assets/css/shell.css \| grep -ciE '^(content-security-policy\|x-content-type-options\|cross-origin-resource-policy\|strict-transport-security):'` | `4` | the asset block is in effect and each header arrives exactly once |
| `curl -skD - -o /dev/null https://HOST/assets/js/gibtesnicht.js \| grep -icE '^(content-security-policy\|cross-origin-resource-policy):'` | `2` | **the one that matters most.** One of each, both out of the assets block. More than 2 means PHP wrote that answer too, so `Header always set` put a second header beside `Page::headers`' — and two `Cross-Origin-Resource-Policy` are neither `same-origin` nor `same-site`, so a browser applies **neither**. That is the hole `FallbackResource disabled` closes |
| `curl -skD - -o /dev/null https://HOST/assets/js/gibtesnicht.js \| grep -icE '^(cross-origin-opener-policy\|referrer-policy\|permissions-policy\|x-powered-by):'` | `0` | the other half of the same question: these are headers only `Page::headers` sends, so one of them here means `FallbackResource disabled` did not take and PHP answered after all. A **find**, not a line to adjust |
| `curl -skD - -o /dev/null https://HOST/irgendwas-das-es-nicht-gibt \| grep -ci '^strict-transport-security:'` | `1` | HSTS reaches an answer **outside** `/assets/` as well — that is what the host-wide line in the virtual host is for. `0` means an Apache error page still goes out without it; more than `1` means `always set` added beside `Page::headers`' own (same value, so no hole) |
| `openssl s_client -connect HOST:443 -tls1_3 -ciphersuites TLS_AES_128_GCM_SHA256 </dev/null` | a handshake **failure** | the `SSLCipherSuite TLSv1.3 …` line really narrows. If it succeeds, Apache read it as the TLS-1.2 list and it narrows nothing. `openssl s_client -connect HOST:443 </dev/null \| grep -i Cipher` must name `TLS_AES_256_GCM_SHA384` |
| `curl -sk "https://HOST/?ngc-probe=GEHEIMNIS" -o /dev/null` then `grep -c GEHEIMNIS /var/log/apache2/ngc-web-access.log` | `0` | the access log writes down no user input. Anything else means the format fell back to one that logs the request line, and then the log is a place secrets land |

Two more, if you want the whole picture: `curl -skI https://HOST/assets/js/` must be `403` and carry
the same four headers, and `curl -skI https://HOST/` must carry `Page::headers`' own policy —
unchanged, with `script-src 'self'` and `connect-src 'self'` in it.

Whatever comes out, write it into `docs/STAND.md`: it is the last unmeasured corner of this
interface.

## Author

Sorglos Thomas Weirich

## Donate via PayPal

[Donate via PayPal](https://www.paypal.com/donate/?hosted_button_id=6CDEVZGJWTNQQ)
