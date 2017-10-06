package cameras

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"net/http"
	"strconv"

	"time"

	log "github.com/sirupsen/logrus"

	"github.com/gabrielperezs/sohestcam/utils"
	"github.com/pbnjay/pixfont"
)

const (
	framesSecondDuration         = (1000 / 15) * time.Millisecond
	imageIgnorePtc       float64 = 1.05 // Ignore image frames who have a difference of % compared with the prev frame
	imageTopLabelY               = 10
	imageTopLabelX               = 600
)

type Camera struct {
	Name    string
	Url     string
	Debug   bool
	counter int
	ignored int

	encoder *encoder

	header bool
	size   int
	date   string

	background *image.RGBA

	config utils.Config

	interval *time.Ticker

	lastImgage image.Image
}

func New(name, url string, config utils.Config) *Camera {

	if config.ImageCodec == "" {
		config.ImageCodec = "png"
	}

	c := &Camera{
		Name:    name,
		Url:     url,
		Debug:   config.Debug,
		counter: 0,

		encoder: newEncoder(name, config),

		header: true,
		size:   0,
		date:   "",

		background: image.NewRGBA(image.Rect(1, 1, imageTopLabelX, imageTopLabelY)),

		config: config,

		interval: time.NewTicker(config.VideoDuration),
	}

	if c.Debug {
		log.SetLevel(log.DebugLevel)
	}

	go c.loop()

	// Video rotation
	go c.timer()

	return c
}

func (c *Camera) timer() {
	for {
		<-c.interval.C
		log.Debugf("[%s] Stop video afer %s", c.Name, c.config.VideoDuration.String())
		select {
		case c.encoder.stop <- true:
		default:
			log.Debugf("[%s] can't send signal to stop to encoder", c.Name)
		}
	}
}

func (c *Camera) loop() {
	for {
		resp, err := http.Get(c.Url)
		if err != nil {
			log.Debugf("[%s] http client: %s", c.Name, err)
			<-time.After(time.Second * 1)
			continue
		}
		c.reader(resp)
	}
}

func (c *Camera) reader(resp *http.Response) {

	c.header = true
	body := &bytes.Buffer{}
	lastFrame := time.Now()

	reader := bufio.NewReader(resp.Body)

	for {

		chunk, err := reader.ReadBytes('\n')
		if err != nil {
			log.Errorf("[%s] reader error: %s", c.Name, err)
			return
		}

		if c.header {
			c.readHeader(chunk)
			continue
		}

		body.Write(chunk)

		if body.Len() >= c.size {
			c.header = true

			if time.Now().Add(framesSecondDuration).Before(lastFrame) {
				log.Debugf("[%s] Discard frame %s", c.Name, lastFrame)
				body.Reset()
				continue
			}

			img, err := c.filter(body)
			if err != nil {
				log.Debugf("[%s] filter: %s", c.Name, err)
				body.Reset()
				continue
			}

			if len(img) <= 0 {
				continue
			}

			select {
			case c.encoder.in <- img:
				lastFrame = time.Now()
				// Send frame
			default:
				log.Errorf("[%s] Chan full, %d images queued", c.Name, len(c.encoder.in))
			}

			body.Reset()

		}
	}

}

func (c *Camera) readHeader(chunk []byte) {

	if bytes.HasPrefix(chunk, []byte("--video boundary--")) {
		return
	}

	if bytes.HasPrefix(chunk, []byte("Content-length")) {
		chunk = bytes.Replace(chunk, []byte("\n"), []byte(""), -1)
		chunk = bytes.Replace(chunk, []byte("\r"), []byte(""), -1)
		s := bytes.Split(chunk, []byte(" "))
		size, err := strconv.Atoi(string(s[1]))
		if err != nil {
			log.Errorf("[%s] Content-length: %s", c.Name, err)
		}
		c.size = size
		return
	}

	if bytes.HasPrefix(chunk, []byte("Date")) {
		chunk = bytes.Replace(chunk, []byte("\n"), []byte(""), -1)
		chunk = bytes.Replace(chunk, []byte("\r"), []byte(""), -1)
		c.date = string(chunk)
		return
	}

	if bytes.HasPrefix(chunk, []byte("Content-type")) {
		return
	}

	if bytes.HasPrefix(chunk, []byte("\r\n")) {
		c.header = false
		return
	}
}

func (c *Camera) filter(body *bytes.Buffer) ([]byte, error) {
	imgSrc, err := jpeg.Decode(body)

	if err != nil {
		log.Errorf("[%s] jpeg ERROR: %s", c.Name, err.Error())
		log.Errorf("[%s] jpeg header err: %s", c.Name, hex.EncodeToString(body.Bytes()[:2]))
		return nil, err
	}

	d := compare(imgSrc, c.lastImgage)
	c.lastImgage = imgSrc

	if d < imageIgnorePtc {
		return []byte(""), nil
	}

	c.lastImgage = imgSrc
	newImg := image.NewRGBA(imgSrc.Bounds())

	// Copy original image over newImg
	draw.Draw(newImg, newImg.Bounds(), imgSrc, imgSrc.Bounds().Min, draw.Over)

	if c.config.Label {
		// Overwrite white background for label
		draw.Draw(newImg, c.background.Bounds(), &image.Uniform{color.White}, image.Point{0, 0}, draw.Over)

		pixfont.DrawString(newImg, 2, 2, fmt.Sprintf("%s: %s", c.Name, c.date), color.Black)
	}

	var imageBytes bytes.Buffer

	switch c.config.ImageCodec {
	case "png":
		png.Encode(&imageBytes, newImg)
		break
	case "jpeg":
		jpeg.Encode(&imageBytes, newImg, nil)
		break
	default:
		log.Panicf("[%s] Invalid image codec: %s", c.Name, c.config.ImageCodec)
	}

	return imageBytes.Bytes(), nil
}

func compare(source, target image.Image) float64 {

	if target == nil {
		return 100
	}

	if source.ColorModel() != target.ColorModel() {
		return 100
	}

	b := source.Bounds()
	if !b.Eq(target.Bounds()) {
		return 100
	}

	var sum int64
	for y := b.Min.Y + imageTopLabelY; y < b.Max.Y; y++ {
		for x := b.Min.X + imageTopLabelX; x < b.Max.X; x++ {
			r1, g1, b1, _ := source.At(x, y).RGBA()
			r2, g2, b2, _ := target.At(x, y).RGBA()
			if r1 > r2 {
				sum += int64(r1 - r2)
			} else {
				sum += int64(r2 - r1)
			}
			if g1 > g2 {
				sum += int64(g1 - g2)
			} else {
				sum += int64(g2 - g1)
			}
			if b1 > b2 {
				sum += int64(b1 - b2)
			} else {
				sum += int64(b2 - b1)
			}
		}
	}

	//nPixels := (b.Max.X - (b.Min.X + imageTopLabelX)) * (b.Max.Y - (b.Min.Y + imageTopLabelY))
	nPixels := (b.Max.X - b.Min.X - imageTopLabelX) * (b.Max.Y - b.Min.Y - imageTopLabelY)

	return float64(sum*100) / (float64(nPixels) * 0xffff * 3)
}
