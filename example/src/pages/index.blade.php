<x-layouts.main title="DevDojo Framework Example">
<section class="mx-auto max-w-2xl py-16 text-center">
    <h1 class="text-4xl font-bold">{{ $site->name }}</h1>
    <p class="mt-4 text-lg text-gray-600">{{ $site->tagline ?? 'A DevDojo Framework example' }}</p>
    <a href="mailto:hello@example.com" class="underline">hello@example.com</a>
</section>
<x-sections.feature-list heading="Why DevDojo Framework" :items="$features"/>
</x-layouts.main>
