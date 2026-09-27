.PHONY: screenshot moon-baseline image-boxes compatibility compatibility-check

compatibility:
	go run ./cmd/wptbench

compatibility-check:
	go run ./cmd/wptbench -check

# Refresh the checked-in offline render after intentional painting changes.
screenshot:
	mkdir -p docs/screenshots
	go run ./cmd/simplebrowser -o docs/screenshots/hn-fixture.png testdata/hn/news.html
	chmod 644 docs/screenshots/hn-fixture.png

# Refresh the non-blocking Wikipedia Moon baseline (#245). This is a record of
# current output, not a golden; see docs/wikipedia-moon-baseline.md.
moon-baseline:
	mkdir -p docs/screenshots/wikipedia-moon
	go run ./cmd/simplebrowser -o docs/screenshots/wikipedia-moon/baseline.png testdata/wikipedia-moon/moon.html
	chmod 644 docs/screenshots/wikipedia-moon/baseline.png

# Refresh the image-box layout diagnostic (magenta = decoded, gray = placeholder).
image-boxes:
	mkdir -p docs/screenshots
	go run ./cmd/simplebrowser -image-boxes -o docs/screenshots/hn-image-boxes.png testdata/hn/news.html
	chmod 644 docs/screenshots/hn-image-boxes.png
