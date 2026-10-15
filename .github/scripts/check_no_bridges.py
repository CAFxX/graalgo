#!/usr/bin/env python3
"""
No-bridges check (2026-10-15, his direct order).

Fails if the diff introduces new bridges:
- New synth_*.go files
- New package-name conditionals in frontend/backend
- New panic stubs
- New wire node kinds for stdlib functions
- New synthetic function registrations

Existing bridges are grandfathered (they're in the removal queue).
This check only looks at ADDED lines in the diff.
"""

import re
import subprocess
import sys

def get_diff():
    """Get the diff of added lines (staged + unstaged + vs base)."""
    # Try vs origin/master first (for PRs), fall back to working tree
    for cmd in [
        ["git", "diff", "origin/master...HEAD", "--", "."],
        ["git", "diff", "HEAD", "--", "."],
        ["git", "diff", "--cached", "--", "."],
    ]:
        try:
            result = subprocess.run(cmd, capture_output=True, text=True, timeout=30)
            if result.stdout.strip():
                return result.stdout
        except:
            continue
    return ""

def check_no_bridges(diff):
    errors = []
    added_lines = []
    current_file = None
    
    for line in diff.split("\n"):
        if line.startswith("+++ b/"):
            current_file = line[6:]
        elif line.startswith("+") and not line.startswith("+++"):
            added_lines.append((current_file, line[1:]))
    
    # 1. New synth_*.go files
    new_synth = set()
    for line in diff.split("\n"):
        if line.startswith("+++ b/") and "synth_" in line and line.endswith(".go"):
            new_synth.add(line[6:])
    for f in new_synth:
        errors.append(f"NEW SYNTHETIC FILE: {f} — synthetic stdlib packages are banned")
    
    # 2. Package-name conditionals (stdlib names)
    stdlib_pkgs = ["fmt", "sync", "os", "net", "http", "runtime", "reflect", "time", 
                   "strings", "bytes", "strconv", "sort", "math", "encoding", "crypto",
                   "syscall", "unsafe", "io", "bufio", "context", "errors"]
    for fname, line in added_lines:
        if not fname or not (fname.endswith(".go") or fname.endswith(".java")):
            continue
        # Match: if pkg == "fmt", case "sync", pkg.Name == "os", etc.
        for pkg in stdlib_pkgs:
            patterns = [
                rf'==\s*"{pkg}"',
                rf'case\s*"{pkg}"',
                rf'Name\s*==\s*"{pkg}"',
            ]
            for pat in patterns:
                if re.search(pat, line):
                    # Allow in test files and docs
                    if "/test" not in fname and "test_" not in fname:
                        errors.append(f"PACKAGE-NAME CONDITIONAL in {fname}: {line.strip()[:80]}")
                        break
    
    # 3. New panic stubs
    for fname, line in added_lines:
        if not fname or not fname.endswith(".go"):
            continue
        if re.search(r'panic\s*\(\s*"(not implemented|unsupported|todo|TODO)', line, re.I):
            errors.append(f"PANIC STUB in {fname}: {line.strip()[:80]}")
    
    # 4. New wire node kinds for stdlib
    for fname, line in added_lines:
        if not fname or "export.go" not in fname:
            continue
        m = re.search(r'e\.node\s*\(\s*"([^"]+)"', line)
        if m:
            node_kind = m.group(1)
            # Flag runtime*/fmt*/sync*/etc. node kinds (stdlib-specific)
            if re.match(r'^(runtime|fmt|sync|os|net)[a-z]', node_kind):
                errors.append(f"SUSPICIOUS WIRE NODE in {fname}: {node_kind}")
    
    # 5. New synthetic function registrations
    for fname, line in added_lines:
        if not fname or not fname.endswith(".go"):
            continue
        if re.search(r'addFunc\s*\(|synthetic.*Package\s*\(|registerSynthetic', line):
            # Allow in existing synth files (grandfathered), flag in new contexts
            if "synth_" not in fname:
                errors.append(f"SYNTHETIC REGISTRATION in {fname}: {line.strip()[:80]}")
    
    return errors

def main():
    diff = get_diff()
    if not diff.strip():
        print("No diff found — skipping no-bridges check")
        return 0
    
    errors = check_no_bridges(diff)
    if errors:
        print("NO-BRIDGES CHECK FAILED:")
        print()
        for e in errors:
            print(f"  ✗ {e}")
        print()
        print("His order (2026-10-15): No new bridges. Fix the compiler/runtime properly.")
        return 1
    
    print("No-bridges check passed.")
    return 0

if __name__ == "__main__":
    sys.exit(main())
