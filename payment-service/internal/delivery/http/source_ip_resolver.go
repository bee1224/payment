package http

import (
	"net"
	nethttp "net/http"
	"strings"
)

type sourceIPResolver struct {
	trustedProxies *sourceIPAllowlist
}

func newSourceIPResolver(trustedProxyCIDRs []string) (*sourceIPResolver, error) {
	allowlist, err := newSourceIPAllowlist(trustedProxyCIDRs)
	if err != nil {
		return nil, err
	}
	return &sourceIPResolver{trustedProxies: allowlist}, nil
}

func (r *sourceIPResolver) Resolve(req *nethttp.Request) string {
	return requestSourceIP(req, r.trustedProxies)
}

func requestSourceIP(r *nethttp.Request, trustedProxies *sourceIPAllowlist) string {
	remoteIP := requestRemoteIP(r)
	if trustedProxies != nil && len(trustedProxies.networks) > 0 && trustedProxies.Allows(remoteIP) {
		if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
			parts := strings.Split(forwarded, ",")
			if len(parts) > 0 && strings.TrimSpace(parts[0]) != "" {
				return strings.TrimSpace(parts[0])
			}
		}
		if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
			return realIP
		}
	}
	return remoteIP
}

func requestRemoteIP(r *nethttp.Request) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}
