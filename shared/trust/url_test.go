package trust_test

import (
	"fmt"
	"net"
	neturl "net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/internal/golden"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
)

// urlVectors is url.json: hosts and addresses, each valid or not.
type urlVectors struct {
	Hosts    []urlCase `json:"hosts"`    // ValidHost
	HostPort []urlCase `json:"hostPort"` // ValidHostPort
	Tunnel   []urlCase `json:"tunnel"`   // ValidTunnelURL; Web is WebURL of the valid ones
	Web      []urlCase `json:"web"`      // ValidWebURL
	Origins  []urlCase `json:"origins"`  // ValidOrigin
	Store    []urlCase `json:"store"`    // ValidStoreURL
}

type urlCase struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Valid bool   `json:"valid"`
	Web   string `json:"web,omitempty"`
}

// addressCases feeds every test of this file. Each invalid case breaks
// exactly one rule, so no case can pass by failing another one.
func addressCases() urlVectors {
	h := trusttest.Host
	return urlVectors{
		Hosts: []urlCase{
			{Name: "DNS name", Value: "api-1.test", Valid: true},
			{Name: "single label", Value: "localhost", Valid: true},
			{Name: "one character", Value: "a", Valid: true},
			{Name: "digits first in a label", Value: "1password.example", Valid: true},
			{Name: "punycode label", Value: "xn--mnchen-3ya.example", Valid: true},
			{Name: "label of 63 characters", Value: strings.Repeat("a", 63) + ".test", Valid: true},
			{Name: "name of 253 bytes", Value: h(253), Valid: true},
			{Name: "IPv4", Value: "192.0.2.1", Valid: true},
			{Name: "IPv6", Value: "[2001:db8::1]", Valid: true},
			{Name: "IPv6 loopback", Value: "[::1]", Valid: true},
			{Name: "empty", Value: ""},
			{Name: "upper case", Value: "API-1.test"},
			{Name: "label starting with a hyphen", Value: "-api.test"},
			{Name: "label ending with a hyphen", Value: "api-.test"},
			{Name: "empty label", Value: "api..test"},
			{Name: "trailing dot", Value: "api.test."},
			{Name: "label of 64 characters", Value: strings.Repeat("a", 64) + ".test"},
			{Name: "name of 254 bytes", Value: h(254)},
			{Name: "underscore", Value: "api_1.test"},
			{Name: "last label all digits", Value: "1.2.3"},
			{Name: "IPv4 with a leading zero", Value: "192.0.2.01"},
			{Name: "IPv4 out of range", Value: "256.0.0.1"},
			{Name: "IPv6 without brackets", Value: "2001:db8::1"},
			{Name: "IPv6 in upper case", Value: "[2001:DB8::1]"},
			{Name: "IPv6 not compressed", Value: "[2001:db8:0:0:0:0:0:1]"},
			{Name: "IPv4-mapped IPv6", Value: "[::ffff:192.0.2.1]"},
			{Name: "IPv6 with a zone", Value: "[fe80::1%eth0]"},
			{Name: "IPv4 in brackets", Value: "[192.0.2.1]"},
			// The umlaut as its UTF-8 bytes: an editor must not turn it into
			// anything else, and no escape may reach the vector as text.
			{Name: "non-ASCII", Value: "m\xc3\xbcnchen.example"},
			{Name: "space", Value: "api 1.test"},
			{Name: "line feed", Value: "api-1.test\x0a"},
			{Name: "quote", Value: "api\".test"},
		},
		HostPort: []urlCase{
			{Name: "host name", Value: "db-1.ngc.test:3306", Valid: true},
			{Name: "IPv4", Value: "192.0.2.5:3306", Valid: true},
			{Name: "IPv6", Value: "[2001:db8::5]:3306", Valid: true},
			// The rule bounds the host (253) and the port (5), not the whole:
			// the longest entry is 259 bytes, and a caller that wants less says
			// so itself (config.MaxField on config.DB.Nodes).
			{Name: "longest host and port", Value: h(253) + ":65535", Valid: true},
			{Name: "host of 254 bytes", Value: h(254) + ":65535"},
			{Name: "no port", Value: "db-1.ngc.test"},
			{Name: "port 0", Value: "db-1.ngc.test:0"},
			{Name: "port 65536", Value: "db-1.ngc.test:65536"},
			{Name: "port with a leading zero", Value: "db-1.ngc.test:03306"},
			{Name: "DSN parameters", Value: "db-1.ngc.test:3306)/ngc?allowAllFiles=true"},
			{Name: "upper case", Value: "DB-1.ngc.test:3306"},
			{Name: "IPv6 without brackets", Value: "2001:db8::5:3306"},
		},
		Tunnel: []urlCase{
			{Name: "host name", Value: "wss://api-1.test:7443/tunnel", Valid: true, Web: "https://api-1.test:7443"},
			{Name: "IPv4", Value: "wss://192.0.2.1:7443/tunnel", Valid: true, Web: "https://192.0.2.1:7443"},
			{Name: "IPv6", Value: "wss://[2001:db8::1]:7443/tunnel", Valid: true, Web: "https://[2001:db8::1]:7443"},
			{Name: "port 1", Value: "wss://a:1/tunnel", Valid: true, Web: "https://a:1"},
			{Name: "port 65535", Value: "wss://a:65535/tunnel", Valid: true, Web: "https://a:65535"},
			{Name: "256 bytes", Value: "wss://" + h(238) + ":7443/tunnel", Valid: true, Web: "https://" + h(238) + ":7443"},
			{Name: "empty", Value: ""},
			{Name: "scheme only", Value: "wss://"},
			{Name: "https", Value: "https://api-1.test:7443/tunnel"},
			{Name: "scheme in upper case", Value: "WSS://api-1.test:7443/tunnel"},
			{Name: "no port", Value: "wss://api-1.test/tunnel"},
			{Name: "port 0", Value: "wss://api-1.test:0/tunnel"},
			{Name: "port 65536", Value: "wss://api-1.test:65536/tunnel"},
			{Name: "port with a leading zero", Value: "wss://api-1.test:07443/tunnel"},
			{Name: "port with a sign", Value: "wss://api-1.test:+7443/tunnel"},
			{Name: "no path", Value: "wss://api-1.test:7443"},
			{Name: "root path", Value: "wss://api-1.test:7443/"},
			{Name: "trailing slash", Value: "wss://api-1.test:7443/tunnel/"},
			{Name: "path in other case", Value: "wss://api-1.test:7443/Tunnel"},
			{Name: "query", Value: "wss://api-1.test:7443/tunnel?x=1"},
			{Name: "fragment", Value: "wss://api-1.test:7443/tunnel#x"},
			{Name: "user info", Value: "wss://user@api-1.test:7443/tunnel"},
			{Name: "host in upper case", Value: "wss://API-1.test:7443/tunnel"},
			{Name: "line feed and an Apache directive", Value: "wss://api-1.test:7443/tunnel\x0aInclude /etc/shadow"},
			{Name: "quote in the host", Value: "wss://api\".test:7443/tunnel"},
			{Name: "257 bytes", Value: "wss://" + h(239) + ":7443/tunnel"},
		},
		Web: []urlCase{
			{Name: "host name", Value: "https://api-1.test:7443", Valid: true},
			{Name: "IPv6", Value: "https://[2001:db8::1]:443", Valid: true},
			{Name: "no port", Value: "https://api-1.test"},
			{Name: "root path", Value: "https://api-1.test:7443/"},
			{Name: "tunnel path", Value: "https://api-1.test:7443/tunnel"},
			{Name: "plain http", Value: "http://api-1.test:7443"},
			{Name: "line feed", Value: "https://api-1.test:7443\x0a"},
			{Name: "257 bytes", Value: "https://" + h(244) + ":7443"},
		},
		Origins: []urlCase{
			{Name: "host only", Value: "https://ngc.example.org", Valid: true},
			{Name: "port", Value: "https://ngc.example.org:8443", Valid: true},
			{Name: "IPv6", Value: "https://[2001:db8::1]:443", Valid: true},
			{Name: "256 bytes", Value: "https://" + h(248), Valid: true},
			{Name: "plain http", Value: "http://ngc.example.org"},
			{Name: "trailing slash", Value: "https://ngc.example.org/"},
			{Name: "path", Value: "https://ngc.example.org/x"},
			{Name: "query", Value: "https://ngc.example.org?y=1"},
			{Name: "fragment", Value: "https://ngc.example.org#z"},
			{Name: "user info", Value: "https://user@ngc.example.org"},
			{Name: "host in upper case", Value: "https://NGC.example.org"},
			{Name: "trailing space", Value: "https://ngc.example.org "},
			{Name: "line feed", Value: "https://ngc.example.org\x0a"},
			{Name: "quote", Value: "https://ngc.example.org\""},
			{Name: "port 0", Value: "https://ngc.example.org:0"},
			{Name: "257 bytes", Value: "https://" + h(249)},
		},
		Store: []urlCase{
			{Name: "host only", Value: "https://store.ngc.example", Valid: true},
			{Name: "port", Value: "https://store.ngc.example:8443", Valid: true},
			{Name: "path", Value: "https://example.org/ngc/store", Valid: true},
			{Name: "path characters", Value: "https://example.org/a-b_c.d~e", Valid: true},
			{Name: "IPv6", Value: "https://[2001:db8::1]:8443/ngc", Valid: true},
			{Name: "256 bytes", Value: "https://" + h(248), Valid: true},
			{Name: "plain http", Value: "http://store.ngc.example"},
			{Name: "trailing slash", Value: "https://store.ngc.example/"},
			{Name: "empty segment", Value: "https://example.org/a//b"},
			{Name: "dot segment", Value: "https://example.org/./a"},
			{Name: "dot-dot segment", Value: "https://example.org/a/../b"},
			{Name: "query", Value: "https://store.ngc.example?x=1"},
			{Name: "fragment", Value: "https://store.ngc.example#x"},
			{Name: "user info", Value: "https://user@store.ngc.example"},
			{Name: "space in the path", Value: "https://example.org/a b"},
			{Name: "percent escape", Value: "https://example.org/%41"},
			{Name: "host in upper case", Value: "https://Store.ngc.example"},
			{Name: "port 0", Value: "https://store.ngc.example:0"},
			{Name: "257 bytes", Value: "https://" + h(249)},
		},
	}
}

func TestAddresses(t *testing.T) {
	v := addressCases()
	check := func(kind string, cases []urlCase, f func(string) bool) {
		t.Helper()
		for _, c := range cases {
			if got := f(c.Value); got != c.Valid {
				t.Errorf("%s %s: %q gives %v, want %v", kind, c.Name, c.Value, got, c.Valid)
			}
		}
	}
	check("host", v.Hosts, trust.ValidHost)
	check("hostPort", v.HostPort, trust.ValidHostPort)
	check("tunnel", v.Tunnel, trust.ValidTunnelURL)
	check("web", v.Web, trust.ValidWebURL)
	check("origin", v.Origins, trust.ValidOrigin)
	check("store", v.Store, trust.ValidStoreURL)
	// Every tunnel address has exactly one address behind Apache, and that one
	// is an address the web configuration may hold.
	for _, c := range v.Tunnel {
		web, ok := trust.WebURL(c.Value)
		if ok != c.Valid || web != c.Web {
			t.Errorf("tunnel %s: WebURL(%q) = %q, %v, want %q, %v", c.Name, c.Value, web, ok, c.Web, c.Valid)
		}
		if ok && !trust.ValidWebURL(web) {
			t.Errorf("tunnel %s: WebURL gives %q, which ValidWebURL refuses", c.Name, web)
		}
	}
}

func TestURLVectors(t *testing.T) {
	golden.Check(t, *update, "url.json", addressCases())
}

// TestHostScript runs ulh/host.sh, the template for the ulh scripts, against
// the hosts of url.json: it takes the same names and IPv4 addresses as
// trust.ValidHost and refuses IPv6, which no POSIX shell checks reliably. It
// needs a POSIX shell and grep; the Linux servers have them, and there the
// test must pass.
func TestHostScript(t *testing.T) {
	for _, tool := range []string{"sh", "grep"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s missing", tool)
		}
	}
	var v urlVectors
	golden.Load(t, "url.json", &v)
	dir := t.TempDir()
	b, err := os.ReadFile(filepath.Join(golden.Dir, "ulh", "host.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "host.sh"), b, 0o755); err != nil {
		t.Fatal(err)
	}
	// A shell passes the hosts on, because Windows drops a line feed from an
	// argument on its way to sh while an environment variable keeps it: the
	// script must see the bytes of the vector, line feed included.
	run := func(args ...string) (string, int) {
		t.Helper()
		script, env := "sh host.sh", os.Environ()
		for i, a := range args {
			name := fmt.Sprintf("NGC_HOST%d", i)
			script += ` "$` + name + `"`
			env = append(env, name+"="+a)
		}
		cmd := exec.Command("sh", "-c", script)
		cmd.Dir = dir
		cmd.Env = env
		out, err := cmd.Output()
		code := 0
		if exit, ok := err.(*exec.ExitError); ok {
			code = exit.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		return string(out), code
	}
	for _, c := range v.Hosts {
		want := 1
		// The scripts write what they read from the admin, and that is a name
		// or an IPv4 address; an IPv6-only network is reached by name.
		if c.Valid && !strings.HasPrefix(c.Value, "[") {
			want = 0
		}
		if out, code := run(c.Value); code != want || out != "" {
			t.Errorf("%s: %q gives exit %d and %q, want exit %d and no output", c.Name, c.Value, code, out, want)
		}
	}
	if out, code := run(); code != 2 || out != "" {
		t.Errorf("no argument: exit %d, %q", code, out)
	}
	if out, code := run("a", "b"); code != 2 || out != "" {
		t.Errorf("two arguments: exit %d, %q", code, out)
	}
}

// FuzzAddresses holds the grammar to what it promises the programs that read
// these values: nothing an Apache directive, a DSN or a shell could read as
// more than one word, and a URL that net/url reads as the same address.
func FuzzAddresses(f *testing.F) {
	v := addressCases()
	for _, cases := range [][]urlCase{v.Hosts, v.HostPort, v.Tunnel, v.Web, v.Origins, v.Store} {
		for _, c := range cases {
			f.Add(c.Value)
		}
	}
	f.Fuzz(func(t *testing.T, s string) {
		accepted := trust.ValidHost(s) || trust.ValidHostPort(s) || trust.ValidTunnelURL(s) ||
			trust.ValidWebURL(s) || trust.ValidOrigin(s) || trust.ValidStoreURL(s)
		if accepted && !safeASCII(s) {
			t.Fatalf("%q accepted", s)
		}
		if trust.ValidTunnelURL(s) {
			u, err := neturl.Parse(s)
			if err != nil || u.Scheme != "wss" || u.User != nil || u.RawQuery != "" || u.ForceQuery ||
				u.Fragment != "" || u.Path != "/tunnel" || u.Port() == "" {
				t.Fatalf("tunnel %q: %+v, %v", s, u, err)
			}
			web, ok := trust.WebURL(s)
			if !ok || web != "https://"+u.Host || !trust.ValidWebURL(web) {
				t.Fatalf("tunnel %q: web %q, %v", s, web, ok)
			}
		}
		if trust.ValidOrigin(s) {
			// A browser writes scheme, host and port and nothing else, so an
			// origin has no path at all to compare.
			u, err := neturl.Parse(s)
			if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.ForceQuery ||
				u.Fragment != "" || u.Path != "" || s != "https://"+u.Host {
				t.Fatalf("origin %q: %+v, %v", s, u, err)
			}
		}
		if trust.ValidStoreURL(s) {
			u, err := neturl.Parse(s)
			if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
				t.Fatalf("store %q: %+v, %v", s, u, err)
			}
		}
		if trust.ValidHostPort(s) {
			if _, _, err := net.SplitHostPort(s); err != nil {
				t.Fatalf("hostPort %q: %v", s, err)
			}
		}
	})
}

// safeASCII reports whether s is printable ASCII without the characters that
// end a word or start something of their own in an Apache configuration, a
// DSN or a URL.
func safeASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x21 || c > 0x7e || strings.IndexByte("\"\\<>`{}|^", c) >= 0 {
			return false
		}
	}
	return true
}
