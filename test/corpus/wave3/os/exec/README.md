# `os/exec` wave-3 corpus

Differential corpus for the os/exec package (L11, REVIEW-2026-11-18-05).

## Scope boundary

LookPath (found + missing), /bin/echo args + captured stdout, ExitError exit code, CombinedOutput, stdin piping, verbatim args. No shell, no network. No .scopelimited programs. Only local /bin helpers are used; process-group / SysProcAttr surface is out of scope.

## Programs

- `00_basic.go` — LookPath, echo capture, exit code, CombinedOutput, stdin piping, verbatim args.

## Retest trigger

exec.Command behaviour changes, or the backend gains process spawning it lacks today.
