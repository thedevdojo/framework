<?php

namespace Devdojo\Pocketknife\Engine\Exceptions;

/**
 * The binary reported an engine version below the pinned minimum — the host
 * refuses it rather than risk a protocol the client was not written against.
 */
class EngineVersionException extends PocketknifeEngineException {}
