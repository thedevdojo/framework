<?php

namespace Devdojo\Framework\Tests;

use Devdojo\Framework\Engine\CompileRequest;
use Devdojo\Framework\Engine\Engine;
use Devdojo\Framework\Engine\Exceptions\BinaryFailedException;
use Devdojo\Framework\Engine\Exceptions\BinaryNotFoundException;
use PHPUnit\Framework\TestCase;

/**
 * The Engine's failure paths — a missing/empty binary path, and a binary that
 * exists but exits non-zero. None of these needs the real `devdojo` binary:
 * the missing/empty cases never exec anything, and the failure cases run a tiny
 * self-written shell script. So, unlike EngineTest, they carry no skip gate and
 * run everywhere — including PHP-only CI without the Go toolchain.
 */
class EngineExceptionPathsTest extends TestCase
{
    public function test_missing_binary_path_throws_binary_not_found(): void
    {
        $engine = new Engine('/does/not/exist/devdojo', 30, '0.1.0');

        $this->assertFalse($engine->isAvailable());
        $this->expectException(BinaryNotFoundException::class);
        $engine->compile(new CompileRequest([
            'resources/views/pages/index.blade.php' => '<p>hi</p>',
        ]));
    }

    public function test_empty_binary_path_throws_binary_not_found(): void
    {
        $engine = new Engine('', 30, '0.1.0');

        $this->assertFalse($engine->isAvailable());
        $this->expectException(BinaryNotFoundException::class);
        $engine->compile(new CompileRequest([
            'resources/views/pages/index.blade.php' => '<p>hi</p>',
        ]));
    }

    public function test_garbage_binary_throws_binary_failed(): void
    {
        // A binary that exists and is executable (so it passes isAvailable())
        // but exits non-zero with no JSON on stdout. Written into the temp dir
        // rather than hardcoding /bin/false, whose path differs across OSes.
        $garbage = sys_get_temp_dir().'/devdojo-garbage-'.getmypid();
        file_put_contents($garbage, "#!/bin/sh\nexit 1\n");
        chmod($garbage, 0755);

        try {
            $engine = new Engine($garbage, 30, '0.1.0');

            $this->assertTrue($engine->isAvailable());
            $this->expectException(BinaryFailedException::class);
            $engine->compile(new CompileRequest([
                'resources/views/pages/index.blade.php' => '<p>hi</p>',
            ]));
        } finally {
            @unlink($garbage);
        }
    }

    public function test_binary_failure_message_carries_stderr(): void
    {
        // A binary that writes to stderr then exits non-zero — the failure
        // message must surface that noise so the cause is diagnosable.
        $noisy = sys_get_temp_dir().'/devdojo-noisy-'.getmypid();
        file_put_contents($noisy, "#!/bin/sh\necho 'boom on stderr' >&2\nexit 3\n");
        chmod($noisy, 0755);

        try {
            $this->expectException(BinaryFailedException::class);
            $this->expectExceptionMessageMatches('/code 3.*boom on stderr/s');
            (new Engine($noisy, 30, '0.1.0'))->compile(new CompileRequest([
                'resources/views/pages/index.blade.php' => '<p>hi</p>',
            ]));
        } finally {
            @unlink($noisy);
        }
    }
}
