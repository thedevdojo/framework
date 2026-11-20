@props(['menu' => [], 'items' => ['a', 'b']])
<ul>@foreach ($items as $i)<li>{{ $i }}</li>@endforeach</ul>
@if ($menu) HAS-MENU @else NO-MENU @endif
@foreach ($menu as $m)<p>{{ $m }}</p>@endforeach
