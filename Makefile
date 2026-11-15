test:
	go test ./...
fuzz:
	go test ./blade/scanner -fuzz=FuzzScan -fuzztime=30s
	go test ./blade/parser -fuzz='^FuzzParse$$' -fuzztime=30s
	go test ./blade/htmlscan -fuzz='^FuzzScan$$' -fuzztime=30s
	go test ./blade/htmlscan -fuzz='^FuzzExpr$$' -fuzztime=30s
	go test ./blade/parser -fuzz='^FuzzParseHTML$$' -fuzztime=30s
	go test ./blade/render -fuzz='^FuzzCompileHTML$$' -fuzztime=30s
	go test ./blade/convert -fuzz='^FuzzFile$$' -fuzztime=30s
conformance:
	cd oracle && php artisan pocketknife:conformance ../fixtures
