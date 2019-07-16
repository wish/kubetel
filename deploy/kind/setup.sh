#!/bin/bash

kind delete cluster --name test
kind create cluster --name test --config cluster.yaml
export KUBECONFIG="$(kind get kubeconfig-path --name="test")"

eval $(aws ecr get-login --no-include-email --region us-west-1 --profile docker)
kubectl apply -f namespace.yaml --validate=false
kubens kubetel
kubectl create secret generic regcred --from-file=.dockerconfigjson=$HOME/.docker/config.json --type=kubernetes.io/dockerconfigjson

kubectl create rolebinding bind1 --clusterrole=admin --serviceaccount=kube-system:default --namespace=kube-system
kubectl create clusterrolebinding cbind1 --clusterrole=cluster-admin --serviceaccount=kube-system:default

kubectl create rolebinding bind2 --clusterrole=admin --serviceaccount=kubetel:default --namespace=kubetel
kubectl create clusterrolebinding cbind2 --clusterrole=cluster-admin --serviceaccount=kubetel:default

kubectl apply -f kcd.yaml --validate=false
kubectl apply -f kubetel.yaml --validate=false
