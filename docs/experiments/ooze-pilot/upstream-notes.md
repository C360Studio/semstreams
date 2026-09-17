# Pinned Ooze source observations

These are source observations used to select calibration controls. Execution evidence is separate.
All links refer to commit `87c15dcb180492ba96f30278efc0146dd248f09b`;
[upstream-source.json](upstream-source.json) records the module checksum and examined file hashes.

## Runner interpretation

The command runner executes the supplied command without a context/deadline and inherits its environment.
Supervision errors panic. An unsuccessful command exit produces the result the console reporter calls
"killed"; the implementation does not require proof of compilation, test selection, or a named assertion.
Thus a kill requires independent interpretation under the SemStreams testing policy.
[Command runner](https://github.com/gtramontina/ooze/blob/87c15dcb180492ba96f30278efc0146dd248f09b/internal/cmdtestrunner/cmdtestrunner.go#L27),
[console reporter](https://github.com/gtramontina/ooze/blob/87c15dcb180492ba96f30278efc0146dd248f09b/internal/consolereporter/consolereporter.go).

Release iterates mutants without first running an unmodified baseline. Serial execution is the default.
The normal command is `go test -count=1 ./...`; the default minimum score is 1.0.
The pilot uses its own fixed command and an observational threshold, and records independent baseline results.
[Release](https://github.com/gtramontina/ooze/blob/87c15dcb180492ba96f30278efc0146dd248f09b/release.go),
[mutation loop](https://github.com/gtramontina/ooze/blob/87c15dcb180492ba96f30278efc0146dd248f09b/internal/ooze/ooze.go).

Zero evaluated mutations produces a score of -1, below an observational threshold of zero.
This is different from selecting zero tests inside a command that still exits successfully.
[Score calculator](https://github.com/gtramontina/ooze/blob/87c15dcb180492ba96f30278efc0146dd248f09b/internal/scorecalculator/scorecalculator.go#L7).

## Source and process isolation

Unix temporary materialization symlinks source files; overwriting the selected mutation removes that
file's link before writing. Other files retain their links. The pilot therefore supplies disposable real
copies and separately measures writes to an unmutated sentinel. Source isolation is not inferred from
the temporary-directory name.
[Materialization](https://github.com/gtramontina/ooze/blob/87c15dcb180492ba96f30278efc0146dd248f09b/internal/fsrepository/materialize_unix.go#L10),
[overwrite](https://github.com/gtramontina/ooze/blob/87c15dcb180492ba96f30278efc0146dd248f09b/internal/fsrepository/fstemporaryrepository.go).

The Darwin launcher creates a separate process group and supervises its exit. The cancellation experiment
must observe that group as well as the parent. No result here generalizes to Linux or Windows cleanup.
[Darwin supervision](https://github.com/gtramontina/ooze/blob/87c15dcb180492ba96f30278efc0146dd248f09b/internal/cmdtestrunner/process_tree_darwin.go).

## Candidate scope

Discovery excludes `_test.go`, uses the default Go build context, and sorts source paths.
A test command's custom build tags do not become source-discovery tags.
The selected comparison operator toggles strict and inclusive inequalities; it does not encode every
plausible semantic fault, such as omitting an error filter, choosing the wrong state owner, or skipping
required cleanup. Custom operators are possible but their authoring and review are additional work.
[Discovery](https://github.com/gtramontina/ooze/blob/87c15dcb180492ba96f30278efc0146dd248f09b/internal/fsrepository/fsrepository.go#L47),
[comparison operator](https://github.com/gtramontina/ooze/blob/87c15dcb180492ba96f30278efc0146dd248f09b/viruses/comparison/comparison.go).
