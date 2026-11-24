package tunnel

import (
	"encoding/binary"
	"slices"
	"testing"
)

func TestParseProcargs(t *testing.T) {
	t.Parallel()
	raw := binary.NativeEndian.AppendUint32(nil, 3)
	raw = append(raw, "/Users/Jane Doe/tsh\x00\x00\x00\x00"...)
	raw = append(raw, "/Users/Jane Doe/tsh\x00proxy app\x00skypilot\x00HOME=/Users/Jane Doe\x00"...)
	want := []string{"/Users/Jane Doe/tsh", "proxy app", "skypilot"}
	if got := parseProcargs(raw); !slices.Equal(got, want) {
		t.Errorf("parseProcargs = %q, want %q", got, want)
	}
	if got := parseProcargs(raw[:30]); got != nil {
		t.Errorf("parseProcargs of a truncated buffer = %q, want nil", got)
	}
}
