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
	"log"
	"net/http"
	"strconv"

	"time"

	"github.com/gabrielperezs/sohestcam/utils"
	"github.com/pbnjay/pixfont"
)

type Camera struct {
	Name    string
	Url     string
	Debug   bool
	counter int

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

	log.Println(config.VideoDuration)

	c := &Camera{
		Name:    name,
		Url:     url,
		Debug:   false,
		counter: 0,

		encoder: newEncoder(name, config),

		header: true,
		size:   0,
		date:   "",

		background: image.NewRGBA(image.Rect(1, 1, 600, 10)),

		config: config,

		interval: time.NewTicker(config.VideoDuration),
	}

	go c.loop()

	// Video rotation
	go c.timer()

	return c
}

func (c *Camera) timer() {
	for {
		<-c.interval.C
		c.logf("Stop video afer %s", c.config.VideoDuration.String())
		select {
		case c.encoder.stop <- true:
		default:
			c.logf("can't send signal to stop to encoder")
		}
	}
}

func (c *Camera) loop() {
	for {

		resp, err := http.Get(c.Url)
		if err != nil {
			c.logf("http client: %s", err.Error())

			// Retry in 1 second
			<-time.After(time.Second * 1)
			continue
		}

		c.reader(resp)
	}
}

func (c *Camera) reader(resp *http.Response) {

	c.header = true

	reader := bufio.NewReader(resp.Body)

	body := &bytes.Buffer{}

	for {

		chunk, err := reader.ReadBytes('\n')
		if err != nil {
			c.logf("reader error: %s", err.Error())
			return
		}

		if c.header {

			c.readHeader(chunk)

		} else {

			body.Write(chunk)

			if body.Len() >= c.size {
				c.header = true
				c.debugf("End of body %d", body.Len())

				img, err := c.filter(body)
				if err != nil {
					c.logf("Filter image error %s", err)
					body.Reset()
					continue
				}

				select {
				case c.encoder.in <- img:
					// Send frame
				default:
					c.logf("Chan full")
				}

				body.Reset()
			}

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
			log.Println("Error", err)
		}
		c.debugf("%s | %s", string(chunk), size)
		c.size = size

		return
	}

	if bytes.HasPrefix(chunk, []byte("Date")) {
		c.debugf("%s", string(chunk))

		chunk = bytes.Replace(chunk, []byte("\n"), []byte(""), -1)
		chunk = bytes.Replace(chunk, []byte("\r"), []byte(""), -1)

		c.date = string(chunk)

		return
	}

	if bytes.HasPrefix(chunk, []byte("Content-type")) {
		c.debugf("%s", string(chunk))
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
		c.logf("jpeg ERROR: %s", err.Error())
		c.logf("jpeg header err: %s", hex.EncodeToString(body.Bytes()[:2]))
		return nil, err
	}

	if c.lastImgage != nil {
		if d := compare(imgSrc, c.lastImgage); d < 4 {
			imgSrc = c.lastImgage
		} else {
			c.lastImgage = imgSrc
		}
	}

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
		log.Panic("Invalid image codec:", c.config.ImageCodec)
	}

	return imageBytes.Bytes(), nil
}

func (c *Camera) debugf(format string, e ...interface{}) {
	if c.Debug {
		log.Printf(fmt.Sprint("[%s] debug", format), c.Name, e)
	}
}

func (c *Camera) logf(format string, e ...interface{}) {
	log.Printf(fmt.Sprint("[%s] ", format), c.Name, e)
}

func compare(source, target image.Image) float64 {

	if source.ColorModel() != target.ColorModel() {
		fmt.Println("different color models")
		return 100
	}

	b := source.Bounds()
	if !b.Eq(target.Bounds()) {
		fmt.Println("different image sizes")
		return 100
	}

	var sum int64
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
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

	nPixels := (b.Max.X - b.Min.X) * (b.Max.Y - b.Min.Y)

	return float64(sum*100) / (float64(nPixels) * 0xffff * 3)
}
