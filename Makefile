.PHONY: build test kernel rootfs

build:
	go build -o bin/sparrow ./cmd/sparrow

test:
	go test ./...

kernel:
	./scripts/build-kernel.sh

rootfs:
	docker build -t sparrow-claude images/claude
	SSH_KEY=$(HOME)/.ssh/id_ed25519.pub ./scripts/docker-to-rootfs.sh sparrow-claude assets/rootfs/claude.ext4 2G
