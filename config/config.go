package config

type KubeDeployConfig struct {
	Server     ServerConfig     `mapstructure:"server"`
	Controller ControllerConfig `mapstructure:"controller"`
	Tracker    TrackerConfig    `mapstructure:"tracker"`
	Log        LogConfig        `mapstructure:"log"`
	Image      string           `mapstructure:"image"`
	Namespace  string           `mapstructure:"namespace"`
	Cluster    string           `mapstructure:"cluster"`
}

type LogConfig struct {
	Level string `mapstructure:"level"`
}
type ServerConfig struct {
	Port int `mapstructure:"port"`
}
type ControllerConfig struct {
	MaxRetries  int `mapstructure:"maxretries"`
	WorkerCount int `mapstructure:"workercount"`
}
type TrackerConfig struct {
	MaxRetries              int    `mapstructure:"maxretries"`
	WorkerCount             int    `mapstructure:"workercount"`
	Version                 string `mapstructure:"workercount"`
	KCDApp                  string `mapstructure:"workercount"`
	Namespace               string `mapstructure:"namespace"`
	DeployStatusEndpointAPI string `mapstructure:"endpoint"`
}
