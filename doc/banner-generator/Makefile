.PHONY: generate install clean

generate: node_modules
	bun run generate.js

install: node_modules

node_modules: package.json
	bun install
	touch node_modules

clean:
	rm -rf node_modules tygor-banner.svg
