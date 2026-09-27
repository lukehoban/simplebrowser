.PHONY: screenshot image-boxes compatibility compatibility-check

compatibility:
	go run ./cmd/wptbench

compatibility-check:
	go run ./cmd/wptbench -check

# Refresh the checked-in offline render after intentional painting changes.
screenshot:
	mkdir -p docs/screenshots
	go run ./cmd/simplebrowser -o docs/screenshots/hn-fixture.png testdata/hn/news.html
	chmod 644 docs/screenshots/hn-fixture.png

# Refresh the image-box layout diagnostic (magenta = decoded, gray = placeholder).
image-boxes:
	mkdir -p docs/screenshots
	go run ./cmd/simplebrowser -image-boxes -o docs/screenshots/hn-image-boxes.png testdata/hn/news.html
	chmod 644 docs/screenshots/hn-image-boxes.png
