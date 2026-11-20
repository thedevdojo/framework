<?php

namespace Devdojo\Framework\Engine\Exceptions;

/**
 * The configured binary path is empty, missing, or not executable — the
 * client never even started a process.
 */
class BinaryNotFoundException extends FrameworkEngineException {}
