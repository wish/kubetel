package cmd

import (
	"fmt"
	"time"

	"github.com/golang/glog"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	clientset "github.com/Wish/kubetel/gok8s/client/clientset/versioned"
	informer "github.com/Wish/kubetel/gok8s/client/informers/externalversions"
	"github.com/Wish/kubetel/tracker"
	k8sinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc" //For authenthication
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// deployCmd represents the deploy command
var createTracker = &cobra.Command{
	Use:   "tracker",
	Short: "Starts a KCD deplyment tracker",

	RunE: func(cmd *cobra.Command, args []string) (err error) {

		stopCh := make(chan struct{})
		var config *rest.Config

		if k8sConfig != "" {
			config, err = clientcmd.BuildConfigFromFlags("", k8sConfig)
		} else {
			config, err = rest.InClusterConfig()
		}
		if err != nil {
			log.Errorf("Failed to get k8s config: %v", err)
			return errors.Wrap(err, "Error building k8s configs either run in cluster or provide config file")
		}
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

		s, _ := tracker.NewTracker(deployInformer, kcdcInformer, MessageQ)
		val := ""
		for {
			select {
			case val = <-s.MessageQ:
				glog.Warning(val)
				fmt.Println(val)
			}
		}
	},
}

func init() {
	rootCmd.AddCommand(createTracker)
}
