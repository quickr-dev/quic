vm-recreate:
	time multipass delete quic-e2e --purge || true
	time multipass launch --name quic-e2e --cpus 4 --disk 20GB --memory 8GB --cloud-init e2e/cloud-init.yaml --verbose
	multipass mount . quic-e2e:/home/ubuntu/quic
	time multipass stop quic-e2e
	time multipass snapshot quic-e2e --name base
	time multipass start quic-e2e

vm-restore:
	time multipass stop quic-e2e || true
	time multipass restore --destructive quic-e2e.base
	time multipass start quic-e2e

vm-rebuild-agent:
	GOOS=linux GOARCH=arm64 go build -o bin/quicd-linux ./cmd/quicd
	multipass transfer bin/quicd-linux quic-e2e:/tmp/quicd
	multipass exec quic-e2e -- sudo systemctl stop quicd || true
	multipass exec quic-e2e -- sudo mv /tmp/quicd /opt/quic/bin/quicd
	multipass exec quic-e2e -- sudo chown postgres:postgres /opt/quic/bin/quicd
	multipass exec quic-e2e -- sudo chmod +x /opt/quic/bin/quicd
	multipass exec quic-e2e -- sudo systemctl enable quicd.service
	multipass exec quic-e2e -- sudo systemctl start quicd.service

e2e-agent: vm-rebuild-agent
	multipass exec quic-e2e -- sudo --login --user=postgres bash -c "cd /home/ubuntu/quic && go test ./e2e/agent -v -run TestQuicdInit -count=1"
	$(MAKE) vm-restore

e2e-cli: vm-rebuild-agent
	go build -o bin/quic ./cmd/quic
	go test ./e2e/cli -v -count=1

e2e: e2e-agent e2e-cli e2e-init

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
	@echo "Releasing $(VERSION)"
	make release-build VERSION=$(VERSION)
	make release-tag VERSION=$(VERSION)
	@echo "GitHub Action: https://github.com/quickr-dev/quic/actions"

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
	echo "Replaced ~/.local/bin/quic with version $$current_version"
