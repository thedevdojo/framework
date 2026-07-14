<?php

namespace Devdojo\Pocketknife\Engine\Exceptions;

/**
 * The configured binary path is empty, missing, or not executable — the
 * client never even started a process.
 */
class BinaryNotFoundException extends PocketknifeEngineException {}
