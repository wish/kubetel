package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	log "github.com/sirupsen/logrus"
)

var (
	k8sConfig   string
	logLevel    string
	environment string
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
	Use:   "kube-informer",
	Short: "Deployment tracker for kubernetes",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		configureLogging()
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func setDefaults() {
	viper.SetDefault("log.level", "info")
}

func init() {
	setDefaults()

	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.
	rootCmd.PersistentFlags().StringVar(&k8sConfig, "k8s-config", "", "Path to the kube config file. Only required for running outside k8s cluster. In cluster, pods credentials are used")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log", "", "log level (warn, info, debug)")
	rootCmd.PersistentFlags().Bool("crash-logging", false, "Enable crash logging")

	if err := viper.BindPFlag("log.level", rootCmd.PersistentFlags().Lookup("log")); err != nil {
		fmt.Printf("Error binding to log.level %s", err)
		os.Exit(1)
	}
	if err := viper.BindPFlag("crash_logging.enabled", rootCmd.PersistentFlags().Lookup("crash-logging")); err != nil {
		fmt.Printf("error binding viper to crash_logging ")
		os.Exit(1)
	}
}
