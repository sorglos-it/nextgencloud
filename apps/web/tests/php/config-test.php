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

function check(string $group, string $name, bool $want, bool $got): void
{
    global $failed, $passed;
    if ($want === $got) {
        $passed++;
        return;
    }
    $failed++;
    printf("FAIL  %s: %s — expected %s, got %s\n", $group, $name,
        $want ? 'valid' : 'invalid', $got ? 'valid' : 'invalid');
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

// --- Config against real files -------------------------------------------

$tmp = sys_get_temp_dir() . '/ngc-web-config-test';
@mkdir($tmp, 0700, true);

/** Writes the two files and reports whether Config::load accepts them. */
function loads(string $tmp, string $web, ?string $anchor): bool
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
    } catch (Throwable) {
        return false;
    }
}

$goodWeb = (string)file_get_contents(__DIR__ . '/../web.json');
$goodAnchor = (string)file_get_contents(__DIR__ . '/../trust-anchor.json');

$cases = [
    // name, web.json, trust-anchor.json, accepted
    ['the shipped test files', $goodWeb, $goodAnchor, true],
    ['no anchor yet', $goodWeb, null, true],
    ['no rp block at all', '{"apis":[{"url":"https://a.test:7443","tls":"x"}]}', null, true],
    ['empty rp block', '{"apis":[{"url":"https://a.test:7443","tls":"x"}],"rp":{}}', null, true],

    // Wichtig 1: a trailing line feed must not slip through an anchor.
    ['line feed on the anchor hash', $goodWeb,
        '{"seq":1,"hash":"q6urq6urq6urq6urq6urq6urq6urq6urq6urq6urq6s\n"}', false],
    ['line feed on rp.id', '{"apis":[{"url":"https://a.test:7443","tls":"x"}],'
        . '"rp":{"id":"ngc.example.org\n","origins":["https://ngc.example.org"]}}', null, false],

    // Wichtig 2: the same answers as the vectors, through Config.
    ['api address without a port', '{"apis":[{"url":"https://a.test","tls":"x"}]}', null, false],
    ['api address with a path', '{"apis":[{"url":"https://a.test:7443/tunnel","tls":"x"}]}', null, false],
    ['origin with the default port', '{"apis":[{"url":"https://a.test:7443","tls":"x"}],'
        . '"rp":{"id":"ngc.example.org","origins":["https://ngc.example.org:443"]}}', null, false],
    ['origin with port 0', '{"apis":[{"url":"https://a.test:7443","tls":"x"}],'
        . '"rp":{"id":"ngc.example.org","origins":["https://ngc.example.org:0"]}}', null, false],
    ['origin of colons', '{"apis":[{"url":"https://a.test:7443","tls":"x"}],'
        . '"rp":{"id":"ngc.example.org","origins":["https://:::"]}}', null, false],
    ['origin of brackets', '{"apis":[{"url":"https://a.test:7443","tls":"x"}],'
        . '"rp":{"id":"ngc.example.org","origins":["https://]]]"]}}', null, false],

    // Wichtig 3: a wrong type has to fail as loudly as a wrong value.
    ['rp is a string', '{"apis":[{"url":"https://a.test:7443","tls":"x"}],"rp":"ngc"}', null, false],
    ['rp.id is a number', '{"apis":[{"url":"https://a.test:7443","tls":"x"}],'
        . '"rp":{"id":42,"origins":["https://ngc.example.org"]}}', null, false],
    ['rp.origins is a string', '{"apis":[{"url":"https://a.test:7443","tls":"x"}],'
        . '"rp":{"id":"ngc.example.org","origins":"https://ngc.example.org"}}', null, false],
    ['rp.id without origins', '{"apis":[{"url":"https://a.test:7443","tls":"x"}],'
        . '"rp":{"id":"ngc.example.org"}}', null, false],
    ['origins without rp.id', '{"apis":[{"url":"https://a.test:7443","tls":"x"}],'
        . '"rp":{"origins":["https://ngc.example.org"]}}', null, false],

    // Strict JSON: a field nobody reads is a typo or a leftover, never a
    // reason to carry on.
    ['unknown field at the top', '{"apis":[{"url":"https://a.test:7443","tls":"x"}],"foo":1}', null, false],
    ['unknown field in apis[]', '{"apis":[{"url":"https://a.test:7443","tls":"x","foo":1}]}', null, false],
    ['unknown field in rp', '{"apis":[{"url":"https://a.test:7443","tls":"x"}],'
        . '"rp":{"id":"ngc.example.org","origins":["https://ngc.example.org"],"foo":1}}', null, false],
    ['unknown field in the anchor', $goodWeb,
        '{"seq":1,"hash":"q6urq6urq6urq6urq6urq6urq6urq6urq6urq6urq6s","foo":1}', false],
    ['field spelled differently', '{"apis":[{"url":"https://a.test:7443","tls":"x"}],"APIs":1}', null, false],

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

// The message of an unknown field has to name it, and nothing else: that is
// the only reason index.php is allowed to show.
/** @return string the message, or '' if the file was accepted */
function messageOf(string $tmp, string $web): string
{
    file_put_contents($tmp . '/web.json', $web);
    @unlink($tmp . '/anchor.json');
    try {
        Config::load($tmp . '/web.json', $tmp . '/anchor.json');
        return '';
    } catch (Ngc\ConfigError $e) {
        return $e->getMessage();
    } catch (Throwable) {
        return '(kein ConfigError)';
    }
}

$messages = [
    ['{"apis":[{"url":"https://a.test:7443","tls":"x"}],"foo":1}', 'Unbekanntes Feld in web.json: foo'],
    ['{"apis":[{"url":"https://a.test:7443","tls":"x","bar":1}]}', 'Unbekanntes Feld in web.json: apis[].bar'],
    ['{"apis":[{"url":"https://a.test:7443","tls":"x"}],"rp":{"id":"a.test","origins":[],"baz":1}}',
        'Unbekanntes Feld in web.json: rp.baz'],
    // A name that is long or odd never reaches the page as it stands.
    ['{"apis":[{"url":"https://a.test:7443","tls":"x"}],"a<script>b":1}',
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
