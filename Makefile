test:
	go test ./...
fuzz:
	go test ./blade/scanner -fuzz=FuzzScan -fuzztime=30s
	go test ./blade/parser -fuzz=FuzzParse -fuzztime=30s
conformance:
	cd oracle && php artisan pocketknife:conformance ../fixtures
