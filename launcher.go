package main

import (
	"fmt"
	"math/rand"
	"os"
	"runtime"
	"strconv"
	"time"

	"github.com/ivpn/desktop-app-daemon/api"
	"github.com/ivpn/desktop-app-daemon/logger"
	"github.com/ivpn/desktop-app-daemon/netchange"
	"github.com/ivpn/desktop-app-daemon/protocol"
	"github.com/ivpn/desktop-app-daemon/service"
	"github.com/ivpn/desktop-app-daemon/service/platform"
)

var log *logger.Logger
var activeProtocol service.Protocol

func init() {
	log = logger.NewLogger("launch")
	rand.Seed(time.Now().UnixNano())
}

// Launch -  initialize and start service
func Launch() {
	defer func() {
		log.Info("IVPN daemon stopped.")

		// OS-specific service finalizer
		doStopped()
	}()

	tzName, tzOffsetSec := time.Now().Zone()
	log.Info("Starting IVPN daemon", fmt.Sprintf(" [%s]", runtime.GOOS), fmt.Sprintf(" [timezone: %s %d (%dh)]", tzName, tzOffsetSec, tzOffsetSec/(60*60)), " ...")
	log.Info(fmt.Sprintf("args: %s", os.Args))
	log.Info(fmt.Sprintf("pid : %d ppid: %d", os.Getpid(), os.Getppid()))
	log.Info(fmt.Sprintf("arch: %d bit", strconv.IntSize))

	if !doCheckIsAdmin() {
		logger.Warning("------------------------------------")
		logger.Warning("!!! NOT A PRIVILEGED USER !!!")
		logger.Warning("Please, ensure you are running an application with privileged rights.")
		logger.Warning("Otherwise, application will not work correctly.")
		logger.Warning("------------------------------------")
	}

	secret := rand.Uint64()

	// obtain (over callback channel) a service listening port
	startedOnPortChan := make(chan int, 1)
	go func() {
		// waiting for port number info
		openedPort := <-startedOnPortChan

		// for Windows and macOS-debug we need to save port info into a file
		if isNeedToSavePortInFile() == true {
			file, err := os.Create(platform.ServicePortFile())
			if err != nil {
				logger.Panic(err.Error())
			}
			defer file.Close()
			file.WriteString(fmt.Sprintf("%d:%x", openedPort, secret))
		}
		// inform OS-specific implementation about listener port
		doStartedOnPort(openedPort, secret)
	}()

	defer func() {
		if isNeedToSavePortInFile() == true {
			os.Remove(platform.ServicePortFile())
		}
	}()

	// perform OS-specific preparetions (if necessary)
	if err := doPrepareToRun(); err != nil {
		logger.Panic(err.Error())
	}

	// run service
	launchService(secret, startedOnPortChan)
}

// Stop the service
func Stop() {
	p := activeProtocol
	if p != nil {
		p.Stop()
	}
}

// initialize and start service
func launchService(secret uint64, startedOnPort chan<- int) {
	// API object
	apiObj, err := api.CreateAPI()
	if err != nil {
		log.Panic("API object initialization failed: ", err)
	}

	// servers updater
	updater, err := service.CreateServersUpdater(apiObj)
	if err != nil {
		log.Panic("ServersUpdater initialization failed: ", err)
	}

	// network change detector
	netDetector := netchange.Create()

	// initialize service
	serv, err := service.CreateService(apiObj, updater, netDetector)
	if err != nil {
		log.Panic("Failed to initialize service:", err)
	}

	// communication protocol
	protocol, err := protocol.CreateProtocol()
	if err != nil {
		log.Panic("Protocol object initialization failed: ", err)
	}

	// save protocol (to be able to stop it)
	activeProtocol = protocol

	// start receiving requests from client (synchronous)
	if err := protocol.Start(secret, startedOnPort, serv); err != nil {
		log.Error("Protocol stopped with error:", err)
	}
}
