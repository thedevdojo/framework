<p>Mail hi@example.com, a @@handle, {!! '@' !!}media (min-width: 1px) and @@media(min-width: 1px).</p>
<button @@click="open = !open" @@keydown.escape.window="open = false" :class="{ on: open }">Menu</button>
<p>@@if is text here; so are @@endif, {!! '@' !!}foreach ($a as $b), @@props(['x']) and @@auth.</p>
<p>Doubled: {!! '@' !!}@@if, @@, {!! '@' !!}@1, a@b and a lone @.</p>
<p>Braces: @{{ shown }}, @{{ $also }}, { one } and {!! '{' !!}@{{ three }}}.</p>
<p>At then braces: {!! '@' !!}@{{ x }}</p>
<p>Blade tags as text: {!! '<' !!}x-card> and {!! '<' !!}/x-card>, {!! '<' !!}x-slot:head>, {!! '<' !!} x-spaced> and {!! '<' !!}x:colon>.</p>
{!! '<' !!}?xml version="1.0"?>
<style>
{!! '@' !!}media (min-width: 640px) { .a { color: red } }
@@keyframes spin { to { transform: rotate(360deg) } }
@@font-face { font-family: x }
</style>
@if ($a)yes{{ '' }}@endif,@if ($b)no{{ '' }}@else{{ '' }}else{{ '' }}@endif.
@if ($b)
never
@else
{{ '' }}(a parenthesis right after a tag)
@endif
@foreach ($items as $x)
@break($loop->index >= 2)
{{ $x }}
@endforeach
<x-card :title="'Hi ' . $name" @click.prevent="go" x-on:keydown.escape="close" ::class="{ on: open }" :data-id="'' . $id">body</x-card>
<div @class(['a', 'b' => $a, 'c' => $b])></div>
<i @class @style>empty lists</i> @class{{ '' }}(after) @style{{ '' }}after
<!-- an html comment is copied through: <If a> @{{ name }} @@click -->
<!-- one opened by hand still runs what is inside: {{ $id }} -->
<p>Openers never closed: {!! '{' !!}{-- and {!! '{' !!}!! and {!! '{' !!}{ to the end</p>
