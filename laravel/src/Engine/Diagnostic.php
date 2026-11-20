<?php

namespace Devdojo\Framework\Engine;

use Stringable;

/**
 * One diagnostic from a compile, positioned at the source file/line/col that
 * produced it. Diagnostics are DATA — the client never throws for them.
 *
 * Two kinds (§3.7 of the design spec):
 *
 *  - `error`    — malformed input the engine cannot render (undefined
 *                 variables, unclosed directives, missing components).
 *  - `app-tier` — valid Blade that lives outside the static subset; the host
 *                 surfaces these as "this needs your site running as an app,"
 *                 with the offending `construct` named (`@auth`, `$attributes`).
 */
final class Diagnostic implements Stringable
{
    public function __construct(
        public readonly string $file,
        public readonly int $line,
        public readonly int $col,
        public readonly string $kind,
        public readonly ?string $construct,
        public readonly string $message,
    ) {}

    public static function fromArray(array $diagnostic): self
    {
        $construct = $diagnostic['construct'] ?? null;

        return new self(
            file: (string) ($diagnostic['file'] ?? ''),
            line: (int) ($diagnostic['line'] ?? 0),
            col: (int) ($diagnostic['col'] ?? 0),
            kind: (string) ($diagnostic['kind'] ?? ''),
            construct: $construct === null ? null : (string) $construct,
            message: (string) ($diagnostic['message'] ?? ''),
        );
    }

    public function isError(): bool
    {
        return $this->kind === 'error';
    }

    public function isAppTier(): bool
    {
        return $this->kind === 'app-tier';
    }

    /**
     * The engine's own lint format: `file:line:col [kind] message`.
     */
    public function __toString(): string
    {
        return "{$this->file}:{$this->line}:{$this->col} [{$this->kind}] {$this->message}";
    }
}
