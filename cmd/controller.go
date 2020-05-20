package cmd

import (
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/wish/kubetel/controller"
	clientset "github.com/wish/kubetel/gok8s/client/clientset/versioned"
	informer "github.com/wish/kubetel/gok8s/client/informers/externalversions"
	"github.com/wish/kubetel/healthmonitor"
	"github.com/wish/kubetel/signals"

	k8sinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
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
	Short: "Starts a kubetel controller",
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
		log.Trace("Suscessfully completed k8s authentication")

		stopCh := signals.SetupSignalHandler()

		//Set up k8s clients
		k8sClient, err := kubernetes.NewForConfig(config)
		if err != nil {
			panic(err.Error())
		}
		customClient, err := clientset.NewForConfig(config)
		if err != nil {
			panic(err.Error())
		}

		kcdcInformerFactory := informer.NewSharedInformerFactory(customClient, time.Second*30)
		k8sInformerFactory := k8sinformers.NewSharedInformerFactory(k8sClient, time.Second*30)

		log.Debug("Creating New Controller")
		c, err := controller.NewController(k8sClient, customClient, kcdcInformerFactory, k8sInformerFactory)
		if err != nil {
			panic(err.Error())
		}
		go func() {
			if err = c.Run(2, stopCh); err != nil {
				log.Infof("Shutting down container version controller: %v", err)
			}
		}()

		log.Debug("Staring Server")
		err = healthmonitor.NewServer(viper.GetInt("server.port"), stopCh)
		if err != nil {
			return errors.Wrap(err, "failed to start new server")
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(deployController)
}
