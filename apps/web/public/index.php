<?php
declare(strict_types=1);

// The only entry point of the web interface. Apache's DocumentRoot is this
// folder and serves everything below assets/ itself; every other path lands
// here. No app code ever runs in the web server.
require __DIR__ . '/../classes/Config.php';
require __DIR__ . '/../classes/Page.php';

$version = trim((string)@file_get_contents(__DIR__ . '/../VERSION'));
$webFile = getenv('NGC_WEB_CONFIG') ?: '/etc/ngc/web.json';
$anchorFile = getenv('NGC_WEB_ANCHOR') ?: '/etc/ngc/trust-anchor.json';

$path = parse_url($_SERVER['REQUEST_URI'] ?? '/', PHP_URL_PATH);
if (!is_string($path)) {
    Page::fail(400, 'Ungültige Anfrage.');
    return true;
}
// The built-in PHP server calls this file for existing files below public/
// too; there it must hand them back unchanged, as Apache does.
if (PHP_SAPI === 'cli-server' && $path !== '/' && is_file(__DIR__ . $path)) {
    return false;
}
if (($_SERVER['REQUEST_METHOD'] ?? 'GET') !== 'GET') {
    Page::fail(405, 'Nur GET.');
    return true;
}

try {
    $config = Config::load($webFile, $anchorFile);
} catch (Throwable) {
    // Never show the reason: it would name paths of the server.
    Page::fail(500, 'NGC ist nicht eingerichtet.');
    return true;
}

switch ($path) {
    case '/':
        Page::render(__DIR__ . '/../templates/desktop.php', []);
        break;
    case '/config.json':
        Page::headers('application/json; charset=utf-8');
        echo json_encode([
            'version' => $version,
            'anchor' => $config->anchor,
            'rp' => $config->rp,
            'apis' => $config->apis,
        ], JSON_THROW_ON_ERROR | JSON_UNESCAPED_SLASHES);
        break;
    default:
        Page::fail(404, 'Seite nicht gefunden.');
}
return true;
