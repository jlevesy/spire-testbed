.PHONY: run
run: create_cluster apply_infra install_spire install_external_traefik install_internal_traefik install_server install_gateway

.PHONY: create_cluster
create_cluster:
	k3d cluster create \
		--image="rancher/k3s:v1.24.3-k3s1" \
		-p "8000:80@loadbalancer" \
		-p "8443:443@loadbalancer" \
		--registry-create=voidev.localhost:0.0.0.0:5000 \
		--k3s-arg="--disable=traefik@server:0" \
		voidev

.PHONY: delete_cluster
delete_cluster:
	k3d cluster delete voidev

.PHONY:
apply_infra:
	kubectl apply -f ./k8s/infra

.PHONY: install_external_traefik
install_external_traefik:
	kubectl apply -f ./k8s/traefik-external

.PHONY: install_internal_traefik
install_internal_traefik:
	kubectl apply -f ./k8s/traefik-internal

.PHONY: install_spire
install_spire:
	KO_DOCKER_REPO=voidev.localhost:5000 ko apply -f ./k8s/spire

.PHONY: install_server
install_server:
	KO_DOCKER_REPO=voidev.localhost:5000 ko apply -f ./k8s/echo-server

.PHONY: install_gateway
install_gateway:
	KO_DOCKER_REPO=voidev.localhost:5000 ko apply -f ./k8s/echo-gateway
