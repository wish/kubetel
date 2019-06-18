package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	clientset "github.com/Wish/kubetel/gok8s/client/clientset/versioned"

	controller "github.com/Wish/kubetel/controller"
	"github.com/golang/glog"

	k8sinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"

	informer "github.com/Wish/kubetel/gok8s/client/informers/externalversions"
)

func main() {
	flag.Parse()
	stopCh := make(chan struct{})

	var kubeconfig *string
	if home := homeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	flag.Parse()

	// use the current context in kubeconfig
	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		panic(err.Error())
	}

	/*
		config, err := rest.InClusterConfig()
		if err != nil {
			panic(err.Error())
		}
	*/
	k8sClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())

	}

	customClient, err := clientset.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	k8sInformerFactory := k8sinformers.NewSharedInformerFactory(k8sClient, time.Second*30)
	kcdcInformerFactory := informer.NewSharedInformerFactory(customClient, time.Second*30)

	k8sInformerFactory.Start(stopCh)
	kcdcInformerFactory.Start(stopCh)

	deployInformer := k8sInformerFactory.Apps().V1().Deployments().Informer()
	kcdcInformer := kcdcInformerFactory.Custom().V1().KCDs().Informer()

	MessageQ := make(chan string, 100)
	MessageQ2 := make(chan string, 100)

	s, _ := controller.NewController(deployInformer, kcdcInformer, MessageQ, MessageQ2)
	val := ""
	for {
		select {
		case val = <-s.MessageQ:
			glog.Warning(val)
			fmt.Println(val)
		case val = <-s.MessageQ2:
			fmt.Println(val)
			glog.Warning(val)
		}
	}
}

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return os.Getenv("USERPROFILE") // windows
}
