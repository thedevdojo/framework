<?php

namespace Tests\Feature;

// use Illuminate\Foundation\Testing\RefreshDatabase;
use Tests\TestCase;

class ExampleTest extends TestCase
{
    /**
     * The oracle defines no web routes of its own (pages are served by
     * pocketknife's opt-in fallback), so the framework health endpoint is the
     * stable proof that the application boots and app routes respond.
     */
    public function test_the_application_returns_a_successful_response(): void
    {
        $response = $this->get('/up');

        $response->assertStatus(200);
    }
}
