<?php
declare(strict_types=1);

// Config reads the two files ngc-web and the controller with role web write:
// /etc/ngc/web.json (config.Web) and /etc/ngc/trust-anchor.json (trust.Anchor).
// It only reads and checks; nothing here ever writes them.
final class Config
{
    public const MAX_FILE = 1048576;      // config.MaxConfig
    public const MAX_APIS = 16;           // config: maxList
    public const MAX_ORIGINS = 8;         // trust.MaxOrigins

    /** @var list<string> */ public array $apis = [];
    /** @var array{id:string,origins:list<string>} */ public array $rp = ['id' => '', 'origins' => []];
    /** @var array{seq:int,hash:string}|null */ public ?array $anchor = null;

    public static function load(string $webFile, string $anchorFile): self
    {
        $c = new self();
        $web = self::readJson($webFile);
        if (!is_array($web['apis'] ?? null) || count($web['apis']) < 1 || count($web['apis']) > self::MAX_APIS) {
            throw new RuntimeException('web.json: apis');
        }
        foreach ($web['apis'] as $a) {
            // The address grammar is trust.ValidWebURL; PHP only passes it on,
            // so it checks the shape and never builds one of its own.
            if (!is_array($a) || !is_string($a['url'] ?? null) || !preg_match('#^https://[^\s"\'<>\\\\]{1,248}$#', $a['url'])) {
                throw new RuntimeException('web.json: apis[].url');
            }
            $c->apis[] = $a['url'];
        }
        $rp = $web['rp'] ?? null;
        if (is_array($rp) && is_string($rp['id'] ?? null) && $rp['id'] !== '') {
            if (!preg_match('/^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$/', $rp['id'])
                || strlen($rp['id']) > 253) {
                throw new RuntimeException('web.json: rp.id');
            }
            $origins = $rp['origins'] ?? [];
            if (!is_array($origins) || count($origins) < 1 || count($origins) > self::MAX_ORIGINS) {
                throw new RuntimeException('web.json: rp.origins');
            }
            foreach ($origins as $o) {
                if (!is_string($o) || !preg_match('#^https://[a-z0-9.\[\]:-]{1,248}$#', $o)) {
                    throw new RuntimeException('web.json: rp.origins[]');
                }
            }
            $c->rp = ['id' => $rp['id'], 'origins' => array_values($origins)];
        }
        // The anchor arrives only once the controller of the web server is
        // paired; until then the shell keeps the one of the setup.
        if (is_file($anchorFile)) {
            $a = self::readJson($anchorFile);
            if (!is_int($a['seq'] ?? null) || $a['seq'] < 1 || !is_string($a['hash'] ?? null)
                || !preg_match('#^[A-Za-z0-9_-]{43}$#', $a['hash'])) {
                throw new RuntimeException('trust-anchor.json');
            }
            $c->anchor = ['seq' => $a['seq'], 'hash' => $a['hash']];
        }
        return $c;
    }

    /** @return array<string,mixed> */
    private static function readJson(string $path): array
    {
        $size = @filesize($path);
        if ($size === false || $size > self::MAX_FILE) {
            throw new RuntimeException("cannot read $path");
        }
        $raw = @file_get_contents($path, false, null, 0, self::MAX_FILE + 1);
        if ($raw === false) {
            throw new RuntimeException("cannot read $path");
        }
        $v = json_decode($raw, true, 32, JSON_THROW_ON_ERROR | JSON_BIGINT_AS_STRING);
        if (!is_array($v)) {
            throw new RuntimeException("$path is no object");
        }
        return $v;
    }
}
