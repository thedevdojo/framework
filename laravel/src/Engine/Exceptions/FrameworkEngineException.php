<?php

namespace Devdojo\Framework\Engine\Exceptions;

use RuntimeException;

/**
 * Base type for every process-level failure of the protocol client. Template
 * problems are never exceptions — they arrive as CompileResult diagnostics.
 * Catch this to handle any engine failure regardless of cause.
 */
class FrameworkEngineException extends RuntimeException {}
