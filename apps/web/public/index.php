<?php
declare(strict_types=1);

// The only entry point of the web interface. Apache's DocumentRoot is this
// folder and serves everything below assets/ itself; every other path lands
// here. No app code ever runs in the web server.
require_once __DIR__ . '/../classes/Config.php';
require_once __DIR__ . '/../classes/Page.php';

use Ngc\Config;
use Ngc\Lang;
use Ngc\ConfigError;
use Ngc\Page;

// VERSION is the only file read without a contract behind it, and its content
// goes out in the answer: bound it and check it like everything else. An
// unreadable or odd VERSION leaves the field empty rather than taking the
// whole page down over a version string.
$version = '';
$raw = @file_get_contents(__DIR__ . '/../VERSION', false, null, 0, 32);
if (is_string($raw) && preg_match('/^\d+\.\d+\.\d+\z/', trim($raw)) === 1) {
    $version = trim($raw);
}

// In operation the two paths are fixed. Only the test server may point them
// elsewhere, so no Apache setting can hand NGC a different configuration.
$webFile = '/etc/ngc/web.json';
$anchorFile = '/etc/ngc/trust-anchor.json';
if (PHP_SAPI === 'cli-server') {
    $webFile = getenv('NGC_WEB_CONFIG') ?: $webFile;
    $anchorFile = getenv('NGC_WEB_ANCHOR') ?: $anchorFile;
}

$path = parse_url($_SERVER['REQUEST_URI'] ?? '/', PHP_URL_PATH);
if (!is_string($path)) {
    Page::fail(400, Lang::t('page.badrequest'));
    return true;
}
// The built-in PHP server calls this file for existing files below public/
// too; there it must hand them back unchanged, as Apache does. Only assets
// take that way, and only under a name that cannot climb out of the web root:
// the list holds no backslash, and '..' is turned away by name.
if (PHP_SAPI === 'cli-server'
    && preg_match('#^/assets/[A-Za-z0-9._/-]+\z#', $path) === 1
    && !str_contains($path, '..')
    && is_file(__DIR__ . $path)) {
    return false;
}
if (($_SERVER['REQUEST_METHOD'] ?? 'GET') !== 'GET') {
    Page::fail(405, Lang::t('page.onlyget'));
    return true;
}

try {
    $config = Config::load($webFile, $anchorFile);
} catch (ConfigError $e) {
    // The one reason that may be shown: it names a field of the
    // configuration, so whoever mistyped it can see which one.
    Page::fail(500, $e->getMessage());
    return true;
} catch (Throwable) {
    // Never show any other reason: it would name paths of the server.
    Page::fail(500, Lang::t('page.notsetup'));
    return true;
}

switch ($path) {
    case '/':
        Page::render(__DIR__ . '/../templates/desktop.php', []);
        break;
    // An app window. It is a document of its own because only a document
    // fetched over http gets a policy of its own: a frame built with srcdoc
    // inherits this page's, and then the app's scripts are held to a
    // script-src written for the shell. See Page::FRAME_CSP.
    case '/frame.html':
        Page::frame(__DIR__ . '/assets/js/shell/frame-boot.js');
        break;
    case '/config.json':
        try {
            // Build the body first: a failure here must still be able to
            // become an error page.
            //
            // An installation whose web controller is not paired yet has no
            // anchor, and the member is left out rather than written as
            // null. Strict JSON has no null on either side - Go answers
            // enc.ErrNull and the shell's json.js refuses it the same way -
            // so "anchor": null was a body the shell could not read at all.
            // Leaving the member out is what Go's omitempty does for exactly
            // this case, and net/config.js reads an absent anchor as none.
            $payload = ['version' => $version];
            if ($config->anchor !== null) {
                $payload['anchor'] = $config->anchor;
            }
            $payload['rp'] = $config->rp;
            $payload['apis'] = $config->apis;
            $body = json_encode($payload, JSON_THROW_ON_ERROR | JSON_UNESCAPED_SLASHES);
        } catch (Throwable) {
            Page::fail(500, Lang::t('page.notsetup'));
            return true;
        }
        Page::send('application/json; charset=utf-8', $body);
        break;
    // THE LANGUAGES, AND THE FOLDER IS THE LIST. The shell asks for
    // /lang/index.json to learn which languages there are and then for the
    // one it wants. Neither this file nor the shell holds a list of them: a
    // new language is one more file in apps/web/lang/ and nothing else.
    //
    // The files are NOT below public/. Only a tag that Lang::read accepts
    // ever becomes a file name - two lower-case letters and an optional
    // region, so no '.', no '/' and no backslash can travel in - and the
    // answer is built out of the decoded table rather than passed through,
    // which is what keeps a file in that folder from becoming a way to send
    // arbitrary bytes under this origin.
    case '/lang/index.json':
        try {
            $body = json_encode(['languages' => Lang::available()],
                JSON_THROW_ON_ERROR | JSON_UNESCAPED_SLASHES);
        } catch (Throwable) {
            Page::fail(500, Lang::t('page.notsetup'));
            return true;
        }
        Page::send('application/json; charset=utf-8', $body);
        break;
    default:
        // One language file, under a name that was checked before it was
        // used. A tag nobody has is a 404 and not an error: the shell asks
        // for what the list named, and anybody else asking has simply asked
        // for a page that is not there.
        if (preg_match('#^/lang/([a-z]{2}(?:-[a-z]{2})?)\.json\z#', $path, $m) === 1) {
            $table = Lang::read($m[1]);
            if ($table === null) {
                Page::fail(404, Lang::t('page.notfound'));
                return true;
            }
            try {
                $body = json_encode($table, JSON_THROW_ON_ERROR | JSON_UNESCAPED_SLASHES);
            } catch (Throwable) {
                Page::fail(500, Lang::t('page.notsetup'));
                return true;
            }
            Page::send('application/json; charset=utf-8', $body);
            break;
        }
        Page::fail(404, Lang::t('page.notfound'));
}
return true;
