package cmd

import (
	"fmt"
	"time"

	"github.com/golang/glog"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/Wish/kubetel/controller"
	clientset "github.com/Wish/kubetel/gok8s/client/clientset/versioned"
	informer "github.com/Wish/kubetel/gok8s/client/informers/externalversions"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc" //For authenthication
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	version string
	env     string
)

// deployCmd represents the deploy command
var deployController = &cobra.Command{
	Use:   "controller",
	Short: "Starts a KCD controller",
	RunE: func(cmd *cobra.Command, args []string) (err error) {

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

		stopCh := make(chan struct{})

		customClient, err := clientset.NewForConfig(config)
		if err != nil {
			panic(err.Error())
		}

		kcdcInformerFactory := informer.NewSharedInformerFactory(customClient, time.Second*30)
		kcdcInformerFactory.Start(stopCh)
		kcdcInformer := kcdcInformerFactory.Custom().V1().KCDs().Informer()

		s, _ := controller.NewController(kcdcInformer)
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
	},
}

func init() {
	rootCmd.AddCommand(deployController)
}
