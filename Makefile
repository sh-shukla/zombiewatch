CONTROLLER_GEN = go run sigs.k8s.io/controller-tools/cmd/controller-gen@latest
IMG ?= ghcr.io/sh-shukla/zombiewatch:latest

.PHONY: build test vet generate manifests docker-build deploy undeploy

build: ## Build the manager binary
	go build -o bin/zombiewatch .

test: vet ## Run unit tests
	go test ./...

vet:
	go vet ./...

generate: ## Regenerate deepcopy code after editing api/
	$(CONTROLLER_GEN) object paths=./api/...

manifests: ## Regenerate CRD + RBAC after editing api/ or markers
	$(CONTROLLER_GEN) crd rbac:roleName=zombiewatch paths=./... \
		output:crd:artifacts:config=config/crd \
		output:rbac:artifacts:config=config/deploy

docker-build:
	docker build -t $(IMG) .

deploy: ## Install CRD, RBAC and the operator
	kubectl apply -f config/crd/ -f config/deploy/

undeploy:
	kubectl delete -f config/deploy/ -f config/crd/ --ignore-not-found
