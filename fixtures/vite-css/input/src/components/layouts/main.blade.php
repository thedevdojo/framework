@props(['title' => 'Untitled'])
<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<title>{{ $title }}</title>
@vite(['src/css/site.css'])
</head>
<body>{{ $slot }}</body>
</html>
