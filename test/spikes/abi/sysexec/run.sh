#!/usr/bin/env bash
# P2 prototype check (docs/arch/abi-spike.md): Go-shaped Syscall6 calls with
# handle-table pointers, executed through the role table and one Panama
# downcall. Usage: run.sh [workdir] [bench]. Needs go1.27.1 and JDK 22+.
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
work="${1:-$here/out}"
mkdir -p "$work/classes"
(cd "$here/../descview" && go run ./typetab "$here/fixture") > "$work/sys-types.tab"
javac -Xlint:all -Werror -d "$work/classes" "$here"/../descview/java/abispike/*.java "$here"/java/abispike/*.java
J=(java -Xmx256m -XX:ErrorFile="$work/hs_err_%p.log" --enable-native-access=ALL-UNNAMED -cp "$work/classes" abispike.SysMain "$work/sys-types.tab" "$work")
"${J[@]}" | tee "$work/sysexec.log"
grep -q '^sysexec: hello from write(2)$' "$work/sysexec.log" || { echo "sysexec: write(1) output missing"; exit 1; }
if [ "${2:-}" == bench ]; then
  "${J[@]}" bench
  (cd "$here" && go run ./gobench)
fi
