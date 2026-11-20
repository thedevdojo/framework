@props(['title' => 'T', 'note' => 'can\'t say "no" & mean it'])
<div {{ $attributes }}>{{ $title }} / {{ $note }}: {{ $slot }}</div>
