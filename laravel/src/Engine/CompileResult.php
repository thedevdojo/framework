<?php

namespace Devdojo\Framework\Engine;

/**
 * One `devdojo compile` response. `files` and `deps` are keyed by OUTPUT
 * path (`pages/index.blade.php` renders to `index.html`) — not by the entry
 * that produced them. Template problems land on `diagnostics`; the process
 * exiting cleanly does not mean the site compiled without diagnostics.
 *
 * `notices` are apart from both: things worth telling the site's author (it is
 * still in an older folder shape, say) that are never a reason to fail a build.
 * An engine older than 1.1.0 sends none.
 */
final class CompileResult
{
    /**
     * @param  string  $engine  version reported by the binary (e.g. "0.1.0")
     * @param  array<string, string>  $files  output path => rendered contents
     * @param  array<string, list<string>>  $deps  output path => every source path read to render it
     * @param  list<Diagnostic>  $diagnostics
     * @param  list<Notice>  $notices
     */
    public function __construct(
        public readonly string $engine,
        public readonly array $files,
        public readonly array $deps,
        public readonly array $diagnostics,
        public readonly array $notices = [],
    ) {}

    public static function fromArray(array $payload): self
    {
        return new self(
            engine: (string) ($payload['engine'] ?? ''),
            files: array_map('strval', $payload['files'] ?? []),
            deps: array_map(
                fn ($paths) => array_values(array_map('strval', (array) $paths)),
                $payload['deps'] ?? []
            ),
            diagnostics: array_values(array_map(
                fn (array $diagnostic) => Diagnostic::fromArray($diagnostic),
                array_filter($payload['diagnostics'] ?? [], 'is_array')
            )),
            notices: array_values(array_map(
                fn (array $notice) => Notice::fromArray($notice),
                array_filter($payload['notices'] ?? [], 'is_array')
            )),
        );
    }

    public function file(string $name): ?string
    {
        return $this->files[$name] ?? null;
    }

    /**
     * @return list<Diagnostic>
     */
    public function diagnosticsFor(string $file): array
    {
        return array_values(array_filter(
            $this->diagnostics,
            fn (Diagnostic $diagnostic) => $diagnostic->file === $file
        ));
    }

    /**
     * @return list<Diagnostic>
     */
    public function errors(): array
    {
        return array_values(array_filter(
            $this->diagnostics,
            fn (Diagnostic $diagnostic) => $diagnostic->isError()
        ));
    }

    /**
     * @return list<Diagnostic>
     */
    public function appTier(): array
    {
        return array_values(array_filter(
            $this->diagnostics,
            fn (Diagnostic $diagnostic) => $diagnostic->isAppTier()
        ));
    }
}
