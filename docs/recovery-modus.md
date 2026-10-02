# Recovery-Modus — Vorgabe für Claude Code

Gilt für das Projekt, in dem diese Datei liegt. Der Recovery-Modus ist ein Bestandteil dieses Projekts: kein eigenes Projekt, kein eigenes Repo, keine Umbenennung von Bestehendem.
Vollständiges Konzept, Handlungsplan und Handbuch: https://claude.ai/code/artifact/2d4e9e59-e944-4c14-b811-98f69d02fa16

Diese Datei ist verbindlich. In CLAUDE.md des Projekts steht ein Verweis darauf. Bevor an Login, Admin-Rechten, Benutzerdatenbank, Backup, Installation oder Boot gearbeitet wird, diese Datei lesen. Änderungen an dieser Datei nur nach Rückfrage.

---

## 1. Einbindung in das bestehende Projekt

| Bestehender Teil | Was dazukommt |
| --- | --- |
| CLAUDE.md | Eine Zeile: `Recovery-Modus: docs/recovery-modus.md lesen, bevor an Login, Admin-Rechten, Datenbank, Backup, Installation oder Boot gearbeitet wird.` |
| apps/ | Zwei neue Teilprogramme in Rust: `cli-recovery/` (recover, enroll) und `cli-stick/` (Zeremonie, Prüfung, Ersatz, Rotation, Druck). Dazu ein Systemdienst `server-recovery/` (Boot-Unlock, Audit-Versand, Alarm), falls das bestehende Server-Teilprogramm nicht in Rust ist |
| Bestehendes Server-/Web-Teilprogramm | `classes/AdminVerify` in der Sprache des Teilprogramms: prüft die Signatur jedes Admin-Datensatzes bei jedem Login. Login-Pfad ruft AdminVerify auf, sonst nichts |
| Benutzerdatenbank | Keine Änderung an der DB-Software. Volume unter dem Datenverzeichnis wird verschlüsselt. Admin-Tabelle bekommt vier Spalten: `erteilt_von`, `erteilt_am`, `laeuft_ab`, `signatur`. Migration rückwärtskompatibel in `sql/`, bestehende Admins werden bei der Einführung einmalig mit Quorum Sticks signiert |
| Backup | Bestehende Backup-Routine verschlüsselt mit dem aus dem DEK abgeleiteten Schlüssel und legt den Recovery-Blob bei |
| Installation / Images | Produktiv-, Recovery- und Installations-UKI werden in `tools/` gebaut; bestehende Installationsroutine bekommt den Schritt „Server einlernen" |
| lib/ (zentrale Bibliothek) | Neuer Sprachordner `rust/` mit crypto, formats, tpm, stick. AdminVerify zusätzlich im Sprachordner des bestehenden Server-Teilprogramms, damit andere Projekte es nutzen |
| docs/ | Diese Datei, Konzept-Kopie, Handbuch Root-Sticks, Abnahmeprotokolle |

Innenaufbau der neuen Teilprogramme nach Projektstruktur-Standard: nur `classes/`, `lang/`, `templates/`, `config/`, `tests/`, was gebraucht wird; direkt im Ordner nur `main.rs`, `Cargo.toml`, `VERSION`. Ordner englisch, klein, Trenner `-`. Verbotene Sammelnamen: misc, utils, common, neu, alt, backup. Secrets und Secure-Boot-Schlüssel außerhalb des Projektordners. `dist/`, `data/`, `target/` nie in Git.

---

## 2. Nicht verhandelbar

1. Kein Geheimnis liegt jemals im Klartext auf einem Datenträger des Zielsystems.
2. Kein einzelner Faktor reicht: Plattform (TPM oder signiertes Recovery-Image) + Besitz (Quorum an Root-Sticks) + Wissen (PINs).
3. Recovery läuft nur in einer eigenen, signierten, gemessenen, netzlosen Boot-Umgebung, nie im Produktiv-OS.
4. Jede Nutzung ist einmalig: TPM-Nonce, monotoner NV-Zähler, Neuversiegelung danach.
5. Minimalismus: cli-recovery kann genau zwei Dinge (recover, enroll). Keine Shell, kein Login, kein Netz, keine Optionen.
6. Kerckhoffs: alles darf öffentlich sein außer Schlüsseln und PINs.
7. Fail-closed: jeder Fehler bricht ab. Kein Fallback mit schwächerer Sicherheit. Nichts ist konfigurierbar, was Sicherheit senkt.
8. Beweisbarkeit: jeder Versuch erzeugt einen signierten Eintrag in einer Hash-Kette plus Alarm.
9. Krypto-Agilität: Verfahren austauschbar, Signaturen hybrid klassisch + Post-Quanten.

---

## 3. Architektur in Kürze

| Komponente | Aufgabe |
| --- | --- |
| Root-Sticks (USB-HSM, Secure Element) | Vertrauensanker. Quorum je Stufe (2 von 4 / 2 von 3 / 1 von 2). Verschiedene Schlüssel je Stick, keine Klone. |
| Recovery-Umgebung (Recovery-UKI) | Einziger Code, der einen Admin anlegen oder die DB auf fremder Hardware öffnen kann. RAM-only, dm-verity, kein Netz. |
| TPM 2.0 je Server (diskret) | Bindet DEK und Recovery an Maschine und Code-Stand. Öffnet Slot A beim Normalstart automatisch. |
| Datenbank-Volume (LUKS2) | Alle Benutzerdaten. DEK 256 Bit in zwei Slots: A = TPM (PCR-Policy), B = Shamir-Shares an die Sticks (ECIES). |
| Admin-Datensätze | Signierte Berechtigungen, keine DB-Flags. Prüfung bei jedem Login. Ohne gültige Signatur: ignoriert + Alarm. |
| Audit-Zeuge | Externer WORM-Speicher (enterprise) / E-Mail + NAS (home) / Datei (test). Hash-Kette, Alarm. |
| Wiederherstellungsblatt | Pro Server ein A4-Blatt mit Recovery-Blob als QR. Liegt in jedem Tresor. Ohne Sticks wertlos. |

---

## 4. Stufen (Cargo-Feature, genau eines pro Build, kein Laufzeitschalter)

| Parameter | enterprise | home | test |
| --- | --- | --- | --- |
| Quorum Sticks | 2 von 4 | 2 von 3 | 1 von 2 |
| PolicyOR-Zweige | 1 + 6 | 1 + 3 | 1 + 2 |
| Stick-Backend (PKCS#11) | hsm (Nitrokey HSM 2, YubiHSM 2) | piv (YubiKey 5, jede 5.x; Nitrokey 3) | softhsm (SoftHSM2) |
| Stick und PIN getrennt | in Zeremonie erzwungen | empfohlen | nein |
| Touch am Stick | ja | ja | nein |
| PIN-Quelle | Tastatur | Tastatur | Datei |
| Audit-Zeuge | extern über TLS | E-Mail + zweites Gerät | Datei |
| Banner im Image | keins | keins | roter Rahmen, TEST |
| Secure-Boot-Signatur | Produktivschlüssel | Produktivschlüssel | Test-Schlüssel |

- `stufe` ist Feld in Org-Root-Liste und Server-Zertifikat. Server lehnt niedrigere Stufe ab, cli-recovery lehnt Zertifikate niedrigerer Stufe ab.
- Build-Skript verweigert: Test-Profil mit Produktiv-Signaturschlüssel und umgekehrt.
- Stufenwechsel nur durch Neuinstallation (test →) bzw. Zeremonie mit neuen Sticks (home → enterprise). Nie per Update.
- Entwicklung beginnt in Stufe test. home kostet keinen Sonder-Code: gleiche Module, kleinere Zahlen.

---

## 5. Kryptografie (Stand 2026, BSI TR-02102-1, FIPS 203/204)

| Zweck | Verfahren | Regel |
| --- | --- | --- |
| Root-Sticks, Org-Root-Liste, Server-Zertifikat | ECDSA P-384 im Secure Element + ML-DSA-65 in Software | Hybrid, beide Signaturen müssen gültig sein. ML-DSA-Seed (32 Byte) nur als Shamir-Shares als Datenobjekt auf den Sticks |
| Admin-Tokens | ECDSA P-256/P-384 (FIDO2, PIV) | was das Token liefert |
| TPM-Objekte | ECC P-384 | SHA-384-PCR-Bank, fixedTPM, fixedParent, sensitiveDataOrigin |
| Datenbank-Volume | AES-256-XTS (LUKS2) | DEK 256 Bit aus dem TPM-RNG, kein Passwort-Slot |
| Slot B (Shares an Sticks) | ECIES P-384: ECDH + HKDF-SHA-512 + AES-256-GCM | ein Share je Stick, AAD = geraete_id + version |
| Backups, Recovery-Blob | AES-256-GCM | 96-Bit-Nonce aus RNG, nie wiederverwendet, AAD = geraete_id |
| Hash, Messung | SHA-384 / SHA-512 | MD5 und SHA-1 verboten, auch für Prüfsummen |
| KDF | HKDF-SHA-512 | Salt = Nonce, Info = Zweck + Version |
| PIN-Ableitung (wo nötig) | Argon2id | 1 GiB, 3 Iterationen, 4 Threads |
| Schlüsselaustausch (falls je Netz) | X25519 + ML-KEM-768 | nur TLS 1.3 |
| Secret Sharing | Shamir GF(2^8), SLIP-0039-Format | Quorum je Stufe, jeder Share mit Prüfsumme |
| Zufall | TPM-RNG XOR Kernel-CSPRNG | Ausfall einer Quelle bricht ab |

Verboten: RSA < 3072, PKCS#1 v1.5, CBC ohne MAC, statische Nonces, Schlüssel in Umgebungsvariablen oder Logs, eigene Primitive, eigene Zufallsquelle.

Offen und dokumentiert: TPM und Secure Elements prüfen nur klassische Signaturen. PQ-Schutz greift für Liste, Zertifikate, Blob. Jährlich neu bewerten.

---

## 6. Schlüssel und TPM-Policy

| Ebene | Schlüssel | Ort |
| --- | --- | --- |
| 0 | Root-Sticks (P-384 im Chip) + ML-DSA-65-Seed als Shares | Tresore |
| 1 | Plattform-Key je Server (P-384 im TPM), von Quorum-Sticks zertifiziert | TPM |
| 2 | DEK je Server; Slot A im TPM versiegelt, Slot B als Shares an die Sticks | TPM + Recovery-Blob |
| 3 | Abgeleitet per HKDF aus DEK: Backup-Schlüssel, Audit-Signaturschlüssel | nur RAM |
| 4 | Admin-Tokens (FIDO2/PIV), zertifiziert von Sticks oder bestehendem Admin | Token |

```
Policy(DEK) = PolicyOR(
    PolicyPCR(0-7, 11 = Produktiv-UKI),                                  # Normalstart, automatisch
    PolicyPCR(0-7, 11 = Recovery-UKI)
      AND PolicySigned(Stick_i) AND PolicySigned(Stick_j)                 # ein Unterzweig je Stick-Paar
      AND PolicyAuthValue(Plattform-PIN)
      AND PolicyNV(Zähler == c)
)
```

- PolicySigned: Stick signiert eine Nonce, die das TPM selbst erzeugt hat. Replay unmöglich.
- Nach jeder Recovery: Zähler +1, DEK mit c+1 neu versiegeln.
- Stick-Ersatz oder Listen-Wechsel: Slot B und Recovery-Policy je Server neu erzeugen, neues Wiederherstellungsblatt.
- Slot B ist TPM-unabhängig: Weg bei totem Server.
- TPM-Sessions immer mit Salt und Parameterverschlüsselung — gemeint sind die Sessions, die wirklich etwas tragen (Policy, HMAC). **Trial-Sessions sind ausgenommen:** sie rechnen nur einen Digest aus, der ohnehin öffentlich ist, und tpm2-tools lässt Salt für sie gar nicht zu (gemessen an 5.7: „Trial sessions cannot be additionally used as encrypt/decrypt session“). DA-Lockout: 3 Versuche, 24 h. EK-Zertifikat gegen Hersteller-CA prüfen.

---

## 7. Datenformate

Deterministisches CBOR (RFC 8949), Versionsfeld in jeder Struktur, Signaturen abgetrennt. Kein JSON, kein XML.

| Struktur | Felder | Signiert von | Liegt wo |
| --- | --- | --- | --- |
| Org-Root-Liste | version, stufe, erstellt, sticks[ id, pub_p384, status ], mldsa_pub | Quorum Sticks + ML-DSA | jeder Server, jedes Image, jeder Tresor |
| Server-Zertifikat | geraete_id, stufe, plattform_pub, ek_hash, erstellt, org_root_version | Quorum Sticks + ML-DSA | Server, Recovery-Blob |
| Recovery-Blob | geraete_id, version, org_root_version, slot_b[ stick_id, ecies_ct ], policy_digest, nv_index, zaehler | Plattform-Key | Server, jedes Backup, Wiederherstellungsblatt |
| Wiederherstellungsblatt | A4-PDF: Firma, Servername, geraete_id, Datum, org_root_version, Blob als 1–2 QR (Base45, EC-Level H), SHA-256-Fingerabdruck in Klartext, Unterschriftsfelder | Fingerabdruck beim Einlesen geprüft | jeder Tresor |
| Audit-Eintrag | seq, prev_hash, zeit, geraete_id, ereignis, stick_ids, pcr_digest, ergebnis | Plattform-Key | Server, Zeuge |
| Admin-Datensatz | benutzer_id, rolle, erteilt_von, erteilt_am, laeuft_ab | Quorum Sticks (Recovery) oder 1 Admin-Token | bestehende Benutzer-DB, Prüfung bei jedem Login |

Größen: ECIES-Share ≈ 150 Byte, Recovery-Blob < 1,5 KB. Jede Struktur trägt AAD mit geraete_id + version. Änderung an einem Format = Versionsfeld + Hauptversion des Teilprogramms erhöhen.

---

## 8. Abläufe

### Recovery (cli-recovery recover) — 8 Schritte, < 20 min, jeder Fehler bricht ab
1. Boot Recovery-Eintrag. UEFI prüft UKI-Signatur. PCR 0–7 Firmware, PCR 11 Recovery-UKI, PCR 15 dm-verity-Root-Hash. Keine Netztreiber im Image.
2. Selbstprüfung: Org-Root-Liste und Server-Zertifikat gegen eingebaute Root-Keys. Anzeige geraete_id und Zähler. Nonce aus dem TPM.
3. Stick 1: stecken, PIN, Touch. Stick signiert TPM-Nonce, TPM prüft (PolicySigned). 3 Fehlversuche → Stick gesperrt.
4. Stick 2: gleich. Welches Paar ist egal.
5. Plattform-PIN. 3 Fehlversuche → TPM-Lockout 24 h.
6. TPM gibt DEK frei. Volume öffnen.
7. Genau einen Admin in der bestehenden Benutzer-DB anlegen: Name mit Datum, 32 Zeichen Zufallspasswort, einmalige Anzeige, Passwortwechsel + Token-Registrierung beim ersten Login erzwungen, Ablauf 24 h. Beide Sticks signieren den Datensatz.
8. Zähler +1, DEK neu versiegeln, Audit-Eintrag signieren, Hash-Kette, beim nächsten Produktiv-Boot an Zeugen, Alarm an alle Kontakte. Speicher nullen, Reboot. Fehlversuche ebenfalls protokollieren + melden.

### Server einlernen (cli-recovery enroll, Schritt der bestehenden Installation) — < 60 min
1. Boot Installations-UKI (enthält Org-Root-Liste). TPM erzeugt Plattform-Key, EK-Zertifikat gegen Hersteller-CA.
2. Quorum Sticks signieren Server-Zertifikat (geraete_id, plattform_pub, ek_hash, Datum).
3. DEK neu erzeugen oder aus Backup über Slot B holen. Slot A im TPM versiegeln, Slot B für alle Sticks erzeugen.
4. Recovery-Objekte anlegen (Policy, NV-Zähler), Wiederherstellungsblatt drucken.
5. Logbuch, Blatt in alle Tresore.

### Stick-Ersatz (cli-stick replace) — Frist 30 Tage
1. Neuer Stick erzeugt Schlüssel, PIN setzen.
2. Quorum gesunde Sticks signieren neue Org-Root-Liste (neuer Key aktiv, alter gesperrt, version +1).
3. Jeder Server übernimmt die Liste im Wartungsfenster, erzeugt Slot B und Recovery-Policy neu, druckt neues Blatt. Alte Blätter vernichten.

### Zeremonie (cli-stick ceremony)
Alle Sticks nacheinander einlernen (Schlüssel im Chip, PIN, Touch-Test), Reservestick unbenutzt, ML-DSA-Seed erzeugen und als Shares auf die Sticks, Org-Root-Liste mit Quorum signieren, Fingerabdruck anzeigen, Logbuchseite drucken.

### Produktivbetrieb
Beim Boot Slot A über PCR-Policy öffnen (server-recovery). AdminVerify im bestehenden Login prüft die Signatur jedes Admin-Datensatzes. Audit-Einträge versenden, Alarm bei jedem Recovery-Boot und jedem Fehlversuch.

---

## 9. Härtung der Images

| Schicht | Pflicht |
| --- | --- |
| Firmware | Boot Guard / AMD PSB, SPI-Flash schreibgeschützt, UEFI-Passwort, Boot-Reihenfolge gesperrt, USB-Boot aus, Thunderbolt aus |
| Secure Boot | eigene PK/KEK/db, Microsoft-CAs entfernt, dbx gepflegt; nur Produktiv-, Recovery-, Installations-UKI signiert |
| Image | UKI (Kernel + initramfs + cmdline), rootfs read-only mit dm-verity (Root-Hash in cmdline), keine Shell, kein Login, kein SSH, kein Netzmodul, kein Swap |
| Kernel | lockdown=confidentiality, IOMMU + DMA-Schutz, SMT aus, KASLR, nur benötigte Module |
| Prozess | statisch gelinkt, seccomp auf die benötigten Syscalls, mlockall, Zeroize, keine Konfigdatei |
| TPM | diskret (kein fTPM in enterprise), verschlüsselte Sessions, PCR-Bank SHA-384, Platform Hierarchy nach Boot deaktiviert |
| Speicher | TSME/TME an, Ruhezustand aus |
| Anti-Rollback | NV-Zähler, alte Images per dbx gesperrt, Version im Zertifikat |
| Build | reproduzierbar auf zwei Rechnern, Hash-Vergleich, SBOM, Signatur mit Quorum Sticks |

---

## 10. Code-Regeln

- Alles, was Schlüssel berührt: Rust, Edition 2024, `#![forbid(unsafe_code)]` — Ausnahme nur in den zwei FFI-Wrappern (TPM, PKCS#11), getrennt reviewt. AdminVerify in der Sprache des bestehenden Server-Teilprogramms. Kein Shell, Perl, PHP im Recovery-Pfad.
- Krypto nur aus geprüften Crates: aws-lc-rs oder ring, ml-dsa, tss-esapi, cryptoki. Keine eigenen Primitive.
- Geheimnisse: Zeroize für jeden Puffer, kein Debug auf Schlüsseltypen, kein Logging von Schlüsseln/PINs/Nonces, Vergleiche in konstanter Zeit, keine Panics im Krypto-Pfad.
- Stick-Zugriff ausschließlich über die Backend-Schnittstelle (hsm / piv / softhsm). Übriger Code kennt den Stick-Typ nicht.
- Build: `cargo --locked`, vendorierte Abhängigkeiten, `cargo audit` + `cargo deny` in CI, reproduzierbar.
- Tests: Unit, Property, `cargo fuzz` auf alle Parser und Formate, Integrationstest auf swtpm + SoftHSM2 in CI, Abdeckung lib > 90 %.
- CI-Job bei jedem Commit: QEMU + swtpm + SoftHSM2 fahren Zeremonie, Einlernen, Recovery, Stick-Ersatz, Neuaufsetzen aus Backup komplett durch. Rot = kein Merge.
- Reviews: zwei unabhängige pro Paket, Security-Review vor jedem Tag.
- Anwendertexte (lang/): eine Frage pro Bildschirm, große Schrift, DE + EN, jede Fehlermeldung sagt, was der Mensch als Nächstes tut. Fehlercodes nur im Log. Zielgruppe: Menschen ohne IT-Wissen.
- Nichts konfigurierbar, was Sicherheit senkt. Parameter fest kompiliert. config/ enthält nur Alarm-Kontakte und Zeugen-Adresse.
- VERSION je Teilprogramm, semantische Versionierung. Jedes Paket endet mit Commit, Tag, CHANGELOG-Eintrag.
- Bestehenden Code des Projekts nicht umbauen. Nur die in Abschnitt 1 genannten Anknüpfpunkte anfassen: Login (AdminVerify), Admin-Tabelle (Migration), Backup (Verschlüsselung + Blob), Installation (enroll-Schritt).

---

## 11. Arbeitspakete (Reihenfolge zwingend)

| Nr. | Paket | Inhalt | Abnahme (Nr. aus Abschnitt 12) |
| --- | --- | --- | --- |
| 0 | Testumgebung | QEMU + OVMF Secure Boot, swtpm, SoftHSM2, 4 Test-Sticks + Reserve, Prüf-Notebook ohne Netz, CI-Job | Seal/Unseal mit PolicyOR aus 7 Zweigen auf swtpm |
| 1 | Krypto-Bibliothek (lib/rust) | ECIES P-384, Shamir SLIP-0039, HKDF-SHA-512, AES-256-GCM, Hybrid P-384 + ML-DSA-65, Zeroize, CBOR-Formate | Testvektoren, 24 h Fuzzing ohne Absturz, Abdeckung > 90 % |
| 2 | Stick-Modul | PKCS#11: keygen, PIN + Sperre, sign, ECDH, Datenobjekt; Backends hsm/piv/softhsm | alle Funktionen auf allen Backends, Sperre nach 3 Versuchen |
| 3 | TPM-Modul | Primary, Seal mit PolicyOR, PolicySigned, PolicyNV, verschlüsselte Sessions, DA-Parameter, EK-Prüfung | 1–4, 9, 10 auf swtpm und echtem TPM |
| 4 | cli-stick | ceremony, check, replace, rotate, print | Handbuch-Kapitel Erstellung und Ersatz laufen ohne Nachfrage durch |
| 5 | Volume + Slots | LUKS2 unter dem bestehenden Datenverzeichnis, DEK, Slot A, Slot B, Recovery-Blob, Wiederherstellungsblatt | 5, 6, 7 |
| 6 | cli-recovery | recover (8 Schritte), enroll, UI-Texte, Hash-Kette, Neuversiegelung c+1 | 1–8, 13–16 |
| 7 | Anbindung Projekt | AdminVerify im Login, Migration Admin-Tabelle, Backup-Verschlüsselung, server-recovery (unlock, audit-ship, alarm) | 8, 12 |
| 8 | Images | Produktiv-, Recovery-, Installations-UKI (mkosi, ukify), dm-verity, Secure-Boot-Schlüssel, reproduzierbar; enroll in der bestehenden Installation | 4, 11, 17 |
| 9 | Härtung + Prüfung | seccomp, Lockdown, SMT aus, zwei Reviews, externer Pentest, Findings beheben | 18 |
| 10 | Doku + Probe | Handbuch final, Runbook, erste Probe mit Personen ohne Vorwissen, Abnahmeprotokoll | 14, 15 mit Zeitmessung |

Paket 4 vor 5 (Slot B braucht Stick-Keys). Paket 8 vor 9 (nur fertige Images härten und testen).

---

## 12. Abnahmekriterien (alle 18, protokolliert mit Datum und Prüfer)

1. Mit nur einem Stick keine Freigabe; mit Sticks ohne PINs keine Freigabe
2. Jedes Stick-Paar gibt frei; ein gesperrter Stick in keinem Paar
3. Mitschnitt eines Ablaufs mit altem Zählerstand: keine Freigabe (Replay)
4. Recovery-Image um 1 Byte geändert: PCR-Abweichung, keine Freigabe über das TPM
5. Datenträger in anderer Maschine ohne Sticks: unlesbar; mit Quorum Sticks + Blatt: lesbar
6. Backup ohne Sticks und ohne Server-TPM: unlesbar
7. Live-System oder fremdes OS auf dem Server: Volume bleibt zu
8. Admin-Datensatz direkt in der DB eingetragen: Login verweigert, Alarm
9. 3 falsche Stick-PINs: Stick gesperrt; 3 falsche Plattform-PINs: TPM 24 h gesperrt, Produktivbetrieb läuft
10. Logic-Analyzer am TPM-Bus zeigt nur verschlüsselte Sessions — außer Trial-Sessions, die nichts Geheimes übertragen
11. Portscan und Konsole in der Recovery-Umgebung: kein Netz, keine Shell, kein Login
12. Audit-Eintrag beim Zeugen signiert vorhanden, Alarm an alle Kontakte innerhalb 60 s
13. Recovery-Account erzwingt Passwortwechsel + Token-Registrierung, verfällt nach 24 h
14. Vollständige Recovery durch Personen ohne Vorwissen nur mit Handbuch in < 20 min
15. Neuaufsetzen eines Servers aus Backup + Sticks in < 60 min
16. Stick-Ersatz: neue Liste, alter Stick abgelehnt, neuer akzeptiert, Blätter neu
17. Reproduzierbarer Build: identischer Image-Hash auf zwei unabhängigen Rechnern
18. Externer Penetrationstest ohne Findings der Stufe hoch oder kritisch

---

## 13. Arbeitsweise

- Zuerst CLAUDE.md, README.md und die bestehende Struktur des Projekts lesen. Dann diese Datei. Nichts Bestehendes umbenennen oder verschieben.
- Vor jedem Paket: Abschnitt 11 und das zugehörige Abnahmekriterium lesen. Erst Tests, dann Code.
- Nie ein Sicherheitsmerkmal „vorläufig" weglassen oder abschwächen. Wenn etwas nicht geht: stoppen und fragen, nicht umgehen.
- Datenformate, Policy-Aufbau, Quorum und Krypto-Verfahren nur nach ausdrücklicher Freigabe ändern.
- Test-Schlüssel, Test-PINs und SoftHSM2-Tokens dürfen im Repo liegen, klar als TEST gekennzeichnet. Produktivschlüssel nie.
- Jede Abweichung von dieser Datei wird als offener Punkt in CHANGELOG.md unter „Offen" eingetragen.
