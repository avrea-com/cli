# Why CI is slow

```sh
avr status --repo owner/name --since 7d
avr workflow list --repo owner/name
avr workflow view <workflow> --repo owner/name
avr run diagnose <run> --json
avr cache usage --repo owner/name
```

Find the job that gates the workflow longest (its p95 in `workflow view`),
then split its time: queue wait or execution (`run diagnose`), which steps
(`avr job view <job-id>`), and whether the runner was saturated
(`job metrics`). Check caches before blaming the runner, and right-size last.

Inside a test job, `avr tests rank --repo owner/name` ranks tests by mean
duration over the repository's most recent reports. Durations are test time,
not job time, and the header says which reports were searched.

Do not delete cache entries to experiment. Caches are shared by every run of
the repository.

## Right-size runners

Avrea suggests a larger or smaller runner label per job from the CPU, memory
and disk its recent successful runs used:

```sh
avr org list --json organization_id,name
avr repo list --json full_name,repository_id
avr api /orgs/<org-id>/repos/<repo-id>/workflows/right-sizing -X GET -F window_days=7
```

Each workflow in `data` lists its jobs with a verdict and, where one fits, a
suggested label. Before changing `runs-on`:

- Read the job's steps in the workflow file. Low usage can be self-imposed:
  `pytest -n 1`, `make -j1`, `--max-workers`, `GOMAXPROCS`, a matrix that
  splits the work, or a step that waits on a service. Then the fix may be more
  parallelism on the same runner, not a smaller one.
- Confirm with `avr job metrics <job-id>` on a recent run, and
  `avr workflow view <workflow>` for its duration trend.
- Change only the label the suggestion names, and propose it as a pull
  request. A new label starts fresh suggestions; suggestions refresh at most
  hourly.
