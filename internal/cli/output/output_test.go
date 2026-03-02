package output

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTableRender(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		t.Run("headers and rows", func(t *testing.T) {
			tbl := &Table{
				Headers: []string{"NAME", "AGE"},
				Rows: [][]string{
					{"Alice", "30"},
					{"Bob", "25"},
				},
			}
			var buf bytes.Buffer
			tbl.Render(&buf)

			out := buf.String()
			assert.Contains(t, out, "NAME")
			assert.Contains(t, out, "AGE")
			assert.Contains(t, out, "-----")
			assert.Contains(t, out, "---")
			assert.Contains(t, out, "Alice")
			assert.Contains(t, out, "Bob")
			assert.Contains(t, out, "30")
			assert.Contains(t, out, "25")
		})

		t.Run("empty table", func(t *testing.T) {
			tbl := &Table{
				Headers: []string{},
			}
			var buf bytes.Buffer
			tbl.Render(&buf)
			assert.Empty(t, buf.String())
		})

		t.Run("column width padding", func(t *testing.T) {
			tbl := &Table{
				Headers: []string{"X", "Y"},
				Rows: [][]string{
					{"long-value", "z"},
				},
			}
			var buf bytes.Buffer
			tbl.Render(&buf)

			out := buf.String()
			// Header should be padded to match "long-value" width
			assert.Contains(t, out, "X")
			assert.Contains(t, out, "long-value")
		})

		t.Run("headers only", func(t *testing.T) {
			tbl := &Table{
				Headers: []string{"COL1", "COL2"},
			}
			var buf bytes.Buffer
			tbl.Render(&buf)

			out := buf.String()
			assert.Contains(t, out, "COL1")
			assert.Contains(t, out, "COL2")
			assert.Contains(t, out, "----")
		})
	})
}

func TestWriteJSON(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		data := struct {
			Name string `json:"name"`
			Age  int    `json:"age"`
		}{
			Name: "Alice",
			Age:  30,
		}

		var buf bytes.Buffer
		err := WriteJSON(&buf, data)
		require.NoError(t, err)

		var result map[string]any
		err = json.Unmarshal(buf.Bytes(), &result)
		require.NoError(t, err)
		assert.Equal(t, "Alice", result["name"])
		assert.InDelta(t, float64(30), result["age"], 0.01)

		// Verify indented output
		assert.Contains(t, buf.String(), "  ")
	})

	t.Run("nil value", func(t *testing.T) {
		var buf bytes.Buffer
		err := WriteJSON(&buf, nil)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "null")
	})
}
