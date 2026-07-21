package http

import (
	"fmt"
	"net"
	"strings"
)

type sourceIPAllowlist struct {
	networks []*net.IPNet
}

func newSourceIPAllowlist(entries []string) (*sourceIPAllowlist, error) {
	networks := make([]*net.IPNet, 0, len(entries))
	for _, raw := range entries {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if ip := net.ParseIP(value); ip != nil {
			bits := 32
			if ip.To4() == nil {
				bits = 128
			}
			networks = append(networks, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
			continue
		}
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			return nil, fmt.Errorf("invalid callback allowlist entry %q", value)
		}
		networks = append(networks, network)
	}
	return &sourceIPAllowlist{networks: networks}, nil
}

func (a *sourceIPAllowlist) Allows(rawIP string) bool {
	if a == nil || len(a.networks) == 0 {
		return false
	}
	ip := net.ParseIP(strings.TrimSpace(rawIP))
	if ip == nil {
		return false
	}
	for _, network := range a.networks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}
