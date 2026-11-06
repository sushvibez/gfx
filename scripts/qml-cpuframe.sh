#!/usr/bin/env bash
# CPU per frame for one of our renderers, without a start-up term: one
# bounded uncapped run; each GFX_FPS line carries the frame count and the
# process's CPU so far, and the answer is the slope between the first
# steady line (after warm-up) and the last. Uncapped, so the core stays at
# speed — at 60fps on the powersave governor a frame measures the clock
# ramp after its sleep more than the code (0.6ms of work reads as 3.3).
#
#   scripts/qml-cpuframe.sh "gml buttons" /tmp/qmlrun -file .../Main.qml -w 900 -h 700
#   scripts/qml-cpuframe.sh "go buttons"  /tmp/go-buttons
# Knobs: CPUFRAME_FRAMES (default 2400), CPUFRAME_SKIP (steady lines to
# drop first, default 3).
set -u
label="$1"; shift
frames="${CPUFRAME_FRAMES:-2400}"; skip="${CPUFRAME_SKIP:-3}"
GFX_FPS=1 scripts/bounded.sh "$@" -frames "$frames" 2>&1 >/dev/null |
  sed -n 's/^shell: .*frames=\([0-9]*\) cpu=\([0-9.]*\)s.*/\1 \2/p' |
  awk -v label="$label" -v skip="$skip" '
    { n++; f[n]=$1; c[n]=$2 }
    END {
      if (n <= skip+1) { printf "%s: not enough steady lines (%d)\n", label, n; exit }
      df = f[n]-f[skip+1]; dc = c[n]-c[skip+1]
      printf "%s: %.2f ms CPU/frame  (%d frames over %d lines, %.0f fps)\n", label, dc*1000/df, df, n-skip-1, df/(n-skip-1)
    }'
