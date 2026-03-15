package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/zhubert/erg/internal/logger"
)

// maxScannerBufSize is the maximum buffer size for scanning log lines.
// erg.log can contain very long JSON lines, so we use 1 MiB instead of
// bufio's default 64 KiB.
const maxScannerBufSize = 1024 * 1024

var (
	auditEvent    string
	auditWorkitem string
	auditRepo     string
	auditSince    string
	auditJSON     bool
)

var auditCmd = &cobra.Command{
	Use:     "audit",
	Short:   "Query the structured audit log",
	GroupID: "daemon",
	Long: `Reads and filters the JSON-structured erg.log file.

Each log entry is a JSON object. This command allows you to filter by event type,
work item, repo, or time range and display matching entries.

Examples:
  erg audit                             # Show all audit entries
  erg audit --event session.created     # Show session creation events
  erg audit --event pr.merged           # Show PR merge events
  erg audit --workitem owner/repo-123   # Filter by work item ID
  erg audit --repo owner/repo           # Filter by repo
  erg audit --since 24h                 # Events from the last 24 hours
  erg audit --json                      # Raw JSON output`,
	RunE: runAudit,
}

func init() {
	auditCmd.Flags().StringVar(&auditEvent, "event", "", "Filter by event type (e.g. session.created, pr.merged)")
	auditCmd.Flags().StringVar(&auditWorkitem, "workitem", "", "Filter by work item ID")
	auditCmd.Flags().StringVar(&auditRepo, "repo", "", "Filter by repo field")
	auditCmd.Flags().StringVar(&auditSince, "since", "", "Show entries from the last duration (e.g. 24h, 7d)")
	auditCmd.Flags().BoolVar(&auditJSON, "json", false, "Output raw JSON lines instead of formatted table")
	rootCmd.AddCommand(auditCmd)
}

func runAudit(cmd *cobra.Command, args []string) error {
	logPath, err := logger.DefaultLogPath()
	if err != nil {
		return fmt.Errorf("failed to resolve log path: %w", err)
	}

	f, err := os.Open(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no audit log found at %s — has the daemon run yet?", logPath)
		}
		return fmt.Errorf("failed to open audit log: %w", err)
	}
	defer f.Close()

	var since time.Time
	if auditSince != "" {
		d, parseErr := parseDuration(auditSince)
		if parseErr != nil {
			return fmt.Errorf("invalid --since value %q: %w", auditSince, parseErr)
		}
		since = time.Now().Add(-d)
	}

	if auditJSON {
		return streamAuditJSON(cmd.OutOrStdout(), f, since)
	}
	return streamAuditTable(cmd.OutOrStdout(), f, since)
}

// parseDuration extends time.ParseDuration to support "d" (days) suffix.
func parseDuration(s string) (time.Duration, error) {
	if len(s) > 1 && s[len(s)-1] == 'd' {
		d, err := strconv.ParseFloat(s[:len(s)-1], 64)
		if err != nil || d <= 0 {
			return 0, fmt.Errorf("invalid days value: %s", s)
		}
		return time.Duration(d * float64(24*time.Hour)), nil
	}
	return time.ParseDuration(s)
}

// matchesFilters returns true if the entry passes all active filters.
func matchesFilters(entry map[string]any, since time.Time) bool {
	if auditEvent != "" {
		ev, _ := entry["event"].(string)
		if ev != auditEvent {
			return false
		}
	}
	if auditWorkitem != "" {
		wi, _ := entry["workItem"].(string)
		if wi != auditWorkitem {
			return false
		}
	}
	if auditRepo != "" {
		repo, _ := entry["repo"].(string)
		if repo != auditRepo {
			return false
		}
	}
	if !since.IsZero() {
		ts, _ := entry["time"].(string)
		if ts == "" {
			return false
		}
		t, err := time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			// Try other common time formats that slog might emit
			t, err = time.Parse(time.RFC3339, ts)
			if err != nil {
				return false
			}
		}
		if t.Before(since) {
			return false
		}
	}
	return true
}

func streamAuditJSON(w io.Writer, r io.Reader, since time.Time) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, bufio.MaxScanTokenSize), maxScannerBufSize)
	for scanner.Scan() {
		line := scanner.Bytes()
		var entry map[string]any
		if err := json.Unmarshal(line, &entry); err != nil {
			continue // skip non-JSON lines
		}
		if !matchesFilters(entry, since) {
			continue
		}
		fmt.Fprintln(w, string(line))
	}
	return scanner.Err()
}

func streamAuditTable(w io.Writer, r io.Reader, since time.Time) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "TIME\tLEVEL\tEVENT\tMESSAGE\tWORK ITEM")

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, bufio.MaxScanTokenSize), maxScannerBufSize)
	found := 0
	for scanner.Scan() {
		var entry map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue // skip non-JSON lines
		}
		if !matchesFilters(entry, since) {
			continue
		}

		ts := formatAuditTime(entry["time"])
		level, _ := entry["level"].(string)
		event, _ := entry["event"].(string)
		msg, _ := entry["msg"].(string)
		workItem, _ := entry["workItem"].(string)

		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", ts, level, event, msg, workItem)
		found++
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	tw.Flush()
	if found == 0 {
		fmt.Fprintln(w, "(no matching entries)")
	}
	return nil
}

func formatAuditTime(v any) string {
	ts, ok := v.(string)
	if !ok {
		return ""
	}
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		t, err = time.Parse(time.RFC3339, ts)
		if err != nil {
			return ts
		}
	}
	return t.Format("2006-01-02 15:04:05")
}
