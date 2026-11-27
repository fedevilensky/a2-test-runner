# Test Runner

This tool runs programming exercise tests for one or more student folders.

It supports:

- Running selected exercises (or all available ones)
- Running against one student folder or multiple folders using a glob pattern
- Compiling C++ (`ejercicioN.cpp`) or Java (`EjercicioN.java`) per exercise
- Timing each test case
- Classifying failures as compilation error, runtime error, or diff error
- Generating HTML reports per student and a global summary across students

## Ways to run

The command line runs once and prints progress and summaries in the terminal.
Specify a student folder or glob pattern, select all exercises or numbers and
ranges, and apply C++ flags to the whole run:

```bash
go run . -s ./student-a
```

The local server stays running while you configure runs and browse reports.
Enter or pick paths, scan a base path and pattern, or upload folder copies. Select
exercises or individual input files, and set global or per-student C++ flags.
The browser shows live progress and reports as each student finishes:

```bash
go run . serve --listen 8080
```

Both modes write SQLite data and static HTML reports.

Both modes use `test-results` by default and can use the same output directory.
When the server opens the output directory used by a CLI run, its saved results
appear in the report sidebar. For relative output paths, start both commands from
the same working directory.
Run `go run . --help` for CLI options and `go run . serve --help` for server options.

## Command-line options

These options configure a one-shot run without the `serve` subcommand:

- `--tests-folder` or `-t` (default: `./tests`): folder containing exercise test folders.
- `--test-numbers` or `-n` (optional): comma-separated list/ranges like `1-3,5,7-9`.
 If omitted, all discovered exercises are executed.
- `--student-folder` or `-s` (required unless `-p` is provided): folder of a single student.
- `--student-folder-pattern` or `-p` (optional): glob pattern to select multiple student folders.
 If provided, `--student-folder` is ignored.
- `--output-dir` or `-o` (default: `test-results`): directory for SQLite data and static reports.
- `--cpp-standard` (default: `c++11`) and `--optimization` (for example `-O2`): C++ build settings.
- `--compiler-flag`: an additional compiler argument; repeat the flag for multiple arguments.
- `--timeout` or `-T` (default: 30 seconds): time limit for each test case.
- `--incremental`: reuse results when the relevant inputs have not changed.
- `--force`: run all selected tests even when `--incremental` is set.

## Expected Test Layout

The runner expects `--tests-folder` to contain subfolders with an exercise number in the name,
for example:

```text
tests/
 ejercicio1/
  01.in.txt
  01.out.txt
 ejercicio2/
  01.in.txt
  01.out.txt
```

Within each exercise folder:

- Input files: `*.in.txt`
- Expected output files: matching `*.out.txt`

## Expected Student Source Names

For exercise `N`, the runner looks for:

- C++: `ejercicioN.cpp`
- Java: `EjercicioN.java`

If both exist, C++ is used.

## Command-line examples

Run all exercises for one student (uses `./tests` by default):

```bash
go run . -s ./student-a
```

Run all exercises for one student with an explicit tests folder:

```bash
go run . -t ./Pruebas -s ./student-a
```

Run selected exercises for one student:

```bash
go run . -n 1-3,5,8 -s ./student-a
```

Run selected exercises for all matching folders:

```bash
go run . -n 1-6 -p "./students/*"
```

## Local server

Start the server and choose a port (8080 is the default):

```bash
go run . serve --listen 8080 --output-dir test-results
```

Open `http://127.0.0.1:8080`. The server listens on the local loopback address.
`--listen` accepts a port number, and `--output-dir` (or `-o`) selects the shared
results directory. Configure test and student folders, timeout, build flags and
incremental mode in the browser; the one-shot CLI flags do not configure server runs.

Enter a tests path and scan it, or use the native folder picker. For students,
scan a base path and pattern or add individual folder paths. The native picker
requires `zenity` on Linux. You can also upload folder copies with the browser
picker or drag and drop; uploaded folders are temporary and must be uploaded
again after the server restarts.

The server does not automatically restore or autocomplete the tests folder,
student base path, or folder pattern from CLI runs. Saved student paths remain
available through the sidebar rerun button, but you must first enter or choose
and scan the tests folder before rerunning.

Choose whole exercises or expand them to select individual input files. A partial
rerun retains results for unselected exercises and cases. The sidebar stays
available during a run, showing progress and each completed student's report.
Its rerun button runs that student with the current selection and settings.

## Output

Both modes save results and generate HTML reports in `test-results` by default:

- SQLite results: `test-results/data/results.sqlite`
- Per-student report: `test-results/static/<student>/report.html`
- Cross-student summary: `test-results/static/summary/index.html`
- Combined report: `test-results/static/index.html`

Running a student again replaces that student's saved results; a partial rerun
replaces only selected exercises or cases. Reports can be regenerated from the
database. Student folders whose names map to the same report path are rejected.

Each test case includes:

- Pass/fail status
- Execution time
- Failure type (`compilation_error`, `runtime_error`, `diff_error`)
- For diff errors: expected and actual output

The reports are built from the `templates/*_report.templ` components. After changing a
component, run `go generate ./...` (or `go tool templ generate`) and commit the
generated `*_templ.go` files alongside it.

Run `make format` to format Go source files with gofumpt, and `make lint` to
check the project with golangci-lint. Both commands use the tools in `go.mod`.

## Incremental runs

Incremental runs are opt-in. In the CLI, pass `--incremental`; add `--force` to
run all selected tests even when `--incremental` is present. In the web interface,
select **Reuse unchanged passing results** under Run selection; leave it unchecked
to run all selected tests. Only passing results are reused; failed cases run again.
Only incremental runs record fingerprints, so the first incremental run after a
regular or forced run executes the tests and establishes a new cache.

The fingerprint covers the exercise source and its compiler-discovered
transitive dependencies, test input and expected output, compiler settings and
version, and timeout. Changes to unrelated student files do not invalidate the
exercise. Java dependency discovery invokes `javac` even when results can be
reused. Programs that depend on changing runtime files, the network, the clock,
or other outside state should be run with incremental mode disabled. Cached
timings are from the original run.
