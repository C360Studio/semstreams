# Probe containment review

Mode: narrow post-approval implementation review. Reviewer: `semstreams-reviewer`, configured `gpt-6-astra`.
Source MD5: `87186d432ecdb43e0462b87b9974aea7`.
Unchanged production runner MD5: `ef0dde707bec1ab6a39d5ad7293896fe`.

**APPROVE: probe containment correction. No additional findings.**

The reviewer read the source and final evidence appendix. Main fixture and regression control share a private
constructor that passes the supplied context to `exec.CommandContext` and creates a distinct file-backed output
destination. It preserves runner output and avoids output-copy goroutine dependencies.

The control starts synchronously, transfers `Wait` ownership once, closes inherited parent descriptors afterward,
observes readiness and cancels while the probe is held. Its two-second wait bounds terminal joining after cancellation;
it is not a readiness or performance assertion. Mutable process state is read only after completed `Wait`. Failure
cleanup releases the owned gate and checks completion through the existing waiter.

The recorded `CommandContext` to `Command` mutation discriminates this obligation: cancellation stopped reaching the
probe and the control failed. Checksum restoration, the final testinfra race pass and lint pass are recorded.
The reviewer ran no tests or mutations; `git diff --check` passed.

The raw-PID-kill mutation deferral remains accepted only within its recorded limits. This probe evidence does not
establish sensitivity to the prohibited mutation or resolve the explicit-shell-`wait` survivor.
Full and hosted gates remain with the PR owner; this review does not claim they passed.
