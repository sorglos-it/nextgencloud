<?php
declare(strict_types=1);

namespace Ngc;

require_once __DIR__ . '/Lang.php';

// Page sends the headers of the web interface and fills the template.
//
// The policy stays as tight as the page allows, because this is the document
// that will hold the session keys. It carries no inline script and no inline
// style, so nothing loosens script-src or style-src.
//
// data: stays for images, fonts and media: an embedded icon or font is the
// normal way, and no code runs from there.
//
// WHERE APP WINDOWS COME FROM was left open here for Task 21 to decide. It is
// decided now, and the decision is FRAME_CSP below: an app window is
// /frame.html, a document of this server with a policy of its own. The other
// way - an iframe with srcdoc, which the task brief asked for - was MEASURED
// AND DOES NOT WORK: a document of a local scheme (about:srcdoc, data:, blob:)
// INHERITS the policy of the page that framed it, so the app's scripts are
// held to the script-src of this page. Chromium 141 and WebKit 26 both refuse
// them ("Refused to load the script 'data:text/javascript,...' because it
// violates ... script-src 'self'"), and a meta policy in the frame can only
// add to that, never take it back. The only way to give the app's frame a
// policy of its own is to serve it over http, which is what happens here.
// Nothing about THIS page's policy was loosened for it: frame-src 'self'
// already allowed a frame of this server.
final class Page
{
    public const CSP = "default-src 'none'; script-src 'self'; style-src 'self'; "
        . "img-src 'self' data:; font-src 'self' data:; media-src data:; connect-src 'self'; "
        . "frame-src 'self'; worker-src 'self'; manifest-src 'none'; "
        . "base-uri 'none'; form-action 'none'; frame-ancestors 'none'";

    // The policy of an app window. Three things carry it:
    //
    //   sandbox allow-scripts - the browser gives this document an opaque
    //     origin however it is loaded, so it can never reach this server's
    //     origin, its storage or its cookies. It is in the HEADER and not
    //     only on the iframe, so a page that framed it without the attribute,
    //     or a person who opened it on its own, gets the same walls.
    //   script-src data: - the app's own scripts, which the shell writes into
    //     the document as data: addresses out of the proven ui-tar. NOT
    //     'self': with 'self' the app could ask this server for any address
    //     it liked, and a request is a way to send something out.
    //   connect-src 'none' - no connection of the app's own. It talks to its
    //     services through the shell (ngc.js) and in no other way.
    //
    // frame-ancestors 'self' so that only NGC may frame it. It carries no
    // secret and has an origin of its own, so this is about tidiness and not
    // about a hole: whatever it shows, it shows out of what NGC gave it.
    public const FRAME_CSP = "sandbox allow-scripts; default-src 'none'; script-src data:; "
        . "style-src data: 'unsafe-inline'; img-src data:; font-src data:; media-src data:; "
        . "connect-src 'none'; worker-src 'none'; manifest-src 'none'; "
        . "base-uri 'none'; form-action 'none'; frame-ancestors 'self'";

    public static function headers(string $type, string $csp = self::CSP): void
    {
        header('Content-Type: ' . $type);
        header('Cache-Control: no-store');
        header('Content-Security-Policy: ' . $csp);
        header('X-Content-Type-Options: nosniff');
        header('Referrer-Policy: no-referrer');
        header('Cross-Origin-Opener-Policy: same-origin');
        header('Cross-Origin-Resource-Policy: same-origin');
        header('Permissions-Policy: publickey-credentials-get=(self), camera=(), microphone=(), geolocation=()');
        header('Strict-Transport-Security: max-age=63072000; includeSubDomains');
        header_remove('X-Powered-By');
    }

    public static function render(string $template, array $vars, string $csp = self::CSP): void
    {
        self::headers('text/html; charset=utf-8', $csp);
        extract($vars, EXTR_SKIP);
        require $template;
    }

    // frame sends an app window: a fixed document that holds nothing but the
    // bootstrap of shell/frame-boot.js, which waits for the shell to hand it
    // the app's proven user interface.
    //
    // The bootstrap goes in as a data: address, because FRAME_CSP allows
    // scripts from data: and nothing else - the same one rule the app's own
    // scripts follow, rather than a second one for NGC's own line of code.
    // It is read from the file every time so that the script that runs is the
    // file in the repository and cannot drift from it; the file is a few
    // hundred bytes and a window is opened by hand, not in a loop.
    public static function frame(string $bootFile): void
    {
        $js = @file_get_contents($bootFile);
        // No bootstrap, no window. An empty frame would look like an app that
        // hangs, and there is nothing the person could do about it either way.
        if (!is_string($js) || $js === '') {
            self::fail(500, Lang::t('page.notsetup'));
            return;
        }
        self::render(__DIR__ . '/../templates/frame.php', [
            'boot' => 'data:text/javascript;base64,' . base64_encode($js),
        ], self::FRAME_CSP);
    }

    // send writes a body that is already complete. Headers go out last, so a
    // failure while the body is being built can still turn into an error
    // page instead of a 200 with nothing behind it.
    public static function send(string $type, string $body): void
    {
        self::headers($type);
        echo $body;
    }

    public static function fail(int $status, string $text): void
    {
        http_response_code($status);
        self::headers('text/html; charset=utf-8');
        $lang = htmlspecialchars(Lang::tag(), ENT_QUOTES | ENT_SUBSTITUTE, 'UTF-8');
        echo "<!DOCTYPE html>\n<html lang=\"$lang\"><head><meta charset=\"utf-8\"><title>NGC</title></head>"
            . '<body><p>' . htmlspecialchars($text, ENT_QUOTES | ENT_SUBSTITUTE, 'UTF-8') . '</p></body></html>';
    }
}
