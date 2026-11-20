# Test a change in CI

Avrea runners have warm caches and usually more hardware than the local
machine. Keep quick checks local (format, lint, one focused test) and let CI
run dependency installs, full builds and broad suites.

Push only when the task allows it. Then find the run for the exact commit, not
the latest run on the branch:

```sh
avr run list --repo owner/name --branch <branch> --since 1h --limit 100 --json run_id,head_sha,workflow_name,status,conclusion --jq '.[] | select(.head_sha == "<full-sha>")'
avr run watch <run> --exit-status
```

The filter sees only the returned page. No match means the run has not been
recorded yet or is beyond the page: wait and retry, or continue with
`--cursor`. Never treat a missing run as a passing one.

For a workflow with a `workflow_dispatch` trigger, on a branch or tag that is
already pushed:

```sh
avr workflow run <workflow> --repo owner/name --ref <branch> --watch --exit-status
```

It watches the run the dispatch started and exits 1 if that run does not
appear within 90 seconds.
