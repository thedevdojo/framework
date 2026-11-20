1:@if ($on)@handle
kept
@endif
2:@foreach ($items as $x)@word
{{ $x }}
@endforeach
3:<x-tag>a</x-tag>@after
kept
4:@if ($on)
@own-line
kept
@endif
5:@first@second
kept
