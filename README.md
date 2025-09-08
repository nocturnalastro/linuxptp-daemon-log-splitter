# linuxptp-daemon-log-splitter

Split combined PTP daemon logs into per-run files.

The tool scans for tokens like `ptp4l.N.config` or `phc2sys.N.config` and writes
each line to the corresponding run `N` output. Lines without a run token are
treated as global and included in all run files. If no run tokens are present,
a single `run_unknown` file is produced containing all lines.

## Install

```bash
go build -o linuxptp-daemon-log-splitter
```

## Usage

```bash
linuxptp-daemon-log-splitter [-input file] [-outprefix prefix]
```

- `-input`: path to the input log file. If omitted, reads from stdin.
- `-outprefix`: prefix for output files. If omitted, derived from `-input`,
  or defaults to `split` when reading from stdin.
- `-h`, `-help`: show help.

## Examples

### From a file

```bash
./linuxptp-daemon-log-splitter -input ptp.log -outprefix ptp
```

Produces files such as:

```text
ptp.run_1.log
ptp.run_2.log
ptp.run_3.log
```

### From stdin

```bash
cat combined.log | ./linuxptp-daemon-log-splitter -outprefix combined
```

### When no run tokens are present

If no `*.N.config` tokens are found, the tool writes all lines to:

```text
<outprefix>.run_unknown.log
```

## Token detection

The run number is matched using the regular expression:

```regex
\b[A-Za-z0-9_-]+\.(\d+)\.config\b
```

The value captured in the first group (`N`) selects the destination run file.
