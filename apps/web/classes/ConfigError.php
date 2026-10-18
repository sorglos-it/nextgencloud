<?php
declare(strict_types=1);

namespace Ngc;

use RuntimeException;

// ConfigError is the one failure of Config whose text may be shown. It names
// a field of the configuration and nothing else: no path, no value, no
// reason that would tell a stranger how the server is laid out. Every other
// failure keeps the short, blank answer of index.php.
//
// It exists so that a typo in web.json says what is wrong. Otherwise the
// person setting NGC up reads "nicht eingerichtet" and has no idea which of
// the two files carries the mistake.
final class ConfigError extends RuntimeException
{
}
