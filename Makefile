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
	cd oracle && php artisan framework:conformance ../fixtures

# Refresh the site `devdojo new` writes from a checkout of the starter template
# (github.com/devdojo-templates/starter). What only the template's listing
# needs stays behind. The tests then prove the copy still compiles clean.
#   make starter STARTER=/path/to/starter
starter:
	@test -d "$(STARTER)/src/pages" || (echo "usage: make starter STARTER=/path/to/starter"; exit 1)
	rm -rf starter/site
	rsync -a --exclude .git --exclude .DS_Store --exclude template.json --exclude thumbnail.png --exclude README.md "$(STARTER)/" starter/site/
	go test ./starter
.PHONY: starter
