// Package url: hosts and addresses have one grammar and exactly one
// spelling, because NGC compares them as text (the trust list against codes,
// configurations against the trust list) and they end up in configuration
// files of other programs, where a space, a quote or a line feed would be a
// second line and not a bad address (Go: trust.ValidHost and its four address
// forms in shared/trust/url.go; vectors: url.json).
//
// Every check takes text and nothing else: what a caller leaves out or hands
// over in another form is no address, and webURL derives one from nothing.
const MAX_URL = 256;
const MAX_HOST = 253;

// The grammars below admit nothing but ASCII, so counting UTF-16 units is
// counting bytes for everything that could pass at all; nothing longer in
// bytes than in units ever gets past the characters.

function allDigits(s) {
  return s.length > 0 && /^[0-9]+$/.test(s);
}

function validDNSName(h) {
  if (h.length === 0 || h.length > MAX_HOST) return false;
  const labels = h.split(".");
  for (const l of labels) {
    if (l.length === 0 || l.length > 63 || l.startsWith("-") || l.endsWith("-")) return false;
    if (!/^[a-z0-9-]+$/.test(l)) return false;
  }
  // A last label of digits alone would be a broken IPv4 address, and resolvers
  // read such a name as an address.
  return !allDigits(labels[labels.length - 1]);
}

function validIPv4(h) {
  const parts = h.split(".");
  if (parts.length !== 4) return false;
  for (const p of parts) {
    if (!allDigits(p) || p.length > 3 || (p.length > 1 && p[0] === "0") || Number(p) > 255) return false;
  }
  return true;
}

// validIPv6 accepts exactly the spelling RFC 5952 asks for, the one Go's
// netip.Addr.String writes: lower case, the longest run of zero groups
// compressed once, no zone, not IPv4-mapped.
function validIPv6(s) {
  if (!/^[0-9a-f:]{2,45}$/.test(s) || s.includes(":::")) return false;
  const groups = expandIPv6(s);
  if (!groups) return false;
  // ::ffff:a.b.c.d would give an IPv4 address a second spelling.
  if (groups.slice(0, 5).every((g) => g === 0) && groups[5] === 0xffff) return false;
  return canonicalIPv6(groups) === s;
}

function expandIPv6(s) {
  const halves = s.split("::");
  if (halves.length > 2) return null;
  const head = halves[0] === "" ? [] : halves[0].split(":");
  const tail = halves.length === 2 ? (halves[1] === "" ? [] : halves[1].split(":")) : [];
  if (halves.length === 2 && head.length + tail.length > 7) return null;
  const parts = halves.length === 2
    ? [...head, ...Array(8 - head.length - tail.length).fill("0"), ...tail]
    : head;
  if (parts.length !== 8) return null;
  const out = [];
  for (const p of parts) {
    if (p.length === 0 || p.length > 4 || !/^[0-9a-f]+$/.test(p)) return null;
    out.push(parseInt(p, 16));
  }
  return out;
}

// canonicalIPv6 writes the groups as netip.Addr.String does: the longest run
// of zero groups is compressed, of two equally long ones the first, and a
// single zero group is written out (RFC 5952).
function canonicalIPv6(groups) {
  let bestStart = -1, bestLen = 0, start = -1, len = 0;
  for (let i = 0; i < 8; i++) {
    if (groups[i] === 0) {
      if (start < 0) { start = i; len = 0; }
      len++;
      if (len > bestLen) { bestLen = len; bestStart = start; }
    } else {
      start = -1; len = 0;
    }
  }
  const hex = (g) => g.toString(16);
  if (bestLen < 2) return groups.map(hex).join(":");
  return groups.slice(0, bestStart).map(hex).join(":") + "::" + groups.slice(bestStart + bestLen).map(hex).join(":");
}

// validHost reports whether h is a host: a DNS name of 1 to 253 bytes in
// lower case, an IPv4 address in dotted decimal without leading zeros, or an
// IPv6 address in brackets exactly as netip.Addr.String writes it.
export function validHost(h) {
  if (typeof h !== "string") return false;
  if (h.startsWith("[")) {
    return h.endsWith("]") && validIPv6(h.slice(1, -1));
  }
  if (validIPv4(h)) return true;
  // A bare IPv6 address is no host: only the bracketed spelling passes, and
  // netip reads an address as an address, never as a name.
  if (h.includes(":")) return false;
  return validDNSName(h);
}

function validPort(p) {
  if (p.length === 0 || p.length > 5 || p[0] === "0" || !allDigits(p)) return false;
  return Number(p) <= 65535;
}

// validHostPort reports whether s is host ":" port, the port 1 to 65535 in
// decimal without sign or leading zero.
export function validHostPort(s) {
  if (typeof s !== "string") return false;
  // The port is split off at the last colon, which comes after "]" for IPv6.
  const i = s.lastIndexOf(":");
  return i >= 0 && validHost(s.slice(0, i)) && validPort(s.slice(i + 1));
}

// validTunnelURL reports whether u is the tunnel address of an API: exactly
// "wss://" host ":" port "/tunnel", at most 256 bytes.
export function validTunnelURL(u) {
  if (typeof u !== "string" || u.length > MAX_URL || !u.startsWith("wss://") || !u.endsWith("/tunnel")) {
    return false;
  }
  return validHostPort(u.slice("wss://".length, u.length - "/tunnel".length));
}

// webURL returns the address behind Apache of the API with tunnel address u,
// which web.json lists and Apache proxies to, or null if u is no tunnel
// address: an address is derived from an address, never from anything else.
export function webURL(u) {
  if (!validTunnelURL(u)) return null;
  return "https://" + u.slice("wss://".length, u.length - "/tunnel".length);
}

// validWebURL reports whether u is such an address: exactly "https://" host
// ":" port, at most 256 bytes.
export function validWebURL(u) {
  if (typeof u !== "string" || u.length > MAX_URL || !u.startsWith("https://")) return false;
  return validHostPort(u.slice("https://".length));
}

// validOrigin reports whether o is an origin for passkeys: exactly "https://"
// host, and ":" port only for a port other than 443, at most 256 bytes.
export function validOrigin(o) {
  if (typeof o !== "string" || o.length > MAX_URL || !o.startsWith("https://")) return false;
  const rest = o.slice("https://".length);
  // A browser writes the origin without the default port, so an entry with
  // :443 would match nothing and lock everyone out instead of failing loudly.
  // validHostPort has parsed the port by then, so the suffix is the port and
  // not part of an IPv6 host, which validHost takes without one.
  return validHost(rest) || (validHostPort(rest) && !rest.endsWith(":443"));
}

// validStoreURL reports whether u is the address of a store: "https://", a
// host with an optional port, then nothing or a path of segments of A-Z a-z
// 0-9 . _ ~ - (none empty, none "." or ".."), without a trailing slash, user
// info, query or fragment; at most 256 bytes.
export function validStoreURL(u) {
  if (typeof u !== "string" || u.length > MAX_URL || !u.startsWith("https://")) return false;
  const rest = u.slice("https://".length);
  // A bracketed IPv6 host holds no "/", so the first one starts the path;
  // user info, query and fragment fail as part of the host.
  const cut = rest.indexOf("/");
  const authority = cut < 0 ? rest : rest.slice(0, cut);
  if (!validHost(authority) && !validHostPort(authority)) return false;
  if (cut < 0) return true;
  // The unreserved characters of RFC 3986 without percent escapes, so the
  // address has one spelling.
  for (const seg of rest.slice(cut + 1).split("/")) {
    if (seg === "" || seg === "." || seg === ".." || !/^[A-Za-z0-9._~-]+$/.test(seg)) return false;
  }
  return true;
}
