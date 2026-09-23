<?php
declare(strict_types=1);

// Page sends the headers of the web interface and fills the template. The
// content security policy is the one the app frames inherit (Task 21), so it
// allows data: scripts and styles for them and nothing else from outside.
final class Page
{
    public const CSP = "default-src 'none'; script-src 'self' data:; style-src 'self' data: 'unsafe-inline'; "
        . "img-src 'self' data:; font-src 'self' data:; media-src data:; connect-src 'self'; "
        . "frame-src 'self' data:; worker-src 'self'; manifest-src 'none'; "
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

    public static function fail(int $status, string $text): void
    {
        http_response_code($status);
        self::headers('text/html; charset=utf-8');
        echo "<!DOCTYPE html>\n<html lang=\"de\"><head><meta charset=\"utf-8\"><title>NGC</title></head>"
            . '<body><p>' . htmlspecialchars($text, ENT_QUOTES | ENT_SUBSTITUTE, 'UTF-8') . '</p></body></html>';
    }
}
