# On-demand VMs

A VM is billed compute. Create one when the user asks for it or the task needs
a clean machine, another operating system or more hardware than is available
locally, and say so before you create it.

Pick a unique name first, such as `agent-<task>-<random>`, and refer to the VM
by that name from then on:

```sh
avr vm capabilities
avr vm create --name <unique-name> --os linux --size 4-vcpu --ssh-key @~/.ssh/id_ed25519.pub --repo owner/name --ttl 2h --wait > /dev/null
avr vm ssh <unique-name> -- uname -a
avr vm ssh <unique-name> --session work
avr vm delete <unique-name> --yes --wait
avr vm list
```

- `vm create` prints a one-time password on standard output. Send that output
  to `/dev/null` as above, so it never enters your context or a log; SSH with
  the key does not need it. Errors still appear on standard error.
- A VM can exist even when `create` fails or is interrupted. After any
  failure, run `avr vm delete <unique-name> --yes --wait` before trying again,
  and confirm with `avr vm list` that it is gone. Do the same at the end of
  the task, against the same host and organization.
- `avr vm metrics <unique-name> --watch` shows the VM's CPU, memory and IO.
- `--repo` preloads the checkout and gives the VM that repository's caches.
  `--ref` takes a branch name, `refs/pull/<number>/head` or a 40-character
  commit SHA, not a tag.
- Choose the smallest sufficient `--size` and a short `--ttl` as a backstop.
  When the TTL expires the VM is paused, or deleted where pausing is
  unavailable.
- `avr vm bootstrap <vm>` installs tools and forwards credentials. Forward
  only what the task needs and preview the plan with `--print`.
- A one-off `avr vm ssh <vm> -- <command>` runs in a non-login shell. Add
  `--login` when it needs the environment that bootstrap forwarded.
