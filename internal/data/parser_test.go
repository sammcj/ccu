package data

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseJSONLLine(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		wantErr  bool
		wantSkip bool // nil entry, nil error
	}{
		{
			name:    "empty line",
			line:    "",
			wantErr: true,
		},
		{
			name:    "malformed JSON with assistant marker",
			line:    `{"type":"assistant","message":{`,
			wantErr: true,
		},
		{
			name:    "truncated line",
			line:    `{"type":"assistant","timestamp":"2026-07-03T10:00:00Z","message":{"id":"msg_1","usage":{"input_tokens":5`,
			wantErr: true,
		},
		{
			name:     "malformed JSON without assistant marker skipped by pre-filter",
			line:     `{"type":"user","message":{`,
			wantSkip: true,
		},
		{
			name:     "spaced assistant marker accepted by pre-filter",
			line:     `{"type": "assistant", "timestamp": "2026-07-03T10:00:00Z", "requestId": "req_1", "message": {"id": "msg_1", "model": "claude-sonnet-4-20250514", "usage": {"input_tokens": 10, "output_tokens": 5}}}`,
			wantSkip: false,
		},
		{
			name:     "non-assistant type skipped",
			line:     `{"type":"user","timestamp":"2026-07-03T10:00:00Z","message":{"id":"msg_1"}}`,
			wantSkip: true,
		},
		{
			name:     "assistant marker inside nested value of user entry",
			line:     `{"type":"user","timestamp":"2026-07-03T10:00:00Z","message":{"id":"msg_1","nested":{"type":"assistant"}}}`,
			wantSkip: true,
		},
		{
			name:     "zero-token assistant entry skipped",
			line:     `{"type":"assistant","timestamp":"2026-07-03T10:00:00Z","requestId":"req_1","message":{"id":"msg_1","model":"claude-sonnet-4-20250514","usage":{"input_tokens":0,"output_tokens":0}}}`,
			wantSkip: true,
		},
		{
			name:    "invalid timestamp",
			line:    `{"type":"assistant","timestamp":"03/07/2026 10:00","requestId":"req_1","message":{"id":"msg_1","model":"claude-sonnet-4-20250514","usage":{"input_tokens":10,"output_tokens":5}}}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry, err := ParseJSONLLine([]byte(tt.line))
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, entry)
				return
			}
			assert.NoError(t, err)
			if tt.wantSkip {
				assert.Nil(t, entry)
			} else {
				assert.NotNil(t, entry)
			}
		})
	}
}

func TestParseJSONLLineValidEntry(t *testing.T) {
	line := `{"type":"assistant","timestamp":"2026-07-03T10:00:00Z","requestId":"req_1","message":{"id":"msg_1","model":"claude-sonnet-4-20250514","usage":{"input_tokens":100,"output_tokens":50,"cache_creation_input_tokens":10,"cache_read_input_tokens":20}}}`

	entry, err := ParseJSONLLine([]byte(line))
	require.NoError(t, err)
	require.NotNil(t, entry)

	assert.Equal(t, 100, entry.InputTokens)
	assert.Equal(t, 50, entry.OutputTokens)
	assert.Equal(t, 10, entry.CacheCreationTokens)
	assert.Equal(t, 20, entry.CacheReadTokens)
	assert.Equal(t, "claude-sonnet-4-20250514", entry.Model)
	assert.Equal(t, "msg_1", entry.MessageID)
	assert.Equal(t, "req_1", entry.RequestID)
	assert.Equal(t, time.Date(2026, 7, 3, 10, 0, 0, 0, time.UTC), entry.Timestamp)
	assert.Greater(t, entry.CostUSD, 0.0)
	assert.Equal(t, 150, entry.DisplayTokens())
	assert.Equal(t, 180, entry.TotalTokens())
}

func TestParseJSONLLineTimestampFormats(t *testing.T) {
	tests := []struct {
		name string
		ts   string
		want time.Time
	}{
		{
			name: "RFC3339",
			ts:   "2026-07-03T10:00:00Z",
			want: time.Date(2026, 7, 3, 10, 0, 0, 0, time.UTC),
		},
		{
			name: "RFC3339 nano",
			ts:   "2026-07-03T10:00:00.123456789Z",
			want: time.Date(2026, 7, 3, 10, 0, 0, 123456789, time.UTC),
		},
		{
			name: "microsecond precision with offset",
			ts:   "2026-07-03T10:00:00.123456+10:00",
			want: time.Date(2026, 7, 3, 10, 0, 0, 123456000, time.FixedZone("", 10*3600)),
		},
		{
			name: "second precision with offset",
			ts:   "2026-07-03T10:00:00+10:00",
			want: time.Date(2026, 7, 3, 10, 0, 0, 0, time.FixedZone("", 10*3600)),
		},
		{
			name: "no zone treated as UTC",
			ts:   "2026-07-03T10:00:00",
			want: time.Date(2026, 7, 3, 10, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line := `{"type":"assistant","timestamp":"` + tt.ts + `","requestId":"req_1","message":{"id":"msg_1","model":"claude-sonnet-4-20250514","usage":{"input_tokens":10,"output_tokens":5}}}`
			entry, err := ParseJSONLLine([]byte(line))
			require.NoError(t, err)
			require.NotNil(t, entry)
			assert.True(t, entry.Timestamp.Equal(tt.want), "got %v, want %v", entry.Timestamp, tt.want)
		})
	}
}
