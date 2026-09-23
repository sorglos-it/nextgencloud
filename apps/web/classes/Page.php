<?php
declare(strict_types=1);

namespace Ngc;

// Page sends the headers of the web interface and fills the template.
//
// The policy stays as tight as the page allows, because this is the document
// that will hold the session keys. It carries no inline script and no inline
// style, so nothing loosens script-src or style-src. frame-src names no data:
// either: where app windows come from is still open (Task 21), and that
// decision is to be taken openly rather than to stand here already. Whatever
// a frame then needs goes on the csp attribute of that frame.
//
// data: stays for images, fonts and media: an embedded icon or font is the
// normal way, and no code runs from there.
final class Page
{
    public const CSP = "default-src 'none'; script-src 'self'; style-src 'self'; "
        . "img-src 'self' data:; font-src 'self' data:; media-src data:; connect-src 'self'; "
        . "frame-src 'self'; worker-src 'self'; manifest-src 'none'; "
        . "base-uri 'none'; form-action 'none'; frame-ancestors 'none'";

    public static function headers(string $type): void
    {
        header('Content-Type: ' . $type);
        header('Cache-Control: no-store');
        header('Content-Security-Policy: ' . self::CSP);
        header('X-Content-Type-Options: nosniff');
        header('Referrer-Policy: no-referrer');
        header('Cross-Origin-Opener-Policy: same-origin');
        header('Cross-Origin-Resource-Policy: same-origin');
        header('Permissions-Policy: publickey-credentials-get=(self), camera=(), microphone=(), geolocation=()');
        header('Strict-Transport-Security: max-age=63072000; includeSubDomains');
        header_remove('X-Powered-By');
    }

    public static function render(string $template, array $vars): void
    {
        self::headers('text/html; charset=utf-8');
        extract($vars, EXTR_SKIP);
        require $template;
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
        echo "<!DOCTYPE html>\n<html lang=\"de\"><head><meta charset=\"utf-8\"><title>NGC</title></head>"
            . '<body><p>' . htmlspecialchars($text, ENT_QUOTES | ENT_SUBSTITUTE, 'UTF-8') . '</p></body></html>';
    }
}
