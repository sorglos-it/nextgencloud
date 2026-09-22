package trust

// Hosts and addresses. Each has one grammar and exactly one spelling: NGC
// compares them as text (trust list against codes, configurations against
// the trust list), and they end up in configuration files of other
// programs (Apache, the database driver), so they hold nothing but what the
// grammar allows. shared/README.md, rule "Adressen"; vectors: url.json.

import (
	"net/netip"
	"strconv"
	"strings"
)

// maxURL bounds Node.URL and Store.URL, as tunnel.MaxURL bounds the URL of a
// code.
const maxURL = 256

// maxHost is the longest DNS name, as the wire format bounds it.
const maxHost = 253

// ValidHost reports whether h is a host: a DNS name of 1 to 253 bytes in
// lower case (labels of 1 to 63 of a-z, 0-9 and '-', not starting or ending
// with '-', joined by '.', the last label not all digits), an IPv4 address in
// dotted decimal without leading zeros, or an IPv6 address in brackets
// exactly as netip.Addr.String writes it (RFC 5952: lower case, zeros
// compressed), without a zone and not IPv4-mapped.
func ValidHost(h string) bool {
	if inner, ok := strings.CutPrefix(h, "["); ok {
		inner, ok = strings.CutSuffix(inner, "]")
		if !ok {
			return false
		}
		a, err := netip.ParseAddr(inner)
		// Is4In6 would give a second spelling of an IPv4 address, and a zone
		// names an interface of one machine, which no other machine can use.
		return err == nil && a.Is6() && !a.Is4In6() && a.Zone() == "" && a.String() == inner
	}
	// An address netip reads is an address, never a name: only its canonical
	// dotted decimal passes, so 192.0.2.01 and a bare IPv6 do not.
	if a, err := netip.ParseAddr(h); err == nil {
		return a.Is4() && a.String() == h
	}
	return validDNSName(h)
}

// validDNSName checks the name rule of ValidHost.
func validDNSName(h string) bool {
	if len(h) == 0 || len(h) > maxHost {
		return false
	}
	labels := strings.Split(h, ".")
	for _, l := range labels {
		if len(l) == 0 || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
			return false
		}
		for i := 0; i < len(l); i++ {
			c := l[i]
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	// A last label of digits alone would be a broken IPv4 address rather than
	// a name, and resolvers read such a name as an address.
	return !allDigits(labels[len(labels)-1])
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// ValidHostPort reports whether s is host ":" port: host by ValidHost, port 1
// to 65535 in decimal without sign or leading zero (config.DB.Nodes).
// net.SplitHostPort then gives the host (an IPv6 address without brackets)
// and the port.
func ValidHostPort(s string) bool {
	// The port is split at the last ':', which comes after ']' for IPv6.
	i := strings.LastIndexByte(s, ':')
	return i >= 0 && ValidHost(s[:i]) && validPort(s[i+1:])
}

// validPort checks the port rule of ValidHostPort.
func validPort(p string) bool {
	if len(p) == 0 || len(p) > 5 || p[0] == '0' || !allDigits(p) {
		return false
	}
	n, err := strconv.Atoi(p)
	return err == nil && n <= 65535
}

// ValidTunnelURL reports whether u is the tunnel address of an API: exactly
// "wss://" host ":" port "/tunnel" (ValidHostPort), at most 256 bytes.
// Node.URL, the codes and join requests of package tunnel and the API
// addresses in configurations use it.
func ValidTunnelURL(u string) bool {
	if len(u) > maxURL {
		return false
	}
	hp, ok := strings.CutPrefix(u, "wss://")
	if !ok {
		return false
	}
	hp, ok = strings.CutSuffix(hp, "/tunnel")
	return ok && ValidHostPort(hp)
}

// WebURL returns the address behind Apache of the API with tunnel address u:
// "https://" host ":" port, which web.json lists and Apache proxies to. ok is
// false if u is no ValidTunnelURL.
func WebURL(u string) (string, bool) {
	if !ValidTunnelURL(u) {
		return "", false
	}
	return "https://" + strings.TrimSuffix(strings.TrimPrefix(u, "wss://"), "/tunnel"), true
}

// ValidWebURL reports whether u is such an address: exactly "https://" host
// ":" port (ValidHostPort), at most 256 bytes (config.WebAPI.URL).
func ValidWebURL(u string) bool {
	if len(u) > maxURL {
		return false
	}
	hp, ok := strings.CutPrefix(u, "https://")
	return ok && ValidHostPort(hp)
}

// ValidOrigin reports whether o is an origin for passkeys (proof.RP.Origins,
// config.Web.RP.Origins): exactly "https://" host, with ":" port only if the
// page is not served on 443, at most 256 bytes. It carries no path, query or
// fragment, because a browser writes the origin of the page into the client
// data as scheme, host and port alone, and proof.Verify compares it with
// these entries as text: anything a browser would never write could only
// match nothing, and the values go on into web.json.
func ValidOrigin(o string) bool {
	if len(o) > maxURL {
		return false
	}
	rest, ok := strings.CutPrefix(o, "https://")
	return ok && (ValidHost(rest) || ValidHostPort(rest))
}

// ValidStoreURL reports whether u is the address of a store: "https://", a
// host (ValidHost) with an optional ":" port, then nothing or a path of
// segments of A-Z a-z 0-9 . _ ~ - each after a "/" (none empty, none "." or
// ".."), without a trailing "/", user info, query or fragment; at most 256
// bytes. Store.URL and config.API.StoreURL use it.
func ValidStoreURL(u string) bool {
	if len(u) > maxURL {
		return false
	}
	rest, ok := strings.CutPrefix(u, "https://")
	if !ok {
		return false
	}
	// A bracketed IPv6 host holds no '/', so the first one starts the path;
	// user info, query and fragment fail as part of the host.
	authority, path, hasPath := strings.Cut(rest, "/")
	if !ValidHost(authority) && !ValidHostPort(authority) {
		return false
	}
	if !hasPath {
		return true
	}
	for _, seg := range strings.Split(path, "/") {
		if seg == "" || seg == "." || seg == ".." || !validSegment(seg) {
			return false
		}
	}
	return true
}

// validSegment checks the characters of a path segment of ValidStoreURL: the
// unreserved characters of RFC 3986 without percent escapes, so the address
// has one spelling.
func validSegment(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' ||
			c == '.' || c == '_' || c == '~' || c == '-'
		if !ok {
			return false
		}
	}
	return true
}
