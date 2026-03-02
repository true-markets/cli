package output

import (
	"encoding/json"
	"fmt"
	"io"
)

// WriteJSON writes indented JSON to w.
func WriteJSON(w io.Writer, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}
	_, _ = fmt.Fprintln(w, string(data))
	return nil
}
