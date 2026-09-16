<ul>
@foreach ($posts as $post)
<li>{{ $post->slug }}: {{ $post->title }}</li>
@endforeach
</ul>
