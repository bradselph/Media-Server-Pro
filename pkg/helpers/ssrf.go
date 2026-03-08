package helpers

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

// privateRanges lists IP networks that must never be contacted by server-side
// HTTP clients (SSRF protection). Includes RFC-1918 private ranges, loopback,
// link-local, and IANA-reserved blocks.
var privateRanges []*net.IPNet

func init() {
	private := []string{
		"127.0.0.0/8",     // IPv4 loopback
		"10.0.0.0/8",      // RFC-1918
		"172.16.0.0/12",   // RFC-1918
		"192.168.0.0/16",  // RFC-1918
		"169.254.0.0/16",  // link-local
		"100.64.0.0/10",   // shared address space (RFC 6598)
		"192.0.0.0/24",    // IETF protocol assignments
		"198.18.0.0/15",   // benchmarking
		"198.51.100.0/24", // documentation
		"203.0.113.0/24",  // documentation
		"240.0.0.0/4",     // reserved
		"::1/128",         // IPv6 loopback
		"fc00::/7",        // IPv6 ULA
		"fe80::/10",       // IPv6 link-local
	}
	for _, cidr := range private {
		_, block, _ := net.ParseCIDR(cidr)
		privateRanges = append(privateRanges, block)
	}
}

// isPrivateIP reports whether ip falls within any private/reserved range.
func isPrivateIP(ip net.IP) bool {
	for _, block := range privateRanges {
		if block.Contains(ip) {
			return true
		}
	}
	return false
}

// SafeHTTPTransport returns an *http.Transport whose DialContext resolves the
// target hostname and rejects connections to private/loopback IP addresses.
// Use this for any server-side HTTP client that fetches user-supplied URLs.
func SafeHTTPTransport() *http.Transport {
	d := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	return &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}

			ips, err := net.DefaultResolver.LookupHost(ctx, host)
			if err != nil {
				return nil, err
			}

			for _, ipStr := range ips {
				ip := net.ParseIP(ipStr)
				if ip == nil {
					continue
				}
				if isPrivateIP(ip) {
					return nil, fmt.Errorf("connection to private/reserved address %s (%s) is not allowed", host, ipStr)
				}
			}

			if len(ips) == 0 {
				return nil, fmt.Errorf("no addresses resolved for %s", host)
			}

			return d.DialContext(ctx, network, net.JoinHostPort(ips[0], port))
		},
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		MaxIdleConns:          10,
		IdleConnTimeout:       90 * time.Second,
	}
}
