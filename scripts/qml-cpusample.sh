#!/usr/bin/env bash
# Live CPU sample of one renderer at its real frame rate: a bounded run,
# 4s warm-up, then the process's CPU over a 7s window (plus RSS and its
# last GFX_FPS line). This is what a user sees in top — and on a powersave
# governor most of it is the clock ramping after each frame's sleep; see
# qml-cpuframe.sh for the code's own cost.
#
#   scripts/qml-cpusample.sh "gml" /tmp/qmlrun -file .../Main.qml -w 900 -h 700
label="$1"; bin="$2"; shift 2
BOUND_MEM=3G BOUND_CPU=300% scripts/bounded.sh "$bin" "$@" >/tmp/cs.log 2>&1 &
child=$!; sleep 4
pid=$(pgrep -n -f "^$bin" 2>/dev/null)
if [ -z "$pid" ]; then echo "$label: process not found; log: $(tail -2 /tmp/cs.log | tr '\n' ' ' | cut -c1-160)"; kill $child 2>/dev/null; wait $child 2>/dev/null; exit; fi
t0=$(awk '{print $14+$15}' /proc/$pid/stat); sleep 7; t1=$(awk '{print $14+$15}' /proc/$pid/stat 2>/dev/null)
rss=$(awk '/VmRSS/{printf "%.0f", $2/1024}' /proc/$pid/status 2>/dev/null)
kill $pid 2>/dev/null; sleep 0.5; kill -9 $pid 2>/dev/null; wait $child 2>/dev/null
echo "$label: cpu $(( (t1-t0) * 100 / (100*7) ))%  rss ${rss}MiB  $(grep '^shell:' /tmp/cs.log | tail -1 | cut -c8-30)"
