<?php

/**
 * PHPUnit bootstrap. Loads the package autoloader and — because the Engine
 * tests drive the REAL `devdojo` binary over the JSON protocol — builds a
 * fresh binary from the Go source in this repo. If Go is unavailable the build
 * is skipped and the binary-backed tests skip themselves (see EngineTest),
 * so a PHP-only environment still runs the pure request/result unit tests.
 */

require __DIR__.'/../vendor/autoload.php';

$repoRoot = dirname(__DIR__, 2);
$binary = sys_get_temp_dir().'/devdojo-phpunit';

$goAvailable = false;
$buildOutput = '';

exec('command -v go 2>/dev/null', $probe, $probeCode);

if ($probeCode === 0) {
    $command = sprintf(
        'cd %s && go build -o %s ./cmd/devdojo 2>&1',
        escapeshellarg($repoRoot),
        escapeshellarg($binary)
    );

    exec($command, $lines, $buildCode);
    $buildOutput = implode("\n", $lines);
    $goAvailable = $buildCode === 0 && is_file($binary);
}

define('DEVDOJO_TEST_BINARY', $binary);
define('DEVDOJO_BINARY_AVAILABLE', $goAvailable);
define('DEVDOJO_BUILD_OUTPUT', $buildOutput);
