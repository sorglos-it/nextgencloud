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
