package shell

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDecodeLegacyText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "valid utf8 passes through",
			in:   "已 UTF-8: 中文测试 ABC\n",
			want: "已 UTF-8: 中文测试 ABC\n",
		},
		{
			name: "ascii passes through",
			in:   "plain ascii output\n",
			want: "plain ascii output\n",
		},
		{
			name: "empty passes through",
			in:   "",
			want: "",
		},
		{
			name: "gbk decode",
			in:   "\xd6\xd0\xce\xc4\xb2\xe2\xca\xd4\n",
			want: "中文测试\n",
		},
		{
			name: "gbk with ascii and crlf",
			in:   "TOTAL=2444\r\n\xd6\xd0\xce\xc4\r\n",
			want: "TOTAL=2444\r\n中文\r\n",
		},
		{
			name: "truncated multibyte tail keeps the lossy path",
			in:   "\xd6\xd0\xce\xc4\xb2",
			want: "\xd6\xd0\xce\xc4\xb2",
		},
		{
			name: "nul bytes are binary",
			in:   "\x00\xd6\xd0\xce\xc4",
			want: "\x00\xd6\xd0\xce\xc4",
		},
		{
			name: "control heavy stream is binary",
			in:   "\x01\x02\x03\x04\x05\x06\x07\x08\x0e\x0f\x10\x11\x12\x13\x14\x15\x16\x17\x18\x19\x1a\x1c\x1d\x1e\x1f",
			want: "\x01\x02\x03\x04\x05\x06\x07\x08\x0e\x0f\x10\x11\x12\x13\x14\x15\x16\x17\x18\x19\x1a\x1c\x1d\x1e\x1f",
		},
		{
			name: "ansi escapes do not block a decode",
			in:   "\x1b[31m\xd6\xd0\xce\xc4\x1b[0m\n",
			want: "\x1b[31m中文\x1b[0m\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, decodeLegacyText(tt.in))
		})
	}
}

func TestDecodeLegacyTextIsIdempotent(t *testing.T) {
	t.Parallel()

	decoded := decodeLegacyText("\xd6\xd0\xce\xc4\n")
	require.Equal(t, "中文\n", decoded)
	require.Equal(t, decoded, decodeLegacyText(decoded))
}

func TestDecodeTerminalTextLines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "valid utf8 passes through",
			in:   "中文标签\n另一个标签\n",
			want: "中文标签\n另一个标签\n",
		},
		{
			name: "ascii passes through",
			in:   "plain ascii\n",
			want: "plain ascii\n",
		},
		{
			name: "empty passes through",
			in:   "",
			want: "",
		},
		{
			name: "gbk only",
			in:   "\xd6\xd0\xce\xc4\xb2\xe2\xca\xd4\n",
			want: "中文测试\n",
		},
		{
			// The case this exists for: the shell's own UTF-8 label and a
			// child process's CP936 line arriving in one result.
			name: "mixed utf8 line and gbk line",
			in:   "中文标签\n\xbd\xf8\xb6\xc8\xbc\xc7\xc2\xbc\xd0\xd0\xca\xfd\n",
			want: "中文标签\n进度记录行数\n",
		},
		{
			name: "gbk line between utf8 lines",
			in:   "头部\n\xc7\xa7\xc1\xb5\xcd\xf2\xbb\xa8\n尾部\n",
			want: "头部\n千恋万花\n尾部\n",
		},
		{
			name: "gbk lines with crlf",
			in:   "TOTAL=1\r\n\xd6\xd0\xce\xc4\r\n",
			want: "TOTAL=1\r\n中文\r\n",
		},
		{
			name: "truncated gbk tail keeps the lossy path",
			in:   "中文标签\n\xd6\xd0\xce\xc4\xb2",
			want: "中文标签\n\xd6\xd0\xce\xc4\xb2",
		},
		{
			name: "nul bytes stay binary",
			in:   "\x00\xd6\xd0\xce\xc4\n中文\n",
			want: "\x00\xd6\xd0\xce\xc4\n中文\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, DecodeTerminalTextLines(tt.in))
		})
	}
}

func TestDecodeTerminalTextLinesIsIdempotent(t *testing.T) {
	t.Parallel()

	decoded := DecodeTerminalTextLines("标签\n\xd6\xd0\xce\xc4\n")
	require.Equal(t, "标签\n中文\n", decoded)
	require.Equal(t, decoded, DecodeTerminalTextLines(decoded))
}

func TestLooksBinary(t *testing.T) {
	t.Parallel()

	require.True(t, looksBinary("a\x00b"))
	require.False(t, looksBinary("a\tb\nc\rd"))
	require.False(t, looksBinary("\x1b[1mhi\x1b[0m"))
	// One stray control byte in a long stream must not read as binary.
	require.False(t, looksBinary("aaaaaaaaaaaaaaaaaaaaaaaaaaaaa\x01"))
}
