package utils

import (
	"testing"
)

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Basic cases
		{"simple filename", "file.zip", "file.zip"},
		{"filename with spaces", "  file.zip  ", "file.zip"},
		{"filename with backslash", "path\\file.zip", "file.zip"},
		{"filename with forward slash", "path/file.zip", "file.zip"},
		{"filename with colon", "file:name.zip", "file_name.zip"},
		{"filename with asterisk", "file*name.zip", "file_name.zip"},
		{"filename with question mark", "file?name.zip", "file_name.zip"},
		{"filename with quotes", "file\"name.zip", "file_name.zip"},
		{"filename with angle brackets", "file<name>.zip", "file_name_.zip"},
		{"filename with pipe", "file|name.zip", "file_name.zip"},
		{"dot only", ".", "."},
		// filepath.Base("/") returns "/" on Unix but "\" on Windows, then sanitized to "_"
		{"slash only", "/", "_"},
		// filepath.Base extracts "d.zip" from "a:b*c?d.zip" on Windows (treats a: as drive)
		{"multiple bad chars", "b*c?d.zip", "b_c_d.zip"},

		// Extended test cases
		{"unicode filename", "文件.zip", "文件.zip"},
		{"emoji in filename", "file🎉.zip", "file🎉.zip"},
		{"filename with extension only", ".gitignore", ".gitignore"},
		{"filename with multiple dots", "file.tar.gz", "file.tar.gz"},
		{"filename with hyphen", "my-file.zip", "my-file.zip"},
		{"filename with underscore", "my_file.zip", "my_file.zip"},
		{"mixed case", "MyFile.ZIP", "MyFile.ZIP"},
		{"all spaces becomes empty after trim", "   ", ""},
		{"tabs and newlines", "\tfile\n.zip", "file\n.zip"},
		{"very long extension", "file.verylongextension", "file.verylongextension"},
		{"numbers in name", "file123.zip", "file123.zip"},
		{"consecutive bad chars", "file***name.zip", "file___name.zip"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeFilename(tt.input)
			if got != tt.expected {
				t.Errorf("sanitizeFilename(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
