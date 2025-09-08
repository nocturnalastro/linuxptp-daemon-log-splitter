// Command linuxptp-daemon-log-splitter splits combined PTP daemon logs into per-run files.
// It scans for tokens like "ptp4l.N.config" or "phc2sys.N.config" and writes
// each line to the corresponding run N output. Lines without a run token are
// treated as global and included in all run files. If no run tokens are seen,
// a single "run_unknown" file is produced containing all lines.
//
// Input is read from -input (file path) or stdin. Output filenames are
// prefixed by -outprefix or derived from the input filename.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	defaultOutPrefix = "split"
	runTokenPattern  = `\b[A-Za-z0-9_-]+\.(\d+)\.config\b`
)

type cliFlags struct {
	inputFile string
	outPrefix string
	help      bool
}

// parseFlags defines and parses command-line flags and returns them.
func parseFlags() cliFlags {
	var flags cliFlags
	flag.StringVar(&flags.inputFile, "input", "", "Input file (default: stdin)")
	flag.StringVar(&flags.outPrefix, "outprefix", "", "Output file prefix (default: derived from input or 'split')")
	flag.BoolVar(&flags.help, "h", false, "Show help")
	flag.BoolVar(&flags.help, "help", false, "Show help")
	flag.Parse()
	return flags
}

// printUsage writes a brief usage message to stderr.
func printUsage() {
	fmt.Fprintf(os.Stderr, "PTP Log Splitter\n")
	fmt.Fprintf(os.Stderr, "Usage: %s [-input file] [-outprefix prefix]\n", filepath.Base(os.Args[0]))
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Reads PTP logs and splits them into per-run files based on tokens like 'ptp4l.N.config' or 'phc2sys.N.config'.\n")
	fmt.Fprintf(os.Stderr, "Lines without a run token are included in all run files.\n")
}

// main parses flags, streams input once, and writes per-run files. A temporary
// common file is used to seed newly encountered run files with lines that had
// no run token earlier in the stream.
func main() {
	flags := parseFlags()

	if flags.help {
		printUsage()
		os.Exit(0)
	}

	var inputReader io.Reader
	var inName string
	if flags.inputFile != "" {
		f, err := os.Open(flags.inputFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: cannot open input file: %v\n", err)
			os.Exit(2)
		}
		defer f.Close()
		inputReader = f
		inName = filepath.Base(flags.inputFile)
	} else {
		inputReader = os.Stdin
		inName = "stdin"
	}

	outPrefix := flags.outPrefix
	if outPrefix == "" {
		if flags.inputFile != "" {
			// Use input file name without extensions as prefix
			base := filepath.Base(flags.inputFile)
			// Remove common extensions like .log, .txt
			for _, ext := range []string{".log", ".txt"} {
				if strings.HasSuffix(strings.ToLower(base), ext) {
					base = base[:len(base)-len(ext)]
					break
				}
			}
			outPrefix = base
		} else {
			outPrefix = defaultOutPrefix
		}
	}

	// Compile regex to find tokens like something.N.config and capture N
	runToken := regexp.MustCompile(runTokenPattern)

	// Prepare a common temp file for lines without run tokens
	commonTempPath := fmt.Sprintf("%s.common.tmp", outPrefix)
	commonFile, err := os.Create(commonTempPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot create common temp file: %v\n", err)
		os.Exit(2)
	}
	defer func() {
		commonFile.Close()
		_ = os.Remove(commonTempPath)
	}()
	commonWriter := bufio.NewWriter(commonFile)

	// Lazy-open run outputs; seed with contents of common temp file on first open
	type runOut struct {
		file   *os.File
		writer *bufio.Writer
	}
	runOutputs := make(map[string]runOut)
	openRun := func(run string) (runOut, error) {
		if ro, ok := runOutputs[run]; ok {
			return ro, nil
		}
		path := fmt.Sprintf("%s.run_%s.log", outPrefix, run)
		f, err := os.Create(path)
		if err != nil {
			return runOut{}, err
		}
		w := bufio.NewWriter(f)
		if err := commonWriter.Flush(); err != nil {
			f.Close()
			return runOut{}, err
		}
		r, err := os.Open(commonTempPath)
		if err != nil {
			f.Close()
			return runOut{}, err
		}
		_, cErr := io.Copy(w, r)
		r.Close()
		if cErr != nil {
			w.Flush()
			f.Close()
			return runOut{}, cErr
		}
		ro := runOut{file: f, writer: w}
		runOutputs[run] = ro
		return ro, nil
	}

	bufReader := bufio.NewReader(inputReader)
	anyRunFound := false

	for {
		line, err := bufReader.ReadString('\n')
		if err != nil && err != io.EOF {
			fmt.Fprintf(os.Stderr, "error: reading input: %v\n", err)
			os.Exit(2)
		}
		if err == io.EOF && len(line) == 0 {
			break
		}
		if len(line) == 0 || line[len(line)-1] != '\n' {
			line = line + "\n"
		}

		matches := runToken.FindAllStringSubmatch(line, -1)
		if len(matches) == 0 {
			if _, werr := commonWriter.WriteString(line); werr != nil {
				fmt.Fprintf(os.Stderr, "error: writing common temp file: %v\n", werr)
				os.Exit(2)
			}
			for _, ro := range runOutputs {
				if _, werr := ro.writer.WriteString(line); werr != nil {
					fmt.Fprintf(os.Stderr, "error: writing run file: %v\n", werr)
					os.Exit(2)
				}
			}
		} else {
			anyRunFound = true
			runsOnLine := make(map[string]struct{})
			for _, m := range matches {
				if len(m) >= 2 {
					runsOnLine[m[1]] = struct{}{}
				}
			}
			for run := range runsOnLine {
				ro, oerr := openRun(run)
				if oerr != nil {
					fmt.Fprintf(os.Stderr, "error: opening run file for %s: %v\n", run, oerr)
					os.Exit(2)
				}
				if _, werr := ro.writer.WriteString(line); werr != nil {
					fmt.Fprintf(os.Stderr, "error: writing run file for %s: %v\n", run, werr)
					os.Exit(2)
				}
			}
		}

		if err == io.EOF {
			break
		}
	}

	if err := commonWriter.Flush(); err != nil {
		fmt.Fprintf(os.Stderr, "error: flushing common temp file: %v\n", err)
		os.Exit(2)
	}
	for run, ro := range runOutputs {
		if err := ro.writer.Flush(); err != nil {
			fmt.Fprintf(os.Stderr, "error: flushing run file for %s: %v\n", run, err)
			os.Exit(2)
		}
		if err := ro.file.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "error: closing run file for %s: %v\n", run, err)
			os.Exit(2)
		}
	}

	if !anyRunFound {
		unknownPath := fmt.Sprintf("%s.run_unknown.log", outPrefix)
		commonFile.Close()
		if err := os.Rename(commonTempPath, unknownPath); err == nil {
			fmt.Fprintf(os.Stderr, "No run tokens found in %s. Wrote all lines to %s\n", inName, unknownPath)
			return
		}
		r, err := os.Open(commonTempPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: opening common temp for copy: %v\n", err)
			os.Exit(2)
		}
		wf, err := os.Create(unknownPath)
		if err != nil {
			r.Close()
			fmt.Fprintf(os.Stderr, "error: creating %s: %v\n", unknownPath, err)
			os.Exit(2)
		}
		if _, err := io.Copy(wf, r); err != nil {
			wf.Close()
			r.Close()
			fmt.Fprintf(os.Stderr, "error: copying to %s: %v\n", unknownPath, err)
			os.Exit(2)
		}
		wf.Close()
		r.Close()
		fmt.Fprintf(os.Stderr, "No run tokens found in %s. Wrote all lines to %s\n", inName, unknownPath)
		return
	}
}
