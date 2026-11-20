<?php

namespace Devdojo\Framework\Engine;

use JsonSerializable;

/**
 * One `devdojo compile` request: the site's source files keyed by their
 * path in the site (`src/pages/…`, `src/data/…`; an older site's
 * `resources/views/pages/…` is read too),
 * optional global data merged over the site's own data sources, and an
 * optional explicit entry list (omitted → every page is rendered).
 *
 * The wire form is `{"files":{…}, "data":{…}?, "entries":[…]?}` — `data` and
 * `entries` are omitted entirely when empty, matching the protocol's optional
 * fields (§6 of the design spec).
 */
final class CompileRequest implements JsonSerializable
{
    /**
     * @param  array<string, string>  $files  Laravel-layout path => contents
     * @param  array<string, mixed>  $data  merged over $site/collections/content
     * @param  list<string>  $entries  page paths to render; empty = all pages
     */
    public function __construct(
        public readonly array $files,
        public readonly array $data = [],
        public readonly array $entries = [],
    ) {}

    /**
     * @return array<string, mixed>
     */
    public function jsonSerialize(): array
    {
        // Cast empty files to an object so it marshals as `{}`, never `[]`.
        $payload = ['files' => $this->files === [] ? (object) [] : $this->files];

        if ($this->data !== []) {
            $payload['data'] = $this->data;
        }

        if ($this->entries !== []) {
            $payload['entries'] = array_values($this->entries);
        }

        return $payload;
    }

    public function toJson(): string
    {
        // UNESCAPED_SLASHES/UNICODE keep template content byte-faithful on the
        // wire — no `\/` in </tags> or `\uXXXX` for non-ASCII copy.
        return json_encode($this, JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE);
    }
}
