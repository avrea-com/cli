# Diagnose a failed or slow run

Work from the cheapest evidence to the most expensive:

```sh
avr run diagnose <run> --json
avr run view <run> --log-failed
avr tests list <run> --job <job-name> --status failed --status errored
avr job logs <job-id> --failed
avr job metrics <job-id> --source cpu --source memory --source filesystem
avr log search --repo owner/name --query "<error text>" --level error
avr artifact list owner/name --run-id <run>
```

- `run diagnose` combines failed steps, log tails, queue and execution
  timings, runner metrics and a baseline from earlier successful runs. Start
  there.
- `tests list` reads one job. Omit `--job` when the run has a single job. A failed
  test is data, not a command error. A listing returns at most `--limit`
  results; when it reports a continuation cursor, continue with `--cursor`
  before saying which tests failed.
- Check metrics for timeouts, out-of-memory kills, full disks and stalls.
- `log search` covers the repository's recent runs: use it to tell whether an
  error is new or recurring.
- `artifact list` and `artifact download` fetch test reports and build outputs
  a run uploaded.
- `avr job ssh <job-id>` reaches a job only while it is still running.

Before changing code, decide whether the failure is in the change, a flaky
test, the cache, or the infrastructure. Report the run, the commit and the
failing step. Rerun only when the task allows it and the evidence points to a
flake or an infrastructure fault, and say so before you do:

```sh
avr run rerun <run> --failed --yes
```
