//go:build !windows

package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	maxLogSize     = 5 * 1024 * 1024 // rotate the log when it gets bigger than 5 MB
	minBackoff     = 5 * time.Second
	maxBackoff     = time.Minute
	healthyRunTime = time.Minute // a run this long resets the backoff
)

var serviceUsage = errors.New(`usage:
  gator service start [--interval 1m] [--feeds 3]   run agg in the background
  gator service stop                                stop it
  gator service restart [--interval 1m] [--feeds 3] stop, then start
  gator service status                              is it running?
  gator service logs [--lines 20] [--follow]        show the log`)

func handlerService(s *state, cmd command) error {
	if len(cmd.args) < 1 {
		return serviceUsage
	}
	sub, args := cmd.args[0], cmd.args[1:]

	switch sub {
	case "start":
		return serviceStart(args)
	case "stop":
		if len(args) > 0 {
			return serviceUsage
		}
		return serviceStop()
	case "restart":
		if _, running := runningServicePID(); running {
			if err := serviceStop(); err != nil {
				return err
			}
		}
		return serviceStart(args)
	case "status":
		if len(args) > 0 {
			return serviceUsage
		}
		return serviceStatus()
	case "logs":
		return serviceLogs(args)
	case "run":
		// Internal: the background supervisor that `start` launches.
		return serviceRun(args)
	default:
		return serviceUsage
	}
}

// ----- start / stop / status / logs -----

func serviceStart(args []string) error {
	interval, feeds, err := parseServiceFlags(args)
	if err != nil {
		return err
	}
	if pid, running := runningServicePID(); running {
		return fmt.Errorf("gator service is already running (PID %d)", pid)
	}

	pidPath, logPath, err := servicePaths()
	if err != nil {
		return err
	}
	rotateLog(logPath)

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("couldn't open log file: %w", err)
	}
	defer logFile.Close()

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("couldn't find the gator binary: %w", err)
	}

	sup := exec.Command(exe, "service", "run", "--interval", interval, "--feeds", strconv.Itoa(feeds))
	sup.Stdout = logFile
	sup.Stderr = logFile
	// Setsid starts the supervisor in its own session, so it keeps running
	// after you close the terminal.
	sup.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := sup.Start(); err != nil {
		return fmt.Errorf("couldn't start the service: %w", err)
	}
	pid := sup.Process.Pid
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(pid)), 0o644); err != nil {
		_ = sup.Process.Kill()
		return fmt.Errorf("couldn't write PID file: %w", err)
	}
	_ = sup.Process.Release()

	// Give it a moment, then make sure it didn't die straight away.
	time.Sleep(time.Second)
	if !processAlive(pid) {
		_ = os.Remove(pidPath)
		lines, _ := lastLines(logPath, 5)
		return fmt.Errorf("the service stopped right after starting. Last log lines:\n%s", strings.Join(lines, "\n"))
	}

	fmt.Printf("Started gator service (PID %d): up to %d feeds every %s.\n", pid, feeds, interval)
	fmt.Printf("Logs: %s  (gator service logs)\n", logPath)
	return nil
}

func serviceStop() error {
	pidPath, _, err := servicePaths()
	if err != nil {
		return err
	}
	pid, running := runningServicePID()
	if !running {
		_ = os.Remove(pidPath)
		return errors.New("gator service is not running")
	}

	// SIGTERM lets the supervisor stop agg cleanly first.
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return fmt.Errorf("couldn't stop the service: %w", err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for processAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
	}
	if processAlive(pid) {
		_ = syscall.Kill(-pid, syscall.SIGKILL) // the whole process group
	}
	_ = os.Remove(pidPath)

	fmt.Printf("Stopped gator service (PID %d).\n", pid)
	return nil
}

func serviceStatus() error {
	pidPath, logPath, err := servicePaths()
	if err != nil {
		return err
	}

	if pid, running := runningServicePID(); running {
		fmt.Printf("● gator service is running (PID %d)", pid)
		if info, err := os.Stat(pidPath); err == nil {
			fmt.Printf(", up %s", time.Since(info.ModTime()).Round(time.Second))
		}
		fmt.Println()
		if restarts := countRestarts(logPath); restarts > 0 {
			fmt.Printf("  agg has been restarted %d time(s) since the service started\n", restarts)
		}
	} else {
		fmt.Println("○ gator service is not running. Start it with: gator service start")
	}

	lines, err := lastLines(logPath, 5)
	if err == nil && len(lines) > 0 {
		fmt.Println("\nRecent log:")
		for _, l := range lines {
			fmt.Println("  " + l)
		}
	}
	return nil
}

func serviceLogs(args []string) error {
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	n := fs.Int("lines", 20, "how many lines to show")
	follow := fs.Bool("follow", false, "keep printing new lines (Ctrl+C to stop)")
	fs.BoolVar(follow, "f", false, "short for --follow")
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 || *n < 1 {
		return serviceUsage
	}

	_, logPath, err := servicePaths()
	if err != nil {
		return err
	}
	lines, err := lastLines(logPath, *n)
	if errors.Is(err, os.ErrNotExist) {
		return errors.New("no log yet. Start the service with: gator service start")
	}
	if err != nil {
		return err
	}
	for _, l := range lines {
		fmt.Println(l)
	}
	if !*follow {
		return nil
	}

	// Follow: print whatever is appended to the file until Ctrl+C.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	f, err := os.Open(logPath)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return err
	}
	for {
		if _, err := io.Copy(os.Stdout, f); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// ----- the supervisor -----

// serviceRun keeps `gator agg` running, restarting it with a growing delay
// (5s, 10s, 20s ... up to 1m) whenever it exits.
func serviceRun(args []string) error {
	interval, feeds, err := parseServiceFlags(args)
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	pidPath, _, err := servicePaths()
	if err != nil {
		return err
	}
	defer removePIDFileIfOurs(pidPath)

	signal.Ignore(syscall.SIGHUP) // keep running when the terminal closes
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Printf("[service] started (PID %d): agg %s --feeds %d", os.Getpid(), interval, feeds)
	backoff := minBackoff

	for {
		started := time.Now()
		agg := exec.Command(exe, "agg", interval, "--feeds", strconv.Itoa(feeds))
		agg.Stdout = os.Stdout
		agg.Stderr = os.Stderr

		done := make(chan error, 1)
		if err := agg.Start(); err != nil {
			done <- err
		} else {
			go func() { done <- agg.Wait() }()
		}

		select {
		case <-ctx.Done():
			log.Printf("[service] stopping agg...")
			if agg.Process != nil {
				_ = agg.Process.Signal(syscall.SIGTERM)
				select {
				case <-done:
				case <-time.After(10 * time.Second):
					_ = agg.Process.Kill()
					<-done
				}
			}
			log.Printf("[service] stopped")
			return nil

		case err := <-done:
			if time.Since(started) >= healthyRunTime {
				backoff = minBackoff
			}
			reason := "exited"
			if err != nil {
				reason = err.Error()
			}
			log.Printf("[service] agg stopped unexpectedly (%s); restarting in %s", reason, backoff)

			select {
			case <-ctx.Done():
				log.Printf("[service] stopped")
				return nil
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, maxBackoff)
		}
	}
}

// ----- helpers -----

func parseServiceFlags(args []string) (string, int, error) {
	fs := flag.NewFlagSet("service", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	interval := fs.String("interval", "1m", "time between fetches")
	feeds := fs.Int("feeds", 3, "feeds to fetch at the same time")
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 {
		return "", 0, serviceUsage
	}
	d, err := time.ParseDuration(*interval)
	if err != nil {
		return "", 0, fmt.Errorf("invalid --interval: %w", err)
	}
	if d < time.Second {
		return "", 0, errors.New("--interval must be at least 1s")
	}
	if *feeds < 1 || *feeds > 20 {
		return "", 0, errors.New("--feeds must be between 1 and 20")
	}
	return *interval, *feeds, nil
}

// servicePaths returns ~/.gator/service.pid and ~/.gator/agg.log.
func servicePaths() (pidPath, logPath string, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	dir := filepath.Join(home, ".gator")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	return filepath.Join(dir, "service.pid"), filepath.Join(dir, "agg.log"), nil
}

// runningServicePID reads the PID file and checks the process is really
// our supervisor (PIDs get reused after a reboot).
func runningServicePID() (int, bool) {
	pidPath, _, err := servicePaths()
	if err != nil {
		return 0, false
	}
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 || !processAlive(pid) {
		return 0, false
	}
	cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return pid, true // no /proc (macOS): trust the PID file
	}
	return pid, bytes.Contains(cmdline, []byte("service\x00run"))
}

func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	if err != nil && !errors.Is(err, syscall.EPERM) {
		return false
	}
	// A process that has exited but not been cleaned up yet is a "zombie".
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err == nil {
		if i := bytes.LastIndexByte(stat, ')'); i >= 0 && i+2 < len(stat) && stat[i+2] == 'Z' {
			return false
		}
	}
	return true
}

func removePIDFileIfOurs(pidPath string) {
	data, err := os.ReadFile(pidPath)
	if err == nil && strings.TrimSpace(string(data)) == strconv.Itoa(os.Getpid()) {
		_ = os.Remove(pidPath)
	}
}

func rotateLog(logPath string) {
	if info, err := os.Stat(logPath); err == nil && info.Size() > maxLogSize {
		_ = os.Rename(logPath, logPath+".1")
	}
}

func lastLines(path string, n int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		lines = append(lines, sc.Text())
		if len(lines) > n {
			lines = lines[1:]
		}
	}
	return lines, sc.Err()
}

// countRestarts counts restarts since the most recent "[service] started" line.
func countRestarts(logPath string) int {
	f, err := os.Open(logPath)
	if err != nil {
		return 0
	}
	defer f.Close()
	count := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.Contains(line, "[service] started"):
			count = 0
		case strings.Contains(line, "restarting in"):
			count++
		}
	}
	return count
}
