# repo-excludes

This native extension keeps local agent artifacts out of Git's untracked-file list.
At session start, it adds these exact entries to Git's private `info/exclude` file:

```gitignore
.agent-work/
.ouro/
.curator/
```

The extension checks project trust before it starts Git. Untrusted projects and directories outside Git remain unchanged.
It uses `git rev-parse` with fixed arguments, a five-second deadline, and no shell.
Git supplies the common directory and exclude path. Linked worktrees share the correct common exclude file.
Separate Git directories outside the working tree are supported.

The extension preserves existing bytes, adds a missing final newline when necessary, and appends only missing exact entries.
It does not edit `.gitignore`, repository configuration, or tracked files.
Repeated session starts do not add duplicate entries. Existing duplicate entries remain unchanged.
The extension preserves existing file permissions. A new exclude file uses mode `0600`.

The extension uses Git's exclusive `exclude.lock` protocol for concurrent writers.
It reads the exclude file after it acquires the lock, syncs the completed replacement, and renames it atomically.
It never removes another writer's lock. Lock waits use the same bounded deadline.
The extension checks file identity before replacement. It rejects symbolic links, special files, and files larger than 1 MiB.
Rooted file operations prevent path traversal outside Git's resolved common directory.
Failures produce an error notification and a returned event error.

The extension adds no tools, commands, prompts, or LLM requests.
It runs on `session_start`, not after arbitrary working-directory changes during a session.

## Load

```sh
pig -e /absolute/path/to/extensions/repo-excludes
```

The parent plugin manifest can register `extensions/repo-excludes` with identity `repo-excludes`.

## Tests

```sh
go test ./internal/repoexcludes ./extensions/repo-excludes
```

Unit tests use real temporary Git repositories. They cover repeated starts, exact entries, prior bytes, missing newlines, permissions, linked worktrees, separate Git directories, concurrent writers, lock deadlines, and failures.
The RPC smoke test loads the real extension in isolated PiG homes. It checks trusted, untrusted, non-Git, and failed startups without an LLM request.
The smoke test requires `pig` on `PATH`. It skips when that binary is absent.
