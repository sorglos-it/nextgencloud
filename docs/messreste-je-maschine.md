# Was an der Anlage von Hand gesetzt ist

Aufgenommen am 25.10.2026 an allen acht Maschinen der verteilten Anlage. Der
Zweck dieser Datei ist die Frage, die an der Anlage zurzeit niemand mehr
beantworten kann: **was ist Stand, und was ist Messrest?**

Grundlage ist eine Suche je Maschine nach `*.vor-*`, `*.bak`, `*.orig`,
`*erstlauf*`, `*.save` unter `/etc`, `/opt`, `/root`, `/srv`, dazu der Inhalt
von `/root`, `/etc/ngc` und `/etc/hosts`.

## Die Tafel

| Maschine | Von Hand gesetzt, nicht aus den Installationsskripten |
|---|---|
| api-1 (.210) | `/etc/ngc/api.json.vor-drei-knoten` (Rückweg, fremd) · `/root/api-knoten.sh`, `api1mess.sh`, `migprobe.sh`, `ngc-web.env`, `ngc-antworten.env` · nftables |
| api-2 (.211) | `/etc/ngc/api.json.vor-drei-knoten` (Rückweg, fremd) · `/root/api-knoten.sh`, `ngc-antworten.env` · nftables |
| db-1 (.212) | **Schlüsselpaar von Hand** (siehe Punkt 92) · `/root/60-ngc.cnf.orig`, `dbfix.sh`, `dbneu.sh`, `dbneu2.sh`, `dbkeyprobe{,2,3}.sh`, `db-2.csr`, `db-3.csr`, `ngc-db-antworten.env` · nftables |
| svc-1 (.213) | `/root/Containerfile`, `appbau`, `appbau.sh`, `appbau.log` · nftables |
| svc-2 (.214) | `/root/m-last64` · nftables |
| store (.215) | nur nftables und `/root/store` |
| db-2 (.217) | **Schlüsselpaar von Hand** · `/etc/hosts.vor-verbund` (Rückweg, fremd) · `/root/install.log`, `ngc-db-antworten.env` · nftables |
| db-3 (.218) | **Schlüsselpaar von Hand** · `/etc/hosts.vor-verbund` (Rückweg, fremd) · `/root/ngc-db-antworten.env` · nftables |

`haertung-vorher.nft` liegt auf sechs Maschinen und ist überall **0 Byte**. Das
ist kein Rest, sondern eine Aussage: vor der Härtung gab es keine Firewall.

## Drei Angaben des Auftrags, die nicht mehr stimmten

Der Auftrag nannte drei Posten, die es nicht mehr gibt. Alle drei sind am
24.10.2026 abends schon abgeräumt worden — nachzulesen im Nachtrag zu
**Punkt 88**:

1. `99-erstlauf-galera.cnf` — auf keiner Maschine vorhanden.
2. `60-ngc.cnf.vor-adresse-*` und `60-ngc.cnf.vor-zumachen` — nicht vorhanden.
   Geblieben ist nur `/root/60-ngc.cnf.orig` auf db-1.
3. Die `/etc/hosts`-Zeilen auf db-2 und db-3, die den eigenen Namen auf
   `127.0.1.1` legten — schon entfernt. Nur die Sicherungen
   `/etc/hosts.vor-verbund` liegen noch, und die tragen die alte, kaputte
   Zeile.

## Was ich nicht gelöscht habe

Fast alles in der Tafel gehört einer anderen Sitzung: die Messskripte auf db-1,
die `.vor-*`-Rückwege, die `install.log`. Nach der Regel bleiben sie liegen, bis
ihr Besitzer sie freigibt. Diese Datei meldet sie, sie räumt sie nicht weg.

Die einzige Datei, die eindeutig Rest und nicht Rückweg war, ist
`99-erstlauf-log.cnf` — und die ist der Gegenstand von Punkt 91.
