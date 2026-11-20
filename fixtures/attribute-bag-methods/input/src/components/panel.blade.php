@if ($attributes->has('title'))<h2>{{ $attributes->get('title') }}</h2>@endif
<section {{ $attributes->except(['title', 'class'])->class(['panel', 'open' => $attributes->has('data-open')]) }}>
<div {{ $attributes->whereStartsWith('data-') }}></div>
<span {{ $attributes->only('title') }}></span>
@if ($attributes->isNotEmpty())<i>full</i>@endif
</section>
