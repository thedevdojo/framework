<?php

namespace Devdojo\Pocketknife\Tests;

use Illuminate\Filesystem\Filesystem;
use Illuminate\View\FileViewFinder;
use PHPUnit\Framework\TestCase;

/**
 * Pins the DECIDED view-path precedence: the host APP's views win over the
 * site's.
 *
 * PocketknifeServiceProvider::boot() registers the site root by APPENDING it
 * (View::addLocation), and Laravel's FileViewFinder resolves a view against its
 * locations in registration order — returning the FIRST hit. The app already
 * registered its own resources/views during framework boot, so a view the app
 * defines shadows a same-named site view. The devdojo/sites integration
 * direction requires exactly this (the host must be able to override a site
 * layout or shared component without editing the site tree), so append order is
 * correct as-is — the provider must not prepend. This test locks the finder
 * semantics the decision rests on. (Design spec §5.)
 */
class ViewPrecedenceTest extends TestCase
{
    public function test_app_views_shadow_site_views(): void
    {
        $base = sys_get_temp_dir().'/pk-view-precedence-'.uniqid();
        $appViews = "$base/app/resources/views";
        $siteViews = "$base/site/resources/views";
        mkdir($appViews, 0755, true);
        mkdir($siteViews, 0755, true);
        file_put_contents("$appViews/shared.blade.php", 'APP');
        file_put_contents("$siteViews/shared.blade.php", 'SITE');

        try {
            // The app registered its views first (framework boot); the provider
            // APPENDS the site root — mirror that order exactly.
            $finder = new FileViewFinder(new Filesystem, [$appViews]);
            $finder->addLocation($siteViews);

            // Assert on the resolved file's CONTENT, not its path string: on
            // macOS the finder returns the realpath (/private/var/…) while the
            // temp dir is reported as /var/… — the content is the OS-agnostic
            // proof of which location won.
            $this->assertSame(
                'APP',
                file_get_contents($finder->find('shared')),
                'the host app view must win over the site view of the same name'
            );
        } finally {
            @unlink("$appViews/shared.blade.php");
            @unlink("$siteViews/shared.blade.php");
            @rmdir($appViews);
            @rmdir($siteViews);
            @rmdir("$base/app/resources");
            @rmdir("$base/site/resources");
            @rmdir("$base/app");
            @rmdir("$base/site");
            @rmdir($base);
        }
    }
}
