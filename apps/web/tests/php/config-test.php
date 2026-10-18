<?php
declare(strict_types=1);

// Checks the PHP side of the contracts against shared/testdata/, the common
// truth for Go and JavaScript. The address grammar of Url has to decide every
// case of url.json exactly as trust/url.go does; anything laxer would hand
// the browser an address the rest of NGC never accepted.
//
// Run:  php apps/web/tests/php/config-test.php
// It needs nothing but PHP: no npm, no browser, no API.

require_once __DIR__ . '/../../classes/Config.php';

use Ngc\Config;
use Ngc\Url;

$root = dirname(__DIR__, 4);
$failed = 0;
$passed = 0;

// $got is null where the code under test threw something it must not throw.
// That is a failure whichever way the case was expected to go: counting it as
// "refused" would let an unexpected exception pass every case that expects a
// refusal, which is half the bolt missing.
function check(string $group, string $name, bool $want, ?bool $got): void
{
    global $failed, $passed;
    if ($got !== null && $want === $got) {
        $passed++;
        return;
    }
    $failed++;
    printf("FAIL  %s: %s — expected %s, got %s\n", $group, $name,
        $want ? 'valid' : 'invalid',
        $got === null ? 'an exception' : ($got ? 'valid' : 'invalid'));
}

// --- the address vectors -------------------------------------------------

$vectors = json_decode(
    (string)file_get_contents($root . '/shared/testdata/url.json'),
    true, 32, JSON_THROW_ON_ERROR
);

$grammars = [
    'hosts' => static fn(string $v): bool => Url::validHost($v),
    'hostPort' => static fn(string $v): bool => Url::validHostPort($v),
    'web' => static fn(string $v): bool => Url::validWebURL($v),
    'origins' => static fn(string $v): bool => Url::validOrigin($v),
];
foreach ($grammars as $group => $fn) {
    foreach ($vectors[$group] as $case) {
        check($group, $case['name'], $case['valid'], $fn($case['value']));
    }
    printf("%-9s %d cases\n", $group, count($vectors[$group]));
}

// --- IPv6 against Go, not against the platform ---------------------------

// url.json holds only a handful of IPv6 cases. These are the two classes a
// differential run against a compiled shared/trust/url.go turned up, where
// inet_pton and inet_ntop used to answer differently from Go — the second
// class even differently on Windows than on glibc.
$ipv6 = [
    // Six leading zero groups: netip writes exactly this, so it is canonical.
    ['[::1:0]', true],
    ['[::68:0]', true],
    ['[::1:2]', true],
    ['[::a:b]', true],
    // The 4-in-6 form, which netip writes as ::ffff:1.2.3.4 instead.
    ['[::ffff:0:0]', false],
    ['[::ffff:0:1]', false],
    ['[::ffff:7f00:1]', false],
    // The plain rules once more, so a rewrite cannot quietly lose them.
    ['[::]', true],
    ['[::1]', true],
    ['[2001:db8::1]', true],
    ['[1:0:0:1::]', true],
    ['[2001:0db8::1]', false],      // leading zero
    ['[2001:DB8::1]', false],       // upper case
    ['[2001:db8:0:0:0:0:0:1]', false], // not compressed
    ['[1::2::3]', false],           // two ::
    ['[1:2:3:4:5:6:7:8:9]', false], // nine groups
    ['[1:2:3:4:5:6:7]', false],     // seven groups without ::
    ['[1:2:3:4:5:6:7:8::]', false], // :: stands for nothing
    ['[1:0:0:1:0:0:1:1]', false],   // of two equal runs the first is taken
    ['[1::1:0:0:1:1]', true],       // ... which makes this one the spelling
];
foreach ($ipv6 as [$value, $want]) {
    check('ipv6', $value, $want, Url::validHost($value));
}
printf("%-9s %d cases\n", 'ipv6', count($ipv6));

// --- a grammar function answers, it never throws -------------------------

// inet_pton threw a ValueError on a NUL byte, and @ does not suppress
// exceptions. index.php happened to catch it, but a later caller with a
// narrower catch would not, so every valid… function has to answer.
$nasty = [
    "[\0]", "\0", "a\0b", "[\0::1]", "[::\0]", "[]", "[%]", "[:]", "[::%eth0]",
    "[" . str_repeat('a', 300) . "]", str_repeat('a', 300), str_repeat(':', 60),
    "[1:2:3:4:5:6:7:8:9:10:11:12:13:14:15:16]", "https://[\0]:443", "\xff\xfe",
];
foreach ($nasty as $value) {
    foreach (['validHost', 'validHostPort', 'validWebURL', 'validOrigin'] as $fn) {
        try {
            $got = Url::$fn($value);
            check('robust', $fn . ' ' . bin2hex($value), true, $got === false);
        } catch (Throwable $e) {
            check('robust', $fn . ' ' . bin2hex($value) . ' threw ' . $e::class, true, false);
        }
    }
}
printf("%-9s %d cases\n", 'robust', count($nasty) * 4);

// --- Config against real files -------------------------------------------

$tmp = sys_get_temp_dir() . '/ngc-web-config-test';
@mkdir($tmp, 0700, true);

/**
 * Writes the two files and reports whether Config::load accepts them:
 * true accepted, false refused, null threw something it must not throw.
 */
function loads(string $tmp, string $web, ?string $anchor): ?bool
{
    file_put_contents($tmp . '/web.json', $web);
    if ($anchor === null) {
        @unlink($tmp . '/anchor.json');
    } else {
        file_put_contents($tmp . '/anchor.json', $anchor);
    }
    try {
        Config::load($tmp . '/web.json', $tmp . '/anchor.json');
        return true;
    } catch (RuntimeException | JsonException) {
        // The two failures Config is allowed to have.
        return false;
    } catch (Throwable $e) {
        // Anything else is a bug, not a rejected file — whichever way the case
        // was expected to go. null makes it fail either way; returning a bool
        // here would let it pass half the time, the way a ValueError from
        // inet_pton once slipped through a broad catch.
        printf("THREW %s: %s\n", $e::class, $e->getMessage());
        return null;
    }
}

$goodWeb = (string)file_get_contents(__DIR__ . '/../web.json');
$goodAnchor = (string)file_get_contents(__DIR__ . '/../trust-anchor.json');

$cases = [
    // name, web.json, trust-anchor.json, accepted
    ['the shipped test files', $goodWeb, $goodAnchor, true],
    ['no anchor yet', $goodWeb, null, true],
    ['no rp block at all', '{"apis":[{"url":"https://a.test:7443","tls":"4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eE"}]}', null, true],
    ['empty rp block', '{"apis":[{"url":"https://a.test:7443","tls":"4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eE"}],"rp":{}}', null, true],

    // Wichtig 1: a trailing line feed must not slip through an anchor.
    ['line feed on the anchor hash', $goodWeb,
        '{"seq":1,"hash":"q6urq6urq6urq6urq6urq6urq6urq6urq6urq6urq6s\n"}', false],
    ['line feed on rp.id', '{"apis":[{"url":"https://a.test:7443","tls":"4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eE"}],'
        . '"rp":{"id":"ngc.example.org\n","origins":["https://ngc.example.org"]}}', null, false],

    // Wichtig 2: the same answers as the vectors, through Config.
    ['api address without a port', '{"apis":[{"url":"https://a.test","tls":"4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eE"}]}', null, false],
    ['api address with a path', '{"apis":[{"url":"https://a.test:7443/tunnel","tls":"4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eE"}]}', null, false],
    ['origin with the default port', '{"apis":[{"url":"https://a.test:7443","tls":"4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eE"}],'
        . '"rp":{"id":"ngc.example.org","origins":["https://ngc.example.org:443"]}}', null, false],
    ['origin with port 0', '{"apis":[{"url":"https://a.test:7443","tls":"4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eE"}],'
        . '"rp":{"id":"ngc.example.org","origins":["https://ngc.example.org:0"]}}', null, false],
    ['origin of colons', '{"apis":[{"url":"https://a.test:7443","tls":"4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eE"}],'
        . '"rp":{"id":"ngc.example.org","origins":["https://:::"]}}', null, false],
    ['origin of brackets', '{"apis":[{"url":"https://a.test:7443","tls":"4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eE"}],'
        . '"rp":{"id":"ngc.example.org","origins":["https://]]]"]}}', null, false],

    // Wichtig 3: a wrong type has to fail as loudly as a wrong value.
    ['rp is a string', '{"apis":[{"url":"https://a.test:7443","tls":"4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eE"}],"rp":"ngc"}', null, false],
    ['rp.id is a number', '{"apis":[{"url":"https://a.test:7443","tls":"4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eE"}],'
        . '"rp":{"id":42,"origins":["https://ngc.example.org"]}}', null, false],
    ['rp.origins is a string', '{"apis":[{"url":"https://a.test:7443","tls":"4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eE"}],'
        . '"rp":{"id":"ngc.example.org","origins":"https://ngc.example.org"}}', null, false],
    ['rp.id without origins', '{"apis":[{"url":"https://a.test:7443","tls":"4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eE"}],'
        . '"rp":{"id":"ngc.example.org"}}', null, false],
    ['origins without rp.id', '{"apis":[{"url":"https://a.test:7443","tls":"4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eE"}],'
        . '"rp":{"origins":["https://ngc.example.org"]}}', null, false],

    // Strict JSON: a field nobody reads is a typo or a leftover, never a
    // reason to carry on.
    ['unknown field at the top', '{"apis":[{"url":"https://a.test:7443","tls":"4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eE"}],"foo":1}', null, false],
    ['unknown field in apis[]', '{"apis":[{"url":"https://a.test:7443","tls":"4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eE","foo":1}]}', null, false],
    ['unknown field in rp', '{"apis":[{"url":"https://a.test:7443","tls":"4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eE"}],'
        . '"rp":{"id":"ngc.example.org","origins":["https://ngc.example.org"],"foo":1}}', null, false],
    ['unknown field in the anchor', $goodWeb,
        '{"seq":1,"hash":"q6urq6urq6urq6urq6urq6urq6urq6urq6urq6urq6s","foo":1}', false],
    ['field spelled differently', '{"apis":[{"url":"https://a.test:7443","tls":"4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eE"}],"APIs":1}', null, false],

    // config.Web.Validate, the two rules the first round left out.
    ['the same api address twice',
        '{"apis":[{"url":"https://a.test:7443","tls":"4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eE"},'
        . '{"url":"https://a.test:7443","tls":"4uLi4uLi4uLi4uLi4uLi4uLi4uLi4uLi4uLi4uLi4uI"}]}', null, false],
    ['two addresses that differ',
        '{"apis":[{"url":"https://a.test:7443","tls":"4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eE"},'
        . '{"url":"https://b.test:7443","tls":"4uLi4uLi4uLi4uLi4uLi4uLi4uLi4uLi4uLi4uLi4uI"}]}', null, true],
    ['tls missing', '{"apis":[{"url":"https://a.test:7443"}]}', null, false],
    ['tls too short', '{"apis":[{"url":"https://a.test:7443","tls":"4eHh"}]}', null, false],
    ['tls one character too long',
        '{"apis":[{"url":"https://a.test:7443","tls":"4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eEE"}]}', null, false],
    ['tls outside the alphabet',
        '{"apis":[{"url":"https://a.test:7443","tls":"4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4e+"}]}', null, false],
    ['tls is a number', '{"apis":[{"url":"https://a.test:7443","tls":42}]}', null, false],
    ['a NUL byte in the api address',
        "{\"apis\":[{\"url\":\"https://[\\u0000]:443\",\"tls\":\"4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eE\"}]}",
        null, false],

    // The rest of the shape.
    ['apis empty', '{"apis":[]}', null, false],
    ['apis is an object', '{"apis":{"url":"https://a.test:7443"}}', null, false],
    ['web.json is an array', '[1,2]', null, false],
    ['anchor seq 0', $goodWeb, '{"seq":0,"hash":"q6urq6urq6urq6urq6urq6urq6urq6urq6urq6urq6s"}', false],
    ['anchor seq as a string', $goodWeb, '{"seq":"1","hash":"q6urq6urq6urq6urq6urq6urq6urq6urq6urq6urq6s"}', false],
    ['anchor seq beyond 2^53-1', $goodWeb, '{"seq":9007199254740992,"hash":"q6urq6urq6urq6urq6urq6urq6urq6urq6urq6urq6s"}', false],
    ['anchor hash too short', $goodWeb, '{"seq":1,"hash":"q6ur"}', false],
];
foreach ($cases as [$name, $web, $anchor, $want]) {
    check('config', $name, $want, loads($tmp, $web, $anchor));
}
printf("%-9s %d cases\n", 'config', count($cases));

// --- 32 bytes as enc.Bytes writes them -----------------------------------

// 43 base64url characters carry 258 bits while 32 bytes are 256, so the last
// two bits of the final character are unused and Go's decoder insists they
// are zero. Measured against a compiled shared/enc: of the 64 characters
// exactly these 16 may end such a value. Every last character is tried here,
// for the pin and for the anchor hash, so PHP can never take one Go throws
// out — that would put a pin in web.json the browser believes and the server
// refuses.
$b64 = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_';
$goTakes = ['A', 'E', 'I', 'M', 'Q', 'U', 'Y', 'c', 'g', 'k', 'o', 's', 'w', '0', '4', '8'];
$head = '4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4e';   // 42 characters
foreach (str_split($b64) as $last) {
    $want = in_array($last, $goTakes, true);
    check('bytes32', "pin ending in $last", $want,
        loads($tmp, '{"apis":[{"url":"https://a.test:7443","tls":"' . $head . $last . '"}]}', null));
    check('bytes32', "anchor hash ending in $last", $want,
        loads($tmp, $goodWeb, '{"seq":1,"hash":"' . $head . $last . '"}'));
}
printf("%-9s %d cases\n", 'bytes32', strlen($b64) * 2);

// The message of an unknown field has to name it, and nothing else: that is
// the only reason index.php is allowed to show.
// THE LANGUAGE IS AN ARGUMENT NOW, and it is the one thing about this
// helper that is not obvious. Since the five languages arrived the message
// of a ConfigError comes out of apps/web/lang/ through Lang::t, and Lang
// picks the language from HTTP_ACCEPT_LANGUAGE - which a run on the command
// line does not have, so every message came back in English while the cases
// below pin the German one. MEASURED on the merged tree: 4 of 323 cases red,
// all four with the English sentence printed beside the German one.
//
// Setting the header here is not a way round that: it is what a request
// really carries, and asking for the language makes the case measure one
// thing more than it did - that the chosen language reaches the message.
// Lang::reset() is needed because Lang remembers its choice for the process.
/** @return string the message, or '' if the file was accepted */
function messageOf(string $tmp, string $web, string $accept = 'de'): string
{
    file_put_contents($tmp . '/web.json', $web);
    @unlink($tmp . '/anchor.json');
    $_SERVER['HTTP_ACCEPT_LANGUAGE'] = $accept;
    Ngc\Lang::reset();
    try {
        Config::load($tmp . '/web.json', $tmp . '/anchor.json');
        return '';
    } catch (Ngc\ConfigError $e) {
        return $e->getMessage();
    } catch (Throwable) {
        return '(kein ConfigError)';
    } finally {
        unset($_SERVER['HTTP_ACCEPT_LANGUAGE']);
        Ngc\Lang::reset();
    }
}

$messages = [
    ['{"apis":[{"url":"https://a.test:7443","tls":"4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eE"}],"foo":1}', 'Unbekanntes Feld in web.json: foo'],
    ['{"apis":[{"url":"https://a.test:7443","tls":"4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eE","bar":1}]}', 'Unbekanntes Feld in web.json: apis[].bar'],
    ['{"apis":[{"url":"https://a.test:7443","tls":"4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eE"}],"rp":{"id":"a.test","origins":[],"baz":1}}',
        'Unbekanntes Feld in web.json: rp.baz'],
    // A name that is long or odd never reaches the page as it stands.
    ['{"apis":[{"url":"https://a.test:7443","tls":"4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eE"}],"a<script>b":1}',
        'Unbekanntes Feld in web.json: (unlesbarer Name)'],
];
foreach ($messages as [$web, $want]) {
    $got = messageOf($tmp, $web);
    check('message', $want, true, $got === $want);
    if ($got !== $want) {
        printf("      got: %s\n", $got);
    }
}
printf("%-9s %d cases\n", 'message', count($messages));

// THAT THE SENTENCE REALLY COMES OUT OF THE FILES, and not out of a literal
// that happens to be German. The same refusal asked for in English has to
// answer with another sentence - a message written into the code anywhere
// would answer the same words twice, and that is what this makes impossible.
$first = $messages[0][0];
$de = messageOf($tmp, $first, 'de');
$en = messageOf($tmp, $first, 'en');
check('language', 'the German answer is the German file', true, $de === $messages[0][1]);
check('language', 'the English answer is another sentence', true, $en !== '' && $en !== $de);
if ($en === '' || $en === $de) {
    printf("      de: %s\n      en: %s\n", $de, $en);
}
printf("%-9s %d cases\n", 'language', 2);

// The shipped files must also produce exactly the values /config.json sends.
$c = Config::load(__DIR__ . '/../web.json', __DIR__ . '/../trust-anchor.json');
check('config', 'apis of the test files', true,
    $c->apis === ['https://api-1.ngc.test:7443', 'https://api-2.ngc.test:7443']);
check('config', 'rp of the test files', true,
    $c->rp === ['id' => 'ngc.example.org', 'origins' => ['https://ngc.example.org']]);
check('config', 'anchor of the test files', true,
    $c->anchor === ['seq' => 1, 'hash' => 'q6urq6urq6urq6urq6urq6urq6urq6urq6urq6urq6s']);

@unlink($tmp . '/web.json');
@unlink($tmp . '/anchor.json');
@rmdir($tmp);

printf("\n%d passed, %d failed\n", $passed, $failed);
exit($failed === 0 ? 0 : 1);
