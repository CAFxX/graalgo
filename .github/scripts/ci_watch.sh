#!/usr/bin/env bash
# ci_watch.sh [INTERVAL_SECONDS] [LOGFILE] — a resource sampler for long CI
# steps. Run it in the background next to the step; it prints one line per
# interval (to stdout, which the Actions log streams live, and to LOGFILE for
# the artifact):
#   [watch] <utc time> load=<1m> avail=<MB> swap=<MB> procs=<n> java=<n> top=<comm>:<MB>,...
# plus any kernel OOM-killer line it can read. The weekly full-suite job once
# died with "The hosted runner lost communication with the server" and left
# no log at all (runner starved of memory by 4 uncapped JVMs): the last
# [watch] lines before a loss are the evidence a future one needs.
# Never exits on its own; the caller kills it (the job end reaps it).
set -u
interval="${1:-20}"
logfile="${2:-/dev/null}"
last_oom=""
while :; do
  load="$(cut -d' ' -f1 /proc/loadavg)"
  avail_mb="$(awk '/^MemAvailable:/{print int($2/1024)}' /proc/meminfo)"
  swap_mb="$(awk '/^SwapTotal:/{t=$2} /^SwapFree:/{f=$2} END{print int((t-f)/1024)}' /proc/meminfo)"
  procs="$(ls -d /proc/[0-9]* 2>/dev/null | wc -l)"
  # ps already sorted by RSS: top three, "comm:MB".
  top="$(ps -eo rss=,comm= --sort=-rss 2>/dev/null | head -3 \
    | awk '{printf "%s%s:%d", (NR>1?",":""), $2, $1/1024}')"
  java="$(ps -eo comm= 2>/dev/null | grep -c '^java$')"
  line="[watch] $(date -u +%H:%M:%S) load=${load} avail=${avail_mb}MB swap=${swap_mb}MB procs=${procs} java=${java} top=${top}"
  echo "$line" | tee -a "$logfile"
  oom="$( (dmesg 2>/dev/null || sudo -n dmesg 2>/dev/null) | grep -i 'out of memory\|oom-kill\|killed process' | tail -1)"
  if [ -n "$oom" ] && [ "$oom" != "$last_oom" ]; then
    echo "[watch] KERNEL: $oom" | tee -a "$logfile"
    last_oom="$oom"
  fi
  sleep "$interval"
done
