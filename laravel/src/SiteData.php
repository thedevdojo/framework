<?php

namespace Devdojo\Pocketknife;

use Illuminate\Contracts\View\Factory;
use League\CommonMark\Environment\Environment;
use League\CommonMark\Extension\CommonMark\CommonMarkCoreExtension;
use League\CommonMark\Extension\Table\TableExtension;
use League\CommonMark\MarkdownConverter;

/**
 * SiteData binds the site's data contract (spec §3.5) into the view factory so
 * pages, layouts, and components resolve the SAME variables the Go engine's
 * root-scope assembly produces (blade/render/data.go):
 *
 *  - $site         — resources/data/site.json as an object, shared GLOBALLY. Real
 *                    Blade's View::share leaks shared data into anonymous
 *                    components, and the Go engine mirrors that (components see
 *                    $site). Per-page data still overrides it via gatherData.
 *  - $<collection> — each resources/data/collections/<name>.json (a JSON array),
 *                    shared GLOBALLY too since 2026-07-28 (spec
 *                    docs/specs/2026-07-28-collections-global-scope-design.md):
 *                    pages, layouts, and components all read them.
 *  - $<dir>        — each resources/data/content/<dir>/*.md, rendered to items
 *                    (frontmatter fields + `content` HTML + `link`), also global.
 *
 * The frontmatter grammar and markdown configuration are kept byte-identical to
 * the Go tier: scalars only (strings/ints/floats/true/false), goldmark/commonmark
 * both in GFM-table + raw-HTML-passthrough mode. See the parity log in the task
 * report; the one known divergence (hard line breaks: <br> vs <br />) is kept out
 * of authored content.
 */
class SiteData
{
    /**
     * Bind the three data sources under $root into $factory as SHARED data, so
     * pages, layouts, and components resolve them alike.
     *
     * Order matters: collections and content share FIRST and `site` LAST, so a
     * collection literally named `site` can never displace the site object.
     * Per-view data still outranks all shared data (Factory::gatherData merges
     * shared then view data), which is exactly the page-scope collision rule —
     * the protocol's `data` map (a dynamic page's matched entry, say) shadows a
     * same-named collection in the PAGE, while a component reading that name
     * still sees the full collection. The Go engine mirrors this split with its
     * `bindings` (page) / `shared` (global) pair.
     */
    public static function share(Factory $factory, string $root): void
    {
        $bindings = array_merge(self::loadCollections($root), self::loadContent($root));

        foreach ($bindings as $key => $value) {
            $factory->share($key, $value);
        }

        $factory->share('site', self::loadSite($root));
    }

    /**
     * Load site.json as an object. A missing file, malformed JSON, or a
     * non-object (e.g. a top-level array) yields an empty object — the same
     * empty-$site the Go engine binds.
     */
    private static function loadSite(string $root): object
    {
        $path = $root.'/resources/data/site.json';
        if (! is_file($path)) {
            return new \stdClass;
        }
        $decoded = json_decode((string) file_get_contents($path));

        return is_object($decoded) ? $decoded : new \stdClass;
    }

    /**
     * Load every collections/<name>.json into a $<name> array. The basename must
     * be a valid variable name and the file must decode to a JSON array; either
     * failure skips the binding (the Go engine additionally emits a diagnostic).
     *
     * @return array<string, array<int, mixed>>
     */
    private static function loadCollections(string $root): array
    {
        $out = [];
        $dir = $root.'/resources/data/collections';
        if (! is_dir($dir)) {
            return $out;
        }
        foreach (self::sortedFiles($dir, 'json') as $path) {
            $name = basename($path, '.json');
            if (! self::isValidVarName($name)) {
                continue;
            }
            $decoded = json_decode((string) file_get_contents($path));
            if (! is_array($decoded)) {
                continue;
            }
            $out[$name] = $decoded;
        }

        return $out;
    }

    /**
     * Load every content/<dir>/*.md into a $<dir> array of item objects, ordered
     * by filename ascending. Each item carries its scalar frontmatter fields plus
     * `content` (rendered HTML) and `link` (route). A file with non-scalar
     * frontmatter is skipped (the Go engine also emits a diagnostic).
     *
     * @return array<string, array<int, object>>
     */
    private static function loadContent(string $root): array
    {
        $out = [];
        $base = $root.'/resources/data/content';
        if (! is_dir($base)) {
            return $out;
        }
        foreach (self::sortedDirs($base) as $dir) {
            $name = basename($dir);
            if (! self::isValidVarName($name)) {
                continue;
            }
            $files = self::sortedFiles($dir, 'md');
            if ($files === []) {
                // No direct .md files → leave $name unbound, matching the Go
                // engine (groupContentFiles only registers a directory that has
                // at least one markdown file). A directory whose files all fail
                // frontmatter parsing still binds an empty array on both sides.
                continue;
            }
            $items = [];
            foreach ($files as $path) {
                $parsed = self::parseFrontmatter((string) file_get_contents($path));
                if ($parsed === null) {
                    continue; // non-scalar frontmatter → item skipped
                }
                $fields = $parsed['fields'];
                $fields['content'] = (string) self::converter()->convert($parsed['body']);
                $fields['link'] = '/'.$name.'/'.basename($path, '.md');
                $items[] = (object) $fields;
            }
            $out[$name] = $items;
        }

        return $out;
    }

    /**
     * Split an optional leading `---` fence from a markdown source and parse its
     * scalar `key: value` lines. A source without a fence has no frontmatter (the
     * whole input is the body). Returns null when a value is non-scalar (an
     * indented child, a flow `[...]`/`{...}`, or a bare `key:`), matching the Go
     * ErrNested contract.
     *
     * @return array{fields: array<string, mixed>, body: string}|null
     */
    private static function parseFrontmatter(string $src): ?array
    {
        if (str_starts_with($src, "\xEF\xBB\xBF")) {
            $src = substr($src, 3);
        }

        if (str_starts_with($src, "---\n")) {
            $rest = substr($src, 4);
        } elseif (str_starts_with($src, "---\r\n")) {
            $rest = substr($src, 5);
        } else {
            return ['fields' => [], 'body' => $src];
        }

        $lines = explode("\n", $rest);
        $block = [];
        $body = null;
        foreach ($lines as $i => $line) {
            if (rtrim($line, "\r") === '---') {
                $body = implode("\n", array_slice($lines, $i + 1));
                break;
            }
            $block[] = $line;
        }
        if ($body === null) {
            return ['fields' => [], 'body' => $src]; // unterminated fence
        }

        $fields = [];
        foreach ($block as $line) {
            if (trim($line) === '') {
                continue;
            }
            if ($line[0] === ' ' || $line[0] === "\t") {
                return null; // indented child → nested
            }
            $pos = strpos($line, ':');
            if ($pos === false) {
                return null;
            }
            $rawVal = trim(substr($line, $pos + 1));
            if ($rawVal === '' || $rawVal[0] === '[' || $rawVal[0] === '{') {
                return null; // bare key or flow collection → nested
            }
            $fields[trim(substr($line, 0, $pos))] = self::parseScalar($rawVal);
        }

        return ['fields' => $fields, 'body' => $body];
    }

    /**
     * Coerce a trimmed frontmatter value to its scalar type, matching the Go
     * parser: quoted strings unquote, true/false become bool, decimal integers
     * and -?\d+\.\d+ floats become numbers, everything else stays a bare string.
     */
    private static function parseScalar(string $v): string|int|float|bool
    {
        $len = strlen($v);
        if ($len >= 2) {
            $first = $v[0];
            $last = $v[$len - 1];
            if (($first === "'" && $last === "'") || ($first === '"' && $last === '"')) {
                return substr($v, 1, $len - 2);
            }
        }
        if ($v === 'true') {
            return true;
        }
        if ($v === 'false') {
            return false;
        }
        if (preg_match('/^-?\d+$/', $v)) {
            return (int) $v;
        }
        if (preg_match('/^-?\d+\.\d+$/', $v)) {
            return (float) $v;
        }

        return $v;
    }

    /**
     * The shared markdown converter: CommonMark core + GFM tables, with raw HTML
     * and unsafe links allowed — the exact goldmark configuration the Go tier
     * uses (extension.Table + html.WithUnsafe).
     */
    private static function converter(): MarkdownConverter
    {
        static $converter = null;
        if ($converter === null) {
            $environment = new Environment([
                'html_input' => 'allow',
                'allow_unsafe_links' => true,
            ]);
            $environment->addExtension(new CommonMarkCoreExtension);
            $environment->addExtension(new TableExtension);
            $converter = new MarkdownConverter($environment);
        }

        return $converter;
    }

    private static function isValidVarName(string $name): bool
    {
        return preg_match('/^[a-zA-Z_][a-zA-Z0-9_]*$/', $name) === 1;
    }

    /**
     * Files with the given extension directly under $dir, sorted by name.
     *
     * @return array<int, string>
     */
    private static function sortedFiles(string $dir, string $ext): array
    {
        $files = glob($dir.'/*.'.$ext) ?: [];
        sort($files);

        return $files;
    }

    /**
     * Immediate subdirectories of $dir, sorted by name.
     *
     * @return array<int, string>
     */
    private static function sortedDirs(string $dir): array
    {
        $dirs = array_filter(glob($dir.'/*') ?: [], 'is_dir');
        sort($dirs);

        return $dirs;
    }
}
