# Erste Anmeldung sagt "Anmeldung fehlgeschlagen."

Erscheinung (Thomas, wörtlich): "der fehler is immer noch drin das er das erste
mal anmelden sagt passwort falsch". Zweiter Versuch mit demselben Passwort geht
durch.

Gelesene Lesart: der erste Versuch JEDER Sitzung (nach jedem Neuladen), nicht
der allererste nach der Ersteinrichtung. Grund: "immer noch drin" heißt, Thomas
erlebt es wiederholt; der allererste Versuch nach einer Ersteinrichtung
passiert nur ein einziges Mal. Frage 2 liegt bei Thomas; bis zur Antwort wird
diese Lesart gemessen.

## Erwartungen, aufgeschrieben VOR der Messung (30.10.2026)

- E1: Das von der Anlage AUSGELIEFERTE shell/main.js ruft loginView ohne
  openTrust auf, so wie main es tut (main.js:268). Dann läuft im Betrieb die
  Attrappe openTrustFromChain, die chain.versions[0] nimmt -- Version 1 der
  Vertrauensliste statt der aktuellen.
  Falsifiziert, wenn im ausgelieferten main.js ein openTrust übergeben wird.
- E2: GET /api/v1/trust?from=1 OHNE Sitzung antwortet E_LOGIN, nicht 204.
  Falsifiziert durch 204 oder 200.
- E3: Die KDF-Parameter der Anlage sind m=65536, t=3. Dann ist die Frist von
  proof.js 108 s und die Argon2-Frist kann die Erscheinung nicht erklären.
  Falsifiziert durch deutlich größere Parameter.
- E4: Die Vertrauensliste der Anlage steht über Version 1. Dann holt die
  Attrappe eine ANDERE Liste als die aktuelle, und session.verifyAPI prüft das
  API-Zertifikat gegen veraltete Schlüssel.
  Falsifiziert, wenn die Liste auf Version 1 steht.

## Was ohne Thomas nicht messbar ist

Ein echter Anmeldeversuch braucht sein Passwort. Ich gebe an einer Anlage kein
Passwort ein (das ist keine Entwicklungsumgebung auf diesem Rechner), also
tippt Thomas, und mitgeschnitten wird Netzwerk und Konsole.

## Messung

Gemessen am Code (30.10.2026). Jede Zeile nennt die Datei, die sie belegt oder
widerlegen würde.

- M1: `apps/server-api/classes/login/trust.go` antwortet 204 nur für die Nummer
  EINE HINTER der neuesten; `from=1` liefert bei einer Liste ab Version 1 immer
  eine Version. **Verdacht 1 ist widerlegt** -- daraus kann kein Fehler
  entstehen, der nur beim ersten Versuch auftritt.
- M2: `finish.go`, `deny()` schreibt KEINEN Grund ins Protokoll (nur eine
  gedrosselte Zeile, wenn die Zählung nicht geschrieben werden konnte). Das
  API-Protokoll nennt den echten Grund also nicht; er ist nur im Browser zu
  holen.
- M3: `apps/web/public/assets/js/shell/main.js:268` ruft `loginView` OHNE
  `openTrust` auf. Im Betrieb läuft damit die als Platzhalter gekennzeichnete
  `openTrustFromChain` (`auth/login.js`), die `chain.versions[0]` nimmt --
  **Version 1** der Vertrauensliste statt der aktuellen.
- M4: `contracts/session.js:320-328`, `verifyAPI` sucht den signierenden Knoten
  per Schlüsselkennung IN DER ÜBERGEBENEN LISTE. Hält Version 1 den heutigen
  Signierschlüssel des API-Knotens nicht, ist `node === null` und es fliegt
  `Error("session: malformed certificate")` -- ein gewöhnlicher Fehler, also
  derselbe eine Satz, und das ERST NACHDEM `loginFinish` durchgelaufen ist:
  Cookie gesetzt, Nonce verbraucht, Sitzungszeile geschrieben.
- M5: `shared/session/session.go:87`, `Path=/api/; Secure; HttpOnly;
  SameSite=Strict`, und `net/http.js` ruft mit `credentials: "same-origin"`.
  Das Cookie erreicht `/api/v1/trust`. „Das Cookie kam nicht an" ist kein
  Pfad- oder Attributproblem.
- M6: `httpapi.Cookie` verlangt GENAU EIN Cookie dieses Namens
  (`httpapi.go:173`). Ein zweites `ngc_session` macht jede Anfrage sitzungslos.
  Das erzeugt die umgekehrte Asymmetrie (erst geht es, später nicht) und ist
  nicht Thomas' Erscheinung -- hier notiert, damit es niemand zweimal sucht.
- M7: Frist für Argon2id in `contracts/proof.js:189-202`: 10 s + 0,5 ms je
  KiB-Durchgang, bei m=65536 t=3 also 108 s. Ein kalter Start erklärt die
  Erscheinung nicht.
- M8: Die Browser-Messung an der Anlage steckt: `https://10.2.3.216/` lädt hier
  nicht, ERR_CERT_AUTHORITY_INVALID. Die Zertifikatsprüfung wird nicht
  abgeschaltet und es wird nicht drumherum gegangen.
- M9: **Das ungeprüfte Stück und das verdächtige Stück sind dasselbe Stück.**
  `tests/shell/login.spec.js` ersetzt `openTrust`/`verifyAPI` per `standInOn`
  (Begründung dort: die Kette der Attrappe passt nicht zum Anker des
  Testservers), und `login.spec.js:402` macht als ERSTE Handlung eine richtige
  Anmeldung und erwartet die Taskleiste. Der erste Versuch ist gegen die
  Attrappe also schon abgedeckt -- die Attrappe kann die Erscheinung darum
  nicht nachstellen.

## Was noch nicht gemessen ist

Die Asymmetrie selbst. Jeder Mechanismus, den ich im Quelltext finde,
scheitert IMMER oder NIE; „erster Versuch tot, zweiter geht" steckt in Zustand,
der zwischen den beiden Versuchen wechselt, und den sehe ich im Code nicht. M3
und M4 sind am Code gemessen; **dass sie Thomas' Erscheinung erzeugen, ist
nicht gemessen** und wird hier nicht behauptet.

Offen, in dieser Reihenfolge: E1, E4 und E3 liest die Leitsitzung über SSH; den
sterbenden ersten Versuch muss Thomas selbst tippen, mit Mitschnitt von Netz
und Konsole. Ein Test, der die erste Anmeldung festhält, kommt danach -- einer
vorher hielte nichts fest.

## An der Anlage gemessen (30.10.2026, über die Leitsitzung per SSH)

- **E1 bestätigt.** `ngc-ein` (10.2.3.216), Webroot `/var/www/ngc`, `apps/web`
  VERSION 0.2.1: die ausgelieferte `shell/main.js` ruft `loginView` mit drei
  Feldern und `onSession` auf, **ohne** `openTrust`. Im Betrieb läuft die
  Attrappe.
- **E4 gestützt, nicht widerlegt.** `ngc-db-1` (10.2.3.212),
  `select kind, id, seq, length(data) from ngc_blob` -- nur Schlüssel und
  Längen: `trust` 1 bis 8, lückenlos. Die Anlage läuft auf **Fassung 8**,
  `chain.versions[0]` holt **Fassung 1**. Sieben Fassungen Abstand.
- **E3 widerlegt.** `ngc-api-1`, `GET /api/v1/login/start` mit einem
  Phantasienamen, 200: `argon2id`, m=65536, t=3, p=1. Damit ist die Frist 108 s
  und Argon2 als Ursache erledigt. (Die Leitsitzung hat dafür auf der Maschine
  selbst `curl -sk` gegen 127.0.0.1 benutzt, also die Zertifikatsprüfung für
  diesen einen lokalen Aufruf übergangen; das ist hier festgehalten, weil eine
  Messung sagen muss, wie sie zustande kam.) Nebenbefund: für einen unbekannten
  Namen `passwordToo` = false, `totp` = false.
- Nebenbei erledigt: 8 Fassungen sprengen keine Grenze.
  `MAX_CHAIN_VERSIONS` = 64, `MAX_CHAIN` = 32 MiB.

### Was E4 ist, und was es nicht ist

E4 ist ein **Sicherheitsmangel für sich**: die Anmeldung prüft das
API-Zertifikat gegen Fassung 1, während die Anlage auf 8 läuft. Ein Knoten, der
in den Fassungen 2 bis 8 aus der Liste GENOMMEN wurde, wird von der Anmeldung
weiter angenommen. Nach der Rangfolge Sicherheit > Stabilität > Geschwindigkeit
gehört das behoben, auch wenn es Thomas' Erscheinung nicht erzeugt.

E4 erzeugt die Erscheinung **wahrscheinlich nicht**, und das ist der Grund:
hält Fassung 1 den heutigen Signierschlüssel des API-Knotens nicht, scheitert
`verifyAPI` bei JEDEM Versuch, auch beim zweiten -- dann käme Thomas nie
hinein. Hält sie ihn, erklärt E4 den ersten Versuch nicht. Die Nachmessung, die
das entscheidet (hält Fassung 1 denselben Signierschlüssel wie Fassung 8?),
liegt bei der Leitsitzung.

Die Asymmetrie selbst bleibt damit ungemessen. Sie misst Thomas am Browser,
nach der Liste, die über die Leitsitzung bei ihm liegt; die eine Frage daran
ist, ob in Durchlauf 1 hinter `login/finish` eine 200 steht.
