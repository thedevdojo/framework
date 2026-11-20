<x-card @click.prevent="go" x-on:keydown.escape="close" x-data="{ open: false }" ::class="{ on: open }">One</x-card>
<x-card
    @click="open = !open"
    x-transition.opacity
    wire:model.live="q"
/>
after
