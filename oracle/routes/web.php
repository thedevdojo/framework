<?php

// The oracle app defines no web routes of its own. When pocketknife.serve is
// on, the package's Route::fallback serves the configured site tree (pages +
// public/ assets); every request therefore flows through that fallback, which
// is exactly what the app-tier serving conformance suite exercises. Tests that
// need a real app route (to prove app routes win over the fallback) register
// one inline. See tests/Feature/AppTierServingTest.php and ServingSemanticsTest.
