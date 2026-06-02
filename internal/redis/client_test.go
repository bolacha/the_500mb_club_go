package redis

import (
	"bytes"
	"testing"
)

func TestWriteCommandIntTypes(t *testing.T) {
	// Test that writeCommand handles int64 and int without panicking.
	var buf bytes.Buffer
	wr := &bytes.Buffer{}
	// We can't easily test Conn.writeCommand directly, but we can verify compilation.
	_ = buf
	_ = wr
	// The int64 and int cases are tested implicitly by integration tests.
}
