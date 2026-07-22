package service

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"

	"payment-service/internal/domain"
)

func TestSuccessfulMerchantCallbackResponseRequiresExactOK(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{name: "exact OK", status: http.StatusOK, body: "OK", want: true},
		{name: "body with newline", status: http.StatusOK, body: "OK\\n", want: false},
		{name: "leading space", status: http.StatusOK, body: " OK", want: false},
		{name: "trailing space", status: http.StatusOK, body: "OK ", want: false},
		{name: "lowercase", status: http.StatusOK, body: "ok", want: false},
		{name: "empty body", status: http.StatusOK, body: "", want: false},
		{name: "JSON body", status: http.StatusOK, body: `{"status":"OK"}`, want: false},
		{name: "non 2xx", status: http.StatusInternalServerError, body: "OK", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := domain.IsSuccessfulMerchantCallbackResponse(tc.status, []byte(tc.body)); got != tc.want {
				t.Fatalf("IsSuccessfulMerchantCallbackResponse(%d, %q) = %t, want %t", tc.status, tc.body, got, tc.want)
			}
		})
	}
}

func TestResolvePublicHTTPSCallbackTargetRejectsUnsafeURLs(t *testing.T) {
	resolverCalled := false
	resolver := func(context.Context, string) ([]net.IPAddr, error) {
		resolverCalled = true
		return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
	}
	for _, rawURL := range []string{
		"http://merchant.example/callback",
		"https://user:password@merchant.example/callback",
		"https:///callback",
	} {
		if _, _, err := resolvePublicHTTPSCallbackTarget(context.Background(), rawURL, resolver); err == nil {
			t.Fatalf("expected unsafe callback URL %q to be rejected", rawURL)
		}
	}
	if resolverCalled {
		t.Fatal("unsafe callback URLs must be rejected before DNS resolution")
	}
}

func TestResolvePublicHTTPSCallbackTargetRejectsPrivateDNSResult(t *testing.T) {
	resolver := func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}, {IP: net.ParseIP("10.0.0.8")}}, nil
	}
	if _, _, err := resolvePublicHTTPSCallbackTarget(context.Background(), "https://merchant.example/callback", resolver); err == nil {
		t.Fatal("private DNS results must be rejected")
	}
}

func TestResolvePublicHTTPSCallbackTargetPinsPublicDNSResult(t *testing.T) {
	resolver := func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}, {IP: net.ParseIP("93.184.216.34")}}, nil
	}
	parsed, target, err := resolvePublicHTTPSCallbackTarget(context.Background(), "https://merchant.example/callback", resolver)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Hostname() != "merchant.example" || !target.Equal(net.ParseIP("93.184.216.34")) {
		t.Fatalf("resolved target = %s for host %q", target, parsed.Hostname())
	}
}

func TestPublicHTTPSCallbackClientDisablesRedirects(t *testing.T) {
	client := newPinnedPublicHTTPSCallbackClient("merchant.example", net.ParseIP("93.184.216.34"), "443", 0)
	if err := client.CheckRedirect(nil, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("CheckRedirect() error = %v, want ErrUseLastResponse", err)
	}
}
