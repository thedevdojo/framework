<?php

namespace Tests\Feature;

use Devdojo\Framework\SiteServer;
use Illuminate\Support\Facades\Route;

/**
 * The serving-behaviour contract beyond byte-parity: routing precedence, 404
 * semantics, app-routes-win, GET-only, public asset serving (mime + traversal),
 * and the opt-in switch. The routing-table fixture is reused as a real site
 * tree; asset/edge trees are scaffolded per test.
 */
class ServingSemanticsTest extends ServingTestCase
{
    private function routingTableInput(): string
    {
        return dirname(__DIR__, 3).'/fixtures/routing-table/input';
    }

    // --- Routing precedence -------------------------------------------------

    public function test_flat_page_wins_over_directory_index(): void
    {
        $this->serveFrom($this->routingTableInput());

        $response = $this->get('/about');

        $response->assertOk();
        // pages/about.blade.php ("<h1>About</h1>"), NOT pages/about/index
        // ("<h1>About Index</h1>").
        $this->assertSame("<h1>About</h1>\n", $response->getContent());
    }

    // --- 404 semantics ------------------------------------------------------

    public function test_unresolved_path_renders_site_404_with_404_status(): void
    {
        $this->serveFrom($this->routingTableInput());

        $response = $this->get('/nope/definitely-missing');

        $response->assertNotFound(); // 404 status
        $this->assertSame("<h1>Not Found</h1>\n", $response->getContent());
    }

    public function test_falls_through_to_app_404_without_a_site_404_page(): void
    {
        // A tree with an index page but NO pages/404.blade.php.
        $root = $this->makeSite([
            'src/pages/index.blade.php' => '<h1>Home</h1>',
        ]);
        $this->serveFrom($root);

        $response = $this->get('/missing');

        $response->assertNotFound();
        // The site has no 404 page, so this is the app's own 404 — never a
        // served site page.
        $this->assertStringNotContainsString('<h1>Home</h1>', $response->getContent());
    }

    // --- App routes always win ---------------------------------------------

    public function test_app_route_wins_over_a_page_at_the_same_url(): void
    {
        $this->serveFrom($this->routingTableInput());

        // A real app route registered at a URL the site also has a page for.
        Route::get('/about', fn () => 'APP ROUTE WINS');

        $response = $this->get('/about');

        $response->assertOk();
        $this->assertSame('APP ROUTE WINS', $response->getContent());
    }

    // --- GET/HEAD only ------------------------------------------------------

    public function test_post_to_a_page_url_is_not_served(): void
    {
        $this->serveFrom($this->routingTableInput());

        $response = $this->post('/about');

        // The page-serving fallback is GET/HEAD only, so a POST never reaches
        // it: Laravel reports the URI exists for another verb (405), and the
        // body is never the rendered page.
        $response->assertStatus(405);
        $this->assertStringNotContainsString('<h1>About</h1>', $response->getContent());
    }

    public function test_head_request_serves_a_page(): void
    {
        $this->serveFrom($this->routingTableInput());

        $response = $this->head('/about');

        $response->assertOk();
    }

    // --- Public asset serving ----------------------------------------------

    public function test_public_assets_are_served_with_the_right_mime(): void
    {
        $root = $this->makeSite([
            'src/pages/index.blade.php' => '<h1>Home</h1>',
            'public/assets/app.css' => 'body{color:red}',
            'public/img/logo.png' => "\x89PNG\r\n\x1a\nFAKE",
            'public/data.json' => '{"ok":true}',
        ]);
        $this->serveFrom($root);

        $css = $this->get('/assets/app.css');
        $css->assertOk();
        $css->assertHeader('Content-Type', 'text/css; charset=UTF-8');
        $this->assertSame('body{color:red}', $css->getContent());

        $png = $this->get('/img/logo.png');
        $png->assertOk();
        $png->assertHeader('Content-Type', 'image/png');
        $this->assertSame("\x89PNG\r\n\x1a\nFAKE", $png->getContent());

        $json = $this->get('/data.json');
        $json->assertOk();
        $json->assertHeader('Content-Type', 'application/json; charset=UTF-8');
    }

    public function test_public_assets_win_over_a_same_named_page(): void
    {
        // A public/ file takes precedence over page resolution (spec: assets
        // are tried first so a bare tree is fully self-serving).
        $root = $this->makeSite([
            'src/pages/index.blade.php' => '<h1>Home</h1>',
            'src/pages/robots.txt.blade.php' => 'PAGE',
            'public/robots.txt' => "User-agent: *\nDisallow:",
        ]);
        $this->serveFrom($root);

        $response = $this->get('/robots.txt');

        $response->assertOk();
        $response->assertHeader('Content-Type', 'text/plain; charset=UTF-8');
        $this->assertSame("User-agent: *\nDisallow:", $response->getContent());
    }

    public function test_dotfiles_are_not_served(): void
    {
        $root = $this->makeSite([
            'src/pages/index.blade.php' => '<h1>Home</h1>',
            'public/.env' => 'SECRET=leak',
        ]);
        $this->serveFrom($root);

        $response = $this->get('/.env');

        // Dotfile refused -> no page, no site 404 -> app 404. Never the secret.
        $response->assertNotFound();
        $this->assertStringNotContainsString('SECRET=leak', $response->getContent());
    }

    public function test_a_symlink_to_a_dotfile_inside_public_is_not_served(): void
    {
        // Defense in depth: a symlink whose NAME is clean but whose target is a
        // sibling dotfile inside public/ must still be refused — the request-path
        // dotfile guard alone wouldn't catch it (realpath follows the link).
        $root = $this->makeSite([
            'src/pages/index.blade.php' => '<h1>Home</h1>',
            'public/.env' => 'SECRET=leak',
        ]);
        @symlink($root.'/public/.env', $root.'/public/config');
        $this->serveFrom($root);

        $response = $this->get('/config');

        $response->assertNotFound();
        $this->assertStringNotContainsString('SECRET=leak', $response->getContent());
    }

    public function test_directory_paths_are_not_listed(): void
    {
        $root = $this->makeSite([
            'src/pages/index.blade.php' => '<h1>Home</h1>',
            'public/assets/app.css' => 'body{color:red}',
        ]);
        $this->serveFrom($root);

        // /assets is a directory under public/ — no listing, and no page.
        $response = $this->get('/assets');

        $response->assertNotFound();
    }

    public function test_asset_traversal_is_refused(): void
    {
        $root = $this->makeSite([
            'src/pages/index.blade.php' => '<h1>Home</h1>',
            'public/assets/app.css' => 'body{color:red}',
            'secret.txt' => 'TOP SECRET',
        ]);
        $this->serveFrom($root);

        // A path that tries to climb out of public/ must never reach secret.txt.
        $response = $this->get('/assets/../../secret.txt');

        $this->assertStringNotContainsString('TOP SECRET', $response->getContent());
    }

    // --- Opt-in switch ------------------------------------------------------

    public function test_serve_false_registers_no_fallback(): void
    {
        // site_root is set, but serve is false: no route may be registered.
        $this->serveFrom($this->routingTableInput(), serve: false);

        $hasFallback = collect($this->app['router']->getRoutes()->getRoutes())
            ->contains(fn ($route) => $route->isFallback);
        $this->assertFalse($hasFallback, 'serve=false must register no fallback route');

        // And a page URL is not served — it is the app's own 404.
        $response = $this->get('/about');
        $response->assertNotFound();
        $this->assertStringNotContainsString('<h1>About</h1>', $response->getContent());
    }

    public function test_serve_true_registers_a_route_cache_safe_fallback(): void
    {
        $this->serveFrom($this->routingTableInput());

        $fallback = collect($this->app['router']->getRoutes()->getRoutes())
            ->first(fn ($route) => $route->isFallback);

        $this->assertNotNull($fallback, 'serve=true must register the serving fallback');
        // An invokable controller (not a Closure) so the fallback survives
        // `php artisan route:cache` in production.
        $this->assertStringContainsString(
            SiteServer::class,
            $fallback->getActionName(),
            'the fallback must be a serializable controller action, not a closure',
        );
    }
}
