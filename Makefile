.PHONY: screenshot

# Refresh the checked-in offline render after intentional painting changes.
screenshot:
	mkdir -p docs/screenshots
	go run ./cmd/simplebrowser -o docs/screenshots/hn-fixture.png testdata/hn/news.html
	chmod 644 docs/screenshots/hn-fixture.png
