<?php

namespace Devdojo\Framework\Tests;

use Devdojo\Framework\Engine\CompileRequest;
use Devdojo\Framework\Engine\CompileResult;
use Devdojo\Framework\Engine\Diagnostic;
use PHPUnit\Framework\TestCase;

/**
 * Pure protocol-shape coverage — no binary required, so these run everywhere.
 */
class RequestResultTest extends TestCase
{
    public function test_request_json_carries_only_files_when_data_and_entries_are_empty(): void
    {
        $json = (new CompileRequest([
            'resources/views/pages/index.blade.php' => '<p>hi</p>',
        ]))->toJson();

        $decoded = json_decode($json, true);

        $this->assertSame(['files'], array_keys($decoded));
        $this->assertArrayNotHasKey('data', $decoded);
        $this->assertArrayNotHasKey('entries', $decoded);
    }

    public function test_request_json_includes_data_and_entries_when_present(): void
    {
        $json = (new CompileRequest(
            files: ['resources/views/pages/index.blade.php' => '<p>{{ $site->title }}</p>'],
            data: ['site' => ['title' => 'Hi']],
            entries: ['resources/views/pages/index.blade.php'],
        ))->toJson();

        $decoded = json_decode($json, true);

        $this->assertSame(['title' => 'Hi'], $decoded['data']['site']);
        $this->assertSame(['resources/views/pages/index.blade.php'], $decoded['entries']);
    }

    public function test_request_json_is_slash_and_unicode_safe(): void
    {
        // Template content routinely holds </tags> and non-ASCII — the wire
        // form must keep them literal, not escape them to \/ or \uXXXX.
        $json = (new CompileRequest([
            'resources/views/pages/index.blade.php' => '<a href="https://devdojo.com/café">déjà</a>',
        ]))->toJson();

        $this->assertStringContainsString('https://devdojo.com/café', $json);
        $this->assertStringNotContainsString('\\/', $json);
        $this->assertStringNotContainsString('\\u', $json);
    }

    public function test_request_json_keeps_html_markup_literal(): void
    {
        // json_encode must NOT HTML-escape template markup: <, >, & pass
        // through verbatim so what the engine receives is what was authored.
        $json = (new CompileRequest([
            'resources/views/pages/index.blade.php' => '<div class="a & b">{!! $raw !!}</div>',
        ]))->toJson();

        $this->assertStringContainsString('<div class=\"a & b\">', $json);
        $this->assertStringNotContainsString('\\u003C', $json);
        $this->assertStringNotContainsString('\\u0026', $json);
    }

    public function test_empty_files_serialize_as_a_json_object_not_an_array(): void
    {
        $json = (new CompileRequest([]))->toJson();

        $this->assertStringContainsString('"files":{}', $json);
    }

    public function test_result_from_array_maps_files_deps_and_diagnostics(): void
    {
        $result = CompileResult::fromArray([
            'engine' => '0.1.0',
            'files' => ['index.html' => '<h1>Hi</h1>'],
            'deps' => ['index.html' => ['resources/views/pages/index.blade.php']],
            'diagnostics' => [
                ['file' => 'a.blade.php', 'line' => 2, 'col' => 3, 'kind' => 'error', 'construct' => 'undefined-variable', 'message' => 'undefined variable $x'],
                ['file' => 'b.blade.php', 'line' => 1, 'col' => 1, 'kind' => 'app-tier', 'construct' => '@auth', 'message' => 'requires the app tier'],
            ],
        ]);

        $this->assertSame('0.1.0', $result->engine);
        $this->assertSame('<h1>Hi</h1>', $result->file('index.html'));
        $this->assertNull($result->file('missing.html'));
        $this->assertSame(['resources/views/pages/index.blade.php'], $result->deps['index.html']);

        $this->assertCount(1, $result->errors());
        $this->assertCount(1, $result->appTier());
        $this->assertCount(1, $result->diagnosticsFor('a.blade.php'));
        $this->assertSame([], $result->diagnosticsFor('nope.blade.php'));
    }

    public function test_diagnostic_construct_is_nullable_when_omitted(): void
    {
        $diagnostic = Diagnostic::fromArray([
            'file' => 'a.blade.php',
            'line' => 4,
            'col' => 7,
            'kind' => 'error',
            'message' => 'malformed input JSON',
        ]);

        $this->assertNull($diagnostic->construct);
        $this->assertTrue($diagnostic->isError());
        $this->assertFalse($diagnostic->isAppTier());
    }

    public function test_diagnostic_stringifies_to_the_lint_format(): void
    {
        $diagnostic = Diagnostic::fromArray([
            'file' => 'a.blade.php',
            'line' => 4,
            'col' => 7,
            'kind' => 'error',
            'construct' => 'undefined-variable',
            'message' => 'undefined variable $x',
        ]);

        $this->assertSame(
            'a.blade.php:4:7 [error] undefined variable $x',
            (string) $diagnostic
        );
    }

    public function test_result_tolerates_a_missing_diagnostics_key(): void
    {
        $result = CompileResult::fromArray([
            'engine' => '0.1.0',
            'files' => [],
            'deps' => [],
        ]);

        $this->assertSame([], $result->diagnostics);
        $this->assertSame([], $result->errors());
    }
}
