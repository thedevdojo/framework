@props(['title' => 'Untitled'])
<!DOCTYPE html>
<html>
<head><title>{{ $title }}</title>{{ $head ?? '' }}</head>
<body>{{ $slot }}</body>
</html>
