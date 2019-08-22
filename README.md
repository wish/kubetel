# Kubetel [![Build Status](https://travis-ci.org/wish/kubetel.svg?branch=master)](https://travis-ci.org/wish/kubetel) [![Go Report Card](https://goreportcard.com/badge/github.com/wish/kubetel)](https://goreportcard.com/report/github.com/wish/kubetel)

Kubetel is a tool that works in conjunction with [KCD](https://github.com/wish/kcd) to better track deployments, as well as collect relevent information when deployments fail.

## Table of Contents

- [Overview](#Overview)
- [Architecture](#Architecture)
- [Usage](#Usage)

## Architecture

Kubetel has 2 main components:

- Kubetel Controller
  - Creates a tracker job when KCD starts a a deploy
  - Clean old jobs from the API server
- Deployment tracker
  - Watches deployments and KCD of tracked deployments
  - Grabs pod contianer logs of failed deployments
  - Creates [deployment messages](https://github.com/wish/kubetel/blob/master/tracker/status.go#L10) when are sent to either http or SQS to get aggrigated and processed

## Usage

### Building Kubetel

```bash
make build
docker build -t wish/kubetel
```

### Running in [KIND](https://github.com/kubernetes-sigs/kind)

```bash
./deploy/kind/setup.sh
```

### Applying to a Kubernetes cluster

Example Kubectl, yaml specs and Helm chart spec are still #TODO, However the configuation spec can be found [here](https://github.com/wish/kubetel/blob/master/config/config.go) and the required RBAC configuation can be found [here](https://github.com/wish/kubetel/blob/master/deploy/kind/kind-setup.yaml)

## Updating kubernetes version

Currently we are running `kubernetes-1.10.11`
Instructions to update the client-go and autogen code can be found [here](https://github.com/wish/kubetel/blob/master/codegen/README.md)
