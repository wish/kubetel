#!/bin/bash

kind delete cluster --name kind
kind create cluster --name kind --config deploy/kind/cluster.yaml
export KUBECONFIG="$(kind get kubeconfig-path --name="kind")"
kubens kube-system
kubectl apply -f deploy/kind/kind-setup.yaml --validate=false
docker build -t kubetel:local .
kind load docker-image kubetel:local
kubectl apply -f deploy/kind/kind-local-deploy.yaml --validate=false


