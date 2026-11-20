<?php

namespace Devdojo\Framework\Engine\Exceptions;

/**
 * The binary started but the process failed: a non-zero exit code, or a
 * timeout that forced the client to kill it. The message carries the binary's
 * stderr (or stdout) so the cause is visible.
 */
class BinaryFailedException extends FrameworkEngineException {}
