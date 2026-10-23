package shell

import (
	"runtime"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// terminalControlShare bounds how much of a stream may be non-whitespace
// control bytes before it is treated as binary rather than text.
const terminalControlShare = 20

// DecodeTerminalText repairs command output that a Windows program wrote in
// the active ANSI code page instead of UTF-8.
//
// Go strings carry arbitrary bytes, so a CP936 command's output reaches the
// model as invalid UTF-8, where JSON encoding replaces every affected byte
// with U+FFFD. Undoing that needs the original bytes, so the repair happens
// here, at the one point where bytes become the tool result.
//
// The function is deliberately conservative: it only acts on Windows, only on
// streams that are not already valid UTF-8, and only when the decode
// re-encodes to the exact same bytes. Anything else keeps the lossy path
// callers already handle.
func DecodeTerminalText(s string) string {
	if runtime.GOOS != "windows" {
		return s
	}
	return decodeLegacyText(s)
}

// decodeLegacyText holds the platform-independent body so the behaviour stays
// testable off Windows.
func decodeLegacyText(s string) string {
	if s == "" || utf8.ValidString(s) || looksBinary(s) {
		return s
	}
	decoded, err := simplifiedchinese.GB18030.NewDecoder().String(s)
	if err != nil {
		return s
	}
	// Round-trip guard: a truncated multibyte tail, a coincidental byte
	// sequence, or a lossy replacement fails to re-encode to the same bytes.
	// Trusting only exact round-trips is what keeps binary and misaligned
	// input on the replacement-character path instead of turning it into
	// plausible-looking CJK noise.
	roundTripped, err := simplifiedchinese.GB18030.NewEncoder().String(decoded)
	if err != nil || roundTripped != s {
		return s
	}
	return decoded
}

// looksBinary reports whether s is too control-heavy to be text. NUL is
// decisive; otherwise a stream needs more than one control byte in
// terminalControlShare to qualify, so a stray one never blocks a decode.
func looksBinary(s string) bool {
	var control int
	for i := 0; i < len(s); i++ {
		switch b := s[i]; {
		case b == 0:
			return true
		case b == '\n', b == '\r', b == '\t', b == '\f', b == 0x1b:
		case b < 0x20:
			control++
		}
	}
	return control*terminalControlShare > len(s)
}
