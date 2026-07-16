<?php

namespace Tests\Feature;

use PHPUnit\Framework\Attributes\DataProvider;

/**
 * HTTP byte-parity: for every oracle-checked conformance fixture, point the
 * package at the fixture's input tree, enable serving, GET each page's URL
 * through Laravel's real HTTP stack, and assert the response body is
 * byte-identical to the fixture's expected/ output — the same bytes the Go
 * engine and real Blade (pocketknife:conformance) already agree on. Three-way
 * agreement: engine == Blade-render == Blade-over-HTTP.
 *
 * Fixture selection mirrors pocketknife:conformance exactly, minus one class:
 *   - oracle:false fixtures are skipped (same as the render conformance).
 *   - fixtures with a case.json "data" override are skipped: that data is the
 *     protocol's per-render data map, injected by the render harness's --data
 *     flag. HTTP serving has no equivalent (SiteData binds only the site's own
 *     file-sourced $site / collections / content — never an ad-hoc data map),
 *     so those pages reference variables that simply don't exist over HTTP.
 *     They stay covered by pocketknife:conformance; serving covers the rest.
 */
class AppTierServingTest extends ServingTestCase
{
    #[DataProvider('oracleFixtures')]
    public function test_pages_serve_byte_identical(string $dir, array $case): void
    {
        $this->serveFrom($dir.'/input');

        $entries = $case['entries'];
        $checked = 0;

        foreach ($entries as $entry) {
            $pageRel = preg_replace('#^resources/views/pages/(.+)\.blade\.html$#', '$1', $entry);

            // A nested directory-index page shadowed by a flat sibling can never
            // be reached over HTTP (flat wins — the routing table). The flat
            // sibling's URL is asserted here; the flat-wins resolution itself is
            // pinned by ServingSemanticsTest::test_flat_page_wins_over_directory_index.
            if (str_ends_with($pageRel, '/index')) {
                $flat = substr($pageRel, 0, -strlen('/index'));
                if (in_array("resources/views/pages/$flat.blade.html", $entries, true)) {
                    continue;
                }
            }

            $url = self::urlFor($pageRel);
            $expected = file_get_contents($dir.'/expected/'.$pageRel.'.html');

            $response = $this->get($url);

            $response->assertOk();
            $this->assertSame(
                $expected,
                $response->getContent(),
                basename($dir)." :: GET $url must be byte-identical to expected/$pageRel.html",
            );
            $checked++;
        }

        $this->assertGreaterThan(0, $checked, basename($dir).' verified no URLs');
    }

    /** Inverse of the conformance output naming: page-relative path -> URL. */
    private static function urlFor(string $pageRel): string
    {
        if ($pageRel === 'index') {
            return '/';
        }
        if (str_ends_with($pageRel, '/index')) {
            return '/'.substr($pageRel, 0, -strlen('/index'));
        }

        return '/'.$pageRel;
    }

    /**
     * Oracle-checked fixtures with no per-render data override. Mirrors
     * pocketknife:conformance's selection (oracle !== false) minus data cases.
     *
     * @return array<string, array{0: string, 1: array}>
     */
    public static function oracleFixtures(): array
    {
        $fixturesDir = dirname(__DIR__, 3).'/fixtures';

        $out = [];
        foreach (glob($fixturesDir.'/*', GLOB_ONLYDIR) as $dir) {
            $caseFile = $dir.'/case.json';
            if (! is_file($caseFile)) {
                continue;
            }
            $case = json_decode(file_get_contents($caseFile), true);
            if (($case['oracle'] ?? true) === false) {
                continue; // render conformance skips these; so do we
            }
            if (! empty($case['data'])) {
                continue; // no HTTP equivalent for the protocol data map
            }
            $out[basename($dir)] = [$dir, $case];
        }

        return $out;
    }
}
