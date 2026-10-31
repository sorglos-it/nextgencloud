#!/bin/sh
# Reads the hardening switches off the container that is REALLY running, not
# off the argument list NGC sends.
#
# That difference is the whole point of points 51 and 72: measured is that NGC
# sends the switches, NOT that podman honours them. This script reads podman's
# own view and, below it, what the kernel actually holds in the cgroup and in
# /proc. Where the two disagree, the kernel wins and the difference is the
# finding.
#
# Rescued on 24.10.2026 off the first single machine, where it lived in a home
# directory (/home/berry/ngcapp/inspect.sh) and was in no repository.
#
#   sh tools/haertung-nachsehen.sh [container]
#
#   container  name or id; default is the first container podman lists
set -u

C="${1:-$(sudo podman ps -a --format '{{.Names}}' | head -1)}"
if [ -z "$C" ]; then echo "KEIN BEHAELTER - nichts zu messen"; exit 1; fi

echo "Behaelter: $C"
echo "Status   : $(sudo podman inspect -f '{{.State.Status}}' "$C")"
echo "Abbild   : $(sudo podman inspect -f '{{.ImageName}}' "$C")"
echo
echo "1  PidsLimit        : $(sudo podman inspect -f '{{.HostConfig.PidsLimit}}' "$C")"
echo "2/3 Einhaengungen (Ziel | rw | Optionen):"
sudo podman inspect -f '{{range .Mounts}}    {{.Destination}} | rw={{.RW}} | {{.Options}}{{"\n"}}{{end}}' "$C"
echo "4  CapDrop          : $(sudo podman inspect -f '{{.HostConfig.CapDrop}}' "$C")"
echo "   CapAdd           : $(sudo podman inspect -f '{{.HostConfig.CapAdd}}' "$C")"
echo "   EffectiveCaps    : $(sudo podman inspect -f '{{.EffectiveCaps}}' "$C")"
echo "5  SecurityOpt      : $(sudo podman inspect -f '{{.HostConfig.SecurityOpt}}' "$C")"
echo "   NoNewPrivileges  : $(sudo podman inspect -f '{{.HostConfig.Privileged}} priv / {{.Config.Annotations}}' "$C" 2>/dev/null | head -c 300)"
echo "6  ReadonlyRootfs   : $(sudo podman inspect -f '{{.HostConfig.ReadonlyRootfs}}' "$C")"
echo "7  NetworkMode      : $(sudo podman inspect -f '{{.HostConfig.NetworkMode}}' "$C")"
echo "   Netzwerke        : $(sudo podman inspect -f '{{.NetworkSettings.Networks}}' "$C")"
echo "9  User             : $(sudo podman inspect -f '{{.Config.User}}' "$C")"
echo "10 NanoCpus/CpuQuota: $(sudo podman inspect -f '{{.HostConfig.NanoCpus}} / {{.HostConfig.CpuQuota}} per {{.HostConfig.CpuPeriod}}' "$C")"
echo "11 Memory           : $(sudo podman inspect -f '{{.HostConfig.Memory}}' "$C")"
echo
echo "--- was der Kern wirklich haelt (cgroup + /proc), nur wenn er laeuft:"
PID="$(sudo podman inspect -f '{{.State.Pid}}' "$C")"
if [ "$PID" != "0" ]; then
  CG="$(sudo cat /proc/$PID/cgroup | cut -d: -f3)"
  echo "pids.max  : $(sudo cat /sys/fs/cgroup$CG/pids.max 2>/dev/null)"
  echo "memory.max: $(sudo cat /sys/fs/cgroup$CG/memory.max 2>/dev/null)"
  echo "cpu.max   : $(sudo cat /sys/fs/cgroup$CG/cpu.max 2>/dev/null)"
  echo "NoNewPrivs: $(sudo grep NoNewPrivs /proc/$PID/status 2>/dev/null)"
  echo "CapEff    : $(sudo grep CapEff /proc/$PID/status 2>/dev/null)"
  echo "mountinfo /data:"; sudo grep ' /data ' /proc/$PID/mountinfo 2>/dev/null
  echo "mountinfo ngc-hold:"; sudo grep 'ngc-hold' /proc/$PID/mountinfo 2>/dev/null
else
  echo "(Behaelter laeuft nicht - der Kern haelt nichts, nur podmans Sicht oben gilt)"
fi
