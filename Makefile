.PHONY: baselines screenshot moon-baseline github-vscode-baseline image-boxes compatibility compatibility-check

compatibility:
	go run ./cmd/wptbench

compatibility-check:
	go run ./cmd/wptbench -check

# Refresh every checked-in current renderer output. The HN screenshot is the
# blocking golden; the Moon and GitHub VS Code images are diagnostic baselines.
baselines: screenshot moon-baseline github-vscode-baseline

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

# Refresh the non-blocking GitHub repository-page stand-in baseline (#260).
# This is a record of current output, not a golden; see
# docs/github-vscode-baseline.md.
github-vscode-baseline:
	mkdir -p docs/screenshots/github-vscode
	go run ./cmd/simplebrowser -o docs/screenshots/github-vscode/baseline.png testdata/github-vscode/index.html
	chmod 644 docs/screenshots/github-vscode/baseline.png

# Refresh the image-box layout diagnostic (magenta = decoded, gray = placeholder).
image-boxes:
	mkdir -p docs/screenshots
	go run ./cmd/simplebrowser -image-boxes -o docs/screenshots/hn-image-boxes.png testdata/hn/news.html
	chmod 644 docs/screenshots/hn-image-boxes.png
