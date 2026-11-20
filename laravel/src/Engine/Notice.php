<?php

namespace Devdojo\Framework\Engine;

use Stringable;

/**
 * Something a site's author should know that is not a problem with the site:
 * it built, and it will keep building. The engine sends notices in their own
 * list, never among the diagnostics, so a host that fails on any diagnostic is
 * not failed by one.
 *
 * Today every notice is a `deprecation`. The codes are stable, for a host to
 * switch on:
 *
 *  - `resources-folder` — the site keeps its source in resources/, not src/
 *  - `views-folder`     — pages or components sit under views/
 *  - `folder-not-read`  — two folders hold files and the older one is ignored
 *  - `asset-path`       — a stylesheet is named in the folder the site does
 *                         not use (`file` is the view to change)
 */
final class Notice implements Stringable
{
    public function __construct(
        public readonly string $kind,
        public readonly string $code,
        public readonly ?string $file,
        public readonly string $message,
    ) {}

    public static function fromArray(array $notice): self
    {
        $file = (string) ($notice['file'] ?? '');

        return new self(
            kind: (string) ($notice['kind'] ?? ''),
            code: (string) ($notice['code'] ?? ''),
            file: $file === '' ? null : $file,
            message: (string) ($notice['message'] ?? ''),
        );
    }

    public function __toString(): string
    {
        return $this->message;
    }
}
