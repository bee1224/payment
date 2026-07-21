package repository

import (
	"strings"
	"testing"
)

func TestResolveCurrentCallbackSigningKeyQueryOrdersByQualifiedKeyID(t *testing.T) {
	query := strings.ToLower(resolveCurrentCallbackSigningKeySQL)
	if !strings.Contains(query, "order by k.id desc") {
		t.Fatalf("callback signing key resolver query must order by qualified key table id: %s", resolveCurrentCallbackSigningKeySQL)
	}
	if strings.Contains(query, "order by id desc") {
		t.Fatalf("callback signing key resolver query must not use ambiguous ORDER BY id: %s", resolveCurrentCallbackSigningKeySQL)
	}
}
