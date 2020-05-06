package main

import (
	"io/ioutil"
	"os"

	"time"

	"gopkg.in/yaml.v2"
)

// GoogleConfig -
type GoogleConfig struct {
	CredentialsPath string   `yaml:"credentialsPath"`
	AdminEmail      string   `yaml:"adminEmail"`
	Domain          string   `yaml:"domain"`
	GroupBlacklist  []string `yaml:"groupBlacklist"`
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

// may return nil in case of errors
func tryLoadConfig(configPath string) *Config {
	configBytes, err := ioutil.ReadFile(configPath)
	if err != nil {
		log.Errorw("can not read config file", "path", configPath, "error", err)
		return nil
	}

	c := Config{}
	err = yaml.Unmarshal(configBytes, &c)
	if err != nil {
		log.Errorw("parsing error in config file", "error", err)
		return nil
	}

	for i, r := range c.Rules {
		r.Index = i
		err := r.verify()
		if err != nil {
			log.Errorw("error verifying rule", "error", err, "role", r.Role)
			return nil
		}
	}

	c.Grafana.Password = os.Getenv("GRAFANA_PASS")

	return &c
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
