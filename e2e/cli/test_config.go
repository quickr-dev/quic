package e2e_cli

import (
	"os"
	"sync"

	"github.com/spf13/viper"
)

var (
	testConfigOnce sync.Once
	testViper      *viper.Viper
)

func getTestConfig() *viper.Viper {
	testConfigOnce.Do(func() {
		testViper = viper.New()

		testViper.SetConfigName(".env")
		testViper.SetConfigType("env")
		testViper.AddConfigPath(".")
		testViper.AddConfigPath("../..") // For when running from e2e/cli directory

		testViper.AutomaticEnv()

		_ = testViper.ReadInConfig()
	})

	return testViper
}

func getRequiredTestEnv(key string) string {
	config := getTestConfig()

	value := config.GetString(key)
	if value == "" {
		value = os.Getenv(key)
	}

	return value
}
