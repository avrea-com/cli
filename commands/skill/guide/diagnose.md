# Diagnose a failed or slow run

Work from the cheapest evidence to the most expensive:

```sh
avr run diagnose <run> --json
avr run view <run> --log-failed
avr tests list <run> --all-jobs --status failed --status errored
avr job logs <job-id> --failed
avr job metrics <job-id> --source cpu --source memory --source filesystem
avr log search --repo owner/name --query "<error text>" --level error
avr artifact list owner/name --run-id <run>
```

- `run diagnose` combines failed steps, log tails, queue and execution
  timings, runner metrics and a baseline from earlier successful runs. Start
  there.
- `tests list <run> --all-jobs` reads every job of the run; `--job <name>`
  reads one. A failed test is data, not a command error. A listing returns at
  most `--limit` results; when it reports a continuation cursor, continue with
  `--cursor` before saying which tests failed. A job whose results could not
  be listed is named; read it with `avr tests list <job-id>` before saying it
  had no failures.
- For one failed test, `avr tests view <result-id>` prints its failure summary
  and when it started failing on its branch: the last pass and the first
  failure. `tests list`, `tests search` and `tests rank` print that command
  under their table for each failed or errored row; every row's `result_id`
  is in their `--json '*'` output. `avr tests history <test>` lists its
  earlier runs, and
  `avr tests rank --order failed_reports.desc` the tests that failed in the
  most recent reports. Use them to tell a new failure from an old or flaky one.
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
