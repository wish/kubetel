package cmd

import (
	"fmt"
	"os"

	homedir "github.com/mitchellh/go-homedir"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	log "github.com/sirupsen/logrus"
)

var (
	k8sConfig   string
	logLevel    string
	environment string
	cfgFile     string
	port        int
)

func configureLogging() {
	level := viper.GetString("log.level")
	logRusLevel, err := log.ParseLevel(level)
	if err != nil {
		fmt.Printf("%s\n", err)
		os.Exit(1)
	}
	log.SetLevel(logRusLevel)
	log.SetOutput(os.Stdout)
}

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "kubetel",
	Short: "Deployment tracker for kubernetes",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		configureLogging()
	},
}

// Execute is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func setDefaults() {
	viper.SetDefault("log.level", "info")
	viper.SetDefault("controller.maxretries", 2)
	viper.SetDefault("controller.workercount", 2)
	viper.SetDefault("tracker.maxretries", 2)
	viper.SetDefault("tracker.workercount", 2)
	viper.SetDefault("image", "951896542015.dkr.ecr.us-west-1.amazonaws.com/wish/kubetel")
	viper.SetDefault("namespace", "kubetel")
}

func init() {
	setDefaults()

	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is ./kubetel.json)")
	rootCmd.PersistentFlags().StringVar(&k8sConfig, "k8s-config", "", "Path to the kube config file. Only required for running outside k8s cluster. In cluster, pods credentials are used")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log", "", "log level (warn, info, debug, trace)")
	rootCmd.PersistentFlags().Bool("crash-logging", false, "Enable crash logging")
	rootCmd.PersistentFlags().Bool("no-config", false, "Disable config file")
	rootCmd.PersistentFlags().String("cluster", "", "log level (warn, info, debug, trace)")
	rootCmd.PersistentFlags().IntVar(&port, "server-port", 0, "port to bind status endpoint")

	if port != 0 {
		if err := viper.BindPFlag("server.port", rootCmd.PersistentFlags().Lookup("server-port")); err != nil {
			fmt.Printf("Error binding viper to cluster %s", err)
			os.Exit(1)
		}
	}
	if err := viper.BindPFlag("cluster", rootCmd.PersistentFlags().Lookup("cluster")); err != nil {
		fmt.Printf("Error binding viper to cluster %s", err)
		os.Exit(1)
	}
	if err := viper.BindPFlag("log.level", rootCmd.PersistentFlags().Lookup("log")); err != nil {
		fmt.Printf("Error binding viper to log.level %s", err)
		os.Exit(1)
	}
	if err := viper.BindPFlag("crash_logging.enabled", rootCmd.PersistentFlags().Lookup("crash-logging")); err != nil {
		fmt.Printf("error binding viper to crash_logging ")
		os.Exit(1)
	}
}

func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
		viper.Set("cfgFile", cfgFile)
	} else {
		// Find home directory.
		home, err := homedir.Dir()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		viper.AddConfigPath(".")
		viper.AddConfigPath(home)
		viper.SetConfigName("kubetel")
	}
	viper.AutomaticEnv()
	viper.SetEnvPrefix("KUBETEL")

	if err := viper.ReadInConfig(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
