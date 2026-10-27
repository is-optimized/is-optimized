package scanner

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

var deniedPrefixes = func() []netip.Prefix {
	var out []netip.Prefix
	for _, p := range []string{"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4", "::/96", "::ffff:0:0/96", "64:ff9b::/96", "64:ff9b:1::/48", "100::/64", "2001::/23", "2001:db8::/32", "3fff::/20", "2002::/16", "fc00::/7", "fe80::/10", "ff00::/8"} {
		out = append(out, netip.MustParsePrefix(p))
	}
	return out
}()

func publicIP(a netip.Addr) bool {
	if !a.IsValid() || a.Is4In6() || !a.IsGlobalUnicast() {
		return false
	}
	for _, p := range deniedPrefixes {
		if p.Contains(a) {
			return false
		}
	}
	return true
}
func validateURL(u *url.URL) error {
	if len(u.String()) > 2048 {
		return errors.New("target URL exceeds length limit")
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return errors.New("only HTTP(S) URLs are supported")
	}
	if u.User != nil || u.Hostname() == "" || u.Opaque != "" {
		return errors.New("invalid target URL")
	}
	if p := u.Port(); p != "" && !((u.Scheme == "https" && p == "443") || (u.Scheme == "http" && p == "80")) {
		return errors.New("unsupported target port")
	}
	host := strings.ToLower(u.Hostname())
	if strings.HasSuffix(host, ".") || strings.Contains(host, "%") {
		return errors.New("invalid target hostname")
	}
	if a, err := netip.ParseAddr(host); err == nil {
		if !publicIP(a) {
			return errors.New("target must have a public address")
		}
	} else {
		if !strings.Contains(host, ".") || len(host) > 253 {
			return errors.New("target must be a public domain")
		}
		for _, label := range strings.Split(host, ".") {
			if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
				return errors.New("invalid domain")
			}
			for _, c := range label {
				if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
					return errors.New("use an ASCII or punycode domain")
				}
			}
		}
		for _, suffix := range []string{".localhost", ".local", ".internal", ".test", ".invalid", ".example"} {
			if strings.HasSuffix(host, suffix) {
				return errors.New("non-public domain")
			}
		}
	}
	return nil
}

// NormalizeDomain returns a normalized hostname; inputs must identify a homepage.
func NormalizeDomain(input string) (string, error) {
	input = strings.TrimSpace(input)
	if !strings.Contains(input, "://") {
		input = "https://" + input
	}
	u, err := url.Parse(input)
	if err != nil {
		return "", errors.New("invalid domain")
	}
	if err = validateURL(u); err != nil {
		return "", err
	}
	if u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("enter a domain, not a page URL")
	}
	if net.ParseIP(u.Hostname()) != nil {
		return "", errors.New("enter a public domain name")
	}
	return strings.ToLower(u.Hostname()), nil
}
func secureTransport() *http.Transport {
	d := &net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}
	return &http.Transport{Proxy: nil, MaxIdleConns: 32, MaxIdleConnsPerHost: 4, MaxConnsPerHost: 4, IdleConnTimeout: 30 * time.Second, TLSHandshakeTimeout: 3 * time.Second, ResponseHeaderTimeout: 5 * time.Second, MaxResponseHeaderBytes: 64 << 10,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			if port != "80" && port != "443" {
				return nil, errors.New("blocked port")
			}
			ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
			if err != nil {
				return nil, err
			}
			if len(ips) == 0 {
				return nil, errors.New("no public address")
			}
			// Reject mixed public/private answers, then dial the exact vetted address.
			for _, ip := range ips {
				if !publicIP(ip) {
					return nil, errors.New("blocked destination address")
				}
			}
			var last error
			for _, ip := range ips {
				conn, e := d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
				if e == nil {
					return conn, nil
				}
				last = e
			}
			return nil, last
		}}
}
func origin(u *url.URL) string { return strings.ToLower(u.Scheme + "://" + u.Host) }
