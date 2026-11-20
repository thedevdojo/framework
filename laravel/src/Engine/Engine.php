<?php

namespace Devdojo\Framework\Engine;

use Devdojo\Framework\Engine\Exceptions\BinaryFailedException;
use Devdojo\Framework\Engine\Exceptions\BinaryNotFoundException;
use Devdojo\Framework\Engine\Exceptions\EngineVersionException;
use Devdojo\Framework\Engine\Exceptions\InvalidResponseException;

/**
 * The DevDojo Framework protocol client — a thin, typed wrapper around the
 * `devdojo` Go binary.
 *
 * The binary is the single source of truth for the static-tier Blade dialect.
 * Hosts whose site files live in a database or object store talk to it through
 * `devdojo compile`: one JSON document on stdin, one on stdout. This class
 * is the only place that protocol is spoken from PHP — consumers deal in
 * CompileRequest / CompileResult objects.
 *
 * Template problems (undefined variables, missing components, app-tier
 * constructs) are DATA: they come back on the result's `diagnostics` list, and
 * the compile still returns rendered output. Only process-level failures — a
 * missing binary, a non-zero exit, a timeout, malformed JSON, or a version
 * below the pinned minimum — throw.
 */
final class Engine
{
    public function __construct(
        private readonly string $binary,
        private readonly int $timeout = 30,
        private readonly string $minimumVersion = '0.1.0',
    ) {}

    public static function fromConfig(): self
    {
        return new self(
            (string) config('devdojo-framework.binary'),
            (int) config('devdojo-framework.timeout', 30),
            (string) config('devdojo-framework.minimum_version', '0.1.0'),
        );
    }

    /**
     * Whether a usable engine binary is configured on this host. Cheap enough
     * to call per request; does not spawn a process.
     */
    public function isAvailable(): bool
    {
        return $this->binary !== ''
            && is_file($this->binary)
            && is_executable($this->binary);
    }

    /**
     * Compile a site through `devdojo compile`.
     *
     * @throws BinaryNotFoundException|BinaryFailedException|InvalidResponseException|EngineVersionException
     */
    public function compile(CompileRequest $request): CompileResult
    {
        $stdout = $this->run(['compile'], $request->toJson());

        $decoded = json_decode($stdout, true);

        if (! is_array($decoded)) {
            throw new InvalidResponseException(
                'The devdojo binary returned malformed JSON: '.substr($stdout, 0, 200)
            );
        }

        $result = CompileResult::fromArray($decoded);

        $this->assertVersion($result->engine);

        return $result;
    }

    /**
     * @throws EngineVersionException
     */
    private function assertVersion(string $reported): void
    {
        if ($this->minimumVersion === '') {
            return;
        }

        if (version_compare($reported, $this->minimumVersion, '<')) {
            throw new EngineVersionException(
                "The devdojo binary [{$this->binary}] reports version {$reported}, below the required minimum {$this->minimumVersion}."
            );
        }
    }

    /**
     * Spawn the binary and exchange stdin/stdout, interleaving the write and
     * both reads through stream_select so a large payload can never deadlock on
     * a full pipe buffer (write-all-then-read is the classic bug this avoids).
     *
     * @param  list<string>  $arguments
     *
     * @throws BinaryNotFoundException|BinaryFailedException
     */
    private function run(array $arguments, string $stdin): string
    {
        if (! $this->isAvailable()) {
            throw new BinaryNotFoundException(
                $this->binary === ''
                    ? 'No devdojo binary is configured (set DEVDOJO_BINARY).'
                    : "The devdojo binary [{$this->binary}] does not exist or is not executable."
            );
        }

        $process = proc_open(
            [$this->binary, ...$arguments],
            [0 => ['pipe', 'r'], 1 => ['pipe', 'w'], 2 => ['pipe', 'w']],
            $pipes
        );

        if (! is_resource($process)) {
            throw new BinaryFailedException("Could not start the devdojo binary [{$this->binary}].");
        }

        foreach ($pipes as $pipe) {
            stream_set_blocking($pipe, false);
        }

        $stdout = '';
        $stderr = '';
        $written = 0;
        $length = strlen($stdin);
        $deadline = microtime(true) + $this->timeout;

        try {
            while (true) {
                $remaining = $deadline - microtime(true);

                if ($remaining <= 0) {
                    proc_terminate($process, 9);

                    throw new BinaryFailedException(
                        "The devdojo binary timed out after {$this->timeout}s."
                    );
                }

                $read = array_values(array_filter([$pipes[1] ?? null, $pipes[2] ?? null], 'is_resource'));
                $write = isset($pipes[0]) && is_resource($pipes[0]) ? [$pipes[0]] : [];
                $except = null;

                if ($read === [] && $write === []) {
                    break;
                }

                $ready = @stream_select($read, $write, $except, (int) $remaining, (int) (fmod($remaining, 1) * 1_000_000));

                if ($ready === false) {
                    break;
                }

                foreach ($write as $pipe) {
                    $chunk = substr($stdin, $written, 65536);

                    if ($chunk === '') {
                        fclose($pipes[0]);
                        unset($pipes[0]);

                        continue;
                    }

                    // @: a binary that exits before reading its stdin (e.g. a
                    // failing one) leaves a broken pipe; the false return is
                    // handled below, the warning is noise.
                    $bytes = @fwrite($pipe, $chunk);

                    if ($bytes === false) {
                        fclose($pipes[0]);
                        unset($pipes[0]);

                        continue;
                    }

                    $written += $bytes;

                    if ($written >= $length) {
                        fclose($pipes[0]);
                        unset($pipes[0]);
                    }
                }

                foreach ($read as $pipe) {
                    $chunk = fread($pipe, 65536);

                    if ($chunk === false || $chunk === '') {
                        if (feof($pipe)) {
                            $key = $pipe === ($pipes[1] ?? null) ? 1 : 2;
                            fclose($pipe);
                            unset($pipes[$key]);
                        }

                        continue;
                    }

                    if ($pipe === ($pipes[1] ?? null)) {
                        $stdout .= $chunk;
                    } else {
                        $stderr .= $chunk;
                    }
                }
            }
        } finally {
            foreach ($pipes as $pipe) {
                if (is_resource($pipe)) {
                    fclose($pipe);
                }
            }

            $exitCode = proc_close($process);
        }

        if ($exitCode !== 0) {
            throw new BinaryFailedException(
                "The devdojo binary exited with code {$exitCode}: ".trim($stderr !== '' ? $stderr : $stdout)
            );
        }

        return $stdout;
    }
}
