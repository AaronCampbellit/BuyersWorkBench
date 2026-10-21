.PHONY: setup dev down logs owner status migrate test test-integration check web-check browser-check tunnel-build tunnel-up tunnel-down tunnel-status tunnel-logs tunnel-refresh-token backup restore-test

setup:
	python3 scripts/setup-local.py

dev: setup
	docker compose up -d --build --wait

down:
	docker compose down

logs:
	docker compose logs --tail=80 api worker web

owner:
	bash scripts/bootstrap-owner.sh

status:
	docker compose --profile tools run --rm --no-deps admin status

migrate:
	docker compose run --rm migrate

test:
	go test ./...

test-integration:
	bash scripts/test-integration.sh

web-check:
	cd apps/web && npm ci && npm run build

browser-check:
	cd services/browser-runner && npm ci && npm run check

check: test web-check browser-check
	go vet ./...

tunnel-build:
	docker compose -f compose.yaml -f compose.tunnel.yaml --profile tunnel build public-api public-web

tunnel-up:
	docker compose -f compose.yaml -f compose.tunnel.yaml --profile tunnel up -d --wait public-api public-web cloudflared

tunnel-down:
	docker compose -f compose.yaml -f compose.tunnel.yaml --profile tunnel stop cloudflared public-web public-api

tunnel-status:
	docker compose -f compose.yaml -f compose.tunnel.yaml --profile tunnel ps

tunnel-logs:
	docker compose -f compose.yaml -f compose.tunnel.yaml --profile tunnel logs --tail=60 public-api public-web cloudflared

tunnel-refresh-token:
	python3 scripts/fetch-tunnel-token.py
	docker compose -f compose.yaml -f compose.tunnel.yaml --profile tunnel up -d --no-deps --force-recreate cloudflared

backup:
	python3 scripts/backup-local.py

restore-test:
	python3 scripts/restore-test.py
