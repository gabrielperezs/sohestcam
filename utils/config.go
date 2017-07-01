package utils

import (
	"time"
)

type Config struct {
	Label      bool
	CmdEncoder string
	ImageCodec string
	ImageLib   string

	SplitVideoIn  string
	VideoDuration time.Duration

	TempDir              string
	GoogleProject        string
	GoogleProjectContent []byte

	Cameras []CameraConfig `toml:"camera"`
}

type CameraConfig struct {
	Name   string
	Url    string
	Active bool
}
