@props(['links' => []])
<nav>
@foreach ($links as $link)
<a href="{{ $link->url }}">{{ $link->label }}</a>
@endforeach
</nav>
