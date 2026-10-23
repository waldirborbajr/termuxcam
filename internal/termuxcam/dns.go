package termuxcam

import (
	"context"
	"net"
	"os"
	"time"
)

// publicDNS is used when the system has no resolver configuration. Android/Termux
// has no /etc/resolv.conf, so Go falls back to [::1]:53, where nothing is listening.
var publicDNS = []string{"8.8.8.8:53", "1.1.1.1:53"}

func init() {
	if _, err := os.Stat("/etc/resolv.conf"); err == nil {
		return // system resolver config exists: keep Go's default behaviour
	}

	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: 5 * time.Second}
			var lastErr error
			for _, server := range publicDNS {
				conn, err := d.DialContext(ctx, network, server)
				if err == nil {
					return conn, nil
				}
				lastErr = err
			}
			return nil, lastErr
		},
	}
}
