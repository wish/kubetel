#!/bin/bash

kind delete cluster --name kind
kind create cluster --name kind --config cluster.yaml
export KUBECONFIG="$(kind get kubeconfig-path --name="kind")"
kubens kube-system
eval $(aws ecr get-login --no-include-email --region us-west-1 --profile docker)
kubectl create secret generic regcred --from-file=.dockerconfigjson=$HOME/.docker/config.json --type=kubernetes.io/dockerconfigjson
kubectl apply -f kind-setup.yaml --validate=false
kubectl apply -f kind-local-deploy.yaml --validate=false


