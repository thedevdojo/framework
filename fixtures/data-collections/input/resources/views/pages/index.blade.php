<ul>
@foreach ($plans as $plan)
<li>{{ $plan->name }}: {{ $plan->price }}</li>
@endforeach
</ul>
