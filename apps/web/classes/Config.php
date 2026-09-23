<?php
declare(strict_types=1);

namespace Ngc;

use RuntimeException;

// Without a build step there is no autoloader, so each class names what it
// needs itself.
require_once __DIR__ . '/Url.php';
require_once __DIR__ . '/ConfigError.php';

// Config reads the two files ngc-web and the controller with role web write:
// /etc/ngc/web.json (config.Web) and /etc/ngc/trust-anchor.json (trust.Anchor).
// It only reads and checks; nothing here ever writes them.
//
// Every pattern ends in \z, never in $: PHP lets $ match before a closing
// line feed. One stray line feed on the anchor hash would travel into
// /config.json, the comparison against the real chain would fail, and the
// shell would raise the alarm and log the person out over a typo.
//
// Both files are read strictly: only the fields of the contract may appear,
// at the top and in the objects below it. An unknown field throws a
// ConfigError, whose German text names that field and nothing else.
final class Config
{
    public const MAX_FILE = 1048576;      // config.MaxConfig
    public const MAX_APIS = 16;           // config: maxList
    public const MAX_ORIGINS = 8;         // trust.MaxOrigins
    public const MAX_RP_ID = 253;         // proof.MaxRPID
    // trust.Anchor.Seq is a uint64, but the number goes on to the browser in
    // /config.json. Above 2^53-1 JSON numbers lose precision there, so the
    // shell would read a different sequence than the server wrote and compare
    // the wrong one against the chain.
    public const MAX_SEQ = 9007199254740991;

    /** @var list<string> */ public array $apis = [];
    /** @var array{id:string,origins:list<string>} */ public array $rp = ['id' => '', 'origins' => []];
    /** @var array{seq:int,hash:string}|null */ public ?array $anchor = null;

    public static function load(string $webFile, string $anchorFile): self
    {
        $c = new self();
        $web = self::readJson($webFile);
        self::onlyFields($web, ['apis', 'rp'], 'web.json', '');

        $apis = $web['apis'] ?? null;
        if (!is_array($apis) || !array_is_list($apis)
            || count($apis) < 1 || count($apis) > self::MAX_APIS) {
            throw new RuntimeException('web.json: apis');
        }
        foreach ($apis as $a) {
            // The address grammar is trust.ValidWebURL; PHP only passes the
            // address on, so it decides every case of url.json the same way
            // and never builds one of its own.
            if (!is_array($a)) {
                throw new RuntimeException('web.json: apis[]');
            }
            self::onlyFields($a, ['url', 'tls'], 'web.json', 'apis[].');
            if (!is_string($a['url'] ?? null) || !Url::validWebURL($a['url'])) {
                throw new RuntimeException('web.json: apis[].url');
            }
            $c->apis[] = $a['url'];
        }

        // config.Web: ID and origins are both set or both empty. A missing
        // block means "no passkeys yet". A block of the wrong type is a typo,
        // and it has to be as loud as a wrong value: otherwise a mistyped
        // configuration looks exactly like one that is not set up yet, and
        // nobody ever finds out why the passkeys are gone.
        $rp = $web['rp'] ?? null;
        if ($rp !== null) {
            if (!is_array($rp)) {
                throw new RuntimeException('web.json: rp');
            }
            self::onlyFields($rp, ['id', 'origins'], 'web.json', 'rp.');
            $id = $rp['id'] ?? '';
            $origins = $rp['origins'] ?? [];
            if (!is_string($id) || !is_array($origins) || !array_is_list($origins)) {
                throw new RuntimeException('web.json: rp');
            }
            if ($id !== '' || $origins !== []) {
                if ($id === '' || strlen($id) > self::MAX_RP_ID
                    || count($origins) < 1 || count($origins) > self::MAX_ORIGINS) {
                    throw new RuntimeException('web.json: rp');
                }
                // proof.ValidRPID, the same rule config.Web keeps.
                if (preg_match('/^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*\z/', $id) !== 1) {
                    throw new RuntimeException('web.json: rp.id');
                }
                foreach ($origins as $o) {
                    // trust.ValidOrigin: exactly what a browser writes into
                    // the client data, nothing else.
                    if (!is_string($o) || !Url::validOrigin($o)) {
                        throw new RuntimeException('web.json: rp.origins[]');
                    }
                }
                $c->rp = ['id' => $id, 'origins' => array_values($origins)];
            }
        }

        // The anchor arrives only once the controller of the web server is
        // paired; until then the shell keeps the one of the setup.
        if (is_file($anchorFile)) {
            $a = self::readJson($anchorFile);
            self::onlyFields($a, ['seq', 'hash'], 'trust-anchor.json', '');
            $seq = $a['seq'] ?? null;
            $hash = $a['hash'] ?? null;
            if (!is_int($seq) || $seq < 1 || $seq > self::MAX_SEQ
                || !is_string($hash) || preg_match('#^[A-Za-z0-9_-]{43}\z#', $hash) !== 1) {
                throw new RuntimeException('trust-anchor.json');
            }
            $c->anchor = ['seq' => $seq, 'hash' => $hash];
        }
        return $c;
    }

    /**
     * onlyFields is the strict JSON of shared/README.md on the PHP side, the
     * rule enc.DecodeStrict keeps in Go: a field nobody reads is either a typo
     * or a leftover from another version, and in both cases the setup has to
     * fail instead of quietly doing something else than meant.
     *
     * @param array<array-key,mixed> $o
     * @param list<string> $allowed
     */
    private static function onlyFields(array $o, array $allowed, string $file, string $prefix): void
    {
        foreach (array_keys($o) as $k) {
            if (!in_array((string)$k, $allowed, true)) {
                throw new ConfigError(
                    'Unbekanntes Feld in ' . $file . ': ' . $prefix . self::safeName((string)$k)
                );
            }
        }
    }

    // safeName keeps the shown name short and plain. The field name comes out
    // of a file, so without this a long or odd name would travel into the
    // answer; the person only needs to recognise what they mistyped.
    private static function safeName(string $k): string
    {
        if ($k === '' || strlen($k) > 40 || preg_match('/\A[A-Za-z0-9_.-]+\z/', $k) !== 1) {
            return '(unlesbarer Name)';
        }
        return $k;
    }

    /** @return array<string,mixed> */
    private static function readJson(string $path): array
    {
        $size = @filesize($path);
        if ($size === false || $size > self::MAX_FILE) {
            throw new RuntimeException("cannot read $path");
        }
        $raw = @file_get_contents($path, false, null, 0, self::MAX_FILE + 1);
        // The file may have grown between the two calls, so the length of
        // what was really read decides, not the earlier size.
        if ($raw === false || strlen($raw) > self::MAX_FILE) {
            throw new RuntimeException("cannot read $path");
        }
        $v = json_decode($raw, true, 32, JSON_THROW_ON_ERROR | JSON_BIGINT_AS_STRING);
        // A JSON array where an object belongs is a different file, not a
        // configuration with a missing field.
        if (!is_array($v) || ($v !== [] && array_is_list($v))) {
            throw new RuntimeException("$path is no object");
        }
        return $v;
    }
}
