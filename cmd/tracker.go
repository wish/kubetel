package cmd

import (
	"sync"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	clientset "github.com/wish/kubetel/gok8s/client/clientset/versioned"
	informer "github.com/wish/kubetel/gok8s/client/informers/externalversions"
	"github.com/wish/kubetel/router"
	"github.com/wish/kubetel/signals"
	"github.com/wish/kubetel/tracker"
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

		//Set up k8s auth
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

		//Setup signal handler
		stopCh := signals.SetupSignalHandler()
		var waitgroup sync.WaitGroup //Used to see when all workers are finished

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

		c := tracker.Config{
			Namespace:            viper.GetString("tracker.namespace"),
			SQSregion:            viper.GetString("sqsregion"),
			Endpointendpointtype: viper.GetString("tracker.endpointtype"),
			Cluster:              viper.GetString("cluster"),
			Version:              viper.GetString("tracker.version"),
			Endpoint:             viper.GetString("tracker.endpoint"),
			KCDapp:               viper.GetString("tracker.kcdapp"),
		}

		t, err := tracker.NewTracker(k8sClient, kcdcInformerFactory, k8sInformerFactory, c)
		if err != nil {
			panic(err.Error())
		}
		go func() {
			if err = t.Run(viper.GetInt("tracker.workercount"), stopCh, &waitgroup); err != nil {
				log.Infof("Shutting down tracker: %v", err)
			}
		}()

		log.Debug("Staring Server")
		err = router.NewServer(viper.GetInt("server.port"), stopCh)
		if err != nil {
			return errors.Wrap(err, "failed to start new server")
		}
		waitgroup.Wait()
		return nil
	},
}

func init() {
	rootCmd.AddCommand(deployTracker)
	deployTracker.Flags().String("tracker-namespace", "", "namespace app to track is in")
	deployTracker.Flags().String("tracker-kcdapp", "", "kcdapp to track")
	deployTracker.Flags().String("tracker-version", "", "verson of tracked app")
	deployTracker.Flags().String("tracker-endpoint", "", "endpoint to push results to")
	deployTracker.Flags().String("tracker-endpointtype", "", "endpoint type to push results to")
	deployTracker.Flags().Int("tracker-workercount", 2, "number of worker threads to run")
	deployTracker.Flags().Int("tracker-maxretries", 2, "number of times to retry pushing to endpoint")

	viper.BindPFlag("tracker.namespace", deployTracker.Flags().Lookup("tracker-namespace"))
	viper.BindPFlag("tracker.kcdapp", deployTracker.Flags().Lookup("tracker-kcdapp"))
	viper.BindPFlag("tracker.version", deployTracker.Flags().Lookup("tracker-version"))
	viper.BindPFlag("tracker.endpoint", deployTracker.Flags().Lookup("tracker-endpoint"))
	viper.BindPFlag("tracker.endpointtype", deployTracker.Flags().Lookup("tracker-endpointtype"))
	viper.BindPFlag("tracker.workercount", deployTracker.Flags().Lookup("tracker-workercount"))
	viper.BindPFlag("tracker.maxretries", deployTracker.Flags().Lookup("tracker-maxretries"))

}
