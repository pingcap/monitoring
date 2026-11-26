# Rule file updates

The editing API publishes a complete rule file using a temporary file and a
same-directory rename. Readers holding the previous file open finish reading the
previous contents; new readers see the complete replacement. Existing restrictive
permission bits are retained, with executable and group/other write bits removed.
On Unix, replacements also retain the original UID/GID. Ownership and the intended
mode are restored before publication; a failure leaves the old file in place.
The process umask does not further restrict replacements of existing files.
New persistence files are created with mode 0644 (subject to the process umask).
Temporary files are removed on normal error paths.

Editable files must be regular files. Symbolic-link aliases are no longer listed
or accepted for editing. Direct reads of safe aliases within the configured root
remain supported. Deployments that previously edited through an alias must use
its regular target filename.

The API serializes updates through the primary write, optional persistence write,
and Prometheus reload. Both queueing and reload share a bounded request context.
Each file replacement is atomic; the primary and persistence files are not a
multi-file transaction. A persistence or reload error may occur after the primary
file has been published. External writers are not coordinated by this API and
must be restricted to trusted processes. Atomic publication is not a guarantee
against filesystem or host compromise or arbitrary concurrent external edits.
