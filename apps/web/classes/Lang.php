<?php
declare(strict_types=1);

namespace Ngc;

// Lang reads the language files of the web interface.
//
// Two callers, and they want different things from the same folder:
//
//   available() + read() - the answer to /lang/index.json and /lang/<tag>.json,
//     which the browser's shell/lang.js asks for. PHP is the only part that can
//     look into the folder, so the discovery of what languages there are
//     happens here and nowhere else.
//   t() - the handful of sentences PHP itself puts on a page: the error pages
//     of index.php, which are sent before any JavaScript runs and therefore
//     cannot come out of the shell's lookup.
//
// The folder is NOT below public/. The files are read here and handed out
// under a name that is checked first; a request can never name a file.
final class Lang
{
    // The fall-back for a missing key and for a language that is not there.
    // The only tag in the code, and it is not a list of what exists.
    public const FALLBACK = 'en';

    // A language tag as this folder names its files: two lower-case letters,
    // optionally a region. Anything else never becomes a file name, so no
    // request can climb out of the folder - there is no '.', no '/' and no
    // backslash a tag could carry.
    private const TAG = '/^[a-z]{2}(-[a-z]{2})?\z/';

    // A language file is small. A bound stops a folder somebody filled up
    // from being read into memory whole.
    private const MAX_BYTES = 262144;

    private static ?array $table = null;
    private static ?string $chosen = null;

    public static function dir(): string
    {
        return __DIR__ . '/../lang';
    }

    // available lists the languages there are, read from the folder itself.
    // Sorted, so the answer of /lang/index.json does not change with the order
    // the file system happens to hand things back in.
    public static function available(): array
    {
        $out = [];
        $names = @scandir(self::dir());
        if ($names === false) {
            return $out;
        }
        foreach ($names as $name) {
            if (!str_ends_with($name, '.json')) {
                continue;
            }
            $tag = substr($name, 0, -5);
            if (preg_match(self::TAG, $tag) === 1) {
                $out[] = $tag;
            }
        }
        sort($out);
        return $out;
    }

    // read gives back one language file as a table, or null. The tag is
    // checked against TAG before it is used, and the decoded value has to be
    // an object of strings: a file that holds an array or a nested table would
    // make the shell answer undefined for a key, which is the naked key the
    // rule forbids.
    public static function read(string $tag): ?array
    {
        if (preg_match(self::TAG, $tag) !== 1) {
            return null;
        }
        $raw = @file_get_contents(self::dir() . '/' . $tag . '.json', false, null, 0, self::MAX_BYTES + 1);
        if (!is_string($raw) || $raw === '' || strlen($raw) > self::MAX_BYTES) {
            return null;
        }
        try {
            $v = json_decode($raw, true, 8, JSON_THROW_ON_ERROR);
        } catch (\Throwable) {
            return null;
        }
        if (!is_array($v) || $v === [] || array_is_list($v)) {
            return null;
        }
        foreach ($v as $key => $text) {
            if (!is_string($key) || !is_string($text)) {
                return null;
            }
        }
        return $v;
    }

    // choose picks the language out of what the folder has and what the
    // browser asked for. Accept-Language is parsed for its tags and their q
    // values; a header that is nonsense simply leaves the fall-back standing.
    public static function choose(string $header, array $available): string
    {
        $have = [];
        foreach ($available as $tag) {
            $have[$tag] = true;
        }
        $wanted = [];
        foreach (explode(',', $header) as $part) {
            $bits = explode(';', trim($part));
            $tag = strtolower(trim($bits[0]));
            if ($tag === '' || $tag === '*') {
                continue;
            }
            $q = 1.0;
            for ($i = 1; $i < count($bits); $i++) {
                $kv = explode('=', trim($bits[$i]), 2);
                if (count($kv) === 2 && strtolower(trim($kv[0])) === 'q') {
                    $q = (float) trim($kv[1]);
                }
            }
            $wanted[] = ['tag' => $tag, 'q' => $q];
        }
        // A stable sort by q alone: equal weights keep the order the header
        // gave them, which is the person's own order of preference.
        usort($wanted, fn (array $a, array $b) => $b['q'] <=> $a['q']);
        foreach ($wanted as $w) {
            if (isset($have[$w['tag']])) {
                return $w['tag'];
            }
            $base = explode('-', $w['tag'])[0];
            if (isset($have[$base])) {
                return $base;
            }
        }
        return self::FALLBACK;
    }

    // tag is the language PHP's own pages are in.
    public static function tag(): string
    {
        if (self::$chosen === null) {
            $header = $_SERVER['HTTP_ACCEPT_LANGUAGE'] ?? '';
            self::$chosen = self::choose(is_string($header) ? $header : '', self::available());
        }
        return self::$chosen;
    }

    // t is one sentence for a page PHP sends itself. Chosen language first,
    // then English - never the naked key and never an empty place, so the
    // worst case is a sentence in the wrong language and not a word out of a
    // programme.
    public static function t(string $key, array $params = []): string
    {
        if (self::$table === null) {
            $main = self::read(self::tag()) ?? [];
            $base = self::tag() === self::FALLBACK ? [] : (self::read(self::FALLBACK) ?? []);
            self::$table = $main + $base;
        }
        $text = self::$table[$key] ?? null;
        if ($text === null) {
            // No file at all, or a key nobody filled in. The page still has to
            // say something, and the one thing it can say without a file is
            // that the files are missing.
            $text = self::$table['boot.nolang'] ?? 'NGC could not load its language files.';
        }
        foreach ($params as $name => $value) {
            $text = str_replace('{' . $name . '}', (string) $value, $text);
        }
        return $text;
    }

    // page is the block templates/desktop.php writes into the document: the
    // chosen language and English, which is what public/assets/js/lang.js
    // reads instead of fetching anything. Two tables and not one, because the
    // rule is that a key the chosen language has not got comes out of English.
    //
    // It goes in as JSON inside <script type="application/json">, which the
    // browser does not execute - so the page's "script-src 'self'" stays as
    // tight as it was and nothing was loosened for this.
    public static function page(): string
    {
        $tag = self::tag();
        $payload = [
            'tag' => $tag,
            'main' => self::read($tag) ?? new \stdClass(),
            'base' => $tag === self::FALLBACK ? new \stdClass() : (self::read(self::FALLBACK) ?? new \stdClass()),
        ];
        try {
            $json = json_encode($payload, JSON_THROW_ON_ERROR | JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE);
        } catch (\Throwable) {
            // No tables, no block. lang.js then says the one sentence it can
            // say without them, which is better than a page that does not come
            // up at all.
            return '';
        }
        // The one sequence that could end the script element early. Escaping
        // it keeps the text a text however a translation is worded; no </ can
        // reach the parser.
        return str_replace('<', '\u003C', $json);
    }

    // reset throws away what was read and chosen. Only the tests call it:
    // one process answers one request in operation, and there the cache is
    // what keeps a page from reading the same file five times.
    public static function reset(): void
    {
        self::$table = null;
        self::$chosen = null;
    }
}
