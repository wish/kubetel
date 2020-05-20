package cmd

import (
	"fmt"
	"os"

	raven "github.com/getsentry/raven-go"
	homedir "github.com/mitchellh/go-homedir"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	log "github.com/sirupsen/logrus"
)

var (
	k8sConfig string
	cfgFile   string
	useCfg    bool
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
	viper.SetDefault("image", "951896542015.dkr.ecr.us-west-1.amazonaws.com/wish/kubetel:v1.0")
	viper.SetDefault("namespace", "kubetel")
	viper.SetDefault("controller.completejobttl", 1)
	viper.SetDefault("controller.failedjobttl", 5)
}

func init() {
	setDefaults()

	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().BoolVar(&useCfg, "use-config", true, "Disable config file")
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is ./kubetel.json)")
	rootCmd.PersistentFlags().String("sqsregion", "", "aws region for sqs")
	rootCmd.PersistentFlags().StringVar(&k8sConfig, "k8s-config", "", "Path to the kube config file. Only required for running outside k8s cluster. In cluster, pods credentials are used")
	rootCmd.PersistentFlags().String("log", "", "log level (warn, info, debug, trace)")
	rootCmd.PersistentFlags().String("cluster", "", "log level (warn, info, debug, trace)")
	rootCmd.PersistentFlags().Int("server-port", 80, "port to bind status endpoint")
	rootCmd.PersistentFlags().StringToString("nodeSelector", map[string]string{"": ""}, "Node selector for job in form map[string]string")
	rootCmd.PersistentFlags().Bool("crash-logging", false, "Enable crash logging")
	rootCmd.PersistentFlags().String("environment", "dev", "Set environment runtime of kubetel")

	if err := viper.BindPFlag("server.port", rootCmd.PersistentFlags().Lookup("server-port")); err != nil {
		fmt.Printf("Error binding viper to cluster %s", err)
		os.Exit(1)
	}
	if err := viper.BindPFlag("nodeSelector", rootCmd.PersistentFlags().Lookup("nodeSelector")); err != nil {
		fmt.Printf("Error binding viper to nodeSelector %s", err)
		os.Exit(1)
	}
	if err := viper.BindPFlag("sqsregion", rootCmd.PersistentFlags().Lookup("sqsregion")); err != nil {
		fmt.Printf("Error binding viper to sqsregion %s", err)
		os.Exit(1)
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

	if viper.GetBool("crash_logging.enabled") {
		sentryDSN := viper.GetString("crash_logging.sentry.dsn")
		if sentryDSN != "" {
			raven.SetDSN(sentryDSN)
		}
		sentryEnv := viper.GetString("environment")
		if sentryEnv != "" {
			raven.SetEnvironment(sentryEnv)
		}
	}
}

func initConfig() {
	if useCfg {
		if cfgFile != "" {
			// Use config file from the flag.
			viper.SetConfigFile(cfgFile)
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
}
