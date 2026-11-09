Get stdout/stderr from a background shell by ID; returns whatever the job has printed so far. Call it when you need the job's result, not right after starting the job, and set wait=true only when blocking until the job finishes is what you want.

Omit shell_id to list every tracked job with its state. Use that before starting another long command: a previous run that is still going keeps its resources (a GPU, a port, a lock file) and will fight the new one.

The first line of a single job's result reports its state: whether it is running or completed, how long it has been going, how long since it last produced output, and how much output it captured. A running job whose "last output" figure keeps growing while its captured bytes stay the same is producing nothing: it is slow, blocked on a lock, or waiting on input. Report that to the user and offer to read the job again later or to kill it, rather than waiting silently. A job that never finishes on its own can be terminated with job_kill.

A job that captured no output at all may simply be writing to a file: when the command redirects its output, this result names that file, so read it instead of waiting on the job.
