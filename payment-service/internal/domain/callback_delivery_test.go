package domain

import (
	"strings"
	"testing"
)

func TestSanitizeCallbackResponseSummaryNormalizesAndTruncatesRunes(t *testing.T) {
	got := SanitizeCallbackResponseSummary([]byte("  成功\n\t回覆\x00  OK  "))
	if got != "成功 回覆 OK" {
		t.Fatalf("summary = %q", got)
	}
	long := SanitizeCallbackResponseSummary([]byte(strings.Repeat("測", CallbackResponseSummaryMaxRunes+10)))
	if count := len([]rune(long)); count != CallbackResponseSummaryMaxRunes {
		t.Fatalf("rune length = %d, want %d", count, CallbackResponseSummaryMaxRunes)
	}
}
