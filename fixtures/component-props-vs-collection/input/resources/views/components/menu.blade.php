@props(['links' => null])
@if ($links)
@foreach ($links as $link)
<li>{{ $link->label }}</li>
@endforeach
@else
<p>no links</p>
@endif
