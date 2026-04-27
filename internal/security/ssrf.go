// Package security provides SSRF protection, authentication, authorization, and
// resource limit enforcement for the HotPlex Worker Gateway.
package security

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// BlockedCIDRs contains all IP ranges that must never be accessed by worker processes.
// Covers private networks, loopback, link-local, cloud metadata endpoints, and reserved ranges.
// Must be checked after DNS resolution to defend against DNS rebinding attacks.
var BlockedCIDRs = []netip.Prefix{
	// Loopback
	parseCIDR("127.0.0.0/8"),
	parseCIDR("::1/128"),
	// Private networks (RFC 1918)
	parseCIDR("10.0.0.0/8"),
	parseCIDR("172.16.0.0/12"),
	parseCIDR("192.168.0.0/16"),
	// IPv6 unique local (fc00::/7 covers fc00::/8 and fd00::/8)
	parseCIDR("fc00::/7"),
	// Link-local
	parseCIDR("169.254.0.0/16"),
	parseCIDR("fe80::/10"),
	// Cloud metadata endpoints
	parseCIDR("169.254.169.254/32"), // AWS EC2, GCP, Azure IMDS
	parseCIDR("100.100.100.200/32"), // Alibaba Cloud metadata
	parseCIDR("192.0.0.0/24"),       // RFC 8520 DHCP broadcast
	// Multicast
	parseCIDR("224.0.0.0/4"),
	parseCIDR("ff00::/8"),
	// Reserved
	parseCIDR("0.0.0.0/8"),     // 0.0.0.0/8: current host (Linux)
	parseCIDR("100.64.0.0/10"), // Carrier-grade NAT (RFC 6598)
}

// parseCIDR wraps netip.ParsePrefix with panic-on-invalid semantics.
// All constants above are validated at package init; a panic indicates a
// programming error in this file and is unrecoverable.
func parseCIDR(s string) netip.Prefix {
	p, err := netip.ParsePrefix(s)
	if err != nil {
		panic("security: parseCIDR(" + s + "): " + err.Error())
	}
	return p
}

// SSRFProtectionError describes why a URL was rejected by the SSRF check.
type SSRFProtectionError struct {
	URL     string
	Reason  string
	Blocked string // IP or CIDR that caused the block
}

// Error implements the error interface.
func (e *SSRFProtectionError) Error() string {
	return fmt.Sprintf("SSRF blocked: %s (reason=%s, blocked_by=%s)", e.URL, e.Reason, e.Blocked)
}

// ValidateURL performs a layered SSRF check on the given URL:
//   - Protocol: only http and https are permitted
//   - Bare IP: direct IP URLs are rejected if they fall in a blocked range
//   - DNS resolution: the resolved IP(s) are checked against BlockedCIDRs
//
// Returns nil if the URL is safe, or a *SSRFProtectionError describing the
// violation. It does NOT perform DNS rebinding protection; use
// ValidateURLDoubleResolve for high-sensitivity contexts.
func ValidateURL(targetURL string) error {
	u, err := url.Parse(targetURL)
	if err != nil {
		return &SSRFProtectionError{URL: targetURL, Reason: "invalid URL"}
	}

	// Layer 1: protocol check — only http/https allowed.
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		// allowed
	case "":
		return &SSRFProtectionError{URL: targetURL, Reason: "missing scheme"}
	default:
		return &SSRFProtectionError{URL: targetURL, Reason: "disallowed scheme: " + u.Scheme}
	}

	host := u.Hostname()
	if host == "" {
		return &SSRFProtectionError{URL: targetURL, Reason: "empty host"}
	}

	// Layer 2: bare IP check — reject IP literals that are already blocked
	// without requiring a DNS lookup (prevents CIDR bypass).
	ip := net.ParseIP(host)
	if ip != nil {
		if isIPBlocked(ip) {
			return &SSRFProtectionError{
				URL:     targetURL,
				Reason:  "bare IP in URL is blocked",
				Blocked: ip.String(),
			}
		}
		return nil // direct IP, no DNS rebinding risk
	}

	// Layer 3: DNS resolution.
	ips, err := net.LookupHost(host)
	if err != nil {
		return &SSRFProtectionError{URL: targetURL, Reason: "DNS lookup failed: " + err.Error()}
	}

	// Layer 4: check all resolved IPs against BlockedCIDRs.
	for _, ipStr := range ips {
		ip = net.ParseIP(ipStr)
		if ip == nil {
			continue
		}
		if isIPBlocked(ip) {
			return &SSRFProtectionError{
				URL:     targetURL,
				Reason:  "DNS resolved to blocked IP",
				Blocked: ip.String(),
			}
		}
	}

	return nil
}

// ValidateURLDoubleResolve performs the standard ValidateURL check and then
// re-resolves the hostname after a 1 second delay to detect DNS rebinding attacks.
// If the second resolution returns a different (blocked) IP, the URL is rejected.
//
// Use this for high-sensitivity environments where an attacker might control DNS.
// Note: the 1 second sleep adds latency and a DNS round-trip to every call.
func ValidateURLDoubleResolve(targetURL string) error {
	if err := ValidateURL(targetURL); err != nil {
		return err
	}

	// Brief delay to increase the probability that a DNS cache entry expires
	// between the two lookups (only effective if the attacker controls the
	// authoritative nameserver and the TTL is very short).
	time.Sleep(1 * time.Second)

	u, err := url.Parse(targetURL)
	if err != nil {
		return &SSRFProtectionError{URL: targetURL, Reason: "invalid URL (re-parse)"}
	}
	if u.Scheme == "" {
		return nil
	}

	ips, err := net.LookupHost(u.Hostname())
	if err != nil {
		return &SSRFProtectionError{URL: targetURL, Reason: "DNS re-lookup failed: " + err.Error()}
	}

	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}
		if isIPBlocked(ip) {
			return &SSRFProtectionError{
				URL:     targetURL,
				Reason:  "DNS rebind detected: IP changed to blocked range",
				Blocked: ip.String(),
			}
		}
	}

	return nil
}

// isIPBlocked reports whether ip falls within any CIDR range in BlockedCIDRs.
// IPv4-mapped IPv6 addresses (::ffff:x.x.x.x) are un-mapped before the check
// so that IPv4 blocklist entries match correctly.
func isIPBlocked(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	// Strip IPv4-mapped IPv6 prefix so IPv4 blocklist entries apply.
	if !addr.Is4() && addr.Is6() {
		addr = addr.Unmap()
	}

	for _, cidr := range BlockedCIDRs {
		if cidr.Contains(addr) {
			return true
		}
	}
	return false
}

// ValidateURLAndLog is like ValidateURL but also logs the result at warn level
// when the URL is blocked. Pass the caller's logger and any additional context
// fields (e.g., session_id, user_id).
func ValidateURLAndLog(targetURL string, log *slog.Logger, fields ...any) error {
	err := ValidateURL(targetURL)
	if err != nil && log != nil {
		args := append([]any{"url", targetURL}, fields...)
		var ssrfErr *SSRFProtectionError
		if errors.As(err, &ssrfErr) {
			args = append(args, "ssrf_reason", err.Error())
		}
		log.Warn("security: SSRF blocked", args...)
	}
	return err
}
