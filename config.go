package main

import (
	"fmt"
	"io/ioutil"
	"os"

	"gopkg.in/yaml.v3"
)

type Iterm2Badge struct {
	Enable bool     `yaml:"enable"`
	Column []string `yaml:"column"`
}

type Iterm2Tab struct {
	Enable bool              `yaml:"enable"`
	Title  string            `yaml:"title"`
	Colors map[string]uint32 `yaml:"colors"`
}

type Iterm2Config struct {
	Badge Iterm2Badge `yaml:"badge"`
	Tab   Iterm2Tab   `yaml:"tab"`
}

type Config struct {
	Path          string       `yaml:"path"`
	NodeCacheFile string       `yaml:"nodecachefile"`
	Iterm2        Iterm2Config `yaml:"iterm2"`
}

func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}

	return !info.IsDir()
}

func readConfig(cfgFile string) *Config {
	if !fileExists(cfgFile) {
		return nil
	}

	buf, err := ioutil.ReadFile(cfgFile)
	if err != nil {
		return nil
	}

	var cfg Config

	err = yaml.Unmarshal(buf, &cfg)
	if err != nil {
		panic(fmt.Sprintf("Unmarshal: %v", err))
	}

	//fmt.Printf("config:%+v \n", cfg)

	return &cfg
}
