package cmd

import (
	"fmt"
	"os"

	raven "github.com/getsentry/raven-go"
	homedir "github.com/mitchellh/go-homedir"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"time"

	log "github.com/sirupsen/logrus"
)

var (
	cfgFile     string
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

	// Set notification send method default timeout to 5 seconds
	viper.SetDefault("notification.timeout", 3*time.Second)

	// Set notification send method default retries to 3 times
	viper.SetDefault("notification.retry.times", 3)

	// Set notification send method default retry interval
	viper.SetDefault("notification.retry.interval", 3*time.Second)
}

func init() {
	setDefaults()

	cobra.OnInitialize(initConfig)

	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is ./kube-deploy.json)")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log", "", "log level (warn, info, debug)")
	rootCmd.PersistentFlags().Bool("crash-logging", false, "Enable crash logging")
	rootCmd.PersistentFlags().StringVar(&environment, "environment", "dev", "Set environment runtime of kube-deploy")

	if err := viper.BindPFlag("log.level", rootCmd.PersistentFlags().Lookup("log")); err != nil {
		fmt.Printf("Error binding to log.level %s", err)
		os.Exit(1)
	}
	if err := viper.BindPFlag("crash_logging.enabled", rootCmd.PersistentFlags().Lookup("crash-logging")); err != nil {
		fmt.Printf("error binding viper to crash_logging ")
		os.Exit(1)
	}
	if err := viper.BindPFlag("environment", rootCmd.PersistentFlags().Lookup("environment")); err != nil {
		fmt.Printf("error binding viper to environment")
		os.Exit(1)
	}
}

// initConfig reads in config file.
func initConfig() {
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
		viper.SetConfigName("kube-deploy")
		viper.SetConfigType("json")
	}
	viper.AutomaticEnv()
	viper.SetEnvPrefix("KUBEDEPLOY")

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err != nil {
		fmt.Println(err)
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
