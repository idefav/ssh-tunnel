package admin

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseTailLines(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{name: "empty uses default", input: "", want: defaultLogTailLines},
		{name: "invalid uses default", input: "abc", want: defaultLogTailLines},
		{name: "zero uses default", input: "0", want: defaultLogTailLines},
		{name: "valid value", input: "200", want: 200},
		{name: "clamps large value", input: "999999", want: maxLogTailLines},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseTailLines(tt.input); got != tt.want {
				t.Fatalf("parseTailLines(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestReadLastLogLines(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		maxLines int
		want     []string
	}{
		{
			name:     "returns last N lines",
			content:  "line1\nline2\nline3\nline4\n",
			maxLines: 2,
			want:     []string{"line3", "line4"},
		},
		{
			name:     "handles missing trailing newline",
			content:  "line1\nline2\nline3",
			maxLines: 2,
			want:     []string{"line2", "line3"},
		},
		{
			name:     "skips blank lines",
			content:  "line1\n\nline2\r\nline3\n",
			maxLines: 3,
			want:     []string{"line1", "line2", "line3"},
		},
		{
			name:     "empty file",
			content:  "",
			maxLines: 5,
			want:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "app.log")
			if err := os.WriteFile(path, []byte(tt.content), 0o644); err != nil {
				t.Fatalf("write file: %v", err)
			}

			file, err := os.Open(path)
			if err != nil {
				t.Fatalf("open file: %v", err)
			}
			defer file.Close()

			got, err := readLastLogLines(file, tt.maxLines)
			if err != nil {
				t.Fatalf("readLastLogLines returned error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("readLastLogLines() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
