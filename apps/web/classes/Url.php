<?php
declare(strict_types=1);

namespace Ngc;

// Url is the address grammar of shared/trust/url.go, rule for rule. A regular
// expression cannot carry it: the canonical spelling of an IPv6 address, the
// range of a port and the rule that the last label of a name is not all
// digits are decisions, not patterns. The vectors in shared/testdata/url.json
// are the common truth for both sides; apps/web/tests/php/config-test.php
// runs every one of them against this file, so the two cannot drift apart.
//
// The Go side guards what is written into web.json. This file guards what
// leaves the web server for the browser, which is a different door.
final class Url
{
    public const MAX_URL = 256;   // trust: maxURL, config.MaxField
    public const MAX_HOST = 253;  // trust: maxHost, the longest DNS name

    // validWebURL mirrors trust.ValidWebURL: exactly "https://" host ":" port.
    public static function validWebURL(string $u): bool
    {
        if (strlen($u) > self::MAX_URL || !str_starts_with($u, 'https://')) {
            return false;
        }
        return self::validHostPort(substr($u, 8));
    }

    // validOrigin mirrors trust.ValidOrigin: exactly "https://" host, with a
    // port only where it is not 443. A browser writes the origin of the page
    // in exactly this shape, and proof.Verify compares it as text; an entry a
    // browser would never write lets nobody log in instead of failing loudly.
    public static function validOrigin(string $o): bool
    {
        if (strlen($o) > self::MAX_URL || !str_starts_with($o, 'https://')) {
            return false;
        }
        $rest = substr($o, 8);
        if (self::validHost($rest)) {
            return true;
        }
        return self::validHostPort($rest) && !str_ends_with($rest, ':443');
    }

    // validHostPort mirrors trust.ValidHostPort: host ":" port.
    public static function validHostPort(string $s): bool
    {
        // The port is split at the last ':', which comes after ']' for IPv6.
        $i = strrpos($s, ':');
        if ($i === false) {
            return false;
        }
        return self::validHost(substr($s, 0, $i)) && self::validPort(substr($s, $i + 1));
    }

    // validHost mirrors trust.ValidHost: a DNS name in lower case, an IPv4
    // address in dotted decimal, or an IPv6 address in brackets. Each address
    // has exactly one spelling, because NGC compares addresses as text.
    public static function validHost(string $h): bool
    {
        // Nothing longer can be a host, and the bound keeps every step below
        // cheap whatever a caller hands in.
        if ($h === '' || strlen($h) > self::MAX_HOST) {
            return false;
        }
        if (str_starts_with($h, '[')) {
            if (!str_ends_with($h, ']')) {
                return false;
            }
            return self::validIPv6(substr($h, 1, -1));
        }
        if (self::validIPv4($h)) {
            return true;
        }
        return self::validDNSName($h);
    }

    // validIPv6 takes only the canonical form of RFC 5952, which is what
    // netip.Addr.String writes: lower case, zeros compressed, no zone and not
    // IPv4-mapped, because each of those would be a second spelling.
    //
    // It is spelled out rather than left to inet_pton and inet_ntop. Those
    // answer differently on different platforms, and measured against Go both
    // ways: they call ::1:0 invalid although Go writes exactly that, and on
    // Windows they call ::ffff:0:0 valid although Go rejects it as the 4-in-6
    // form. A rule that depends on the machine is no rule. inet_pton also
    // throws on a NUL byte in PHP 8, and a grammar function must answer, not
    // throw: a later caller would not expect it.
    private static function validIPv6(string $s): bool
    {
        $groups = self::parseIPv6($s);
        if ($groups === null) {
            return false;
        }
        // The 4-in-6 form: netip writes it as ::ffff:1.2.3.4, so it would be
        // a second spelling of an address that already has a plain one.
        if ($groups[0] === 0 && $groups[1] === 0 && $groups[2] === 0
            && $groups[3] === 0 && $groups[4] === 0 && $groups[5] === 0xffff) {
            return false;
        }
        return self::writeIPv6($groups) === $s;
    }

    /**
     * parseIPv6 reads the eight groups of an address, or null if s is none.
     * It is stricter than netip.ParseAddr on purpose: upper case, a leading
     * zero and an embedded IPv4 address are turned away here instead of
     * later, because none of them can ever equal the canonical spelling.
     *
     * @return list<int>|null
     */
    private static function parseIPv6(string $s): ?array
    {
        // 45 bytes hold the longest address there is. A zone names an
        // interface of one machine, which no other machine can use; a dot is
        // the embedded IPv4 form.
        if ($s === '' || strlen($s) > 45 || str_contains($s, '%') || str_contains($s, '.')) {
            return null;
        }
        $double = strpos($s, '::');
        if ($double === false) {
            $groups = self::hexGroups($s);
            return $groups !== null && count($groups) === 8 ? $groups : null;
        }
        if (strpos($s, '::', $double + 1) !== false) {
            return null;   // at most one ::
        }
        $left = self::hexGroups(substr($s, 0, $double));
        $right = self::hexGroups(substr($s, $double + 2));
        if ($left === null || $right === null) {
            return null;
        }
        // The :: has to stand for at least one group of zeros, as in Go.
        $fill = 8 - count($left) - count($right);
        if ($fill < 1) {
            return null;
        }
        return array_merge($left, array_fill(0, $fill, 0), $right);
    }

    /**
     * hexGroups reads a run of groups separated by ':'. An empty text gives
     * an empty list, which is what the two sides of a '::' may be.
     *
     * @return list<int>|null
     */
    private static function hexGroups(string $s): ?array
    {
        if ($s === '') {
            return [];
        }
        $out = [];
        foreach (explode(':', $s) as $g) {
            $n = strlen($g);
            if ($n < 1 || $n > 4 || ($n > 1 && $g[0] === '0')) {
                return null;
            }
            $v = 0;
            for ($i = 0; $i < $n; $i++) {
                $c = $g[$i];
                if ($c >= '0' && $c <= '9') {
                    $v = $v * 16 + (ord($c) - 0x30);
                } elseif ($c >= 'a' && $c <= 'f') {
                    $v = $v * 16 + (ord($c) - 0x57);
                } else {
                    return null;
                }
            }
            $out[] = $v;
            if (count($out) > 8) {
                return null;
            }
        }
        return $out;
    }

    /**
     * writeIPv6 is netip.Addr.String for an address that is not 4-in-6: the
     * leftmost longest run of at least two zero groups becomes '::', every
     * other group is lower-case hex without leading zeros.
     *
     * @param list<int> $g
     */
    private static function writeIPv6(array $g): string
    {
        $zeroStart = -1;
        $zeroEnd = -1;
        for ($i = 0; $i < 8; $i++) {
            $j = $i;
            while ($j < 8 && $g[$j] === 0) {
                $j++;
            }
            // Strictly longer, so of two runs of the same length the first
            // one wins, exactly as Go decides it.
            if ($j - $i >= 2 && $j - $i > $zeroEnd - $zeroStart) {
                $zeroStart = $i;
                $zeroEnd = $j;
            }
        }
        $out = '';
        for ($i = 0; $i < 8; $i++) {
            if ($i === $zeroStart) {
                $out .= '::';
                $i = $zeroEnd;
                if ($i >= 8) {
                    break;
                }
            } elseif ($i > 0) {
                $out .= ':';
            }
            $out .= dechex($g[$i]);
        }
        return $out;
    }

    // validIPv4 takes only dotted decimal without leading zeros. It is spelled
    // out rather than left to inet_pton, whose strictness differs between
    // platforms, and this file must decide the same on every platform.
    private static function validIPv4(string $s): bool
    {
        $parts = explode('.', $s);
        if (count($parts) !== 4) {
            return false;
        }
        foreach ($parts as $p) {
            if ($p === '' || strlen($p) > 3 || !self::allDigits($p)) {
                return false;
            }
            if ($p[0] === '0' && $p !== '0') {
                return false;
            }
            if ((int)$p > 255) {
                return false;
            }
        }
        return true;
    }

    // validDNSName checks the name rule of trust.ValidHost.
    private static function validDNSName(string $h): bool
    {
        if ($h === '' || strlen($h) > self::MAX_HOST) {
            return false;
        }
        $labels = explode('.', $h);
        foreach ($labels as $l) {
            if ($l === '' || strlen($l) > 63 || $l[0] === '-' || $l[strlen($l) - 1] === '-') {
                return false;
            }
            // Byte for byte, so a line feed or a non-ASCII byte falls out
            // here and not at an anchor that might let it pass.
            for ($i = 0; $i < strlen($l); $i++) {
                $c = $l[$i];
                if (!(($c >= 'a' && $c <= 'z') || ($c >= '0' && $c <= '9') || $c === '-')) {
                    return false;
                }
            }
        }
        // A last label of digits alone would be a broken IPv4 address rather
        // than a name, and resolvers read such a name as an address.
        return !self::allDigits($labels[count($labels) - 1]);
    }

    // validPort checks the port rule of trust.ValidHostPort: 1 to 65535 in
    // decimal, without sign and without a leading zero.
    private static function validPort(string $p): bool
    {
        if ($p === '' || strlen($p) > 5 || $p[0] === '0' || !self::allDigits($p)) {
            return false;
        }
        $n = (int)$p;
        return $n >= 1 && $n <= 65535;
    }

    private static function allDigits(string $s): bool
    {
        if ($s === '') {
            return true;
        }
        for ($i = 0; $i < strlen($s); $i++) {
            if ($s[$i] < '0' || $s[$i] > '9') {
                return false;
            }
        }
        return true;
    }
}
