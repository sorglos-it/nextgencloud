# NextGenCloud

[![License](https://img.shields.io/github/license/sorglos-it/nextgencloud)](LICENSE)
[![Donate](https://img.shields.io/badge/Donate-PayPal-blue)](https://www.paypal.com/donate/?hosted_button_id=6CDEVZGJWTNQQ)

NGC is an open alternative to Synology's DSM: a desktop in the browser, where apps run in
containers. The web interface, the API, the database and the server controller are separate
parts; they can sit on one machine or on several.

Every claim below names the point in [`docs/vertraege-v1.2.md`](docs/vertraege-v1.2.md) that
measured it. Anything not measured is listed as not measured.

## What runs today, and what measured it

| What | Measured | Point |
| --- | --- | --- |
| A release is signed with NGC's own release key and verified on the target machine | 17.10.2026, three parts, `release ok` each, no placeholder left in any script | 57 |
| Install on real Debian 13 (Apache 2.4.69, PHP 8.4, podman 5.4.2) | 16.10.2026, `https://ngc.test/` answers `http=200` with NGC's own page; `GET /api/v1/setup` goes through Apache | 56 |
| The setup page in a real browser, over HTTPS | 17.10.2026, the page reads its location from `/config.json`; over plain `http://` it refuses and asks for `https://` | 58 |
| Setup and sign-in: a user enters the one-time code, creates the first administrator and stands on the desktop | 17.10.2026, Chromium; the session is one cookie, `httpOnly` and `secure` | 59 |
| Pairing the controller: the trust anchor is written, the service runs, the tunnel stands | 17.10.2026, pairing `RC=0`, `/etc/ngc/trust-anchor.json` appears, tunnel `seconds=122` | 61 |
| A failing channel no longer takes the whole server down: thirteen catch points, the count is reported once a minute, and the line carries the panic's type, never its value | 16.10.2026, 43 packages ok, 0 red | 55 |
| Container hardening: eleven podman switches, line by line from the source, both mounts with `nosuid,nodev`, `--pids-limit 512` from a measured need | 13.10.2026, 145 cases, six mutants dead, one control alive. **Measured is that NGC sends the switches — not that podman honours them.** | 51 |
| API and controller talk to each other over the real path: TLS 1.3, WebSocket, Noise_KK, numbered channel | 13.10.2026, same hash on both sides | 44 |
| The app store tooling: pack, encrypt, catalog, release, and a store the API really fetched from | 13.10.2026, `store ok`, two packages fetched | 45 |
| The small case: one database file and one key per program, SQLite with AES-256-XTS | 13.10.2026 | 47 |

**Where this was measured.** Most rows above were measured on a single Debian 13 machine that
carried all roles at once (`ngc.test`). **That machine has been switched off since 24.10.2026.**
The measurements stand — they were taken while it ran, and a machine being off does not make a
past measurement wrong. What is gone is the ability to look again: re-measuring the one-machine
case needs a machine for it. The installation that runs today has six machines and is a
different case. See `docs/alte-vm-verweise.md`.

## What does not work yet, or is not measured

- **There is no official NGC app store.** The store key is still a placeholder, so every
  installation has to name its own store. The installer stops with a clear sentence instead of
  writing the placeholder (points 56, 57).
- **Whether apps actually run** — containers, terminal — is the next measuring point. So far only
  the tunnel is shown to stand (point 61).
- **Passkeys are not measured.** The first administrator is created with a password, which is
  what the contract foresees; the interface recommends passkeys itself (points 58, 59).
- **The case with several machines (MariaDB/Galera) has never been run.** Everything measured so
  far is the small case: one machine, SQLite (point 47).
- **The secret release key belongs on a hardware stick** (design 4.8). It currently lies in
  `/root/ngc-keys` on one machine; whoever is root there can sign (point 57).
- **The sign-in was measured with a self-signed certificate** and a browser that accepted it for
  that run. What a user with a Let's Encrypt certificate sees is not measured (point 59).

## Layout

| Folder | Contents |
| --- | --- |
| `shared/` | The contracts between the parts: Go packages and test data |
| `apps/server-api` | `ngc-api`: sessions, rights, relaying to the controllers |
| `apps/server-controller` | `ngc-controller`, on every server: tunnel, services, updates |
| `apps/server-db` | Database: MariaDB and SQLite behind `dbapi`, migrations |
| `apps/web` | The web interface: the desktop in the browser |
| `apps/cli` | `ngc-store`: packs apps, signs catalogs, licences and releases |
| `tools/` | Build and check scripts |

`shared/` is frozen on the tag `vertraege-v1.1`. All four parts are built against the same
interfaces, so a change there changes all of them at once; everything that would need such a
change is collected in `docs/vertraege-v1.2.md` for one cut instead of being changed quietly.

## Checking

`tools/check.sh` does format, vet, the Go tests and the build for linux/amd64 and arm64. The
browser tests of the web interface are not part of it and run on their own:

```
cd apps/web/tests && npm install && npm run install-browsers && npm test
```

## Reading along

- [`docs/vertraege-v1.2.md`](docs/vertraege-v1.2.md) is the decision book: 61 points, each saying
  what was measured and what stayed open.
- [`docs/STAND.md`](docs/STAND.md) collects the traps of this environment.

## License

AGPL-3.0 ([`LICENSE`](LICENSE)) – Copyright (c) 2026 Sorglos Thomas Weirich. The contracts in
`shared/` ([`shared/LICENSE`](shared/LICENSE)) and the app library `ngc.js` are MIT, so closed and
paid apps are possible too. Third-party code: [`THIRD-PARTY.md`](THIRD-PARTY.md).

## Donate via PayPal

If this project helps you: [Donate via PayPal](https://www.paypal.com/donate/?hosted_button_id=6CDEVZGJWTNQQ)
