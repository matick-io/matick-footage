# Local development runs the deployed Compose stack, built from this checkout
# and pointed at synthetic samples in dev/footage.

COMPOSE := FOOTAGE_VERSION=dev FOOTAGE_DIR=$(CURDIR)/dev/footage \
	FOOTAGE_STABLE_FOR=5s FOOTAGE_SCAN_INTERVAL=5s FOOTAGE_TRANSCRIBE_MODEL=base \
	FOOTAGE_EXPORT_TARGETS="This machine=$(CURDIR)/dev/footage" \
	docker compose -f deploy/compose.yml -f dev/compose.yml

.PHONY: up down logs ps samples web-dev test

up: ## Build and start the stack on http://localhost:8420
	$(COMPOSE) up --build -d

down:
	$(COMPOSE) down

logs:
	$(COMPOSE) logs -f

ps:
	$(COMPOSE) ps

samples: ## Generate synthetic recordings into dev/footage
	docker build -q -t matick-footage-samples dev/samples
	docker run --rm -v $(CURDIR)/dev/footage:/out matick-footage-samples

web-dev: ## Vite on http://localhost:5173, proxying /api to the stack
	cd web && pnpm install && pnpm dev

test:
	cd server && go test ./...
