.PHONY: gowork fmt-check vet test validate check install

# Plain `go` commands need the ignored go.work that points at PiG's staged SDK.
gowork:
	./scripts/gowork.sh

fmt-check:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

vet:
	go vet ./...

test:
	go test ./...

# Build and register every extension through pig itself (does not install it).
validate:
	@set -e; for dir in extensions/*/; do \
		[ -f "$$dir/extension.go" ] || continue; \
		echo "validate $$dir"; \
		out=$$(pig install "./$$dir" --validate-only --json 2>&1) || { echo "$$out"; exit 1; }; \
	done

check: fmt-check vet test validate

# Copy the repository, local/ prompts and a fused binary into ~/.pig (see scripts/install.sh).
install:
	./scripts/install.sh
