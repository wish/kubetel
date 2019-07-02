package config

type KubeDeployConfig struct {
	Server    ServerConfig `mapstructure:"server"`
	Log       LogConfig    `mapstructure:"log"`
	Image     string       `mapstructure:"image"`
	Namespace string       `mapstructure:"namespace"`
	Cluster   string       `mapstructure:"cluster"`
}

type LogConfig struct {
	Level string `mapstructure:"level"`
}

type ServerConfig struct {
	Port int `mapstructure:"port`
}
