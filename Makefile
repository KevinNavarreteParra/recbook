# There is no GitHub workflow: `make check` is the CI, run before pushing.

.PHONY: build test check fmt nightly-install web-install

build:
	go build -o bin/recbook ./cmd/recbook

test:
	go test ./...

fmt:
	gofmt -w .

check:
	@test -z "$$(gofmt -l .)" || (echo "gofmt needed:"; gofmt -l .; exit 1)
	go vet ./...
	go test ./...

# Install the nightly job as a systemd user timer (02:00-06:00; see
# deploy/systemd). The units are linked, not copied, so edits here apply
# after `systemctl --user daemon-reload`.
nightly-install: build
	mkdir -p ~/.config/systemd/user
	ln -sf $(CURDIR)/deploy/systemd/recbook-nightly.service ~/.config/systemd/user/
	ln -sf $(CURDIR)/deploy/systemd/recbook-nightly.timer ~/.config/systemd/user/
	systemctl --user daemon-reload
	systemctl --user enable --now recbook-nightly.timer

# Install the web app as an always-on, sandboxed systemd user service (see
# deploy/systemd), restarting it so it runs the binary just built.
web-install: build
	mkdir -p ~/.config/systemd/user
	ln -sf $(CURDIR)/deploy/systemd/recbook-web.service ~/.config/systemd/user/
	systemctl --user daemon-reload
	systemctl --user enable recbook-web.service
	systemctl --user restart recbook-web.service
