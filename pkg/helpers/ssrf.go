package helpers

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
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
// IPv4-mapped IPv6 addresses (e.g. ::ffff:10.0.0.1) are unwrapped to their
// 4-byte form before checking so that they match IPv4 CIDR ranges.
func isPrivateIP(ip net.IP) bool {
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	for _, block := range privateRanges {
		if block.Contains(ip) {
			return true
		}
	}
	return false
}

// ValidateURLForSSRF parses rawURL, enforces http/https scheme, and rejects URLs
// whose host resolves to private/loopback/link-local/reserved IP addresses.
// It is intended for validating admin-supplied URLs before any server-side
// HTTP fetching occurs.
//
// DNS rebinding caveat: validation resolves the hostname at call time and rejects
// private IPs. A DNS rebinding attack could cause the hostname to resolve to a
// different (private) IP at actual fetch time. Callers that perform the fetch
// after this validation should use SafeHTTPTransport, which re-validates the
// resolved IP at connection time via a custom DialContext hook.
func ValidateURLForSSRF(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported URL scheme: %s", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("invalid URL: missing host")
	}

	// Bound the DNS lookup so a slow or non-responding resolver can't hang the
	// calling handler goroutine for the full OS resolver timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return fmt.Errorf("failed to resolve host %s: %w", host, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("no addresses resolved for %s", host)
	}

	if slices.ContainsFunc(ips, isPrivateIP) {
		return fmt.Errorf("URL resolves to private/reserved address %s", host)
	}

	return nil
}

// SafeDialContext resolves the target host and refuses to connect to any
// private/loopback/reserved IP, then dials the first validated address. It is the
// shared dial hook behind SafeHTTPTransport and is also usable directly as a
// websocket.Dialer.NetDialContext, so any server-side client reaching a
// user/admin-supplied destination re-validates the resolved IP at connection
// time. This closes the DNS-rebinding gap that a one-time ValidateURLForSSRF
// check leaves open: the hostname may resolve to a public IP at save/validate
// time but an internal IP at connect time.
//
// Only ips[0] is dialed; a mixed result (public first, private second) is
// rejected outright because any private IP in the set fails the loop below.
func SafeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}

	ips, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no addresses resolved for %s", host)
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

	d := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	return d.DialContext(ctx, network, net.JoinHostPort(ips[0], port))
}

// SafeHTTPTransport returns an *http.Transport whose DialContext resolves the
// target hostname and rejects connections to private/loopback IP addresses.
// Use this for any server-side HTTP client that fetches user-supplied URLs.
func SafeHTTPTransport() *http.Transport {
	return &http.Transport{
		DialContext:           SafeDialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		MaxIdleConns:          10,
		IdleConnTimeout:       90 * time.Second,
	}
}
