package main

import (
	"io/ioutil"
	"os"

	"time"

	"gopkg.in/yaml.v2"
)

// GoogleConfig -
type GoogleConfig struct {
	CredentialsPath string `yaml:"credentialsPath"`
	AdminEmail      string `yaml:"adminEmail"`
	Domain          string `yaml:"domain"`
}

// GrafanaConfig -
type GrafanaConfig struct {
	URL      string `yaml:"url"`
	User     string `yaml:"user"`
	Password string `yaml:"-"` // password is retreived from GRAFANA_PASS
}

// Settings -
type Settings struct {
	GroupsFetchInterval time.Duration `yaml:"groupsFetchInterval"`
	ApplyInterval       time.Duration `yaml:"applyInterval"`

	CanDemote         bool `yaml:"canDemote"` // can demote a user to a lower role, or even completely remove them from an org
	RemoveFromMainOrg bool `yaml:"removeFromMainOrg"`
}

// Config -
type Config struct {
	Google   GoogleConfig  `yaml:"google"`
	Grafana  GrafanaConfig `yaml:"grafana"`
	Settings Settings      `yaml:"settings"`
	Rules    []*Rule       `yaml:"rules"`
}

func loadConfig(configPath string) *Config {
	configBytes, err := ioutil.ReadFile(configPath)
	if err != nil {
		log.Fatalw("can not read config.yaml", "path", configPath, "error", err)
	}

	config := Config{}
	err = yaml.Unmarshal(configBytes, &config)
	if err != nil {
		log.Fatalw("parsing error in config.yaml", "error", err)
	}

	for i, r := range config.Rules {
		r.Index = i
		err := r.verify()
		if err != nil {
			log.Fatalw("error verifying rule", "error", err, "role", r.Role)
		}
	}

	config.Grafana.Password = os.Getenv("GRAFANA_PASS")

	return &config
}

func (c *Config) getAllGroups() []string {
	var ar []string
	for _, e := range c.Rules {
		ar = append(ar, e.Groups...)
	}
	return distinct(ar)
}

func (c *Config) getAllUsers() []string {
	var ar []string
	for _, e := range c.Rules {
		ar = append(ar, e.Users...)
	}
	return distinct(ar)
}
