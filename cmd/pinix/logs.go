// Role:    CLI command for viewing Clip stderr log files
// Depends: bufio, fmt, io, os, path/filepath, time, cobra
// Exports: newLogsCommand

package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
)

func newLogsCommand() *cobra.Command {
	var tail int
	var follow bool
	var system bool

	cmd := &cobra.Command{
		Use:   "logs [alias]",
		Short: "Show Clip stderr log output",
		Long: `Show log output from a Clip's stderr stream.

Logs are stored in ~/.pinix/logs/<alias>.log and contain timestamped
lines from the Clip process stderr (console.error, exceptions, etc).`,
		Args: func(cmd *cobra.Command, args []string) error {
			if system {
				if len(args) > 0 {
					return fmt.Errorf("--system does not take an alias argument")
				}
				return nil
			}
			if len(args) != 1 {
				return fmt.Errorf("requires a clip alias argument (or use --system)")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			logPath, err := resolveLogPath(args, system)
			if err != nil {
				return err
			}

			if follow {
				return followLog(logPath)
			}
			return showTail(logPath, tail)
		},
	}

	cmd.Flags().IntVar(&tail, "tail", 50, "number of lines to show")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "follow log output (like tail -f)")
	cmd.Flags().BoolVar(&system, "system", false, "show pinixd system log instead of clip log")
	return cmd
}

func resolveLogPath(args []string, system bool) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home directory: %w", err)
	}
	logsDir := filepath.Join(home, ".pinix", "logs")

	if system {
		return filepath.Join(logsDir, "pinixd.log"), nil
	}
	return filepath.Join(logsDir, args[0]+".log"), nil
}

func showTail(logPath string, n int) error {
	f, err := os.Open(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no log file found at %s", logPath)
		}
		return fmt.Errorf("open log file: %w", err)
	}
	defer f.Close()

	lines, err := readLastLines(f, n)
	if err != nil {
		return err
	}
	for _, line := range lines {
		fmt.Println(line)
	}
	return nil
}

func readLastLines(r io.Reader, n int) ([]string, error) {
	if n <= 0 {
		n = 50
	}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), 256*1024)

	// Ring buffer to keep last N lines
	ring := make([]string, n)
	count := 0
	for scanner.Scan() {
		ring[count%n] = scanner.Text()
		count++
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read log file: %w", err)
	}

	if count == 0 {
		return nil, nil
	}

	total := count
	if total > n {
		total = n
	}
	result := make([]string, total)
	start := count - total
	for i := 0; i < total; i++ {
		result[i] = ring[(start+i)%n]
	}
	return result, nil
}

func followLog(logPath string) error {
	// Open the file, seek to end, then poll for new content
	f, err := os.Open(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Wait for the file to be created
			fmt.Fprintf(os.Stderr, "waiting for log file %s ...\n", logPath)
			f, err = waitForFile(logPath)
			if err != nil {
				return err
			}
		} else {
			return fmt.Errorf("open log file: %w", err)
		}
	}
	defer f.Close()

	// Seek to end
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return fmt.Errorf("seek log file: %w", err)
	}

	reader := bufio.NewReader(f)
	for {
		line, err := reader.ReadString('\n')
		if err == nil {
			// Got a complete line
			fmt.Print(line)
			continue
		}
		if err != io.EOF {
			return fmt.Errorf("read log file: %w", err)
		}
		// EOF — print any partial line content, then poll
		if len(line) > 0 {
			fmt.Print(line)
		}
		time.Sleep(200 * time.Millisecond)

		// Check if the file was rotated (recreated)
		info, statErr := f.Stat()
		if statErr != nil {
			continue
		}
		newInfo, statErr := os.Stat(logPath)
		if statErr != nil {
			continue
		}
		if !os.SameFile(info, newInfo) {
			// File was rotated; reopen
			_ = f.Close()
			f, err = os.Open(logPath)
			if err != nil {
				return fmt.Errorf("reopen rotated log file: %w", err)
			}
			reader = bufio.NewReader(f)
		}
	}
}

func waitForFile(path string) (*os.File, error) {
	for i := 0; i < 300; i++ { // up to ~60 seconds
		time.Sleep(200 * time.Millisecond)
		f, err := os.Open(path)
		if err == nil {
			return f, nil
		}
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("open log file: %w", err)
		}
	}
	return nil, fmt.Errorf("timed out waiting for log file %s", path)
}
