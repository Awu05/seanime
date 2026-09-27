package security

import (
	"net/netip"
	"testing"
)

func TestValidateOutboundURL(t *testing.T) {
	t.Cleanup(func() {
		SetSecureMode("")
	})

	t.Run("allows localhost outside strict mode", func(t *testing.T) {
		SetSecureMode("")
		if err := ValidateOutboundUrl("http://127.0.0.1:8080"); err != nil {
			t.Fatalf("expected localhost to be allowed outside strict mode: %v", err)
		}
	})

	t.Run("blocks localhost in strict mode", func(t *testing.T) {
		SetSecureMode(SecureModeStrict)
		if err := ValidateOutboundUrl("http://localhost:8080"); err == nil {
			t.Fatal("expected localhost to be blocked in strict mode")
		}
	})

	t.Run("blocks loopback ip in strict mode", func(t *testing.T) {
		SetSecureMode(SecureModeStrict)
		if err := ValidateOutboundUrl("http://127.0.0.1:8080"); err == nil {
			t.Fatal("expected loopback ip to be blocked in strict mode")
		}
	})

	t.Run("blocks private ip in strict mode", func(t *testing.T) {
		SetSecureMode(SecureModeStrict)
		if err := ValidateOutboundUrl("http://192.168.1.10:8080"); err == nil {
			t.Fatal("expected private ip to be blocked in strict mode")
		}
	})

	t.Run("allows public ip in strict mode", func(t *testing.T) {
		SetSecureMode(SecureModeStrict)
		if err := ValidateOutboundUrl("https://1.1.1.1"); err != nil {
			t.Fatalf("expected public ip to be allowed in strict mode: %v", err)
		}
	})
}

func TestIsPrivateNetworkAddrTailscaleCGNATRange(t *testing.T) {
	tests := []struct {
		addr    string
		private bool
	}{
		{"100.64.0.1", true},      // start of the Tailscale/CGNAT range
		{"100.127.255.254", true}, // end of the Tailscale/CGNAT range
		{"100.128.0.1", false},    // just outside the range
		{"8.8.8.8", false},        // ordinary public address
	}

	for _, tt := range tests {
		if got := IsPrivateNetworkAddr(netip.MustParseAddr(tt.addr)); got != tt.private {
			t.Errorf("IsPrivateNetworkAddr(%s) = %v, want %v", tt.addr, got, tt.private)
		}
	}
}
