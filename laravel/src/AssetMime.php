<?php

namespace Devdojo\Framework;

/**
 * AssetMime resolves the Content-Type for a raw file served out of a site's
 * public/ directory. It is a package-local port of the approach devdojo/sites
 * uses (App\Services\Sites\UrlRewriter::assetMimeType) — kept package-local so
 * this package carries no dependency on the platform.
 *
 * Active document types (html/htm/xhtml/shtml) are forced to text/plain so a
 * stray .html file dropped in public/ can't be served as an active page from
 * the app's origin; page HTML always comes from src/pages instead.
 * Unknown extensions fall back to text/plain (never an executable type).
 */
class AssetMime
{
    public static function for(string $path): string
    {
        $ext = strtolower(pathinfo($path, PATHINFO_EXTENSION));

        if (in_array($ext, ['html', 'htm', 'xhtml', 'shtml'], true)) {
            return 'text/plain; charset=UTF-8';
        }

        return match ($ext) {
            'css' => 'text/css; charset=UTF-8',
            'js', 'mjs' => 'application/javascript; charset=UTF-8',
            'json' => 'application/json; charset=UTF-8',
            'svg' => 'image/svg+xml',
            'xml' => 'application/xml',
            'txt', 'md' => 'text/plain; charset=UTF-8',
            'ico' => 'image/x-icon',
            'png' => 'image/png',
            'jpg', 'jpeg' => 'image/jpeg',
            'gif' => 'image/gif',
            'webp' => 'image/webp',
            'avif' => 'image/avif',
            'woff' => 'font/woff',
            'woff2' => 'font/woff2',
            'ttf' => 'font/ttf',
            'otf' => 'font/otf',
            'pdf' => 'application/pdf',
            default => 'text/plain; charset=UTF-8',
        };
    }
}
