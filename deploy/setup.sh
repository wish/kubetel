#!/bin/bash

kind delete cluster --name test
kind create cluster --name test --config cluster.yaml
export KUBECONFIG="$(kind get kubeconfig-path --name="test")"

eval $(aws ecr get-login --no-include-email --region us-west-1 --profile docker)
kubens kube-system
kubectl create secret generic regcred --from-file=.dockerconfigjson=$HOME/.docker/config.json --type=kubernetes.io/dockerconfigjson

kubectl create rolebinding varMyRoleBinding --clusterrole=admin --serviceaccount=kube-system:default --namespace=kube-system
kubectl create clusterrolebinding varMyClusterRoleBinding --clusterrole=cluster-admin --serviceaccount=kube-system:default

kubectl apply -f kcd.yaml --validate=false
kubectl apply -f kubetel.yaml --validate=false
