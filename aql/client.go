package aql

import (
	http "github.com/asmild/artifactory-cleaner/client"
)

type AQL struct {
	client *http.Client
//	pathMatcher	  string
//	discriminator string
}

func New() (*AQL, error) {
	config, err := http.NewConfig()
	//util.Debug(true,"cleaner.go: check config",config)
	if nil != err {
		return nil, err
	}

	client, err := http.NewClient(config)
	//util.Debug(true,"cleaner.go: check client",client)
	if nil != err {
		return nil, err
	}

	return &AQL{
		client: client,
	}, err
}
