vm-recreate:
	time multipass delete quic-e2e-base --purge || true
	time multipass launch --name quic-e2e-base --cpus 4 --disk 40GB --memory 8GB --cloud-init scripts/e2e-cloud-init.yaml --verbose
	time multipass stop quic-e2e-base
	time multipass snapshot quic-e2e-base --name quic-e2e-ready
	time multipass start quic-e2e-base

vm-restore:
	time multipass stop quic-e2e-base || true
	time multipass restore --destructive quic-e2e-base.quic-e2e-ready
	time multipass start quic-e2e-base

vm-rebuild-agent:
	GOOS=linux GOARCH=arm64 go build -o bin/quicd-linux ./cmd/quicd
	multipass exec quic-e2e-base -- sudo systemctl stop quicd || true
	multipass transfer bin/quicd-linux quic-e2e-base:/tmp/quicd
	multipass exec quic-e2e-base -- sudo mv /tmp/quicd /tank/bin/quicd
	multipass exec quic-e2e-base -- sudo chown postgres:postgres /tank/bin/quicd
	multipass exec quic-e2e-base -- sudo chmod +x /tank/bin/quicd
	multipass exec quic-e2e-base -- sudo systemctl enable quicd.service
	multipass exec quic-e2e-base -- sudo systemctl start quicd.service
	sleep 0.5

e2e-agent: vm-rebuild-agent
	go test ./e2e/agent -v -run TestCheckoutFlow -count=1

e2e-cli: vm-rebuild-agent
	go build -o bin/quic ./cmd/quic
	go test ./e2e/cli -v -count=1

e2e: e2e-agent e2e-cli

.PHONY: proto
proto:
	protoc --go_out=. --go-grpc_out=. proto/*.proto

lint:
	go vet ./...
	go fmt ./...

build-cli:
	go build -o bin/quic ./cmd/quic

build-cli-versioned:
	go build -ldflags="-X 'github.com/quickr-dev/quic/internal/version.Version=$(VERSION)'" -o bin/quic ./cmd/quic

release-build:
	@if [ -z "$(VERSION)" ]; then echo "Usage: make release-build VERSION=v1.0.0"; exit 1; fi
	GOOS=darwin GOARCH=amd64 go build -ldflags="-X 'github.com/quickr-dev/quic/internal/version.Version=$(VERSION)'" -o bin/quic-darwin-amd64 ./cmd/quic
	GOOS=darwin GOARCH=arm64 go build -ldflags="-X 'github.com/quickr-dev/quic/internal/version.Version=$(VERSION)'" -o bin/quic-darwin-arm64 ./cmd/quic
	GOOS=linux GOARCH=amd64 go build -ldflags="-X 'github.com/quickr-dev/quic/internal/version.Version=$(VERSION)'" -o bin/quic-linux-amd64 ./cmd/quic
	@echo "✅ Built release binaries for $(VERSION)"

release-tag:
	@if [ -z "$(VERSION)" ]; then echo "Usage: make release-tag VERSION=v1.0.0"; exit 1; fi
	@echo "Creating and pushing tag $(VERSION)..."
	git tag $(VERSION)
	git push origin $(VERSION)
	@echo "✅ Tagged and pushed $(VERSION)"
	@echo "🚀 GitHub Actions will now build and release automatically"

release:
	@if [ -z "$(VERSION)" ]; then echo "Usage: make release VERSION=v1.0.0"; exit 1; fi
	@echo "🚀 Starting release process for $(VERSION)"
	make release-build VERSION=$(VERSION)
	make release-tag VERSION=$(VERSION)
	@echo ""
	@echo "✅ Release $(VERSION) initiated!"
	@echo "📦 Check GitHub Actions: https://github.com/quickr-dev/quic/actions"
	@echo "📋 View releases: https://github.com/quickr-dev/quic/releases"

deploy:
	GOOS=linux GOARCH=amd64 go build -o bin/quicd-linux-amd64 ./cmd/quicd
	cd ansible && time ansible-playbook -i inventory.ini deploy.yml --limit lhr.quickr.dev --vault-password-file ../.vault_pass

ansible-edit:
	ansible-vault edit ansible/group_vars/all/vault.yml --vault-password-file .vault_pass

replace-quic-cli:
	@current_version=$$(quic version 2>/dev/null | grep -o 'v[0-9]\+\.[0-9]\+\.[0-9]\+' | head -1 || echo "v0.0.1"); \
	echo "Building quic with version: $$current_version"; \
	GOOS=darwin GOARCH=arm64 go build -ldflags="-X 'github.com/quickr-dev/quic/internal/version.Version=$$current_version'" -o bin/quic-darwin-arm64 ./cmd/quic; \
	cp bin/quic-darwin-arm64 ~/.local/bin/quic; \
	echo "✅ Replaced ~/.local/bin/quic with version $$current_version"
