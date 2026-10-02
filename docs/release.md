# Ein Release von NGC bauen

Drei Schritte, zwei Rechner, ein Mensch. Der Schlüssel verlässt seinen Rechner nie.

## 1. Packen — auf jedem Rechner

```bash
tools/pack-release.sh v0.3.0 1
```

Das erste ist der git-Stand, aus dem gebaut wird. **Für ein veröffentlichtes Release ein Tag,
kein Branch:** ein Branch wandert, und dann ist „derselbe Stand" morgen etwas anderes. Beim
Schreiben dieser Datei ist `web` an einem Tag von `62d618e` auf `fe5a0e4` gewandert. Das
Werkzeug schreibt den Commit, den es gepackt hat, in seine Ausgabe, und es warnt, wenn man ihm
einen Namen statt eines Commits gibt.

Das zweite ist die Nummer des Release: sie darf nie kleiner werden als die, die beim Nutzer
liegt, und dieselbe Nummer darf noch einmal kommen, um eine Installation zu reparieren.

Danach liegen in `dist/release/<version>/`:

| Datei | Was es ist |
|---|---|
| `ngc-web-all.tar` | die Weboberfläche, ohne `tests/` |
| `release.payload.json` | noch **nicht** unterschrieben |

**Danach die Prüfungen fahren, denn der Packer prüft die Vollständigkeit nicht:**

```bash
go test ./tools/release/
```

Das ist die Stelle, die jedem Verweis der Oberfläche folgt — jedem `import`, jedem
`require_once`, jedem `src=` — und meldet, wenn eine Datei fehlt, auf die eine andere zeigt.
Ohne diesen Lauf weiß niemand, ob das Archiv vollständig ist.

Dass derselbe Stand immer dasselbe Archiv ergibt:

```bash
tools/pack-release.sh v0.3.0 1 /tmp/a
tools/pack-release.sh v0.3.0 1 /tmp/b
sha256sum /tmp/a/ngc-web-all.tar /tmp/b/ngc-web-all.tar
# Die zwei Prüfsummen müssen gleich sein.
```

## 2. Unterschreiben — nur auf dem Rechner mit dem Schlüssel

Das Verzeichnis aus Schritt 1 dorthin kopieren, dann:

```bash
tools/sign-release.sh dist/release/<version> ~/ngc-release.pem
```

Der private Schlüssel gehört **nicht** in dieses Verzeichnis und **nicht** in das
Repositorium. Das Skript weigert sich, wenn er im Verzeichnis liegt. Es schreibt
`release.json` und `release-key.pem` (den **öffentlichen** Schlüssel) und prüft sich selbst
mit demselben Skript, das später beim Nutzer läuft. Klappt das nicht, löscht es
`release.json` wieder — ein halb unterschriebenes Release gibt es nicht.

Es gibt keinen Weg, das automatisch zu machen, und das ist Absicht. Der Preis in einem Satz:
jedes Release braucht einen Menschen an diesem einen Rechner.

## 3. Veröffentlichen

Hoch müssen `release.json` und `ngc-web-all.tar`, und zwar dorthin, wo `NGC_RELEASE_BASE`
hinzeigt — bei ulh ist das
`https://github.com/sorglos-it/nextgencloud/releases/latest/download`. `release-key.pem` geht
**nicht** mit: dieser Schlüssel steht fest im ulh-Skript, und ein Schlüssel, den man mit dem
Release herunterlädt, prüft nichts.

## Ohne Internet

Liegen `release.json` und `ngc-web-all.tar` von Hand in `/var/lib/ngc/staged`, nimmt
`ngc-web.sh` sie von dort und lädt nichts. Solange NGC noch kein Release veröffentlicht hat,
ist das der einzige Weg, der funktioniert.

## Die Reihenfolge, und warum sie so ist

Entschieden am 02.10.2026.

1. **Der Packer und die ulh-Aufgabe 31 gehen ZUSAMMEN heraus.** Aufgabe 31 ist die, die
   `ngc-web.sh` dazu bringt, `templates/apache-vhost.conf` aus dem entpackten Archiv zu
   nehmen, statt seinen virtuellen Host selbst zu schreiben.

   Warum zusammen und nicht nacheinander: Der Packer legt die Datei nach
   `/var/www/ngc/templates/`. Solange `ngc-web.sh` seinen eigenen Host schreibt, liegen damit
   **zwei Fassungen** auf jedem Server — und die im Archiv liest Apache nie. Ein Mensch, der
   der Anleitung im Kopf dieser Datei folgt, baut einen virtuellen Host, den der nächste
   ulh-Lauf **ohne Warnung wegräumt**; kein Test merkt das. Vorher gab es die zweite Fassung
   nicht, also macht der Packer allein diese Lücke erst auf. Und umgekehrt kann Aufgabe 31
   nicht vor dem Packer kommen: ohne ihn liegt die Datei gar nicht auf dem Server.

2. **Der Schlüssel danach.** Ein Schlüssel, der ein Archiv unterschreibt, das niemand baut,
   ist nutzlos. Er wird auf einem Rechner erzeugt, den nichts anderes anfasst, und sein
   öffentlicher Teil kommt ins ulh-Skript (heute steht dort `REPLACE-THIS-LINE`, und
   `ngc-web.sh` bricht damit absichtlich ab).
