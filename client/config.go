package client

import (
	"fmt"
	"github.com/spf13/viper"
	"io/ioutil"
	"os"
	"path"
)

const (
	ConfigFileName = "cleaner_properties"
	ConfigFileExt  = ".yaml"
	ConfigFile     = ConfigFileName + ConfigFileExt
)

// Config is used to hold configuration values that can be persisted, loaded from disk or provided via some other
// channel supported by viper
type Config struct {
	Url   string
	Token string
}

// NewConfig loads  environment variables to new Config object and returns it
// error is returned if any variable is missing
func NewConfig() (Config, error) {
	viper.SetEnvPrefix("artifactory")
	err := viper.BindEnv("url")
	if nil != err {
		return Config{}, err
	}

	err = viper.BindEnv("token")
	if nil != err {
		return Config{}, err
	}

	config := Config{
		Url:   viper.GetString("url"),
		Token: viper.GetString("token"),
	}
	if len(config.Url) == 0 {
		return Config{}, fmt.Errorf("missing config: Artifactory URL is not defined")
	}
	if len(config.Token) == 0 {
		return Config{}, fmt.Errorf("missing config: artifactory api token is missing")
	}

	return config, nil
}

func getFileContent(dir, fileName string) ([]byte, error) {
	src, err := os.Open(path.Join(dir, fileName))

	defer src.Close()
	if nil != err {
		return nil, err
	}

	content, err := ioutil.ReadAll(src)
	if nil != err {
		return nil, err
	}

	return content, nil
}
