package security

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

func ValidateOutboundUrl(rawURL string) error {
	if !IsStrict() {
		return nil
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return err
	}

	host := strings.TrimSpace(parsed.Hostname())
	if host == "" {
		return fmt.Errorf("missing host")
	}

	if strings.EqualFold(host, "localhost") {
		return fmt.Errorf("private network access denied: host '%s' resolves to localhost", host)
	}

	if addr, err := netip.ParseAddr(host); err == nil {
		if IsPrivateNetworkAddr(addr) {
			return fmt.Errorf("private network access denied: host '%s' is not reachable from strict mode", host)
		}
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil
	}

	for _, addr := range addrs {
		if IsPrivateNetworkAddr(addr) {
			return fmt.Errorf("private network access denied: host '%s' resolves to a private address", host)
		}
	}

	return nil
}

// tailscaleCGNATRange is the shared carrier-grade NAT block (RFC 6598) that Tailscale and other
// overlay networks assign addresses from - not one of the RFC 1918 ranges netip.IsPrivate covers,
// but still not reachable from the public internet.
var tailscaleCGNATRange = netip.MustParsePrefix("100.64.0.0/10")

// IsPrivateNetworkAddr reports whether addr is loopback, private, link-local, multicast,
// unspecified or Tailscale/CGNAT - anything that isn't a public internet address.
func IsPrivateNetworkAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
	return addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsMulticast() || addr.IsUnspecified() || tailscaleCGNATRange.Contains(addr)
}
