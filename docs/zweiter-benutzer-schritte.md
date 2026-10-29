# Einen zweiten Benutzer anlegen — die Schritte am Gerät

**Stand 29.10.2026** (Anlage und Rückweg nachgetragen; die Schritte selbst sind die vom
24.10.2026). Was hier steht, kann **nur ein Mensch** tun: die neue Fassung der
Vertrauensliste wird im Browser mit dem Schlüssel der Person unterschrieben, und dieser
Schlüssel ist ihr Passwort. Kein Agent tippt es. Alles andere ist gebaut und gemessen
(`docs/vertraege-v1.2.md`, Punkt 73).

## WELCHE Anlage — bitte zuerst lesen

Es gibt seit dem 24.10.2026 **zwei**, und nur auf einer liegt dieser Stand.

- **Einzel-VM `https://ngc.test`** (10.2.3.208, NGC `0.2.0`): **hier** sind die zwei geänderten
  Dateien am 24.10.2026 eingespielt worden, mit Sicherung unter
  `/root/zweiter-benutzer-backup-20261024/`. **Die Schritte unten galten für diese Anlage.**
  **Diese Maschine ist aus.** Nachgemessen am 24.10.2026: Port 22 antwortet nicht, und auf dem
  Proxmox-Wirt steht VM 208 auf `stopped`. Sie ist nicht langsam und nicht gestört, sie läuft
  nicht. Gemessen am Aufgabenbuch des Wirts: um **12:37:27** hat ein `qmstop` sie hart
  angehalten — **kein Absturz**. **Wer die Schritte unten heute geht, kommt nirgends an**, und
  „nicht erreichbar" wäre dann kein Befund über NGC.
- **Neue Einzel-Maschine `ngc-ein`** (10.2.3.216, Name wieder `ngc.test`, NGC `0.2.2`, seit
  24.10.2026): läuft, aber sie läuft **`origin/main`** und hat die beiden Formulare **nicht**.
  Die zwei geänderten Dateien sind dort **nicht** eingespielt. Die Schritte unten sind auch
  hier **nicht** zu gehen, solange dieser Zweig nicht verschmolzen und ausgerollt ist. Die
  Sicherung von vor der Änderung liegt auf der alten Platte und im Schnappschuss
  `vor-bergung-20261024` (siehe `docs/alte-vm-verweise.md`, Abschnitt 6).
- **Verteilte Anlage `https://ngc.mein.lan`** (api-1 = 10.2.3.210, api-2 = 10.2.3.211, NGC
  `0.2.1`): **hier sind die Schritte unten zu gehen.** Am 29.10.2026, 20:07 UTC, sind die zwei
  Dateien aus PR 118 auf api-1 eingespielt; über HTTPS geholt tragen sie `e40f648` und
  `91871de`, und der ganze Importbaum von `users.js` kommt mit 18 Modulen und ohne Lücke
  herunter (Punkt 96). Sicherung: `/root/zweiter-benutzer-backup-20261029-200719/`.
  **Bis dahin lag dort fünf Tage die Fassung ohne die Formulare** — wer die Schritte in dieser
  Zeit gegangen ist, hat die Felder nicht gefunden, und das sah wie „nicht gebaut" aus.
  Die Oberfläche liegt **nur auf api-1**.

Und gegen den **Namen** messen, nie gegen die IP: Zertifikat und Passkey hängen am Namen.

---

## Vorher: die Seite wirklich neu laden

Der Browser hält die alte Fassung der Oberfläche fest. Ohne das hier siehst du die neuen
Felder nicht, und das sieht aus wie „es ist nicht gebaut".

**Strg + F5** auf `https://ngc.mein.lan` — dem Namen, nie der IP: Zertifikat und Passkey
hängen am Namen. Ohne das Neuladen hält der Browser die alte Fassung fest, und dann fehlen die
Felder, obwohl sie da sind.

---

## 1. Eine Rolle anlegen

Ohne sie geht es nicht: eine Anlage kommt mit **genau einer** Rolle aus der Ersteinrichtung,
`admin`. Wer nur einen Benutzer anlegt, legt einen zweiten Verwalter an.

1. Anmelden wie immer.
2. **Benutzer und Rechte** öffnen.
3. Reiter **Rollen und Rechte**.
4. Ganz unten, Kasten **Neue Rolle**: in das Feld `technik` schreiben.
5. **Rolle anlegen** drücken.
6. Es kommt eine Frage. **Rolle anlegen** drücken.
7. Es kommt das Fenster für deinen Schlüssel. **Dein eigenes Passwort** eintippen,
   **Bestätigen**.
8. Es muss dastehen: »Die Rolle »technik« ist angelegt. Sie darf noch nichts.«

## 2. Der Rolle das Recht geben, eine App zu benutzen

Ohne diesen Haken ist die App für den neuen Menschen **unsichtbar** — nicht kaputt, sondern
nicht da.

9. Im selben Reiter steht jetzt ein Kasten **technik**.
10. Den Haken bei **Terminal · benutzen** setzen (oder bei welcher App auch immer).
11. **Rechte speichern**, dann die Frage mit **Rechte vergeben** beantworten.

## 3. Den Benutzer anlegen

12. Reiter **Benutzer**.
13. Ganz unten, Kasten **Neuer Benutzer**.
14. **Anmeldename**: `berta` (kleine Buchstaben, Ziffern, `.` `_` `-`).
15. Bei **Rollen** den Haken bei **technik** setzen — und bei `admin` **nicht**.
16. **Erstes Passwort** und **Erstes Passwort wiederholen**: ein Passwort, das du Berta
    persönlich gibst.
17. **Benutzer anlegen** drücken.
18. Die Frage lesen. Darin steht der Preis dieses Weges, und er ist echt: bis Berta das
    Passwort ändert, kennst du ihren Schlüssel. Dann **Benutzer anlegen**.
19. Wieder das Fenster für deinen Schlüssel: **dein eigenes** Passwort, **Bestätigen**.
20. Es muss dastehen: »»berta« ist angelegt und kann sich anmelden.«
21. In der Tabelle darüber muss `berta` stehen, Rolle `technik`, zuletzt angemeldet
    **noch nie**.

## 4. Die Probe — und das ist die eigentliche Messung

22. **Abmelden.**
23. Mit `berta` und ihrem Passwort anmelden.
24. Nachsehen und **aufschreiben**:
    - Kommt sie überhaupt hinein?
    - Steht die App, für die `technik` das Recht `benutzen` hat, in ihrem Startmenü?
    - Steht **Benutzer und Rechte** dort **nicht**? (Das muss so sein: das ist eine
      Verwalter-App.)
    - Wenn irgendwo ein Satz kommt, weil sie etwas nicht darf: **sagt er, dass ihr ein Recht
      fehlt, oder sagt er, dass etwas kaputt ist?** Den Satz bitte wörtlich notieren. Der
      Unterschied ist der ganze Punkt.

---

## Was Rückmeldung braucht

Zu jedem der vier Punkte aus Schritt 24 ein Satz, und bei allem, was schiefgeht, der Text, der
auf dem Schirm stand. Das wird als **Messung** nachgetragen — bis dahin gelten die Stationen
„meldet sich an" und „sieht seine Apps" als **ungemessen**, nicht als in Ordnung.

## Wenn etwas kaputt geht

Die Oberfläche zurückdrehen, ein Befehl auf der VM:

Auf **api-1** (10.2.3.210), als `root`:

```
sh /root/tuer-zumachen.sh /root/zweiter-benutzer-backup-20261029-200719
```

Das Skript legt die beiden gesicherten Dateien zurück und druckt ihre Hashes; dastehen muss
`83344ee…` für `users.js` und `d0c5ffe…` für `shell.css`. Danach im Browser **Strg + F5**.

Ein Benutzer, der schon in der Vertrauensliste steht, geht davon **nicht** weg: er steht in der
unterschriebenen Liste, nicht in der Oberfläche. Ihn wieder loszuwerden heißt, eine Fassung
ohne ihn zu bauen — dafür gibt es heute kein Formular, und das ist in Punkt 73 vermerkt.
