package main

import (
	"flag"
	"fmt"
	"io/ioutil"

	log "github.com/sirupsen/logrus"

	"time"

	"github.com/BurntSushi/toml"
	"github.com/gabrielperezs/sohestcam/cameras"
	"github.com/gabrielperezs/sohestcam/clients/googledrive"
	"github.com/gabrielperezs/sohestcam/utils"
)

var cfgfile string
var config utils.Config

var done = make(chan bool, 1)

func init() {
	flag.StringVar(&cfgfile, "config", "/etc/sohestcam/config.toml", "Configuration file")
	flag.Parse()

	log.SetLevel(log.InfoLevel)
	if config.Debug {
		log.SetLevel(log.DebugLevel)
	}

	log.SetFormatter(&log.TextFormatter{
		DisableColors: false,
		FullTimestamp: true,
	})
}

func main() {

	startOrReload()

	<-done
}

func startOrReload() {

	var cfg utils.Config

	if _, err := toml.DecodeFile(cfgfile, &cfg); err != nil {
		log.Panic(err)
		return
	}

	config = cfg

	var err error
	config.GoogleProjectContent, err = ioutil.ReadFile(config.GoogleProject)
	if err != nil {
		log.Fatalf("Unable to read client secret file: %v", err)
	}

	config.VideoDuration, err = time.ParseDuration(config.SplitVideoIn)
	if err != nil {
		log.Panic(err)
	}

	if config.VideoDuration < time.Duration(30)*time.Second {
		config.VideoDuration = 30 * time.Second
	}

	testGoogleDrive()

	log.Infof("Starting VideoDuration %s GoogleDrive %v", config.VideoDuration.String(), true)

	for _, c := range config.Cameras {
		if !c.Active {
			continue
		}

		_ = cameras.New(c.Name, c.Url, config)
	}
}

func testGoogleDrive() {
	t := time.Now()
	gdc := googledrive.Start(config.GoogleProjectContent)
	directory := fmt.Sprintf("%s/%s/%s/%s", googledrive.DirectoryName, "testing", t.Format("2006"), t.Format("01"))
	_, _ = gdc.CreateDirectory(directory)
}
