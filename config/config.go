package config

//KubeDeployConfig for config file structure
type KubeDeployConfig struct {
	Server     ServerConfig     `mapstructure:"server"`
	Controller ControllerConfig `mapstructure:"controller"`
	Tracker    TrackerConfig    `mapstructure:"tracker"`
	Log        LogConfig        `mapstructure:"log"`
	Image      string           `mapstructure:"image"`
	Namespace  string           `mapstructure:"namespace"`
	Cluster    string           `mapstructure:"cluster"`
}

//LogConfig for config file structure
type LogConfig struct {
	Level string `mapstructure:"level"`
}

//ServerConfig for config file structure
type ServerConfig struct {
	Port int `mapstructure:"port"`
}

//ControllerConfig for config file structure
type ControllerConfig struct {
	MaxRetries  int `mapstructure:"maxretries"`
	WorkerCount int `mapstructure:"workercount"`
}

//TrackerConfig for config file structure
type TrackerConfig struct {
	MaxRetries   int    `mapstructure:"maxretries"`
	WorkerCount  int    `mapstructure:"workercount"`
	Version      string `mapstructure:"version"`
	KCDApp       string `mapstructure:"kcd"`
	Namespace    string `mapstructure:"namespace"`
	Endpoint     string `mapstructure:"endpoint"`
	Endpointtype string `mapstructure:"endpointtype"`
}
