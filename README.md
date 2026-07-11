# Test Runner

This tool runs programming exercise tests for one or more student folders.

It supports:
- Running selected exercises (or all available ones)
- Running against one student folder or multiple folders using a glob pattern
- Compiling C++ (`ejercicioN.cpp`) or Java (`EjercicioN.java`) per exercise
- Timing each test case
- Classifying failures as compilation error, runtime error, or diff error
- Generating HTML reports per student and a global summary across students

## Flags

- `--tests-folder` or `-t` (default: `./tests`): folder containing exercise test folders.
- `--test-numbers` or `-n` (optional): comma-separated list/ranges like `1-3,5,7-9`.
	If omitted, all discovered exercises are executed.
- `--student-folder` or `-s` (required unless `-p` is provided): folder of a single student.
- `--student-folder-pattern` or `-p` (optional): glob pattern to select multiple student folders.
	If provided, `--student-folder` is ignored.

## Expected Test Layout

The runner expects `--tests-folder` to contain subfolders with an exercise number in the name,
for example:

```
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

## Run Examples

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

## Output

The tool prints per-student and global summaries in the console.

It also generates HTML reports in `test-results`:
- Per-student report: `test-results/<student>/report.html`
- Cross-student summary: `test-results/summary/index.html`

Each test case includes:
- Pass/fail status
- Execution time
- Failure type (`compilation_error`, `runtime_error`, `diff_error`)
- For diff errors: expected and actual output
