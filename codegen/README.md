# Autogen KCD CRD Boilerplate Code

Some background into kubernetes go codeden can be found here [here](https://blog.openshift.com/kubernetes-deep-dive-code-generation-customresources/)

## Setup

### Selecting a Version

Detailed versioning information can be found [here](https://github.com/kubernetes/client-go/blob/master/README.md)

TLDR you want to match your version of client go based on what version of kubernetes you are running on your cluster(s) and you probably don't want to be more than one major revision off.

You can check the version of your cluster with `kubectl version`

### Pin client-go version

First you are going to want to modify `go.mod` based on the version of kubernetes we are running. For example if we are running `kubernetes-1.10.11` we will modify `go.mod` as follows:

```sh
k8s.io/api kubernetes-1.10.11
k8s.io/apimachinery kubernetes-1.10.11
k8s.io/client-go kubernetes-1.10.11
```

Then run:

```bash
export GO111MODULE=on && go mod tidy
```

You should see that `go.mod` is updated to a commit hash. It may be helpful to add a comment after the has to remember what version you are on

```sh
k8s.io/api v0.0.0-20181126191646-05ef6506a18a //kubernetes-1.10.11
k8s.io/apimachinery v0.0.0-20181126123303-08e1968f78a1 //kubernetes-1.10.11
k8s.io/client-go v0.0.0-20181126192138-ff0d167d8bcb //kubernetes-1.10.11
```

NOTE: In v12.0.0 onwards the imports have have changed to no longer not require you to specify the api and api machinery, although I have not tested it. more detail can be found [here](https://github.com/kubernetes/client-go/blob/master/INSTALL.md#go-modules)

### Setup Kubernetes code-generator

The easiest way I found to do this was to manually grab the [release](https://github.com/kubernetes/code-generator/releases) of the kubernetes `code-generator` that matches the kubernetes version and extract the contents into `$GOPATH/src/k8s.io/code-generator` 

## Usage

Run the following commands from the project root directory (`$GOPATH/src/github.com/wish/kubetel`)

### Generate

```sh
 CODEGEN_PKG=../../../k8s.io/code-generator  bash -xe codegen/update-codegen.sh
```

### Verify

```sh
 CODEGEN_PKG=../../../k8s.io/code-generator  bash -xe codegen/verify-codegen.sh
```
