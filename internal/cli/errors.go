package cli

import (
	"encoding/json"
	"fmt"
	"io"
)

// Exit codes for the CLI.
const (
	ExitSuccess = 0
	ExitGeneral = 1
	ExitUsage   = 2
	ExitAuth    = 3
	ExitAPI     = 4
	ExitNetwork = 5
)

// CLIError is a structured error with an exit code.
type CLIError struct {
	Code    int
	Message string
	Err     error
}

func (e *CLIError) Error() string {
	if e.Err != nil {
		return e.Message + ": " + e.Err.Error()
	}
	return e.Message
}

func (e *CLIError) Unwrap() error {
	return e.Err
}

// exitCodeName maps exit codes to string identifiers for JSON output.
func exitCodeName(code int) string {
	switch code {
	case ExitSuccess:
		return "success"
	case ExitGeneral:
		return "general"
	case ExitUsage:
		return "usage"
	case ExitAuth:
		return "auth"
	case ExitAPI:
		return "api"
	case ExitNetwork:
		return "network"
	default:
		return "unknown"
	}
}

// writeErrorText writes an error message to stderr in plain text format.
func writeErrorText(w io.Writer, msg string) {
	_, _ = fmt.Fprintf(w, "Error: %s\n", msg)
}

// writeErrorJSON writes an error as JSON to the given writer.
func writeErrorJSON(w io.Writer, msg string, code int) {
	payload := struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}{
		Error: msg,
		Code:  exitCodeName(code),
	}
	//nolint:errchkjson // best-effort error formatting
	data, _ := json.MarshalIndent(payload, "", "  ")
	_, _ = fmt.Fprintln(w, string(data))
}
