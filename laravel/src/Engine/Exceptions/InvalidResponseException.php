<?php

namespace Devdojo\Framework\Engine\Exceptions;

/**
 * The binary exited cleanly but its stdout was not the parseable JSON
 * document the protocol promises.
 */
class InvalidResponseException extends FrameworkEngineException {}
