package cmd

import (
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	clientset "github.com/Wish/kubetel/gok8s/client/clientset/versioned"
	informer "github.com/Wish/kubetel/gok8s/client/informers/externalversions"
	"github.com/Wish/kubetel/healthmonitor"
	"github.com/Wish/kubetel/signals"
	"github.com/Wish/kubetel/tracker"
	k8sinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc" //For authenthication
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// deployCmd represents the deploy command
var deployTracker = &cobra.Command{
	Use:   "tracker",
	Short: "Starts a kubetel deplyment tracker",

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

		k8sClient, err := kubernetes.NewForConfig(config)
		if err != nil {
			panic(err.Error())
		}

		customClient, err := clientset.NewForConfig(config)
		if err != nil {
			panic(err.Error())
		}

		k8sInformerFactory := k8sinformers.NewFilteredSharedInformerFactory(k8sClient, time.Second*30, viper.GetString("tracker.namespace"), nil)
		kcdcInformerFactory := informer.NewFilteredSharedInformerFactory(customClient, time.Second*30, viper.GetString("tracker.namespace"), nil)

		t, _ := tracker.NewTracker(k8sClient, kcdcInformerFactory, k8sInformerFactory)
		go func() {
			if err = t.Run(2, stopCh); err != nil {
				log.Infof("Shutting down tracker: %v", err)
			}
		}()

		k8sInformerFactory.Start(stopCh)
		kcdcInformerFactory.Start(stopCh)

		log.Debug("Staring Server")
		err = healthmonitor.NewServer(viper.GetInt("server.port"), stopCh)
		if err != nil {
			return errors.Wrap(err, "failed to start new server")
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(deployTracker)
	deployTracker.Flags().String("namespace", "", "namespace app to track is in")
	deployTracker.Flags().String("kcdapp", "", "kcdapp to track")
	deployTracker.Flags().String("version", "", "verson of tracked app")

	viper.BindPFlag("tracker.namespace", deployController.Flags().Lookup("namespace"))
	viper.BindPFlag("tracker.kcdapp", deployController.Flags().Lookup("kcdapp"))
	viper.BindPFlag("tracker.version", deployController.Flags().Lookup("version"))
}
