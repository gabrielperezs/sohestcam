package cameras

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/gabrielperezs/sohestcam/clients/googledrive"
	"github.com/gabrielperezs/sohestcam/utils"
)

type encoder struct {
	name         string
	cmd          string
	filename     string
	in           chan []byte
	stop         chan bool
	subProcess   *exec.Cmd
	stdin        io.WriteCloser
	stdinCloseCh chan bool
	dateStart    time.Time

	config utils.Config
}

func newEncoder(name string, config utils.Config) *encoder {
	e := &encoder{
		name:         name,
		filename:     fmt.Sprintf("/tmp/%s-%s.avi", name, "test"),
		in:           make(chan []byte, 200),
		stop:         make(chan bool, 1),
		stdinCloseCh: make(chan bool, 1),
		config:       config,
	}

	go e.start()
	go e.loop()

	return e
}

func (e *encoder) upload(file string) {

	defer os.Remove(file)

	gdc := googledrive.Start(e.config.GoogleProjectContent)

	t := time.Now()

	directory := fmt.Sprintf("%s/%s/%s/%s", googledrive.DirectoryName, e.name, t.Format("2006"), t.Format("01"))
	d, err := gdc.CreateDirectory(directory)
	if err != nil {
		e.log("Error upload create dir: %s - %s", directory, err)
		return
	}

	gdc.UploadFile(file, d)
}

func (e *encoder) loop() {
	for {
		select {
		case <-e.stop:
			e.log("stop signal")
			e.subProcess.Process.Signal(syscall.SIGINT)
			break
		case in := <-e.in:
			e.stdin.Write(in)
			break
		case <-e.stdinCloseCh:
			go e.start()
		}
	}
}

func (e *encoder) loopStdin() {
	for {
		e.stdin.Write(<-e.in)
	}
}

func (e *encoder) start() {

	e.dateStart = time.Now()

	cmd := strings.Split(fmt.Sprintf(e.config.CmdEncoder, e.filename), " ")
	e.log("start: %s", cmd)

	e.subProcess = exec.Command("nice", cmd...)

	var err error
	e.stdin, err = e.subProcess.StdinPipe()
	if err != nil {
		e.log("ERROR: %s", err)
	}
	defer e.stdin.Close()

	e.subProcess.Stderr = os.Stderr

	if err = e.subProcess.Start(); err != nil {
		e.log("ERROR: %s", err)
	}

	e.subProcess.Wait()

	e.log("%s", "end")

	lastName := fmt.Sprintf("%s/%s-%s.avi", e.config.TempDir, e.name, e.dateStart.Format("2006-01-02_15:04:05"))
	os.Rename(e.filename, lastName)

	e.stdinCloseCh <- true

	go e.upload(lastName)

}

func (e *encoder) log(s string, args ...interface{}) {
	n := fmt.Sprintf("encoder [%s] ", e.name)
	log.Printf(fmt.Sprint(n, s), args...)
}
