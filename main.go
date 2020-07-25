package main

import (
	"flag"
	"fmt"
	"image"
	"io"
	"io/ioutil"
	"log"
	"os"
	"syscall"
	"time"

	"github.com/aarzilli/nucular/font"

	"github.com/lawl/pulseaudio"

	"github.com/aarzilli/nucular"
	"github.com/aarzilli/nucular/style"
)

//go:generate go run scripts/embedlibrnnoise.go
//go:generate go run scripts/embedversion.go
//go:generate go run scripts/embedlicenses.go

type input struct {
	ID             string
	Name           string
	isMonitor      bool
	checked        bool
	dynamicLatency bool
}

func main() {

	var pulsepid int
	flag.IntVar(&pulsepid, "removerlimit", -1, "for internal use only")
	flag.Parse()
	if pulsepid > 0 {
		const MaxUint = ^uint64(0)
		new := syscall.Rlimit{Cur: MaxUint, Max: MaxUint}
		err := setRlimit(pulsepid, &new)
		if err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	date := time.Now().Format("20060102030405")
	f, err := os.OpenFile(fmt.Sprintf("/tmp/noisetorch-%s.log", date), os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		log.Fatalf("error opening file: %v\n", err)
	}
	defer f.Close()

	logwriter := io.MultiWriter(os.Stdout, f)
	log.SetOutput(logwriter)
	log.Printf("Application starting. Version: %s", version)

	initializeConfigIfNot()
	rnnoisefile := dumpLib()
	defer removeLib(rnnoisefile)

	ui := uistate{}
	ui.config = readConfig()
	ui.librnnoise = rnnoisefile

	if ui.config.EnableUpdates {
		go updateCheck(&ui)
	}

	paClient, err := pulseaudio.NewClient()
	defer paClient.Close()

	ui.paClient = paClient
	if err != nil {
		log.Fatalf("Couldn't create pulseaudio client: %v\n", err)
	}

	go updateNoiseSupressorLoaded(paClient, &ui.noiseSupressorState)

	sources, err := paClient.Sources()
	if err != nil {
		log.Fatalf("Couldn't fetch sources from pulseaudio\n")
	}

	inputs := make([]input, 0)
	for i := range sources {
		if sources[i].Name == "nui_mic_remap" {
			continue
		}

		log.Printf("Input %s, %+v\n", sources[i].Name, sources[i])

		var inp input

		inp.ID = sources[i].Name
		inp.Name = sources[i].PropList["device.description"]
		inp.isMonitor = (sources[i].MonitorSourceIndex != 0xffffffff)

		//PA_SOURCE_DYNAMIC_LATENCY = 0x0040U
		inp.dynamicLatency = sources[i].Flags&uint32(0x0040) != 0

		inputs = append(inputs, inp)
	}

	ui.inputList = inputs

	wnd := nucular.NewMasterWindowSize(0, "NoiseTorch", image.Point{550, 300}, func(w *nucular.Window) {
		updatefn(w, &ui)
	})
	ui.masterWindow = &wnd
	style := style.FromTheme(style.DarkTheme, 2.0)
	style.Font = font.DefaultFont(16, 1)
	wnd.SetStyle(style)
	wnd.Main()

}

func dumpLib() string {
	f, err := ioutil.TempFile("", "librnnoise-*.so")
	if err != nil {
		log.Fatalf("Couldn't open temp file for librnnoise\n")
	}
	f.Write(libRNNoise)
	log.Printf("Wrote temp librnnoise to: %s\n", f.Name())
	return f.Name()
}

func removeLib(file string) {
	err := os.Remove(file)
	if err != nil {
		log.Printf("Couldn't delete temp librnnoise: %v\n", err)
	}
	log.Printf("Deleted temp librnnoise: %s\n", file)
}
