package config

import (
	"github.com/spf13/viper"
)

// Config holds the application configuration
type Config struct {
	Redis  RedisConfig
	Minio  MinioConfig
	Server ServerConfig
	Backup BackupConfig
}

// RedisConfig holds Redis connection details
type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

// MinioConfig holds Minio connection details
type MinioConfig struct {
	Endpoint        string `mapstructure:"endpoint"`
	AccessKeyID     string `mapstructure:"access_key_id"`
	SecretAccessKey string `mapstructure:"secret_access_key"`
	UseSSL          bool   `mapstructure:"use_ssl"`
	Bucket          string `mapstructure:"bucket"`
}

// ServerConfig holds server configuration
type ServerConfig struct {
	GrpcPort string `mapstructure:"grpc_port"`
	RestPort string `mapstructure:"rest_port"`
}

// BackupConfig holds the data backup configuration
type BackupConfig struct {
	Enabled bool
	Minio   MinioConfig
}

// LoadConfig loads configuration from a file
func LoadConfig(path string) (config Config, err error) {
	viper.AddConfigPath(path)
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")

	viper.AutomaticEnv()

	err = viper.ReadInConfig()
	if err != nil {
		return
	}

	err = viper.Unmarshal(&config)
	return
}
