# Local development runs the same Compose stack as a deployment, pointed at
# synthetic samples in dev/footage.

# Machine-specific settings, e.g. MATICK_DIR, go in local.mk (not committed).
-include local.mk
MATICK_DIR ?= $(abspath ../matick)
COMPOSE := FOOTAGE_DIR=$(CURDIR)/dev/footage FOOTAGE_STABLE_FOR=5s FOOTAGE_SCAN_INTERVAL=5s \
	FOOTAGE_TRANSCRIBE_MODEL=base \
	FOOTAGE_EXPORT_TARGETS="This machine=$(CURDIR)/dev/footage" \
	docker compose -f deploy/compose.yml

.PHONY: up down logs ps samples link web web-dev test

up: web ## Build and start the stack on http://localhost:8420
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

# Until matick-ui is published, web/ links it from a matick checkout.
link:
	@test -d $(MATICK_DIR)/packages/ui-core || (echo "no matick checkout at $(MATICK_DIR); set MATICK_DIR" && exit 1)
	ln -sfn $(MATICK_DIR) web/.matick

web: link
	cd web && pnpm install && pnpm build

web-dev: link ## Vite on http://localhost:5173, proxying /api to the stack
	cd web && pnpm install && pnpm dev

test:
	cd server && go test ./...
