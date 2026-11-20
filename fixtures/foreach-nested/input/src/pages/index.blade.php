@foreach ($groups as $name => $items)
{{ $name }}:
@foreach ($items as $item)
  {{ $loop->iteration }}/{{ $loop->count }} {{ $item }}
@endforeach
@endforeach
